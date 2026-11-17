package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/backendprofile"
	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/deviceapi"
	"github.com/amnezia-gpt/agent-firmware/internal/discovery"
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/installer"
	"github.com/amnezia-gpt/agent-firmware/internal/service"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	"github.com/amnezia-gpt/agent-firmware/internal/workloadrelease"
)

// hostFirmware gives the installer the firmware's own state operations. The
// installing process is the new version, so nothing is executed through a path.
type hostFirmware struct {
	store    configuration.Store
	accepted *configuration.Input
}

func (f hostFirmware) Preflight(ctx context.Context) error {
	address, err := f.store.EngineAddress()
	if err != nil {
		return err
	}
	engine, err := dockerengine.New(address)
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

func (f hostFirmware) Configure(ctx context.Context) error {
	if _, err := saveConfiguration(ctx, f.store, f.accepted); err != nil {
		return err
	}
	return f.store.PersistEngineAddress()
}

func (f hostFirmware) HoldCode(code string) error {
	_, err := enrollmentCode(f.store, strings.NewReader(code), true)
	return err
}

func (f hostFirmware) ClearCode() { _, _ = enrollmentCode(f.store, nil, false) }

// The two host boundaries are replaced in tests.
var (
	hostReady = func() string {
		if reason := host.InstallCheck(); reason != "" {
			return reason
		}
		if !(service.Systemd{Layout: hostLayout}).Available() {
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
			Layout: hostLayout, Binary: binary, Code: code,
			Services: service.Systemd{Layout: hostLayout}, Firmware: hostFirmware{store, accepted}, Progress: progress,
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
	releaseFile, err := openInput(release)
	if err != nil {
		return report(stderr, "input_read_failed", 1)
	}
	raw, err := io.ReadAll(io.LimitReader(releaseFile, workloadrelease.MaxBytes+1))
	releaseFile.Close()
	if err != nil {
		return report(stderr, "input_read_failed", 1)
	}
	manifest, err := workloadrelease.Parse(bytes.NewReader(raw))
	if err != nil {
		return report(stderr, err.Error(), 1)
	}
	if err := checkDeployableRelease(manifest); err != nil {
		return report(stderr, err.Error(), 1)
	}

	progress("Reading the configuration published by the authorization server")
	lookup, cancel := context.WithTimeout(ctx, time.Minute)
	accepted, err := prepareInstallInput(lookup, configuration.Store{Path: defaultStateDir}, issuer, raw)
	cancel()
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
	progress("To update later, run: sudo agent-firmware update")
	return respond(stdout, stderr, struct {
		Installed bool `json:"installed"`
	}{true})
}

// An update reuses the enrollment's pinned trust and only refreshes resources
// if one of the new manifest's scopes is missing.
func prepareInstallInput(ctx context.Context, store configuration.Store, issuer string, raw []byte) (*configuration.Input, error) {
	current, err := store.Load()
	if errors.Is(err, configuration.ErrMissing) {
		profile, err := discover(ctx, issuer)
		if err != nil {
			return nil, err
		}
		return checkedInstallInput(profile, raw)
	}
	if err != nil {
		return nil, err
	}
	profile := current.Profile()
	if profile.AuthIssuer != issuer {
		return nil, configuration.ErrIdentityConflict
	}
	saved, err := backendprofile.Encode(profile)
	if err != nil {
		return nil, configuration.ErrInvalid
	}
	input, err := checkedInstallInput(saved, raw)
	if !errors.Is(err, backendprofile.ErrScopeMissing) {
		return input, err
	}
	refreshed, err := refreshResources(ctx, profile)
	if err != nil {
		return nil, err
	}
	return checkedInstallInput(refreshed, raw)
}

var refreshResources = func(ctx context.Context, profile backendprofile.Profile) ([]byte, error) {
	client := discovery.NewClient()
	defer client.CloseIdleConnections()
	return discovery.RefreshResources(ctx, client, profile)
}

func checkedInstallInput(profile, raw []byte) (*configuration.Input, error) {
	input, err := configuration.Parse(bytes.NewReader(profile), bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if _, err := workload.FromInput(input); err != nil {
		return nil, err
	}
	return input, nil
}
