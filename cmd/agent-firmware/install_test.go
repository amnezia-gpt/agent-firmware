package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amnezia-gpt/agent-firmware/internal/backendprofile"
	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/installer"
)

const installCode = "enrc_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopq"

func TestInstallCommand(t *testing.T) {
	enableTestRelease(t)
	state := defaultStateDir
	defaultStateDir = filepath.Join(t.TempDir(), "state")
	t.Cleanup(func() { defaultStateDir = state })
	profile, err := os.ReadFile("../../internal/backendprofile/testdata/profile-v4.json")
	if err != nil {
		t.Fatal(err)
	}
	release := "../../internal/workloadrelease/testdata/manifest.json"
	codeFile := filepath.Join(t.TempDir(), "code")
	if err := os.WriteFile(codeFile, []byte(installCode+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalDiscover, originalReady, originalPlace := discover, hostReady, place
	defer func() { discover, hostReady, place = originalDiscover, originalReady, originalPlace }()
	ready, placed, held, asked := "", 0, "", 0
	hostReady = func() string { return ready }
	discover = func(_ context.Context, issuer string) ([]byte, error) {
		asked++
		if issuer != "https://auth.example" {
			return nil, installer.Error("invalid_issuer")
		}
		return profile, nil
	}
	place = func(_ context.Context, accepted *configuration.Input, code string, progress func(string)) error {
		progress("step")
		placed++
		held = code
		if accepted.Profile().AuthIssuer != "https://auth.example" {
			t.Fatal("profile not passed")
		}
		return nil
	}
	base := []string{"--issuer", "https://auth.example", "--release", release}
	with := func(extra ...string) []string { return append(append([]string{"install"}, base...), extra...) }
	for _, test := range []struct {
		name           string
		args           []string
		input          string
		ready          string
		code           int
		stdout, stderr string
		placed         int
		held           string
	}{
		{"without code", with(), "", "", 0, "{\"installed\":true}\n", "", 1, ""},
		{"code on standard input", with("--enrollment-code-stdin"), installCode, "", 0, "{\"installed\":true}\n", "", 1, installCode},
		{"code from a file", with("--enrollment-code-file", codeFile), "", "", 0, "{\"installed\":true}\n", "", 1, installCode},
		{"dry run changes nothing", with("--dry-run"), "", "", 0, "{\"valid\":true}\n", "", 0, ""},
		{"code out of bounds", with("--enrollment-code-stdin"), "enrc_short", "", 1, "", "{\"error_code\":\"invalid_enrollment_code\"}\n", 0, ""},
		{"over-long code is refused, not truncated", with("--enrollment-code-stdin"), strings.Repeat("A", 400), "", 1, "", "{\"error_code\":\"invalid_enrollment_code\"}\n", 0, ""},
		{"no code on standard input", with("--enrollment-code-stdin"), "", "", 1, "", "{\"error_code\":\"invalid_enrollment_code\"}\n", 0, ""},
		{"unreadable code file", with("--enrollment-code-file", codeFile+".missing"), "", "", 1, "", "{\"error_code\":\"input_read_failed\"}\n", 0, ""},
		{"host not ready", with(), "", "root_required", 1, "", "{\"error_code\":\"root_required\"}\n", 0, ""},
		{"issuer refused", []string{"install", "--issuer", "http://auth.example", "--release", release}, "", "", 1, "", "{\"error_code\":\"invalid_issuer\"}\n", 0, ""},
		{"unreadable release", []string{"install", "--issuer", "https://auth.example", "--release", release + ".missing"}, "", "", 1, "", "{\"error_code\":\"input_read_failed\"}\n", 0, ""},
		{"invalid release", []string{"install", "--issuer", "https://auth.example", "--release", codeFile}, "", "", 1, "", "{\"error_code\":\"invalid_workload_release\"}\n", 0, ""},
		{"code as an argument", with("--enrollment-code", installCode), "", "", 2, "", "{\"error_code\":\"invalid_usage\"}\n", 0, ""},
		{"two code sources", with("--enrollment-code-stdin", "--enrollment-code-file", codeFile), "", "", 2, "", "{\"error_code\":\"invalid_usage\"}\n", 0, ""},
		{"dry run takes no code", with("--dry-run", "--enrollment-code-stdin"), installCode, "", 2, "", "{\"error_code\":\"invalid_usage\"}\n", 0, ""},
		{"issuer missing", []string{"install", "--release", release}, "", "", 2, "", "{\"error_code\":\"invalid_usage\"}\n", 0, ""},
		{"release missing", []string{"install", "--issuer", "https://auth.example"}, "", "", 2, "", "{\"error_code\":\"invalid_usage\"}\n", 0, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			ready, placed, held = test.ready, 0, ""
			var stdout, stderr bytes.Buffer
			code := run(test.args, strings.NewReader(test.input), &stdout, &stderr)
			// Progress lines precede the result on standard error; an error is always its last line.
			lines := stderr.String()
			if test.stderr != "" {
				if !strings.HasSuffix(lines, test.stderr) {
					t.Fatalf("stderr=%q", lines)
				}
				lines = test.stderr
			} else if strings.Contains(lines, "error_code") || test.code == 0 && test.stdout == "{\"installed\":true}\n" && !strings.Contains(lines, "agent-firmware: step\n") {
				t.Fatalf("stderr=%q", lines)
			} else {
				lines = ""
			}
			if test.stdout == "{\"valid\":true}\n" && stderr.Len() != 0 {
				t.Fatalf("a dry run printed progress: %q", stderr.String())
			}
			if code != test.code || stdout.String() != test.stdout || lines != test.stderr || placed != test.placed || held != test.held {
				t.Fatalf("exit=%d stdout=%q stderr=%q placed=%d", code, stdout.String(), stderr.String(), placed)
			}
			if strings.Contains(stdout.String()+stderr.String(), installCode) {
				t.Fatal("code in output")
			}
		})
	}
	// An invalid code stops before any network request.
	asked = 0
	var out bytes.Buffer
	run(with("--enrollment-code-stdin"), strings.NewReader("x"), &out, &out)
	if asked != 0 {
		t.Fatal("discovery ran for an invalid code")
	}
}

func TestPrepareInstallRefreshesMissingScopesOnce(t *testing.T) {
	profileBytes, err := os.ReadFile("../../internal/backendprofile/testdata/profile-v4.json")
	if err != nil {
		t.Fatal(err)
	}
	releaseBytes, err := os.ReadFile("../../internal/workloadrelease/testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	input, err := configuration.Parse(bytes.NewReader(profileBytes), bytes.NewReader(releaseBytes))
	if err != nil {
		t.Fatal(err)
	}
	store := configuration.Store{Path: filepath.Join(t.TempDir(), "state")}
	if _, err := store.Save(input); err != nil {
		t.Fatal(err)
	}

	originalRefresh := refreshResources
	t.Cleanup(func() { refreshResources = originalRefresh })
	refreshes := 0
	refreshResources = func(_ context.Context, profile backendprofile.Profile) ([]byte, error) {
		refreshes++
		profile.Resources[0].Scopes = append(profile.Resources[0].Scopes, "example:next")
		return backendprofile.Encode(profile)
	}
	issuer := input.Profile().AuthIssuer
	if _, err := prepareInstallInput(t.Context(), store, issuer, releaseBytes); err != nil || refreshes != 0 {
		t.Fatalf("resolved input refreshed resources: %v (%d)", err, refreshes)
	}
	next := bytes.ReplaceAll(releaseBytes, []byte("example:use"), []byte("example:next"))
	refreshed, err := prepareInstallInput(t.Context(), store, issuer, next)
	if err != nil || refreshed.Profile().IngressTrustBundle != input.Profile().IngressTrustBundle || refreshes != 1 {
		t.Fatalf("missing scope refresh failed: %v (%d)", err, refreshes)
	}
	missing := bytes.ReplaceAll(releaseBytes, []byte("example:use"), []byte("example:absent"))
	if _, err := prepareInstallInput(t.Context(), store, issuer, missing); err != backendprofile.ErrScopeMissing || refreshes != 2 {
		t.Fatalf("unresolved input retried or accepted: %v (%d)", err, refreshes)
	}
}

func TestReleaseGateRejectsUnavailableTransportBeforeDiscovery(t *testing.T) {
	path := "../../internal/workloadrelease/testdata/manifest.json"
	before := hostReady
	hostReady = func() string { return "" }
	t.Cleanup(func() { hostReady = before })
	original := discover
	discover = func(context.Context, string) ([]byte, error) {
		t.Fatal("discovery attempted for unavailable transport")
		return nil, nil
	}
	t.Cleanup(func() { discover = original })
	for _, args := range [][]string{
		{"validate-release", "--release", path},
		{"install", "--dry-run", "--issuer", "https://auth.example", "--release", path},
	} {
		var out, stderr bytes.Buffer
		if code := run(args, strings.NewReader(""), &out, &stderr); code != 1 ||
			stderr.String() != "{\"error_code\":\"unsupported_device_api\"}\n" || out.Len() != 0 {
			t.Fatalf("release gate: %d %q %q", code, &out, &stderr)
		}
	}
}
