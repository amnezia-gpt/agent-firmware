package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/backendprofile"
	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/deviceapi"
	"github.com/amnezia-gpt/agent-firmware/internal/discovery"
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/service"
	"github.com/amnezia-gpt/agent-firmware/internal/supervisor"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
)

var firmwareVersion = "development"

const (
	notInstalled      = "not installed"
	manufactured      = "manufactured and not enrolled"
	enrollmentPending = "enrollment pending"
	enrolled          = "enrolled"
)

func lifecycle(snapshot configuration.Snapshot) string {
	switch {
	case snapshot.Device == nil:
		return notInstalled
	case snapshot.Removal != nil:
		return enrollmentPending
	case snapshot.Enrollment == nil:
		return manufactured
	case snapshot.Enrollment.Outcome != configuration.Enrolled:
		return enrollmentPending
	default:
		return enrolled
	}
}

func localState(store configuration.Store) (configuration.Snapshot, error) {
	snapshot, err := store.Snapshot()
	if errors.Is(err, configuration.ErrMissing) {
		return configuration.Snapshot{}, nil
	}
	return snapshot, err
}

func runStateCommand(args []string, stdout, stderr io.Writer) int {
	path := defaultStateDir
	if len(args) != 1 {
		if len(args) != 3 || args[1] != "--state-dir" || args[2] == "" {
			return report(stderr, "invalid_usage", 2)
		}
		path = args[2]
	}
	if os.Geteuid() != 0 {
		return report(stderr, "root_required", 1)
	}
	store := configuration.Store{Path: path}
	if args[0] == "status" {
		return showStatus(store, stdout, stderr)
	}
	if err := store.CheckLegacy(); err != nil {
		return report(stderr, err.Error(), 1)
	}
	parent, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if args[0] == "serve" {
		if err := serve(parent, store, stdout); err != nil {
			return report(stderr, err.Error(), 1)
		}
		return 0
	}
	result, err := executeManaged(parent, store, nil)
	if err != nil {
		return report(stderr, err.Error(), 1)
	}
	return respond(stdout, stderr, result)
}

var listStatusData = func(ctx context.Context, store configuration.Store) ([]dockerengine.VolumeInfo, error) {
	address, err := store.EngineAddress()
	if err != nil {
		return nil, err
	}
	engine, err := dockerengine.New(address)
	if err != nil {
		return nil, err
	}
	defer engine.Close()
	return engine.RetainedData(ctx, "")
}

