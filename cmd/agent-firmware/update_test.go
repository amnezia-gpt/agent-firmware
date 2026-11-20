package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/amnezia-gpt/agent-firmware/internal/host"
)

func TestUpdateCommandUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"update", "--issuer", "https://auth.example"}, strings.NewReader(""), &stdout, &stderr); code != 2 || stderr.String() != "agent-firmware: run 'agent-firmware help update' for usage\n{\"error_code\":\"invalid_usage\"}\n" {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if reason := host.AccessCheck(); reason != "" {
		stderr.Reset()
		if code := run([]string{"update"}, strings.NewReader(""), &stdout, &stderr); code != 1 || stderr.String() != "{\"error_code\":\""+reason+"\"}\n" {
			t.Fatalf("exit=%d stderr=%q", code, stderr.String())
		}
	}
}

func TestUpdateRunsTheLatestScript(t *testing.T) {
	originalFetch, originalRun, originalDirectory := fetchUpdateScript, runUpdateScript, updateDirectory
	defer func() {
		fetchUpdateScript, runUpdateScript, updateDirectory = originalFetch, originalRun, originalDirectory
	}()
	updateDirectory = t.TempDir()
	script := "#!/bin/sh\necho ok\n"
	var fetched []byte
	var fetchErr, runErr error
	ran := ""
	fetchUpdateScript = func(context.Context) ([]byte, error) { return fetched, fetchErr }
	runUpdateScript = func(_ context.Context, path string, stdout, _ io.Writer) error {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		ran = string(data)
		_, _ = io.WriteString(stdout, "{\"installed\":true}\n")
		return runErr
	}
	for _, test := range []struct {
		name           string
		fetched        string
		fetchErr       error
		runErr         error
		code           int
		stdout, stderr string
		ran            bool
	}{
		{"success", script, nil, nil, 0, "{\"installed\":true}\n", "", true},
		{"download failed", "", errors.New("x"), nil, 1, "", "{\"error_code\":\"update_download_failed\"}\n", false},
		{"empty download", "", nil, nil, 1, "", "{\"error_code\":\"update_download_failed\"}\n", false},
		{"not a script", "<html>", nil, nil, 1, "", "{\"error_code\":\"update_download_failed\"}\n", false},
		{"too large", script + strings.Repeat("#", maxUpdateScript), nil, nil, 1, "", "{\"error_code\":\"update_download_failed\"}\n", false},
		// The script reports its own error; no second one is printed.
		{"script failed", script, nil, errors.New("x"), 1, "{\"installed\":true}\n", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fetched, fetchErr, runErr, ran = []byte(test.fetched), test.fetchErr, test.runErr, ""
			var stdout, stderr bytes.Buffer
			code := updateFrom(&stdout, &stderr)
			errorLine := strings.TrimPrefix(stderr.String(), "agent-firmware: Downloading the latest release\n")
			if code != test.code || stdout.String() != test.stdout || errorLine != test.stderr {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if test.ran != (ran == script) {
				t.Fatalf("ran=%q", ran)
			}
			if entries, _ := os.ReadDir(updateDirectory); len(entries) != 0 {
				t.Fatal("update directory left behind")
			}
		})
	}
}
