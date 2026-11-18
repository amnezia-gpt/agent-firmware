package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"slices"
	"sync/atomic"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/deviceapi"
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/service"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
)

const errAPIServe deviceapi.Error = "device_api_requires_serve"

var checkWorkloadVolumes = func(ctx context.Context, session *configuration.Session, input *configuration.Input, fresh bool) (bool, error) {
	stack := workload.VolumeStack(input.Release(), input.Profile().AuthIssuer)
	address, err := session.EngineAddress()
	if err != nil {
		return false, err
	}
	engine, err := dockerengine.New(address)
	if err != nil {
		return false, err
	}
	defer engine.Close()
	return engine.CheckEnrollmentVolumes(ctx, stack, fresh)
}

type apiManager struct {
	codes             *deviceapi.CodeStore
	vault             *deviceapi.Vault
	handler           *deviceapi.Handler
	target            dockerengine.ClientTarget
	address           string
	command           []string
	cancel            context.CancelCauseFunc
	done              chan struct{}
	linkEvents        chan deviceapi.LinkEvent
	updateLinkStatus  func(deviceapi.LinkEvent)
	droppedLinkEvents atomic.Uint64
	reportedLinkDrops uint64
	link              deviceapi.LinkRunner
	dial              dockerengine.DialCommand
	status            dockerengine.StatusCommand
	refreshGeneration string
	refreshProfile    []byte
	refreshErr        error
}

func (m *apiManager) ensure(parent context.Context, session *configuration.Session, snapshot configuration.Snapshot,
	address string, target dockerengine.ClientTarget, command []string) error {
	if m.handler != nil && m.target == target && m.address == address && slices.Equal(m.command, command) {
		m.codes.Held()
		return nil
	}
	m.stop(deviceapi.LinkTargetChanged)
	key, err := session.RootSecret(snapshot.Device)
	if err != nil {
		return err
	}
	defer clear(key)
	vault, err := deviceapi.NewVault(target.Installation, key)
	if err != nil {
		return err
	}
	codes, err := deviceapi.EnrollmentCodes(hostLayout, target.Installation, false)
	if err != nil {
		vault.Close()
		return err
	}
	engine, err := dockerengine.New(address)
	if err != nil {
		vault.Close()
		return err
	}
	ctx, cancel := context.WithCancelCause(parent)
	m.vault, m.codes = vault, codes
	m.handler = deviceapi.NewHandler(vault, snapshot.Identity(), codes)
	m.target, m.address, m.command = target, address, slices.Clone(command)
	m.cancel, m.done = cancel, make(chan struct{})
	m.initLinkQueue()
	done, handler, argv := m.done, m.handler, m.command
	go func() {
		defer close(done)
		defer engine.Close()
		m.link.Run(ctx, func(ctx context.Context) (net.Conn, error) {
			return m.dial.Attach(ctx, engine, target, argv)
		}, handler, m.recordLink)
	}()
	return nil
}

func (m *apiManager) close() { m.stop(deviceapi.LinkServiceStopping) }

func (m *apiManager) stop(reason deviceapi.LinkReason) {
	if m == nil || m.cancel == nil {
		return
	}
	m.cancel(reason)
	<-m.done
	m.vault.Close()
	m.cancel, m.vault, m.handler, m.codes = nil, nil, nil, nil
}

const linkQueueSize = 16

func (m *apiManager) initLinkQueue() {
	if m.linkEvents == nil {
		m.linkEvents = make(chan deviceapi.LinkEvent, linkQueueSize)
	}
}

func (m *apiManager) recordLink(event deviceapi.LinkEvent) {
	if m.updateLinkStatus != nil {
		m.updateLinkStatus(event)
	}
	m.initLinkQueue()
	select {
	case m.linkEvents <- event:
	default:
		m.droppedLinkEvents.Add(1)
	}
}

func (m *apiManager) drain(out io.Writer) {
	m.drainLink(out)
	if m.handler == nil {
		return
	}
	for range 32 {
		select {
		case event := <-m.handler.Events():
			writeServiceRecord(out, event)
		default:
			return
		}
	}
}

func (m *apiManager) drainLink(out io.Writer) {
	for range len(m.linkEvents) {
		if !writeServiceRecord(out, <-m.linkEvents) {
			m.droppedLinkEvents.Add(1)
		}
	}
	drops := m.droppedLinkEvents.Load()
	if drops != m.reportedLinkDrops && writeServiceRecord(out, struct {
		Event        string `json:"event"`
		DroppedCount uint64 `json:"dropped_count"`
	}{"device_api_link_events_dropped", drops}) {
		m.reportedLinkDrops = drops
	}
}

// Keep sd_notify on a separate worker. systemctl displays the latest line,
// so a full queue can replace its oldest status without delaying the link.
func startLinkStatus(notifier service.Notifier) (func(deviceapi.LinkEvent), func()) {
	if notifier.Socket == "" {
		return nil, func() {}
	}
	pending := make(chan deviceapi.LinkEvent, linkQueueSize)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for event := range pending {
			line := fmt.Sprintf("Device API link %s (reconnects %d)", event.State, event.ReconnectCount)
			_ = notifier.Status(line)
		}
	}()
	publish := func(event deviceapi.LinkEvent) {
		select {
		case pending <- event:
			return
		default:
		}
		select {
		case <-pending:
		default:
		}
		pending <- event
	}
	publish(deviceapi.LinkEvent{State: "inactive"})
	return publish, func() {
		close(pending)
		<-done
	}
}

func writeServiceRecord(out io.Writer, value any) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	data = append(data, '\n')
	n, err := out.Write(data)
	return err == nil && n == len(data)
}
