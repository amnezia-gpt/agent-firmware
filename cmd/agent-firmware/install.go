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
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/installer"
	"github.com/amnezia-gpt/agent-firmware/internal/service"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
)

type hostFirmware struct {
	store  configuration.Store
	device *configuration.Device
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
	if err := engine.Prerequisites(ctx); err != nil {
		return err
	}
	snapshot, err := f.store.Snapshot()
	if errors.Is(err, configuration.ErrMissing) {
		err = engine.CheckManufactureVolumes(ctx)
	} else if err == nil && snapshot.Enrollment != nil {
		stack := workload.VolumeStack(f.device.Release(), snapshot.Enrollment.Profile.AuthIssuer)
		_, err = engine.CheckEnrollmentVolumes(ctx, stack, false)
	}
	if err != nil {
		return err
	}
	return engine.PrepareImages(ctx, f.device.Release())
}

func (f hostFirmware) Existing() error {
	snapshot, err := f.store.Snapshot()
	if snapshot.Removal != nil {
		return configuration.ErrRemovalPending
	}
	if errors.Is(err, configuration.ErrMissing) {
		return nil
	}
	return err
}

func (f hostFirmware) Manufacture(ctx context.Context) error {
	_, err := f.store.Manufacture(f.device, func(session *configuration.Session) error {
		address, err := session.EngineAddress()
		if err != nil {
			return err
		}
		engine, err := dockerengine.New(address)
		if err != nil {
			return err
		}
		defer engine.Close()
		return engine.CheckManufactureVolumes(ctx)
	})
	if err != nil {
		return err
	}
	return f.store.PersistEngineAddress()
}

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
	place = func(ctx context.Context, device *configuration.Device, progress func(string)) error {
		binary, err := os.Executable()
		if err != nil {
			return installer.ErrWrite
		}
		store := configuration.Store{Path: defaultStateDir}
		return installer.Install(ctx, installer.Request{
			Layout: hostLayout, Binary: binary,
			Services: service.Systemd{Layout: hostLayout}, Firmware: hostFirmware{store, device}, Progress: progress,
		})
	}
)

func runInstall(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 || args[0] != "--release" || args[1] == "" {
		return report(stderr, "invalid_usage", 2)
	}
	// Refuse old state before the transport gate without touching the host.
	if err := (configuration.Store{Path: defaultStateDir}).CheckLegacy(); err != nil {
		return report(stderr, err.Error(), 1)
	}
	releaseFile, err := openInput(args[1])
	if err != nil {
		return report(stderr, "input_read_failed", 1)
	}
	defer releaseFile.Close()
	device, err := configuration.ParseDevice(releaseFile)
	if err != nil {
		return report(stderr, err.Error(), 1)
	}
	if err := checkDeployableRelease(device.Release()); err != nil {
		return report(stderr, err.Error(), 1)
	}
	if reason := hostReady(); reason != "" {
		return report(stderr, reason, 1)
	}
	parent, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	ctx, cancel := context.WithTimeout(parent, 45*time.Minute)
	defer cancel()
	progress := func(message string) {
		_, _ = io.WriteString(stderr, "agent-firmware: "+message+"\n")
	}
	if err := place(ctx, device, progress); err != nil {
		return report(stderr, err.Error(), 1)
	}
	return respond(stdout, stderr, struct {
		Installed bool `json:"installed"`
	}{true})
}
