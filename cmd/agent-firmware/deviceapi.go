package main

import (
	"context"
	"encoding/json"
	"io"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/deviceapi"
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
)

const errAPIServe deviceapi.Error = "device_api_requires_serve"

var checkFreshWorkloadVolumes = func(ctx context.Context, session *configuration.Session, input *configuration.Input) error {
	stack, err := workload.FromInput(input)
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
	exists, err := engine.AnyVolumesExist(ctx, stack)
	if err != nil {
		return err
	}
	if exists {
		return configuration.ErrRootSecret
	}
	return nil
}

type apiManager struct {
	runtime           *deviceapi.Runtime
	installation      string
	status            dockerengine.StatusCommand
	refreshGeneration string
	refreshProfile    []byte
	refreshErr        error
}

func (m *apiManager) ensure(session *configuration.Session, device *configuration.Device, issuer string) error {
	installation := workload.EnrollmentID(issuer)
	if m.runtime != nil {
		if m.installation != installation {
			return deviceapi.ErrKeystore
		}
		if m.runtime.Running() {
			m.runtime.Codes.Held() // Removes a code that outlived its hour unredeemed.
			return nil
		}
		m.runtime.Close()
		m.runtime = nil
	}
	key, err := session.RootSecret(device)
	if err != nil {
		return err
	}
	defer clear(key)
	rt, err := deviceapi.Start(installation, key)
	if err != nil {
		return err
	}
	m.runtime = rt
	m.installation = installation
	return nil
}
func (m *apiManager) close() {
	if m != nil && m.runtime != nil {
		m.runtime.Close()
		m.runtime = nil
	}
}

func (m *apiManager) drain(out io.Writer) {
	if m.runtime == nil {
		return
	}
	for i := 0; i < 32; i++ {
		select {
		case event := <-m.runtime.Events:
			b, _ := json.Marshal(event)
			b = append(b, '\n')
			out.Write(b)
		default:
			return
		}
	}
}
