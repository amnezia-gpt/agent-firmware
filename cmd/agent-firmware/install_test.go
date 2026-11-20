package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/amnezia-gpt/agent-firmware/internal/backendprofile"
	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/host"
)

const manifestFixture = "../../internal/workloadrelease/testdata/manifest.json"
const profileFixture = "../../internal/backendprofile/testdata/profile-v4.json"

func fixture(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestValidateReleaseChecksExplicitBuildTarget(t *testing.T) {
	enableTestRelease(t)
	amd64Only := filepath.Join(t.TempDir(), "amd64.json")
	var manifest map[string]json.RawMessage
	if err := json.Unmarshal(fixture(t, manifestFixture), &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["platforms"] = json.RawMessage(`["linux/amd64"]`)
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(amd64Only, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path, platform, diagnostic string
	}{
		{manifestFixture, "linux/arm64", ""},
		{amd64Only, "linux/amd64", ""},
		{amd64Only, "linux/arm64", "{\"error_code\":\"unsupported_workload_platform\"}\n"},
	} {
		var out, stderr bytes.Buffer
		code := run([]string{"validate-release", "--release", test.path, "--platform", test.platform}, nil, &out, &stderr)
		if (code == 0) != (test.diagnostic == "") || stderr.String() != test.diagnostic {
			t.Errorf("%s on %s: %d %q %q", test.path, test.platform, code, &out, &stderr)
		}
	}
}

func TestInstallCommand(t *testing.T) {
	enableTestRelease(t)
	originalLayout, originalDir, originalDiscover, originalReady, originalPlace := hostLayout, defaultStateDir, discover, hostReady, place
	t.Cleanup(func() {
		hostLayout, defaultStateDir, discover, hostReady, place = originalLayout, originalDir, originalDiscover, originalReady, originalPlace
	})
	hostLayout.UserService = false
	defaultStateDir = filepath.Join(t.TempDir(), "state")
	ready, placed := "", 0
	hostReady = func() string { return ready }
	discover = func(context.Context, string) ([]byte, error) {
		t.Fatal("install contacted an issuer")
		return nil, nil
	}
	place = func(_ context.Context, device *configuration.Device, progress func(string)) error {
		placed++
		if device.Generation() == "" {
			t.Fatal("no device release")
		}
		_, err := (configuration.Store{Path: defaultStateDir}).Manufacture(device, nil)
		progress("step")
		return err
	}
	for _, tc := range []struct {
		name      string
		args      []string
		ready     string
		code      int
		errorCode string
	}{
		{"release only", []string{"--release", manifestFixture}, "", 0, ""},
		{"issuer rejected", []string{"--release", manifestFixture, "--issuer", "https://auth.example"}, "", 2, "invalid_usage"},
		{"code rejected", []string{"--release", manifestFixture, "--enrollment-code-stdin"}, "", 2, "invalid_usage"},
		{"release missing", nil, "", 2, "invalid_usage"},
		{"unreadable release", []string{"--release", manifestFixture + ".missing"}, "", 1, "input_read_failed"},
		{"host unavailable", []string{"--release", manifestFixture}, "root_required", 1, "root_required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ready, placed = tc.ready, 0
			var output, errors bytes.Buffer
			code := run(append([]string{"install"}, tc.args...), unreadableStdin{t}, &output, &errors)
			if code != tc.code {
				t.Fatalf("exit=%d stderr=%q", code, &errors)
			}
			if tc.errorCode != "" {
				want := "{\"error_code\":\"" + tc.errorCode + "\"}\n"
				if tc.code == 2 {
					want = "agent-firmware: run 'agent-firmware help install' for usage\n" + want
				}
				if errors.String() != want || placed != 0 || output.Len() != 0 {
					t.Fatalf("stdout=%q stderr=%q placed=%d", &output, &errors, placed)
				}
			} else if output.String() != "{\"installed\":true}\n" || errors.String() != "agent-firmware: step\n" || placed != 1 {
				t.Fatalf("stdout=%q stderr=%q placed=%d", &output, &errors, placed)
			}
		})
	}
}

func TestMacOSInstallWarnsAboutUserDataBeforePlacingFirmware(t *testing.T) {
	enableTestRelease(t)
	originalLayout, originalDir, originalReady, originalPlace := hostLayout, defaultStateDir, hostReady, place
	t.Cleanup(func() {
		hostLayout, defaultStateDir, hostReady, place = originalLayout, originalDir, originalReady, originalPlace
	})
	hostLayout.UserService = true
	defaultStateDir = filepath.Join(t.TempDir(), "state")
	hostReady = func() string { return "" }
	var stdout, stderr bytes.Buffer
	placed := false
	place = func(context.Context, *configuration.Device, func(string)) error {
		for _, detail := range []string{"Docker Desktop's VM", "untouched by default", "Resetting Docker Desktop",
			"clearing its data", "uninstalling it", "Switching engines makes these volumes unavailable", "cloud Linux machine"} {
			if !strings.Contains(stderr.String(), detail) {
				t.Errorf("missing warning before firmware placement: %q", detail)
			}
		}
		placed = true
		return nil
	}
	if code := run([]string{"install", "--release", manifestFixture}, unreadableStdin{t}, &stdout, &stderr); code != 0 || !placed {
		t.Fatalf("install exit %d, placed %t, stderr %q", code, placed, &stderr)
	}
}

type unreadableStdin struct{ t *testing.T }

func (r unreadableStdin) Read([]byte) (int, error) {
	r.t.Fatal("install read stdin")
	return 0, nil
}

func TestServiceRefreshesMissingScopesOncePerRelease(t *testing.T) {
	profile, manifest := fixture(t, profileFixture), fixture(t, manifestFixture)
	store := configuration.Store{Path: filepath.Join(t.TempDir(), "state")}
	device, err := configuration.ParseDevice(bytes.NewReader(manifest))
	if err != nil {
		t.Fatal(err)
	}
	input, err := device.WithProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Manufacture(device, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Exclusive(func(s *configuration.Session) error { return s.BeginEnrollment(input, false) }); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	original := refreshResources
	t.Cleanup(func() { refreshResources = original })
	refreshes := 0
	refreshResources = func(_ context.Context, profile backendprofile.Profile) ([]byte, error) {
		refreshes++
		profile.Resources = slices.Clone(profile.Resources)
		profile.Resources[0].Scopes = append(slices.Clone(profile.Resources[0].Scopes), "example:next")
		return backendprofile.Encode(profile)
	}
	manager := &apiManager{}
	if _, _, err := manager.stack(t.Context(), snapshot); err != nil || refreshes != 0 {
		t.Fatalf("resolved scopes retried: %v, count=%d", err, refreshes)
	}
	for _, scope := range []string{"example:next", "example:absent"} {
		snapshot.Device, err = configuration.ParseDevice(bytes.NewReader(bytes.ReplaceAll(manifest, []byte("example:use"), []byte(scope))))
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			_, refreshed, err := manager.stack(t.Context(), snapshot)
			if scope == "example:next" && (err != nil || len(refreshed) == 0) || scope == "example:absent" && err != backendprofile.ErrScopeMissing {
				t.Fatalf("scope %s: %v", scope, err)
			}
		}
	}
	if refreshes != 2 {
		t.Fatalf("refresh count=%d", refreshes)
	}
}

func TestReleaseGateRejectsUnavailableTransportBeforeDiscovery(t *testing.T) {
	useUnsupportedTestTransport(t)
	beforeDir, beforeDiscover, beforeReady := defaultStateDir, discover, hostReady
	t.Cleanup(func() { defaultStateDir, discover, hostReady = beforeDir, beforeDiscover, beforeReady })
	defaultStateDir = filepath.Join(t.TempDir(), "state")
	hostReady = func() string { t.Fatal("host check ran for unsupported transport"); return "" }
	discover = func(context.Context, string) ([]byte, error) { t.Fatal("unexpected discovery"); return nil, nil }
	for _, command := range []string{"validate-release", "install"} {
		var out, stderr bytes.Buffer
		if code := run([]string{command, "--release", manifestFixture}, strings.NewReader(""), &out, &stderr); code != 1 ||
			stderr.String() != "{\"error_code\":\"unsupported_device_api\"}\n" || out.Len() != 0 {
			t.Fatalf("release gate: %d %q %q", code, &out, &stderr)
		}
	}
	oldDevice, err := json.Marshal(map[string]any{
		"schema_version": 2, "generation": "legacy",
		"workload_release":   []byte(`{"schema_version":3}`),
		"root_secret_sha256": "obsolete", "idevid_key": "obsolete",
	})
	if err != nil {
		t.Fatal(err)
	}
	const want = `agent-firmware: An earlier firmware installation is on this host. It cannot be updated in place.
agent-firmware: Run this command. It deletes the earlier installation and its data on this
agent-firmware: host; this cannot be undone:
agent-firmware:   sudo sh agent-firmware-install.sh --decommission --confirm --delete-data
agent-firmware: Then run your original installation command again, unchanged.
agent-firmware: If agent-firmware-install.sh is not in this directory, download it first:
agent-firmware:   curl -fsSLo agent-firmware-install.sh https://github.com/amnezia-gpt/agent-firmware/releases/latest/download/agent-firmware-install.sh
{"error_code":"legacy_state_layout"}
`
	for _, layout := range []struct {
		name, file string
		record     []byte
	}{
		{"v0.1.x", host.LegacyState, []byte("old configuration")},
		{"v0.2.0", host.DeviceState, oldDevice},
	} {
		t.Run(layout.name, func(t *testing.T) {
			defaultStateDir = filepath.Join(t.TempDir(), "state")
			if err := os.Mkdir(defaultStateDir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(defaultStateDir, layout.file), layout.record, 0600); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			if code := run([]string{"install", "--release", manifestFixture}, unreadableStdin{t}, &out, &stderr); code != 1 ||
				out.Len() != 0 || stderr.String() != operatorText(want) {
				t.Fatalf("legacy install: %d %q %q", code, &out, &stderr)
			}
		})
	}
}

func TestInstallRetainedVolumeRecoveryHint(t *testing.T) {
	enableTestRelease(t)
	beforeDir, beforeReady, beforePlace := defaultStateDir, hostReady, place
	t.Cleanup(func() { defaultStateDir, hostReady, place = beforeDir, beforeReady, beforePlace })
	defaultStateDir = filepath.Join(t.TempDir(), "state")
	hostReady = func() string { return "" }
	place = func(context.Context, *configuration.Device, func(string)) error {
		return configuration.ErrRetainedVolumes
	}
	const want = `agent-firmware: Enrollment or unclassified firmware volumes are still on this host.
agent-firmware: Run this command. It deletes the firmware volumes on this host;
agent-firmware: this cannot be undone:
agent-firmware:   sudo sh agent-firmware-install.sh --decommission --confirm --delete-data
agent-firmware: Then run your original installation command again, unchanged.
agent-firmware: If agent-firmware-install.sh is not in this directory, download it first:
agent-firmware:   curl -fsSLo agent-firmware-install.sh https://github.com/amnezia-gpt/agent-firmware/releases/latest/download/agent-firmware-install.sh
{"error_code":"retained_firmware_volumes"}
`
	var out, stderr bytes.Buffer
	if code := run([]string{"install", "--release", manifestFixture}, unreadableStdin{t}, &out, &stderr); code != 1 ||
		out.Len() != 0 || !strings.HasSuffix(stderr.String(), operatorText(want)) {
		t.Fatalf("retained volumes: %d %q %q", code, &out, &stderr)
	}
}
