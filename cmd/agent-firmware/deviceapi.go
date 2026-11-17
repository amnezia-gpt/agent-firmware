package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/deviceapi"
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	"github.com/amnezia-gpt/agent-firmware/internal/workloadrelease"
)

const errAPIInit deviceapi.Error = "device_api_initialization_required"
const errAPIServe deviceapi.Error = "device_api_requires_serve"

// Explicit preparation is allowed on the legacy profile before its upgrade.
// Automatic preparation admits only a fresh installation without retained identity or data.
func prepareAPI(ctx context.Context, store configuration.Store, explicit bool) (bool, error) {
	enabled := false
	err := store.Exclusive(func(s *configuration.Session) error {
		input, err := s.Load()
		if err != nil {
			return err
		}
		required, e := s.DeviceAPIRequired()
		if e != nil {
			return e
		}
		if required && input.Release().Contract != workloadrelease.DeviceAPIContract {
			return configuration.ErrContractDowngrade
		}
		enabled = input.Release().Contract == workloadrelease.DeviceAPIContract
		if !explicit && !enabled {
			return nil
		}
		stack := workload.FromInput(input)
		if _, err := deviceapi.Inspect(store.Path, stack.Installation); err == nil {
			return nil
		}
		if required {
			return errAPIInit
		}
		// Initialize itself rejects every existing, incomplete or unsafe key directory.
		if explicit {
			if enabled {
				return errAPIInit
			}
		} else {
			address, err := s.EngineAddress()
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
				return errAPIInit
			}
		}
		return deviceapi.Initialize(ctx, store.Path, stack.Installation, deviceapi.WrapHost)
	})
	return enabled, err
}

const errEnrollmentAPI deviceapi.Error = "enrollment_requires_device_api"

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
	if accepted.Release().Contract != workloadrelease.DeviceAPIContract {
		if set {
			return "", errEnrollmentAPI
		}
		return "none", nil
	}
	codes, err := deviceapi.EnrollmentCodes(workload.FromInput(accepted).Installation, set)
	if err != nil {
		return "", err
	}
	if !set {
		return "none", codes.Delete()
	}
	return "held", codes.Put(code)
}
func enrollmentState(accepted *configuration.Input) string {
	if accepted.Release().Contract == workloadrelease.DeviceAPIContract {
		if codes, err := deviceapi.EnrollmentCodes(workload.FromInput(accepted).Installation, false); err == nil && codes.Held() {
			return "held"
		}
	}
	return "none"
}

type apiManager struct {
	state        string
	runtime      *deviceapi.Runtime
	installation string
}

func (m *apiManager) ensure(input *configuration.Input) error {
	if input.Release().Contract != workloadrelease.DeviceAPIContract {
		if m.runtime != nil {
			m.runtime.Close()
			m.runtime = nil
		}
		return nil
	}
	installation := workload.FromInput(input).Installation
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
	key, err := deviceapi.LoadCredential(os.Getenv("CREDENTIALS_DIRECTORY"))
	if err != nil {
		return err
	}
	defer clear(key)
	rt, err := deviceapi.Start(m.state, installation, key)
	if err != nil {
		return err
	}
	m.runtime = rt
	m.installation = installation
	return nil
}
func (m *apiManager) bind(ctx context.Context, e *dockerengine.Engine, stack workload.Stack) {
	if m == nil || m.runtime == nil {
		return
	}
	path, err := e.AgentHome(ctx, stack)
	if err != nil {
		m.runtime.Watch.Unbind()
		return
	}
	m.runtime.Watch.Bind(path) // Optional capability; the next cycle retries a lost watch.
}
func (m *apiManager) close() {
	if m.runtime != nil {
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
