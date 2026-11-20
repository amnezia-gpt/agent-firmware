// Package supervisor runs serialized, cancellation-aware convergence cycles.
package supervisor

import (
	"context"
	"errors"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/backendprofile"
	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/deviceapi"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	"github.com/amnezia-gpt/agent-firmware/internal/workloadrelease"
)

const Interval = 30 * time.Second

type Error string

func (e Error) Error() string { return string(e) }

const ErrOutput Error = "output_failed"
const ErrNotify Error = "service_notify_failed"

type Event struct {
	State     string `json:"state"`
	ErrorCode string `json:"error_code,omitempty"`
}

// Run owns one cycle at a time and emits only bounded state transitions.
func Run(ctx context.Context, cycle func(context.Context) (bool, error), emit func(Event) error, interval time.Duration) error {
	var previous Event
	output := func(event Event) error {
		if event == previous {
			return nil
		}
		if err := emit(event); err != nil {
			if errors.Is(err, ErrNotify) {
				return ErrNotify
			}
			return ErrOutput
		}
		previous = event
		return nil
	}
	for {
		if ctx.Err() != nil {
			return output(Event{State: "stopped"})
		}
		changed, err := cycle(ctx)
		if ctx.Err() != nil {
			return output(Event{State: "stopped"})
		}
		event := Event{State: "converged"}
		if changed {
			event.State = "repaired"
		}
		switch {
		case errors.Is(err, configuration.ErrMissing):
			event.State = "awaiting_configuration"
		case errors.Is(err, configuration.ErrBusy):
			event.State = "busy"
		case err != nil:
			event.State = "failed"
			event.ErrorCode = ErrorCode(err)
		}
		if err := output(event); err != nil {
			return err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return output(Event{State: "stopped"})
		case <-timer.C:
		}
	}
}
func ErrorCode(err error) string {
	if errors.Is(err, workload.ErrEnginePrerequisites) {
		return workload.ErrEnginePrerequisites.Error()
	}
	for _, allowed := range []error{workloadrelease.ErrDeviceAPI, workloadrelease.ErrPlatform, backendprofile.ErrScopeMissing, backendprofile.ErrScopeAmbiguous, configuration.ErrInvalid, configuration.ErrLegacy, configuration.ErrUnsafe, configuration.ErrRead, configuration.ErrWrite, configuration.ErrCorrupt, configuration.ErrUncertain, configuration.ErrIdentityConflict, configuration.ErrRootSecret, deviceapi.ErrKeystore, configuration.ErrPending, workload.ErrEngine, workload.ErrConflict, workload.ErrHealth, workload.ErrTimeout, workload.ErrCanceled, workload.ErrDeadline, workload.ErrJournal, workload.ErrRecovery, workload.ErrCleanup} {
		if errors.Is(err, allowed) {
			return allowed.Error()
		}
	}
	return "supervision_failed"
}
