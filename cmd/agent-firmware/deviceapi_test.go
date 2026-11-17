package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
)

func TestEnrollmentCodeCommandsCheckRootAndExistingConfiguration(t *testing.T) {
	for _, cmd := range []string{"enrollment-code-set", "enrollment-code-clear"} {
		var out, err bytes.Buffer
		input := ""
		if cmd == "enrollment-code-set" {
			input = "enrc_" + strings.Repeat("A", 43)
		}
		code := run([]string{cmd, "--state-dir", filepath.Join(t.TempDir(), "absent")}, strings.NewReader(input), &out, &err)
		want := "not_configured"
		if os.Geteuid() != 0 {
			want = "root_required"
		}
		if code != 1 || out.Len() != 0 || err.String() != `{"error_code":"`+want+`"}`+"\n" {
			t.Fatalf("unexpected key command result: %d %s", code, err.String())
		}
	}
}

func TestRemovedKeyPreparationCommandsRejectRequests(t *testing.T) {
	for _, command := range []string{"device-api-init", "prepare-device-api"} {
		var out, stderr bytes.Buffer
		if code := run([]string{command}, strings.NewReader(""), &out, &stderr); code != 2 || out.Len() != 0 ||
			stderr.String() != "{\"error_code\":\"invalid_usage\"}\n" {
			t.Fatalf("removed command %q: code=%d, stderr=%s", command, code, &stderr)
		}
	}
}

func TestEnrollmentCodeIsNeverAnArgument(t *testing.T) {
	for _, args := range [][]string{
		{"enrollment-code-set", "enrc_" + strings.Repeat("A", 43)},
		{"enrollment-code-set", "--code", "enrc_" + strings.Repeat("A", 43)},
		{"enrollment-code-clear", "--profile", "a"},
	} {
		var out, err bytes.Buffer
		if code := run(args, strings.NewReader(""), &out, &err); code != 2 || out.Len() != 0 || err.String() != `{"error_code":"invalid_usage"}`+"\n" {
			t.Fatalf("unexpected result: %d %s", code, err.String())
		}
	}
}

// Opt-in: writes below /run/agent-firmware, so it runs only as root in a
// disposable container prepared for this check.
func TestEnrollmentCodeCommandsOnDisposableRootHost(t *testing.T) {
	if os.Getenv("AF_ENROLLMENT_ROOT_TEST") != "1" || os.Geteuid() != 0 {
		t.Skip("disposable root host not selected")
	}
	if _, err := os.Stat("/.dockerenv"); err != nil {
		t.Skip("not a container")
	}
	original := checkFreshWorkloadVolumes
	checkFreshWorkloadVolumes = func(context.Context, *configuration.Session, *configuration.Input) error { return nil }
	t.Cleanup(func() { checkFreshWorkloadVolumes = original })
	state := filepath.Join(t.TempDir(), "state")
	manifest, err := os.ReadFile("../../internal/workloadrelease/testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(stdin string, args ...string) (int, string, string) {
		var out, errs bytes.Buffer
		code := run(append(args, "--state-dir", state), strings.NewReader(stdin), &out, &errs)
		return code, out.String(), errs.String()
	}
	configure := func() {
		release := filepath.Join(t.TempDir(), "release.json")
		if err := os.WriteFile(release, manifest, 0600); err != nil {
			t.Fatal(err)
		}
		if code, _, errs := invoke("", "configure", "--profile", "../../internal/backendprofile/testdata/profile-v3.json", "--release", release); code != 0 {
			t.Fatal("configure:", errs)
		}
	}
	example := "enrc_" + strings.Repeat("A", 43)
	configure()
	if code, _, errs := invoke("short\n", "enrollment-code-set"); code != 1 || errs != `{"error_code":"invalid_enrollment_code"}`+"\n" {
		t.Fatal("invalid code:", errs)
	}
	if _, out, _ := invoke("", "status"); !strings.Contains(out, `"enrollment_code":"none"`) {
		t.Fatal("status before:", out)
	}
	code, out, errs := invoke(example+"\n", "enrollment-code-set")
	if code != 0 || out != `{"enrollment_code":"held"}`+"\n" || strings.Contains(out+errs, example) {
		t.Fatal("set:", out, errs)
	}
	matches, _ := filepath.Glob("/run/agent-firmware/*/enrollment-code")
	if len(matches) != 1 {
		t.Fatal("code not held in the runtime directory")
	}
	for _, path := range []string{"/run/agent-firmware", filepath.Dir(matches[0])} {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0700 {
			t.Fatal("runtime directory is not root-only")
		}
	}
	// Reconfiguring the same installation neither needs nor spends the code.
	configure()
	if _, out, _ := invoke("", "status"); !strings.Contains(out, `"enrollment_code":"held"`) || strings.Contains(out, example) {
		t.Fatal("status while held:", out)
	}
	filepath.WalkDir(state, func(path string, d os.DirEntry, _ error) error {
		if b, err := os.ReadFile(path); err == nil && bytes.Contains(b, []byte(example)) {
			t.Fatal("code in retained state")
		}
		return nil
	})
	if code, out, _ := invoke("", "enrollment-code-clear"); code != 0 || out != `{"enrollment_code":"none"}`+"\n" {
		t.Fatal("clear:", out)
	}
	if _, err := os.Lstat(matches[0]); !os.IsNotExist(err) {
		t.Fatal("code survived clear")
	}
}

