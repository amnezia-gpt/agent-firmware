package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
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

func TestUnsupportedManifestRefusesServeBeforeRuntimeOrEngine(t *testing.T) {
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
	if _, err := executeManaged(t.Context(), store, manager); err != workloadrelease.ErrDeviceAPI || manager.runtime != nil {
		t.Fatalf("unsupported manifest reached runtime: %v", err)
	}
}
