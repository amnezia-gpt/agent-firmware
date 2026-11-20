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

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/installer"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
)

type decommissionEngine interface {
	removalEngine
	OwnedInstallations(context.Context) ([]string, error)
	CheckAllVolumeRemoval(context.Context, []string) error
	RemoveOwnedVolumes(context.Context, string, bool) error
	RemoveReleaseImages(context.Context, []string) error
}

func runDecommission(args []string, stdout, stderr io.Writer) int {
	confirmed, deleteData, valid := removalFlags(args)
	if !valid {
		return report(stderr, "invalid_usage", 2)
	}
	if !confirmed {
		_, _ = io.WriteString(stderr, "Decommission deletes device keys and firmware. Add --delete-data to erase local data of all domains.\nRun agent-firmware decommission --confirm to proceed.\n")
		return report(stderr, "confirmation_required", 2)
	}
	if reason := host.AccessCheck(); reason != "" {
		return report(stderr, reason, 1)
	}

	parent, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()
	progress := func(message string) { _, _ = io.WriteString(stderr, "agent-firmware: "+message+"\n") }
	layout := hostLayout
	if layout.UserService {
		var err error
		layout.EngineSocket, err = localEngineAddress(ctx, configuration.Store{Path: layout.State})
		if err != nil {
			return reportDecommissionError(stderr, err)
		}
		if err := checkEngine(ctx, layout.EngineSocket); err != nil {
			return reportDecommissionError(stderr, err)
		}
		if err := installer.CheckParents(layout, true); err != nil {
			return reportDecommissionError(stderr, err)
		}
	}
	result, err := decommissionDevice(ctx, layout, localService(layout),
		func(address string) (decommissionEngine, error) { return dockerengine.New(address) }, deleteData, progress)
	if err != nil {
		return reportDecommissionError(stderr, err)
	}
	return respond(stdout, stderr, result)
}

func reportDecommissionError(stderr io.Writer, err error) int {
	if errors.Is(err, configuration.ErrUnsafe) && !hostLayout.UserService {
		_, _ = io.WriteString(stderr, "agent-firmware: The state directory has an unexpected entry, owner or mode. Inspect its metadata:\n"+
			"agent-firmware:   sudo find -P "+hostLayout.State+" -xdev -maxdepth 2 -printf '%y %m %U:%G links=%n %p\\n'\n")
	}
	return report(stderr, err.Error(), 1)
}

func decommissionDevice(ctx context.Context, layout host.Layout, services installer.ServiceManager,
	openEngine func(string) (decommissionEngine, error), deleteData bool, progress func(string),
) (removalResult, error) {
	unlock, err := installer.Lock(layout)
	if err != nil {
		return removalResult{}, err
	}
	defer unlock()

	progress("Checking ownership of firmware resources")
	store := configuration.Store{Path: layout.State, EngineSocket: layout.EngineSocket}
	if _, err := os.Lstat(layout.State); errors.Is(err, os.ErrNotExist) && !deleteData {
		target, err := installer.InspectRemovalHost(ctx, layout, services, false)
		if err != nil {
			return removalResult{}, err
		}
		if !target.Present() {
			if _, err := os.Lstat(layout.Runtime); errors.Is(err, os.ErrNotExist) {
				engine, err := openEngine(layout.EngineSocket)
				if err != nil {
					return removalResult{}, err
				}
				defer engine.Close()
				kept, err := engine.RetainedData(ctx, "")
				return removalResponse("decommissioned", configuration.NoDeregistration, kept, true), err
			}
		}
	}

	var result removalResult
	err = withStateLock(ctx, func() error {
		return store.ExclusiveForDecommission(func(session *configuration.Session) error {
			record, err := session.DecommissionRecord()
			if err != nil {
				return err
			}
			if record != nil && record.DeleteData != deleteData {
				return configuration.ErrRemovalIntent
			}
			target, err := installer.InspectRemovalHost(ctx, layout, services, record != nil)
			if err != nil {
				return err
			}
			var snapshot configuration.Snapshot
			if record == nil {
				record, snapshot, err = planDecommission(session, deleteData)
				if err != nil {
					return err
				}
				if snapshot.Device == nil && !record.Legacy && target.Present() {
					record.Discover = true
					record.Deregistration = configuration.LegacyDeregistration
				}
			}
			if err := session.CheckDecommissionFiles(record.Legacy); err != nil {
				return err
			}
			address, err := session.EngineAddress()
			if err != nil {
				return err
			}
			engine, err := openEngine(address)
			if err != nil {
				return err
			}
			defer engine.Close()
			if !record.LocalResourcesRemoved {
				if err := removeLocalResources(ctx, session, layout, target, record, snapshot, engine, progress); err != nil {
					return err
				}
			} else if target.Present() {
				return installer.ErrConflict
			}
			kept, err := engine.RetainedData(ctx, "")
			if err != nil {
				return err
			}
			progress("Removing device data and keys")
			result = removalResponse("decommissioned", record.Deregistration, kept, true)
			return session.FinishDecommission(record)
		})
	})
	return result, err
}

