package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/installer"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	"github.com/amnezia-gpt/agent-firmware/internal/workloadrelease"
)

func TestDecommissionRequiresExplicitConfirmation(t *testing.T) {
	var diagnostics bytes.Buffer
	if code := run([]string{"decommission"}, nil, io.Discard, &diagnostics); code != 2 ||
		!strings.Contains(diagnostics.String(), "local data") ||
		!strings.Contains(diagnostics.String(), "confirmation_required") {
		t.Fatalf("exit %d, diagnostics %q", code, &diagnostics)
	}
}

type decommissionTestServices struct{ installer.ServiceManager }

func (decommissionTestServices) Fragment(context.Context) (string, error) { return "", nil }

type decommissionTestEngine struct {
	installation string
	globalPurges int
	purged       bool
	outage       bool
	interrupted  bool
	blocked      bool
	calls        []string
	images       []string
}

func (e *decommissionTestEngine) CheckRemoval(_ context.Context, installation string) error {
	e.calls = append(e.calls, "check")
	if e.blocked || installation != e.installation {
		return workload.ErrConflict
	}
	return nil
}
func (e *decommissionTestEngine) Deregister(_ context.Context, stack workload.Stack) (string, error) {
	e.calls = append(e.calls, "deregister")
	if e.outage || stack.Installation != e.installation || len(stack.Client.Deregister) == 0 {
		return "", errors.New("test domain unavailable")
	}
	return configuration.Deregistered, nil
}
func (e *decommissionTestEngine) RemoveEnrollment(_ context.Context, installation string, _ bool) error {
	e.calls = append(e.calls, "remove")
	if installation != e.installation || e.interrupted {
		return workload.ErrEngine
	}
	return nil
}
func (e *decommissionTestEngine) RemoveReleaseImages(_ context.Context, images []string) error {
	e.calls = append(e.calls, "images")
	e.images = slices.Clone(images)
	return nil
}
func (*decommissionTestEngine) Close() {}
func (e *decommissionTestEngine) OwnedInstallations(context.Context) ([]string, error) {
	return []string{e.installation}, nil
}
func (*decommissionTestEngine) CheckAllVolumeRemoval(context.Context, []string) error { return nil }
func (e *decommissionTestEngine) RemoveOwnedVolumes(_ context.Context, id string, deleteData bool) error {
	if id != "" || !deleteData {
		return workload.ErrConflict
	}
	e.globalPurges++
	e.purged = true
	return nil
}
func (e *decommissionTestEngine) RetainedData(context.Context, string) ([]dockerengine.VolumeInfo, error) {
	if e.installation == "" || e.purged {
		return nil, nil
	}
	return []dockerengine.VolumeInfo{{Name: "af-" + e.installation + "-files", Issuer: "https://auth.example"}}, nil
}

func decommissionTestHost(t *testing.T) (host.Layout, configuration.Store) {
	t.Helper()
	dir := t.TempDir()
	layout := host.Layout{
		Base: filepath.Join(dir, "bin"), Wrapper: filepath.Join(dir, "wrapper"), Unit: filepath.Join(dir, "unit"),
		State: filepath.Join(dir, "state"), Runtime: filepath.Join(dir, "runtime"), Lock: filepath.Join(dir, "lock"),
		UID: uint32(os.Geteuid()), GID: uint32(os.Getegid()),
	}
	return layout, configuration.Store{Path: layout.State}
}

func testDecommission(t *testing.T, layout host.Layout, engine *decommissionTestEngine) (string, error) {
	t.Helper()
	result, err := decommissionDevice(t.Context(), layout, decommissionTestServices{},
		func(string) (decommissionEngine, error) { return engine, nil }, false, func(string) {})
	return result.Deregistration, err
}

func TestDecommissionManufacturedDeviceRemovesReleaseImagesAndRuntime(t *testing.T) {
	layout, store := decommissionTestHost(t)
	device, err := configuration.ParseDevice(bytes.NewReader(fixture(t, manifestFixture)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Manufacture(device, nil); err != nil {
		t.Fatal(err)
	}
	// Unenroll can leave an empty runtime directory for its former domain.
	if err := os.MkdirAll(layout.InstallationRuntime(strings.Repeat("a", 32)), 0700); err != nil {
		t.Fatal(err)
	}
	engine := &decommissionTestEngine{}
	got, err := testDecommission(t, layout, engine)
	if err != nil || got != configuration.NoDeregistration || !slices.Equal(engine.calls, []string{"images"}) ||
		len(engine.images) == 0 {
		t.Fatalf("manufactured got %q, %v, calls %v, images %v", got, err, engine.calls, engine.images)
	}
	for _, path := range []string{layout.State, layout.Runtime} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("local state remains: %v", err)
		}
	}
}