func TestServeClosesDeviceAPIWhenRootSecretDisappears(t *testing.T) {
	if os.Getenv("AF_ENROLLMENT_ROOT_TEST") != "1" || os.Geteuid() != 0 {
		t.Skip("disposable root container required")
	}
	if _, err := os.Stat("/.dockerenv"); err != nil {
		t.Skip("not a container")
	}
	profile, err := os.ReadFile("../../internal/backendprofile/testdata/profile-v3.json")
	if err != nil {
		t.Fatal(err)
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	profileID := `"root-check-` + hex.EncodeToString(nonce[:]) + `"`
	profile = bytes.Replace(profile, []byte(`"example"`), []byte(profileID), 1)
	release, err := os.ReadFile("../../internal/workloadrelease/testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	input, err := configuration.Parse(bytes.NewReader(profile), bytes.NewReader(release))
	if err != nil {
		t.Fatal(err)
	}
	runtime := hostLayout.InstallationRuntime(workload.FromInput(input).Installation)
	if _, err := os.Lstat(runtime); !os.IsNotExist(err) {
		t.Fatalf("test runtime path already exists: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	store := configuration.Store{Path: filepath.Join(t.TempDir(), "state")}
	if _, err := store.Save(input); err != nil {
		t.Fatal(err)
	}
	// The engine socket is intentionally absent. The runtime starts before
	// convergence and the test never reaches the host engine.
	if err := os.WriteFile(filepath.Join(store.Path, "engine-address"), []byte("/tmp/af-no-engine-test.sock\n"), 0600); err != nil {
		t.Fatal(err)
	}
	manager := &apiManager{}
	defer manager.close()
	if _, err := executeManaged(t.Context(), store, manager); err == nil || manager.runtime == nil || !manager.runtime.Running() {
		t.Fatalf("serve did not start the local API before the bounded engine failure: %v", err)
	}
	second := &apiManager{}
	defer second.close()
	if _, err := executeManaged(t.Context(), store, second); err == nil || second.runtime != nil || !manager.runtime.Running() {
		t.Fatal("second service replaced the live listener")
	}
	if err := os.Remove(filepath.Join(store.Path, "root-secret")); err != nil {
		t.Fatal(err)
	}
	if _, err := executeManaged(t.Context(), store, manager); err != configuration.ErrRootSecret || manager.runtime != nil {
		t.Fatalf("serve kept serving with unavailable custody: %v", err)
	}
}