func showStatus(store configuration.Store, stdout, stderr io.Writer) int {
	snapshot, err := localState(store)
	if err != nil {
		return report(stderr, err.Error(), 1)
	}
	state := lifecycle(snapshot)
	message := "Device " + state + "\nFirmware " + firmwareVersion + "\n"
	if snapshot.Device != nil {
		message += "IDevID " + snapshot.Device.Thumbprint() + "\n"
	}
	if state == enrolled {
		message += "Issuer " + snapshot.Enrollment.Profile.AuthIssuer + "\n"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	volumes, err := listStatusData(ctx, store)
	switch {
	case err != nil:
		message += "User data: unknown (local engine unavailable)\n"
	case len(volumes) == 0:
		message += "User data: []\n"
	default:
		message += "User data:\n"
		for _, volume := range volumes {
			issuer := volume.Issuer
			if issuer == "" {
				issuer = "unknown"
			}
			created := volume.CreatedAt
			if created == "" {
				created = "unknown"
			}
			message += fmt.Sprintf("  %s  %s  %s\n", issuer, volume.Name, created)
		}
	}
	if n, err := io.WriteString(stdout, message); err != nil || n != len(message) {
		return report(stderr, "output_failed", 1)
	}
	return 0
}

func serve(parent context.Context, store configuration.Store, stdout io.Writer) error {
	notifier := (service.Systemd{Layout: hostLayout}).Notifier()
	publishLinkStatus, finishLinkStatus := startLinkStatus(notifier)
	manager := &apiManager{updateLinkStatus: publishLinkStatus}
	defer func() {
		manager.close()
		finishLinkStatus()
		manager.drain(stdout)
	}()
	event := func(value any) error {
		if !writeServiceRecord(stdout, value) {
			return supervisor.ErrOutput
		}
		return nil
	}
	var previousState string
	return supervisor.Run(parent, func(ctx context.Context) (bool, error) {
		result, err := executeManaged(ctx, store, manager)
		manager.drain(stdout)
		if snapshot, currentErr := localState(store); currentErr == nil {
			if state := lifecycle(snapshot); state != previousState {
				if err := event(struct {
					DeviceState string `json:"device_state"`
				}{state}); err != nil {
					return false, err
				}
				previousState = state
			}
		}
		return result.Changed, err
	}, func(signal supervisor.Event) error {
		if err := event(signal); err != nil {
			return err
		}
		if signal.State == "converged" || signal.State == "repaired" {
			if notifier.Ready() != nil {
				return supervisor.ErrNotify
			}
		}
		return nil
	}, supervisor.Interval)
}

var refreshResources = func(ctx context.Context, profile backendprofile.Profile) ([]byte, error) {
	client := discovery.NewClient()
	defer client.CloseIdleConnections()
	return discovery.RefreshResources(ctx, client, profile)
}

func (m *apiManager) stack(ctx context.Context, snapshot configuration.Snapshot) (workload.Stack, []byte, error) {
	stack, err := workload.FromInput(snapshot.Enrollment.Input(snapshot.Device))
	if !errors.Is(err, backendprofile.ErrScopeMissing) {
		return stack, nil, err
	}
	// An installed candidate gets one resource refresh in this service run.
	// Manufacture and update never look up the issuer themselves.
	generation := snapshot.Device.Generation()
	if m.refreshGeneration != generation {
		m.refreshGeneration = generation
		lookup, cancel := context.WithTimeout(ctx, time.Minute)
		m.refreshProfile, m.refreshErr = refreshResources(lookup, snapshot.Enrollment.Profile)
		cancel()
	}
	if m.refreshErr != nil {
		return workload.Stack{}, nil, m.refreshErr
	}
	input, err := snapshot.Device.WithProfile(m.refreshProfile)
	if err != nil {
		return workload.Stack{}, nil, err
	}
	before, after := snapshot.Enrollment.Profile, input.Profile()
	if before.ID != after.ID || before.AuthIssuer != after.AuthIssuer ||
		before.IngressTrustBundle != after.IngressTrustBundle || before.IngressTrustSHA256 != after.IngressTrustSHA256 {
		return workload.Stack{}, nil, configuration.ErrIdentityConflict
	}
	// Retain the old scope bindings for a rollback to the preceding release.
	// Discovery is authoritative only for the newly requested missing scopes.
	before.Resources = slices.Clone(before.Resources)
	for _, scope := range snapshot.Device.Release().ResourceScopes() {
		if _, err := before.ResolveScope(scope); !errors.Is(err, backendprofile.ErrScopeMissing) {
			continue
		}
		resource, err := after.ResolveScope(scope)
		if err != nil {
			return workload.Stack{}, nil, err
		}
		index := slices.IndexFunc(before.Resources, func(current backendprofile.Resource) bool {
			return current.Identifier == resource
		})
		if index == -1 {
			before.Resources = append(before.Resources, backendprofile.Resource{Identifier: resource, Scopes: []string{scope}})
		} else {
			before.Resources[index].Scopes = append(slices.Clone(before.Resources[index].Scopes), scope)
		}
	}
	raw, err := backendprofile.Encode(before)
	if err != nil {
		return workload.Stack{}, nil, configuration.ErrInvalid
	}
	input, err = snapshot.Device.WithProfile(raw)
	if err != nil {
		return workload.Stack{}, nil, err
	}
	stack, err = workload.FromInput(input)
	return stack, raw, err
}

func executeManaged(parent context.Context, store configuration.Store, manager *apiManager) (workload.Result, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Minute)
	defer cancel()
	var result workload.Result
	err := store.Exclusive(func(session *configuration.Session) error {
		snapshot, err := session.Snapshot()
		if err != nil {
			return err
		}
		if err := checkDeployableRelease(snapshot.Device.Release()); err != nil {
			return err
		}
		if snapshot.Removal != nil {
			// Deregistration may use the already-connected signing service.
			// Once teardown is journaled, never reconcile or open a new link.
			result.Applied = true
			return nil
		}
		if snapshot.Enrollment == nil {
			manager.stop(deviceapi.LinkTargetChanged)
			result.Applied = true
			return nil
		}
		if manager == nil {
			return errAPIServe
		}
		stack, refreshed, err := manager.stack(ctx, snapshot)
		if err != nil {
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
		if err != nil {
			return err
		}
		// Link readiness is independent of the workload's local convergence.
		target, linkErr := engine.ClientTarget(ctx, stack)
		if linkErr == nil {
			linkErr = manager.ensure(parent, session, snapshot, address, target, stack.Client.Dial)
		}
		if linkErr != nil {
			manager.stop(deviceapi.LinkTargetChanged)
			if parent.Err() == nil {
				manager.recordLink(manager.link.Unavailable(linkErr))
			}
		}
		if refreshed != nil {
			if err := session.RefreshEnrollment(snapshot.Enrollment, refreshed); err != nil {
				return err
			}
		}
		if snapshot.Enrollment.Outcome != configuration.Enrolled {
			outcome, statusErr := manager.status.Run(ctx, engine, stack)
			if statusErr == nil {
				return observeEnrollment(session, snapshot.Enrollment, outcome)
			}
		}
		return nil
	})
	if errors.Is(err, configuration.ErrRootSecret) || errors.Is(err, configuration.ErrCorrupt) ||
		errors.Is(err, configuration.ErrMissing) || errors.Is(err, configuration.ErrRead) ||
		errors.Is(err, configuration.ErrUnsafe) || errors.Is(err, configuration.ErrLegacy) {
		manager.stop(deviceapi.LinkTargetChanged)
	}
	return result, err
}
