package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/deviceapi"
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/installer"
)

// hostFirmware gives the installer the firmware's own state operations. The
// installing process is the new version, so nothing is executed through a path.
type hostFirmware struct {
	store    configuration.Store
	accepted *configuration.Input
}

func (f hostFirmware) Preflight(ctx context.Context) error {
	engine, err := dockerengine.New()
	if err != nil {
		return err
	}
	defer engine.Close()
	return engine.Prerequisites(ctx)
}

func (f hostFirmware) Existing() error {
	if _, err := f.store.Load(); err != nil && !errors.Is(err, configuration.ErrMissing) {
		return err
	}
	return nil
}

func (f hostFirmware) Configure() error {
	_, err := f.store.Save(f.accepted)
	return err
}

func (f hostFirmware) PrepareAPI(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return prepareAPI(ctx, f.store, false)
}

func (f hostFirmware) HoldCode(code string) error {
	_, err := enrollmentCode(f.store, strings.NewReader(code), true)
	return err
}

func (f hostFirmware) ClearCode() { _, _ = enrollmentCode(f.store, nil, false) }

// The two host boundaries are replaced in tests.
var (
	hostReady = func() string {
		if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
			return "unsupported_host"
		}
		if os.Geteuid() != 0 || os.Getegid() != 0 {
			return "root_required"
		}
		if info, err := os.Stat("/run/systemd/system"); err != nil || !info.IsDir() {
			return "systemd_unavailable"
		}
		return ""
	}
	place = func(ctx context.Context, accepted *configuration.Input, code string, progress func(string)) error {
		binary, err := os.Executable()
		if err != nil {
			return installer.ErrWrite
		}
		store := configuration.Store{Path: defaultStateDir}
		return installer.Install(ctx, installer.Request{
			Layout: installer.HostLayout(), Binary: binary, Code: code,
			Systemd: installer.Systemctl{}, Firmware: hostFirmware{store, accepted}, Progress: progress,
		})
	}
)

// runInstall installs this binary as the host service:
//
//	install --issuer <https issuer> --release <file> [--enrollment-code-stdin | --enrollment-code-file <file>]
//	install --issuer <https issuer> --release <file> --dry-run
//
// The one-time code is never an argument. A dry run validates the issuer, the
// discovered profile and the workload release and changes nothing.
func runInstall(args []string, input io.Reader, stdout, stderr io.Writer) int {
	issuer, release, codeSource, codeFile, dry := "", "", "", "", false
	for i := 0; i < len(args); i++ {
		value := func() string {
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch args[i] {
		case "--issuer":
			if issuer != "" {
				return report(stderr, "invalid_usage", 2)
			}
			issuer = value()
		case "--release":
			if release != "" {
				return report(stderr, "invalid_usage", 2)
			}
			release = value()
		case "--enrollment-code-stdin", "--enrollment-code-file":
			if codeSource != "" {
				return report(stderr, "invalid_usage", 2)
			}
			codeSource = args[i]
			if codeSource == "--enrollment-code-file" {
				if codeFile = value(); codeFile == "" {
					return report(stderr, "invalid_usage", 2)
				}
			}
		case "--dry-run":
			dry = true
		default:
			return report(stderr, "invalid_usage", 2)
		}
	}
	if issuer == "" || release == "" || dry && codeSource != "" {
		return report(stderr, "invalid_usage", 2)
	}
	if reason := hostReady(); reason != "" {
		return report(stderr, reason, 1)
	}
	code := ""
	if codeSource != "" {
		source := input
		if codeFile != "" {
			file, err := openInput(codeFile)
			if err != nil {
				return report(stderr, "input_read_failed", 1)
			}
			defer file.Close()
			source = file
		}
		// Bounds only: the code is opaque here and Auth is its authority.
		data, err := io.ReadAll(io.LimitReader(source, deviceapi.MaxEnrollmentCode+2))
		if err != nil {
			return report(stderr, "input_read_failed", 1)
		}
		code = strings.TrimSuffix(string(data), "\n")
		if !deviceapi.ValidEnrollmentCode(code) {
			return report(stderr, string(deviceapi.ErrEnrollmentCode), 1)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	// Progress goes to standard error as plain lines, so standard output stays one JSON result
	// and an error stays one JSON line. A dry run is a silent check. The code is never printed.
	progress := func(message string) {
		if !dry {
			_, _ = io.WriteString(stderr, "agent-firmware: "+message+"\n")
		}
	}
	progress("Reading the configuration published by the authorization server")
	discovery, cancel := context.WithTimeout(ctx, time.Minute)
	profile, err := discover(discovery, issuer)
	cancel()
	if err != nil {
		return report(stderr, err.Error(), 1)
	}
	manifest, err := openInput(release)
	if err != nil {
		return report(stderr, "input_read_failed", 1)
	}
	accepted, err := configuration.Parse(bytes.NewReader(profile), manifest)
	manifest.Close()
	if err != nil {
		return report(stderr, err.Error(), 1)
	}
	if dry {
		return respond(stdout, stderr, struct {
			Valid bool `json:"valid"`
		}{true})
	}
	if err := place(ctx, accepted, code, progress); err != nil {
		return report(stderr, err.Error(), 1)
	}
	if code != "" {
		progress("Installed. The device signs in by itself and appears in your account within a minute")
	} else {
		progress("Installed")
	}
	return respond(stdout, stderr, struct {
		Installed bool `json:"installed"`
	}{true})
}
