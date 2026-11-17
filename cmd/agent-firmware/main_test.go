package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amnezia-gpt/agent-firmware/internal/discovery"
)

func TestValidateProfileCommand(t *testing.T) {
	profile, err := os.ReadFile("../../internal/backendprofile/testdata/profile-v4.json")
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"validate-profile"}, bytes.NewReader(profile), &stdout, &stderr)
	if code != 0 || stdout.String() != "{\"valid\":true}\n" || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCommandErrorsAreBoundedAndRedacted(t *testing.T) {
	for _, test := range []struct {
		name   string
		args   []string
		input  io.Reader
		code   int
		stderr string
	}{
		{"invalid json", []string{"validate-profile"}, strings.NewReader(`{"sensitive":"example"}`), 1, "{\"error_code\":\"invalid_profile_json\"}\n"},
		{"read failure", []string{"validate-profile"}, failedInput{}, 1, "{\"error_code\":\"profile_read_failed\"}\n"},
		{"too large", []string{"validate-profile"}, strings.NewReader(strings.Repeat(" ", 32*1024+1)), 1, "{\"error_code\":\"profile_too_large\"}\n"},
		{"no command", nil, strings.NewReader(""), 2, "{\"error_code\":\"invalid_usage\"}\n"},
		{"unknown command", []string{"sensitive-example"}, strings.NewReader(""), 2, "{\"error_code\":\"invalid_usage\"}\n"},
		{"extra argument", []string{"validate-profile", "sensitive-example"}, strings.NewReader(""), 2, "{\"error_code\":\"invalid_usage\"}\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(test.args, test.input, &stdout, &stderr)
			if code != test.code || stdout.Len() != 0 || stderr.String() != test.stderr {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestDiscoverProfileCommand(t *testing.T) {
	original := discover
	defer func() { discover = original }()
	discover = func(_ context.Context, issuer string) ([]byte, error) {
		if issuer != "https://auth.example" {
			return nil, discovery.ErrIssuer
		}
		return []byte("{\"schema_version\":3}\n"), nil
	}
	for _, test := range []struct {
		args           []string
		code           int
		stdout, stderr string
	}{
		{[]string{"discover-profile", "--issuer", "https://auth.example"}, 0, "{\"schema_version\":3}\n", ""},
		{[]string{"discover-profile", "--issuer", "http://auth.example"}, 1, "", "{\"error_code\":\"invalid_issuer\"}\n"},
		{[]string{"discover-profile"}, 2, "", "{\"error_code\":\"invalid_usage\"}\n"},
		{[]string{"discover-profile", "--profile", "x"}, 2, "", "{\"error_code\":\"invalid_usage\"}\n"},
		{[]string{"discover-profile", "--issuer", "https://auth.example", "extra"}, 2, "", "{\"error_code\":\"invalid_usage\"}\n"},
	} {
		var stdout, stderr bytes.Buffer
		code := run(test.args, strings.NewReader(""), &stdout, &stderr)
		if code != test.code || stdout.String() != test.stdout || stderr.String() != test.stderr {
			t.Fatalf("%v: exit=%d stdout=%q stderr=%q", test.args, code, stdout.String(), stderr.String())
		}
	}
}

func TestCommandReportsOutputFailure(t *testing.T) {
	profile, err := os.ReadFile("../../internal/backendprofile/testdata/profile-v4.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, writer := range []io.Writer{failedOutput{}, shortOutput{}} {
		var stderr bytes.Buffer
		code := run([]string{"validate-profile"}, bytes.NewReader(profile), writer, &stderr)
		if code != 1 || stderr.String() != "{\"error_code\":\"output_failed\"}\n" {
			t.Fatalf("exit=%d stderr=%q", code, stderr.String())
		}
	}
}

func TestCommandFailsWhenDiagnosticCannotBeWritten(t *testing.T) {
	var stdout bytes.Buffer
	if code := run(nil, strings.NewReader(""), &stdout, failedOutput{}); code != 1 || stdout.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q", code, stdout.String())
	}
}

func TestValidatePairedInputWithoutStateEffects(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"validate", "--profile", "../../internal/backendprofile/testdata/profile-v4.json", "--release", "../../internal/workloadrelease/testdata/manifest.json"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 || stdout.String() != "{\"valid\":true}\n" || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestInputFileFailureDoesNotLeakPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"validate", "--profile", filepath.Join(t.TempDir(), "private-example"), "--release", "absent"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 || stdout.Len() != 0 || stderr.String() != "{\"error_code\":\"input_read_failed\"}\n" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestStateCommandsRequireRootBeforeReadingOrWriting(t *testing.T) {
	for _, command := range []string{"status", "apply", "serve"} {
		var stdout, stderr bytes.Buffer
		state := filepath.Join(t.TempDir(), "private-state")
		args := []string{command, "--state-dir", state}
		if command == "serve" && os.Geteuid() == 0 {
			continue // The foreground service owns its signal context.
		}
		code := run(args, strings.NewReader(""), &stdout, &stderr)
		if os.Geteuid() != 0 {
			if code != 1 || stderr.String() != "{\"error_code\":\"root_required\"}\n" {
				t.Fatalf("exit=%d stderr=%q", code, stderr.String())
			}
		} else if command == "status" {
			if code != 0 || !strings.HasPrefix(stdout.String(), "Device not installed\nFirmware ") {
				t.Fatalf("exit=%d stdout=%q", code, stdout.String())
			}
		} else if code != 1 || stderr.String() != "{\"error_code\":\"not_configured\"}\n" {
			t.Fatalf("exit=%d stderr=%q", code, stderr.String())
		}
		if _, err := os.Lstat(state); !os.IsNotExist(err) {
			t.Fatal("command created state on rejected/absent input")
		}
	}
}

func TestRejectsAmbiguousCommandFlags(t *testing.T) {
	for _, args := range [][]string{
		{"validate"}, {"validate", "--profile", "a", "--profile", "b", "--release", "c"},
		{"status", "--profile", "a"}, {"validate", "--profile", "a", "--release", "b", "--state-dir", "c"},
		{"configure", "--state-dir"}, {"status", "--unknown", "example"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, strings.NewReader(""), &stdout, &stderr); code != 2 || stderr.String() != "{\"error_code\":\"invalid_usage\"}\n" {
			t.Fatalf("exit=%d stderr=%q", code, stderr.String())
		}
	}
}

func TestApplyRequiresConfiguredRootStateBeforeEngineAccess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"apply", "--state-dir", filepath.Join(t.TempDir(), "absent")}, strings.NewReader(""), &stdout, &stderr)
	want := "{\"error_code\":\"root_required\"}\n"
	if os.Geteuid() == 0 {
		want = "{\"error_code\":\"not_configured\"}\n"
	}
	if code != 1 || stdout.Len() != 0 || stderr.String() != want {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

type failedInput struct{}

func (failedInput) Read([]byte) (int, error) { return 0, errors.New("sensitive input error") }

type failedOutput struct{}

func (failedOutput) Write([]byte) (int, error) { return 0, errors.New("sensitive output error") }

type shortOutput struct{}

func (shortOutput) Write([]byte) (int, error) { return 0, nil }
