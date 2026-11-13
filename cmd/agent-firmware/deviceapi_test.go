package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPIKeyCommandsCheckRootAndExistingConfiguration(t *testing.T) {
	for _, cmd := range []string{"device-api-init", "prepare-device-api", "enrollment-code-set", "enrollment-code-clear"} {
		var out, err bytes.Buffer
		code := run([]string{cmd, "--state-dir", filepath.Join(t.TempDir(), "absent")}, strings.NewReader(""), &out, &err)
		want := "not_configured"
		if os.Geteuid() != 0 {
			want = "root_required"
		}
		if code != 1 || out.Len() != 0 || err.String() != `{"error_code":"`+want+`"}`+"\n" {
			t.Fatalf("unexpected key command result: %d %s", code, err.String())
		}
	}
}

func TestEnrollmentCodeIsNeverAnArgument(t *testing.T) {
	for _, args := range [][]string{
		{"enrollment-code-set", "agpt_enr_" + strings.Repeat("A", 43)},
		{"enrollment-code-set", "--code", "agpt_enr_" + strings.Repeat("A", 43)},
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
	configure := func(contract string) {
		release := filepath.Join(t.TempDir(), "release.json")
		os.WriteFile(release, bytes.Replace(manifest, []byte("device:v1"), []byte(contract), 1), 0600)
		if code, _, errs := invoke("", "configure", "--profile", "../../internal/backendprofile/testdata/profile-v3.json", "--release", release); code != 0 {
			t.Fatal("configure:", errs)
		}
	}
	example := "agpt_enr_" + strings.Repeat("A", 43)
	configure("device:v1")
	if code, _, errs := invoke(example+"\n", "enrollment-code-set"); code != 1 || errs != `{"error_code":"enrollment_requires_device_api"}`+"\n" {
		t.Fatal("code accepted without the Device API:", errs)
	}
	configure("device:v2")
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
	configure("device:v2")
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
