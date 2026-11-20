package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/deviceapi"
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/installer"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
)

type deregistrationEngine interface {
	Deregister(context.Context, workload.Stack) (string, error)
}

type removalEngine interface {
	deregistrationEngine
	RemoveEnrollment(context.Context, string, bool) error
	CheckRemoval(context.Context, string) error
	RetainedData(context.Context, string) ([]dockerengine.VolumeInfo, error)
	Close()
}

var openRemovalEngine = func(address string) (removalEngine, error) {
	return dockerengine.New(address)
}

var purgeEnrollmentCode = func(layout host.Layout, id string) error {
	codes, err := deviceapi.EnrollmentCodes(layout, id, false)
	if err != nil {
		return err
	}
	return codes.Purge()
}

func runUnenroll(args []string, stdout, stderr io.Writer) int {
	confirmed, deleteData, valid := removalFlags(args)
	if !valid {
		return report(stderr, "invalid_usage", 2)
	}
	const description = "Unenroll removes the domain connection, workloads, and operational keys. Add --delete-data to erase this domain's user data.\n"
	if !confirmed {
		_, _ = io.WriteString(stderr, description+"Run agent-firmware unenroll --confirm to proceed.\n")
		return report(stderr, "confirmation_required", 2)
	}
	if reason := host.AccessCheck(); reason != "" {
		return report(stderr, reason, 1)
	}
	_, _ = io.WriteString(stderr, description)
	parent, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	result, err := unenrollDevice(ctx, configuration.Store{Path: defaultStateDir}, hostLayout, deleteData)
	if err != nil {
		return report(stderr, err.Error(), 1)
	}
	return respond(stdout, stderr, result)
}

func unenrollDevice(ctx context.Context, store configuration.Store, layout host.Layout, deleteData bool) (removalResult, error) {
	unlock, err := installer.Lock(layout)
	if err != nil {
		return removalResult{}, err
	}
	defer unlock()
	result := removalResponse("manufactured", configuration.NoDeregistration, nil, false)
	err = withStateLock(ctx, func() error {
		return store.Exclusive(func(session *configuration.Session) error {
			snapshot, err := session.Snapshot()
			if err != nil {
				return err
			}
			if snapshot.Removal == nil && snapshot.Enrollment == nil {
				return nil
			}
			installation := ""
			if snapshot.Enrollment != nil {
				installation = workload.EnrollmentID(snapshot.Enrollment.Profile.AuthIssuer)
			}
			removal := snapshot.Removal
			if removal != nil {
				if installation != "" && removal.Installation != installation {
					return configuration.ErrCorrupt
				}
				if removal.DeleteData != deleteData {
					return configuration.ErrRemovalIntent
				}
				installation = removal.Installation
			}
			address, err := session.EngineAddress()
			if err != nil {
				return err
			}
			engine, err := openRemovalEngine(address)
			if err != nil {
				return err
			}
			defer engine.Close()
			if err := engine.CheckRemoval(ctx, installation); err != nil {
				return err
			}
			if removal == nil {
				removal, err = session.BeginRemoval(installation, deleteData)
				if err != nil {
					return err
				}
			}
			if removal.Deregistration == configuration.DeregistrationPending {
				outcome := deregister(ctx, engine, snapshot)
				if ctx.Err() != nil {
					return workload.ErrCanceled
				}
				if err := session.SetDeregistration(removal, outcome); err != nil {
					return err
				}
			}
			if err := engine.RemoveEnrollment(ctx, removal.Installation, removal.DeleteData); err != nil {
				return err
			}
			if err := purgeEnrollmentCode(layout, removal.Installation); err != nil {
				return err
			}
			kept, err := engine.RetainedData(ctx, removal.Installation)
			if err != nil {
				return err
			}
			if err := session.FinishRemoval(removal.Installation); err != nil {
				return err
			}
			result = removalResponse("manufactured", removal.Deregistration, kept, false)
			return nil
		})
	})
	return result, err
}

func withStateLock(ctx context.Context, operation func() error) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return workload.ErrCanceled
		}
		err := operation()
		if !errors.Is(err, configuration.ErrBusy) {
			return err
		}
		select {
		case <-ctx.Done():
			return workload.ErrCanceled
		case <-ticker.C:
		}
	}
}

func deregister(ctx context.Context, engine deregistrationEngine, snapshot configuration.Snapshot) string {
	if snapshot.Enrollment == nil {
		return configuration.DeregistrationUnavailable
	}
	stack, err := workload.FromInput(snapshot.Enrollment.Input(snapshot.Device))
	if err != nil {
		return configuration.DeregistrationUnavailable
	}
	outcome, err := engine.Deregister(ctx, stack)
	if err != nil {
		return configuration.DeregistrationUnavailable
	}
	switch outcome {
	case configuration.Deregistered, configuration.DeregistrationFailed:
		return outcome
	default:
		return configuration.DeregistrationUnavailable
	}
}
