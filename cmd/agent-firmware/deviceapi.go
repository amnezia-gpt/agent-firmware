package main

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/deviceapi"
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
)

const errAPIServe deviceapi.Error = "device_api_requires_serve"

func saveConfiguration(parent context.Context, store configuration.Store, input *configuration.Input) (bool, error) {
	store.BootstrapCheck = func(session *configuration.Session, input *configuration.Input) error {
		ctx, cancel := context.WithTimeout(parent, 30*time.Second)
		defer cancel()
		return checkFreshWorkloadVolumes(ctx, session, input)
	}
	return store.Save(input)
}

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

// enrollmentCode holds or removes the one-time code of the configured
// installation. It reports only whether a code is held, never the value.
func enrollmentCode(store configuration.Store, input io.Reader, set bool) (string, error) {
	code := ""
	if set {
		b, err := io.ReadAll(io.LimitReader(input, deviceapi.MaxEnrollmentCode+2))
		code = strings.TrimSuffix(string(b), "\n")
		if err != nil || !deviceapi.ValidEnrollmentCode(code) {
			return "", deviceapi.ErrEnrollmentCode
		}
	}
	accepted, err := store.Load()
	if err != nil {
		return "", err
	}
	codes, err := deviceapi.EnrollmentCodes(workload.EnrollmentID(accepted.Profile().AuthIssuer), set)
	if err != nil {
		return "", err
	}
	if !set {
		return "none", codes.Delete()
	}
	return "held", codes.Put(code)
}
func enrollmentState(accepted *configuration.Input) string {
	if codes, err := deviceapi.EnrollmentCodes(workload.EnrollmentID(accepted.Profile().AuthIssuer), false); err == nil && codes.Held() {
		return "held"
	}
	return "none"
}

type apiManager struct {
	runtime      *deviceapi.Runtime
	installation string
}

func (m *apiManager) ensure(session *configuration.Session, input *configuration.Input) error {
	installation := workload.EnrollmentID(input.Profile().AuthIssuer)
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
	key, err := session.RootSecret(input)
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
