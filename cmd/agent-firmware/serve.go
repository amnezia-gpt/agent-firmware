package main

import (
	"context"
	"errors"
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
	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/installer"
	"github.com/amnezia-gpt/agent-firmware/internal/privatefile"
	"github.com/amnezia-gpt/agent-firmware/internal/service"
	"github.com/amnezia-gpt/agent-firmware/internal/supervisor"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
)

var firmwareVersion = "development"

func localState(store configuration.Store) (configuration.Snapshot, error) {
	snapshot, err := store.Snapshot()
	if errors.Is(err, configuration.ErrMissing) {
		return configuration.Snapshot{}, nil
	}
	if errors.Is(err, configuration.ErrDecommissionPending) {
		return configuration.Snapshot{Decommissioning: true}, nil
	}
	return snapshot, err
}

func runStateCommand(args []string, stdout, stderr io.Writer) int {
	path := defaultStateDir
	var asJSON, hasStateDir bool
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--json":
			if asJSON || args[0] != "status" {
				return report(stderr, "invalid_usage", 2)
			}
			asJSON = true
		case "--state-dir":
			if hasStateDir || i+1 == len(args) || args[i+1] == "" {
				return report(stderr, "invalid_usage", 2)
			}
			i++
			path, hasStateDir = args[i], true
		default:
			return report(stderr, "invalid_usage", 2)
		}
	}
	if reason := host.AccessCheck(); reason != "" {
		return report(stderr, reason, 1)
	}
	store := configuration.Store{Path: path}
	if args[0] == "status" {
		if asJSON {
			return renderStatus(store, true, stdout, stderr)
		}
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

func serve(parent context.Context, store configuration.Store, stdout io.Writer) error {
	userService := hostLayout.UserService && store.Path == defaultStateDir
	if userService {
		if err := installer.CheckParents(hostLayout, false); err != nil {
			return err
		}
		if err := os.Mkdir(hostLayout.Runtime, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return configuration.ErrWrite
		}
		root, err := privatefile.OpenDirectory(hostLayout.Runtime, privatefile.EffectiveOwner(), 0700)
		if err != nil {
			return configuration.ErrUnsafe
		}
		root.Close()
	}
	notifier := localService(hostLayout).Notifier()
	manager := &apiManager{}
	publishRuntime := func(status *runtimeStatus) {
		if store.Path == defaultStateDir {
			writeRuntimeStatus(hostLayout, status)
		}
	}
	publishRuntime(nil)
	publishLinkStatus, finishLinkStatus := startLinkStatus(notifier, manager.currentPhase)
	manager.updateLinkStatus = publishLinkStatus
	defer func() {
		manager.close()
		finishLinkStatus()
		manager.drain(stdout)
		publishRuntime(nil)
	}()
	event := func(value any) error {
		if !writeServiceRecord(stdout, value) {
			return supervisor.ErrOutput
		}
		return nil
	}
	var previousIdentity configuration.IdentityCondition
	readiness := service.Readiness{PID: os.Getpid()}
	if initial, err := localState(store); err == nil && initial.Enrollment != nil {
		previousIdentity = initial.Enrollment.Identity
	}
	manager.observeIdentity = func(current configuration.IdentityCondition) error {
		if current.Status == previousIdentity.Status && current.Reason == previousIdentity.Reason {
			return nil
		}
		if err := event(identityTransitionRecord(previousIdentity, current)); err != nil {
			return err
		}
		previousIdentity = current
		return nil
	}
	return supervisor.Run(parent, func(ctx context.Context) (bool, error) {
		result, err := executeManaged(ctx, store, manager)
		if errors.Is(err, workload.ErrEngineUnavailable) {
			manager.stop(deviceapi.LinkTargetChanged)
		}
		manager.drain(stdout)
		if snapshot, currentErr := localState(store); currentErr == nil {
			now := time.Now().UTC().Truncate(time.Second)
			state, _ := phaseOf(snapshot, now)
			if state != manager.currentPhase() {
				if err := event(struct {
					DeviceState string `json:"device_state"`
				}{state}); err != nil {
					return false, err
				}
				manager.phase.Store(&state)
				if publishLinkStatus != nil {
					publishLinkStatus(manager.currentLink())
				}
			}
			var observed *runtimeStatus
			if e := snapshot.Enrollment; e != nil {
				if err := manager.observeIdentity(e.Identity); err != nil {
					return false, err
				}
				if snapshot.Removal == nil {
					observed = observedRuntime(snapshot, manager.currentLink(), err, now)
				}
			} else {
				previousIdentity = configuration.IdentityCondition{}
			}
			if userService && snapshot.Device != nil && snapshot.Removal == nil {
				if observed == nil {
					observed = &runtimeStatus{Generation: snapshot.Device.Generation()}
				}
				observed.Engine = observeEngine(err, now)
				if err == nil && readiness.ReadyAt.IsZero() {
					readiness.ReadyAt = time.Now().UTC()
				}
				observed.Readiness = readiness
			}
			if !errors.Is(err, configuration.ErrBusy) || observed == nil {
				publishRuntime(observed)
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

type identityTransitionRecordFields struct {
	Event          string                       `json:"event"`
	PreviousStatus string                       `json:"previous_status"`
	PreviousReason configuration.IdentityReason `json:"previous_reason"`
	Status         string                       `json:"status"`
	Reason         configuration.IdentityReason `json:"reason"`
}

func identityTransitionRecord(previous, current configuration.IdentityCondition) identityTransitionRecordFields {
	if previous.Status == "" {
		previous.Status, previous.Reason = "unknown", configuration.IdentityUnobserved
	}
	return identityTransitionRecordFields{"identity_condition_changed", previous.Status, previous.Reason, current.Status, current.Reason}
}

func observedRuntime(snapshot configuration.Snapshot, link deviceapi.LinkEvent, err error, now time.Time) *runtimeStatus {
	e := snapshot.Enrollment
	status := &runtimeStatus{
		Generation: snapshot.Device.Generation(), Binding: workload.EnrollmentID(e.Profile.AuthIssuer), BoundAt: e.BoundAt,
		Workloads: statusCondition{Type: "workloads_ready", Status: "true", Reason: "converged", LastProbeTime: now},
		Link:      statusCondition{Type: "link_ready", Status: "unknown", Reason: "not_observed"},
	}
	if err != nil {
		status.Workloads.Status, status.Workloads.Reason = "false", supervisor.ErrorCode(err)
	}
	if link.State == "connected" {
		status.Link.Status, status.Link.Reason = "true", "connected"
	} else if link.Reason != "" {
		status.Link.Status, status.Link.Reason = "false", string(link.Reason)
	}
	return status
}

var refreshResources = func(ctx context.Context, profile backendprofile.Profile) ([]byte, error) {
	client := discovery.NewClient()
	defer client.CloseIdleConnections()
	return discovery.RefreshResources(ctx, client, profile)
}

func (m *apiManager) stack(ctx context.Context, snapshot configuration.Snapshot) (workload.Stack, []byte, error) {
	stack, stackErr := workload.FromInput(snapshot.Enrollment.Input(snapshot.Device))
	missingOptional := snapshot.Device.OptionalResourceRefreshPending() &&
		slices.ContainsFunc(stack.Specs, func(spec workload.Spec) bool { return len(spec.UnsetEnv) > 0 })
	if !errors.Is(stackErr, backendprofile.ErrScopeMissing) && !missingOptional {
		return stack, nil, stackErr
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
	added := false
	for _, scope := range snapshot.Device.Release().ResourceScopes() {
		if _, err := before.ResolveScope(scope.Scope); !errors.Is(err, backendprofile.ErrScopeMissing) {
			continue
		}
		resource, err := after.ResolveScope(scope.Scope)
		if errors.Is(err, backendprofile.ErrScopeMissing) {
			continue
		}
		if err != nil {
			return workload.Stack{}, nil, err
		}
		index := slices.IndexFunc(before.Resources, func(current backendprofile.Resource) bool {
			return current.Identifier == resource
		})
		if index == -1 {
			before.Resources = append(before.Resources, backendprofile.Resource{Identifier: resource, Scopes: []string{scope.Scope}})
		} else {
			before.Resources[index].Scopes = append(slices.Clone(before.Resources[index].Scopes), scope.Scope)
		}
		added = true
	}
	if !added {
		return stack, nil, stackErr
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
	if err != nil {
		return workload.Stack{}, nil, err
	}
	return stack, raw, nil
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
			if hostLayout.UserService && manager != nil && store.Path == defaultStateDir {
				address, err := session.EngineAddress()
				if err != nil {
					return err
				}
				if err := checkEngine(ctx, address); err != nil {
					return err
				}
			}
			result.Applied = true
			return session.CompleteOptionalResourceRefresh(snapshot.Device)
		}
		if manager == nil {
			return errAPIServe
		}
		if manager.observeIdentity != nil {
			if err := manager.observeIdentity(snapshot.Enrollment.Identity); err != nil {
				return err
			}
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
		if err := engine.Prerequisites(ctx); err != nil {
			if !hostLayout.UserService && errors.Is(err, workload.ErrEngineUnavailable) {
				return workload.ErrEngine
			}
			return err
		}
		stack, refreshed, err := manager.stack(ctx, snapshot)
		if err != nil {
			return err
		}
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
		if err := session.CompleteOptionalResourceRefresh(snapshot.Device); err != nil {
			return err
		}
		if target.ID != "" {
			outcome, statusErr := manager.status.Run(ctx, engine, target, stack.Client.EnrollmentStatus)
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
