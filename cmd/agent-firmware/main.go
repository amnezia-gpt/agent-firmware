package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/backendprofile"
	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/deviceapi"
	"github.com/amnezia-gpt/agent-firmware/internal/discovery"
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/service"
	"github.com/amnezia-gpt/agent-firmware/internal/supervisor"
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
		return runInstall(args[1:], input, stdout, stderr)
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
		return runConfiguration(args, input, stdout, stderr)
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

// The existing socket transport serves the earlier protocol. SELF-19 moves
// this runtime gate to the v3 stream when that transport is available.
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

func runConfiguration(args []string, input io.Reader, stdout, stderr io.Writer) int {
	command := args[0]
	enrollment := command == "enrollment-code-set" || command == "enrollment-code-clear"
	if command != "validate" && command != "configure" && command != "status" && command != "apply" && command != "serve" && !enrollment {
		return report(stderr, "invalid_usage", 2)
	}
	options := map[string]string{}
	for i := 1; i < len(args); i += 2 {
		if i+1 >= len(args) || args[i+1] == "" || options[args[i]] != "" {
			return report(stderr, "invalid_usage", 2)
		}
		switch args[i] {
		case "--profile", "--release", "--state-dir":
		default:
			return report(stderr, "invalid_usage", 2)
		}
		options[args[i]] = args[i+1]
	}
	if command == "status" || command == "apply" || command == "serve" || enrollment {
		if options["--profile"] != "" || options["--release"] != "" {
			return report(stderr, "invalid_usage", 2)
		}
	} else if options["--profile"] == "" || options["--release"] == "" || command == "validate" && options["--state-dir"] != "" {
		return report(stderr, "invalid_usage", 2)
	}
	if command != "validate" && os.Geteuid() != 0 {
		return report(stderr, "root_required", 1)
	}
	dir := options["--state-dir"]
	if dir == "" {
		dir = defaultStateDir
	}
	store := configuration.Store{Path: dir}
	if enrollment {
		// The code arrives on standard input only: arguments are visible in process listings.
		state, err := enrollmentCode(store, input, command == "enrollment-code-set")
		if err != nil {
			return report(stderr, err.Error(), 1)
		}
		return respond(stdout, stderr, struct {
			EnrollmentCode string `json:"enrollment_code"`
		}{state})
	}
	if command == "apply" || command == "serve" {
		parent, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if command == "serve" {
			manager := &apiManager{}
			defer manager.close()
			notifier := (service.Systemd{Layout: hostLayout}).Notifier()
			err := supervisor.Run(parent, func(ctx context.Context) (bool, error) {
				result, err := executeManaged(ctx, store, manager)
				manager.drain(stdout)
				return result.Changed, err
			}, func(event supervisor.Event) error {

				data, _ := json.Marshal(event)
				data = append(data, '\n')
				n, err := stdout.Write(data)
				if err != nil || n != len(data) {
					return supervisor.ErrOutput
				}
				if event.State == "converged" || event.State == "repaired" {
					if notifier.Ready() != nil {
						return supervisor.ErrNotify
					}
				}
				return nil
			}, supervisor.Interval)
			if err != nil {
				return report(stderr, err.Error(), 1)
			}
			return 0
		}
		result, err := execute(parent, store)
		if err != nil {
			return report(stderr, err.Error(), 1)
		}
		return respond(stdout, stderr, result)
	}

	if command == "status" {
		accepted, err := store.Load()
		if errors.Is(err, configuration.ErrMissing) {
			return respond(stdout, stderr, struct {
				Configured bool `json:"configured"`
			}{false})
		}
		if err != nil {
			return report(stderr, err.Error(), 1)
		}
		return respond(stdout, stderr, struct {
			Configured     bool   `json:"configured"`
			Generation     string `json:"generation"`
			EnrollmentCode string `json:"enrollment_code"`
		}{true, accepted.Generation(), enrollmentState(accepted)})
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
	accepted, err := configuration.Parse(profile, release)
	if err != nil {
		return report(stderr, err.Error(), 1)
	}
	if err := checkPlatform(accepted.Release()); err != nil {
		return report(stderr, err.Error(), 1)
	}
	if _, err := workload.FromInput(accepted); err != nil {
		return report(stderr, err.Error(), 1)
	}
	if command == "validate" {
		return respond(stdout, stderr, struct {
			Valid bool `json:"valid"`
		}{true})
	}

	if err := checkDeployableRelease(accepted.Release()); err != nil {
		return report(stderr, err.Error(), 1)
	}
	changed, err := saveConfiguration(context.Background(), store, accepted)
	if err != nil {
		return report(stderr, err.Error(), 1)
	}
	return respond(stdout, stderr, struct {
		Configured bool   `json:"configured"`
		Changed    bool   `json:"changed"`
		Generation string `json:"generation"`
	}{true, changed, accepted.Generation()})
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

func execute(parent context.Context, store configuration.Store) (workload.Result, error) {
	return executeManaged(parent, store, nil)
}
func executeManaged(parent context.Context, store configuration.Store, manager *apiManager) (workload.Result, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Minute)
	defer cancel()
	var result workload.Result
	err := store.Exclusive(func(session *configuration.Session) error {
		input, err := session.Load()
		if err != nil {
			return err
		}
		if err := checkDeployableRelease(input.Release()); err != nil {
			return err
		}
		stack, err := workload.FromInput(input)
		if err != nil {
			return err
		}
		if manager == nil {
			return errAPIServe
		}
		if err := manager.ensure(session, input); err != nil {
			manager.close()
			return err
		}
		address, err := session.EngineAddress()
		if err != nil {
			return err
		}
		engine, err := dockerengine.New(address)
		if err != nil {
			return err
		}
		defer engine.Close()
		result, err = workload.Apply(ctx, engine, session, stack)
		return err
	})
	if errors.Is(err, configuration.ErrRootSecret) || errors.Is(err, configuration.ErrCorrupt) ||
		errors.Is(err, configuration.ErrMissing) || errors.Is(err, configuration.ErrRead) ||
		errors.Is(err, configuration.ErrUnsafe) {
		manager.close()
	}
	return result, err
}
