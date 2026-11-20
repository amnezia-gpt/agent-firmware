package main

import (
	"context"
	"errors"
	"strings"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/installer"
	"github.com/amnezia-gpt/agent-firmware/internal/service"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
)

func operatorText(text string) string {
	if hostLayout.UserService {
		return strings.ReplaceAll(text, "sudo ", "")
	}
	return text
}

type platformService interface {
	installer.ServiceManager
	Available() bool
	Notifier() service.Notifier
}

func localService(layout host.Layout) platformService {
	if layout.UserService {
		return service.Launchd{Layout: layout}
	}
	return service.Systemd{Layout: layout}
}

func localEngineAddress(ctx context.Context, store configuration.Store) (string, error) {
	address, err := store.EngineAddress()
	if hostLayout.UserService && errors.Is(err, configuration.ErrMissing) {
		return dockerengine.ContextSocket(ctx)
	}
	return address, err
}

func checkEngine(ctx context.Context, address string) error {
	engine, err := dockerengine.New(address)
	if err != nil {
		return err
	}
	defer engine.Close()
	err = engine.Prerequisites(ctx)
	if !hostLayout.UserService && errors.Is(err, workload.ErrEngineUnavailable) {
		return workload.ErrEngine
	}
	return err
}
