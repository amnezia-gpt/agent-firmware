package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/backendprofile"
	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/deviceapi"
	"github.com/amnezia-gpt/agent-firmware/internal/discovery"
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	"github.com/amnezia-gpt/agent-firmware/internal/workloadrelease"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, input io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "preflight" {
		if os.Geteuid() != 0 {
			return report(stderr, "root_required", 1)
		}
		address, err := (configuration.Store{Path: defaultStateDir}).EngineAddress()
		if err != nil {
			return report(stderr, err.Error(), 1)
		}
		engine, err := dockerengine.New(address)
		if err != nil {
			return report(stderr, err.Error(), 1)
		}
		defer engine.Close()
		if err := engine.Prerequisites(context.Background()); err != nil {
			return report(stderr, err.Error(), 1)
		}
		return respond(stdout, stderr, struct {
			Ready bool `json:"ready"`
		}{true})
	}
	if len(args) > 0 && args[0] == "validate-release" {
		return validateRelease(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "update" {
		return runUpdate(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "install" {
		return runInstall(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "enroll" {
		return runEnroll(args[1:], input, stdout, stderr)
	}
	if len(args) > 0 && args[0] == "unenroll" {
		return runUnenroll(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "discover-profile" {
		// The profile is assembled from the issuer's standard metadata; nothing on the host changes.
		if len(args) != 3 || args[1] != "--issuer" {
			return report(stderr, "invalid_usage", 2)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		profile, err := discover(ctx, args[2])
		if err != nil {
			return report(stderr, err.Error(), 1)
		}
		if n, err := stdout.Write(profile); err != nil || n != len(profile) {
			return report(stderr, "output_failed", 1)
		}
		return 0
	}
	if len(args) > 0 && args[0] != "validate-profile" {
		switch args[0] {
		case "validate":
			return validateConfiguration(args[1:], stdout, stderr)
		case "status", "apply", "serve":
			return runStateCommand(args, stdout, stderr)
		default:
			return report(stderr, "invalid_usage", 2)
		}
	}
	if len(args) != 1 {
		return report(stderr, "invalid_usage", 2)
	}
	if _, err := backendprofile.Parse(input); err != nil {
		return report(stderr, err.Error(), 1)
	}
	const success = "{\"valid\":true}\n"
	if n, err := io.WriteString(stdout, success); err != nil || n != len(success) {
		return report(stderr, "output_failed", 1)
	}
	return 0
}

// discover is replaced in tests, which have no public issuer to ask.
var discover = func(ctx context.Context, issuer string) ([]byte, error) {
	client := discovery.NewClient()
	defer client.CloseIdleConnections()
	return discovery.Profile(ctx, client, issuer)
}

// Keep the manifest compatibility check at each deployment entry point.
var servedDeviceAPI = deviceapi.Protocol

func checkPlatform(release workloadrelease.Release) error {
	return release.CheckPlatform("linux/amd64")
}

func checkDeployableRelease(release workloadrelease.Release) error {
	if err := checkPlatform(release); err != nil {
		return err
	}
	return release.CheckDeviceAPI(servedDeviceAPI)
}

func validateRelease(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 || args[0] != "--release" || args[1] == "" {
		return report(stderr, "invalid_usage", 2)
	}
	release, err := openInput(args[1])
	if err != nil {
		return report(stderr, "input_read_failed", 1)
	}
	defer release.Close()
	manifest, err := workloadrelease.Parse(release)
	if err != nil {
		return report(stderr, err.Error(), 1)
	}
	if err := checkDeployableRelease(manifest); err != nil {
		return report(stderr, err.Error(), 1)
	}
	return respond(stdout, stderr, struct {
		Valid bool `json:"valid"`
	}{true})
}

var hostLayout = host.DefaultLayout()
var defaultStateDir = hostLayout.State

func validateConfiguration(args []string, stdout, stderr io.Writer) int {
	if len(args) != 4 {
		return report(stderr, "invalid_usage", 2)
	}
	options := make(map[string]string, 2)
	for i := 0; i < len(args); i += 2 {
		if args[i] != "--profile" && args[i] != "--release" || args[i+1] == "" || options[args[i]] != "" {
			return report(stderr, "invalid_usage", 2)
		}
		options[args[i]] = args[i+1]
	}
	profile, err := openInput(options["--profile"])
	if err != nil {
		return report(stderr, "input_read_failed", 1)
	}
	defer profile.Close()
	release, err := openInput(options["--release"])
	if err != nil {
		return report(stderr, "input_read_failed", 1)
	}
	defer release.Close()
	input, err := configuration.Parse(profile, release)
	if err != nil {
		return report(stderr, err.Error(), 1)
	}
	if err := checkPlatform(input.Release()); err != nil {
		return report(stderr, err.Error(), 1)
	}
	if _, err := workload.FromInput(input); err != nil {
		return report(stderr, err.Error(), 1)
	}
	return respond(stdout, stderr, struct {
		Valid bool `json:"valid"`
	}{true})
}

func openInput(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, configuration.ErrRead
	}
	return f, nil
}

func respond(stdout, stderr io.Writer, value any) int {
	data, err := json.Marshal(value)
	if err != nil {
		return report(stderr, "output_failed", 1)
	}
	data = append(data, '\n')
	if n, err := stdout.Write(data); err != nil || n != len(data) {
		return report(stderr, "output_failed", 1)
	}
	return 0
}

func report(stderr io.Writer, code string, exit int) int {
	message := "{\"error_code\":\"" + code + "\"}\n"
	if n, err := io.WriteString(stderr, message); err != nil || n != len(message) {
		return 1
	}
	return exit
}