func planDecommission(session *configuration.Session, deleteData bool) (*configuration.DecommissionRecord, configuration.Snapshot, error) {
	legacy, err := session.LegacyDecommission()
	if legacy != nil {
		legacy.DeleteData = deleteData
	}
	if err != nil || legacy != nil {
		return legacy, configuration.Snapshot{}, err
	}
	record := &configuration.DecommissionRecord{Deregistration: configuration.NoDeregistration, DeleteData: deleteData}
	snapshot, err := session.DecommissionSnapshot()
	switch {
	case errors.Is(err, configuration.ErrMissing):
		return record, snapshot, nil
	case errors.Is(err, configuration.ErrLegacy), errors.Is(err, configuration.ErrCorrupt), errors.Is(err, configuration.ErrRootSecret):
		record.Discover = true
		record.Deregistration = configuration.LegacyDeregistration
		return record, snapshot, nil
	case err != nil:
		return nil, snapshot, err
	}
	for _, spec := range snapshot.Device.Release().Services {
		record.Images = append(record.Images, spec.Image.Reference())
	}
	slices.Sort(record.Images)
	record.Images = slices.Compact(record.Images)
	if snapshot.Enrollment != nil {
		record.Installation = workload.EnrollmentID(snapshot.Enrollment.Profile.AuthIssuer)
		record.Deregistration = configuration.DeregistrationPending
	}
	if snapshot.Removal != nil {
		if record.Installation != "" && record.Installation != snapshot.Removal.Installation {
			return nil, snapshot, configuration.ErrCorrupt
		}
		if snapshot.Removal.DeleteData != deleteData {
			return nil, snapshot, configuration.ErrRemovalIntent
		}
		record.Installation = snapshot.Removal.Installation
		record.Deregistration = snapshot.Removal.Deregistration
	}
	return record, snapshot, nil
}

func removeLocalResources(ctx context.Context, session *configuration.Session, layout host.Layout, target *installer.RemovalHost,
	record *configuration.DecommissionRecord, snapshot configuration.Snapshot, engine decommissionEngine, progress func(string),
) error {
	if err := installer.CheckRemovalRuntime(layout, record.Installation, record.Legacy); err != nil {
		return err
	}
	var installations []string
	if record.Discover {
		var err error
		installations, err = engine.OwnedInstallations(ctx)
		if err != nil {
			return err
		}
	} else if record.Installation != "" {
		installations = []string{record.Installation}
	}
	for _, id := range installations {
		if err := engine.CheckRemoval(ctx, id); err != nil {
			return err
		}
	}
	if record.DeleteData {
		if err := engine.CheckAllVolumeRemoval(ctx, installations); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return workload.ErrCanceled
	}
	if record.Version == 0 {
		if err := session.SaveDecommission(record); err != nil {
			return err
		}
	}
	if record.Deregistration == configuration.DeregistrationPending {
		if snapshot.Device == nil {
			var err error
			snapshot, err = session.DecommissionSnapshot()
			if err != nil {
				return err
			}
		}
		progress("Attempting domain deregistration")
		record.Deregistration = deregister(ctx, engine, snapshot)
		if ctx.Err() != nil {
			return workload.ErrCanceled
		}
		if err := session.SaveDecommission(record); err != nil {
			return err
		}
	}
	progress("Stopping and disabling the firmware service")
	if err := target.Stop(ctx); err != nil {
		return err
	}
	progress("Removing owned workload resources")
	for _, id := range installations {
		if err := engine.RemoveEnrollment(ctx, id, record.DeleteData); err != nil {
			return err
		}
	}
	if record.DeleteData {
		if err := engine.RemoveOwnedVolumes(ctx, "", record.DeleteData); err != nil {
			return err
		}
	}
	if len(record.Images) != 0 {
		if err := engine.RemoveReleaseImages(ctx, record.Images); err != nil {
			return err
		}
	}
	if err := installer.RemoveRuntime(ctx, layout, record.Installation, record.Legacy); err != nil {
		return err
	}
	progress("Removing installed firmware")
	if err := target.Remove(ctx); err != nil {
		return err
	}
	record.LocalResourcesRemoved = true
	return session.SaveDecommission(record)
}
