package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/deviceapi"
	"github.com/amnezia-gpt/agent-firmware/internal/service"
	"github.com/amnezia-gpt/agent-firmware/internal/workloadrelease"
)

func enableTestRelease(t *testing.T) {
	t.Helper()
	before := servedDeviceAPI
	servedDeviceAPI = workloadrelease.DeviceAPIV3
	t.Cleanup(func() { servedDeviceAPI = before })
}

func TestRemovedKeyPreparationCommandsRejectRequests(t *testing.T) {
	for _, command := range []string{"device-api-init", "prepare-device-api", "configure", "enrollment-code-set", "enrollment-code-clear"} {
		var out, stderr bytes.Buffer
		if code := run([]string{command}, strings.NewReader(""), &out, &stderr); code != 2 || out.Len() != 0 ||
			stderr.String() != "{\"error_code\":\"invalid_usage\"}\n" {
			t.Fatalf("removed command %q: code=%d, stderr=%s", command, code, &stderr)
		}
	}
}

func TestEnrollmentCodeIsNeverAnArgument(t *testing.T) {
	for _, args := range [][]string{
		{"enroll", "--issuer", "https://auth.example", "enrc_" + strings.Repeat("A", 43)},
		{"enroll", "--issuer", "https://auth.example", "--code", "enrc_" + strings.Repeat("A", 43)},
		{"enroll", "--issuer", "https://auth.example", "--enrollment-code-stdin", "--enrollment-code-file", "a"},
	} {
		var out, err bytes.Buffer
		if code := run(args, strings.NewReader(""), &out, &err); code != 2 || out.Len() != 0 || err.String() != `{"error_code":"invalid_usage"}`+"\n" {
			t.Fatalf("unexpected result: %d %s", code, err.String())
		}
	}
}

func useUnsupportedTestTransport(t *testing.T) {
	t.Helper()
	before := servedDeviceAPI
	servedDeviceAPI = "urn:example:unsupported-device-api"
	t.Cleanup(func() { servedDeviceAPI = before })
}

func TestUnsupportedManifestRefusesServeBeforeRuntimeOrEngine(t *testing.T) {
	useUnsupportedTestTransport(t)
	release, err := os.ReadFile("../../internal/workloadrelease/testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	device, err := configuration.ParseDevice(bytes.NewReader(release))
	if err != nil {
		t.Fatal(err)
	}
	store := configuration.Store{Path: filepath.Join(t.TempDir(), "state")}
	if _, err := store.Manufacture(device, nil); err != nil {
		t.Fatal(err)
	}
	// A malformed engine record would be observable if serve reached the engine.
	if err := os.WriteFile(filepath.Join(store.Path, "engine-address"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	manager := &apiManager{}
	defer manager.close()
	if _, err := executeManaged(t.Context(), store, manager); err != workloadrelease.ErrDeviceAPI || manager.handler != nil {
		t.Fatalf("unsupported manifest reached runtime: %v", err)
	}
}

func TestLinkJournalReportsDroppedEventsAfterRunnerStops(t *testing.T) {
	manager := &apiManager{}
	for range 32 {
		manager.recordLink(deviceapi.LinkEvent{Event: "device_api_link", State: "connected"})
	}
	manager.close()
	var output bytes.Buffer
	manager.drain(&output)
	if got := strings.Count(output.String(), `"event":"device_api_link"`); got != 16 {
		t.Fatalf("journal contains %d link events, want 16", got)
	}
	if !strings.Contains(output.String(), `{"event":"device_api_link_events_dropped","dropped_count":16}`) {
		t.Fatalf("lost events were not reported: %s", output.String())
	}
	output.Reset()
	manager.drain(&output)
	if output.Len() != 0 {
		t.Fatal("a previously reported drop counter was emitted again")
	}
}

func TestLinkTransitionPublishesSystemdStatusBeforeJournalFlush(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "af-link-notify-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "notify.sock")
	socket, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	publish, finish := startLinkStatus(service.Notifier{Socket: path})
	defer finish()
	manager := &apiManager{updateLinkStatus: publish}
	manager.recordLink(deviceapi.LinkEvent{Event: "device_api_link", State: "connected", ReconnectCount: 2})
	manager.recordLink(deviceapi.LinkEvent{Event: "device_api_link", State: "reconnecting", ReconnectCount: 2})
	_ = socket.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 128)
	for _, want := range []string{
		"STATUS=Device API link inactive (reconnects 0)",
		"STATUS=Device API link connected (reconnects 2)",
		"STATUS=Device API link reconnecting (reconnects 2)",
	} {
		n, _, err := socket.ReadFromUnix(buf)
		if err != nil || string(buf[:n]) != want {
			t.Fatalf("want %q, got %q, err=%v", want, buf[:n], err)
		}
	}
}