func TestDecommissionResumesOfflineRemovalAndManufacturesNewIdentity(t *testing.T) {
	stubStatusData(t)
	layout, store := decommissionTestHost(t)
	device, err := configuration.ParseDevice(bytes.NewReader(fixture(t, manifestFixture)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Manufacture(device, nil); err != nil {
		t.Fatal(err)
	}
	first, err := store.LoadDevice()
	if err != nil {
		t.Fatal(err)
	}
	input, err := device.WithProfile(fixture(t, profileFixture))
	if err != nil {
		t.Fatal(err)
	}
	prepare := func(s *configuration.Session) error {
		if err := s.BeginEnrollment(input, false); err != nil {
			return err
		}
		return s.SetOutcome(input.Profile().AuthIssuer, configuration.Enrolled, false)
	}
	if err := store.Exclusive(prepare); err != nil {
		t.Fatal(err)
	}
	engine := &decommissionTestEngine{installation: workload.EnrollmentID(input.Profile().AuthIssuer), outage: true, blocked: true}
	if _, err := testDecommission(t, layout, engine); err != workload.ErrConflict || !slices.Equal(engine.calls, []string{"check"}) {
		t.Fatalf("ownership preflight got %v, calls %v", err, engine.calls)
	}
	if _, err := store.Snapshot(); err != nil {
		t.Fatalf("preflight changed the device: %v", err)
	}
	engine.blocked, engine.interrupted = false, true
	if _, err := testDecommission(t, layout, engine); err != workload.ErrEngine {
		t.Fatalf("interrupted got %v", err)
	}
	if _, err := store.Snapshot(); err != configuration.ErrDecommissionPending {
		t.Fatalf("lifecycle gate got %v", err)
	}
	engine.interrupted = false
	got, err := testDecommission(t, layout, engine)
	want := []string{"check", "check", "deregister", "remove", "check", "remove", "images"}
	if err != nil || got != configuration.DeregistrationUnavailable || !slices.Equal(engine.calls, want) || len(engine.images) == 0 {
		t.Fatalf("resume got %q, %v, calls %v, images %v", got, err, engine.calls, engine.images)
	}
	if _, err := os.Lstat(layout.State); !os.IsNotExist(err) {
		t.Fatalf("state directory remains: %v", err)
	}
	var status bytes.Buffer
	if code := showStatus(store, &status, io.Discard); code != 0 || !strings.HasPrefix(status.String(), "Device not installed\n") {
		t.Fatalf("status after removal got %d %q", code, status.String())
	}
	if _, err := testDecommission(t, layout, engine); err != nil || !slices.Equal(engine.calls, want) {
		t.Fatalf("no-op got %v, calls %v", err, engine.calls)
	}
	if _, err := store.Manufacture(device, nil); err != nil {
		t.Fatal(err)
	}
	second, err := store.LoadDevice()
	if err != nil || second.Thumbprint() == first.Thumbprint() {
		t.Fatalf("manufacture after removal did not produce a new identity: %v", err)
	}
	if err := store.Exclusive(prepare); err != nil {
		t.Fatalf("could not reenroll: %v", err)
	}
}

func TestDecommissionLegacyOnlyRemovesLocally(t *testing.T) {
	layout, _ := decommissionTestHost(t)
	if err := os.Mkdir(layout.State, 0700); err != nil {
		t.Fatal(err)
	}
	profile := []byte(`{"schema_version":3,"backend_profile":"lab","auth_issuer":"https://AUTH.example.test/","router_base_url":"https://router.example.test/v1","runtime_gateway_base_url":"https://runtime.example.test","ingress_trust_bundle":"","ingress_trust_sha256":""}`)
	first := workloadrelease.Image{Repository: "example.test/workloads/gateway", Digest: "sha256:" + strings.Repeat("a", 64)}
	second := workloadrelease.Image{Repository: "example.test/workloads/service", Digest: "sha256:" + strings.Repeat("b", 64)}
	manifest, err := json.Marshal(map[string]any{"schema_version": 2, "images": map[string]any{
		"gateway": map[string]string{"repository": first.Repository, "digest": first.Digest, "reference": first.Reference()},
		"service": map[string]string{"repository": second.Repository, "digest": second.Digest, "reference": second.Reference()},
	}})
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := json.Marshal(map[string]any{"schema_version": 1, "generation": "legacy", "backend_profile": profile, "workload_release": manifest})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(layout.State, host.LegacyState), accepted, 0600); err != nil {
		t.Fatal(err)
	}
	legacyAPI := filepath.Join(layout.State, "device-api")
	if err := os.Mkdir(legacyAPI, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"key.cred", "state.json"} {
		if err := os.WriteFile(filepath.Join(legacyAPI, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	engine := &decommissionTestEngine{installation: "7f2097e2753d88bc0f3448d1178eeb74"}
	lock := filepath.Join(legacyAPI, "api.lock")
	if err := os.Symlink("state.json", lock); err != nil {
		t.Fatal(err)
	}
	if _, err := testDecommission(t, layout, engine); err != configuration.ErrUnsafe || len(engine.calls) != 0 {
		t.Fatalf("unsafe legacy lock reached removal: %v, calls %v", err, engine.calls)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, nil, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := testDecommission(t, layout, engine)
	slices.Sort(engine.images)
	if err != nil || got != configuration.LegacyDeregistration || !slices.Equal(engine.calls, []string{"check", "remove", "images"}) ||
		!slices.Equal(engine.images, []string{first.Reference(), second.Reference()}) {
		t.Fatalf("legacy got %q, %v, calls %v, images %v", got, err, engine.calls, engine.images)
	}
	if _, err := os.Lstat(layout.State); !os.IsNotExist(err) {
		t.Fatalf("legacy state remains after decommission: %v", err)
	}
}

func TestDecommissionUnsafeStorageHint(t *testing.T) {
	const want = `agent-firmware: The state directory has an unexpected entry, owner or mode. Inspect its metadata:
agent-firmware:   sudo find -P /var/lib/agent-firmware -xdev -maxdepth 2 -printf '%y %m %U:%G links=%n %p\n'
{"error_code":"unsafe_state_storage"}
`
	var stderr bytes.Buffer
	if code := reportDecommissionError(&stderr, configuration.ErrUnsafe); code != 1 || stderr.String() != want {
		t.Fatalf("unexpected diagnostic: %d %q", code, stderr.String())
	}
}

func TestDecommissionSchema3AndLaterPurgeWithoutState(t *testing.T) {
	layout, store := decommissionTestHost(t)
	if err := os.Mkdir(layout.State, 0700); err != nil {
		t.Fatal(err)
	}
	record, err := json.Marshal(map[string]any{
		"schema_version": 2, "generation": "legacy", "workload_release": []byte(`{"schema_version":3}`),
		"root_secret_sha256": "obsolete", "idevid_key": "obsolete",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(layout.State, host.DeviceState), record, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.CheckLegacy(); err != configuration.ErrLegacy {
		t.Fatalf("install preflight: %v", err)
	}
	if _, err := store.Snapshot(); err != configuration.ErrLegacy {
		t.Fatalf("loaded obsolete state as a current device: %v", err)
	}
	engine := &decommissionTestEngine{installation: workload.EnrollmentID("https://auth.example")}
	run := func(purge bool) (removalResult, error) {
		return decommissionDevice(t.Context(), layout, decommissionTestServices{}, func(string) (decommissionEngine, error) { return engine, nil }, purge, func(string) {})
	}
	engine.interrupted = true
	if _, err := run(false); err != workload.ErrEngine {
		t.Fatalf("expected interrupted cleanup: %v", err)
	}
	if _, err := run(true); err != configuration.ErrRemovalIntent || engine.globalPurges != 0 {
		t.Fatalf("a retry widened cleanup: %v", err)
	}
	engine.interrupted = false
	first, err := run(false)
	if err != nil || first.Deregistration != configuration.LegacyDeregistration || len(first.KeptVolumes) != 1 || len(engine.images) != 0 {
		t.Fatalf("offline cleanup: %+v %v", first, err)
	}
	if _, err := os.Stat(layout.State); !os.IsNotExist(err) {
		t.Fatalf("local state remained: %v", err)
	}
	final, err := run(true)
	if err != nil || len(final.KeptVolumes) != 0 || engine.globalPurges != 1 {
		t.Fatalf("purge without firmware: %+v %v", final, err)
	}
}
