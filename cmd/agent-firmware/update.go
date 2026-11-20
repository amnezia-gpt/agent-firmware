package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/host"
)

// The permanent address of the installation script of the latest release.
const updateScriptURL = "https://github.com/amnezia-gpt/agent-firmware/releases/latest/download/agent-firmware-install.sh"

const maxUpdateScript = 1 << 20

// The boundaries of an update are replaced in tests.
var (
	fetchUpdateScript = func(ctx context.Context) ([]byte, error) {
		client := &http.Client{Timeout: time.Minute, CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if next.URL.Scheme != "https" || len(via) > 5 {
				return errors.New("redirect refused")
			}
			return nil
		}}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, updateScriptURL, nil)
		if err != nil {
			return nil, err
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, errors.New("unexpected status")
		}
		return io.ReadAll(io.LimitReader(response.Body, maxUpdateScript+1))
	}
	runUpdateScript = func(ctx context.Context, script string, stdout, stderr io.Writer) error {
		command := exec.CommandContext(ctx, "sh", script)
		command.Stdout, command.Stderr = stdout, stderr
		return command.Run()
	}
)

// runUpdate installs the latest release over this installation:
//
//	agent-firmware update
//
// The bootstrap takes no arguments or standard input.
func runUpdate(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		return report(stderr, "invalid_usage", 2)
	}
	if reason := host.AccessCheck(); reason != "" {
		return report(stderr, reason, 1)
	}
	_, err := (configuration.Store{Path: defaultStateDir}).LoadDevice()
	if err != nil {
		return report(stderr, err.Error(), 1)
	}
	return updateFrom(stdout, stderr)
}

// updateDirectory holds the downloaded script; it is read by sh, so a noexec mount does not matter.
var updateDirectory = hostLayout.UpdateDirectory

func updateFrom(stdout, stderr io.Writer) int {
	ctx := context.Background()
	_, _ = io.WriteString(stderr, "agent-firmware: Downloading the latest release\n")
	script, err := fetchUpdateScript(ctx)
	if err != nil || len(script) == 0 || len(script) > maxUpdateScript || !strings.HasPrefix(string(script), "#!/bin/sh\n") {
		return report(stderr, "update_download_failed", 1)
	}
	directory, err := os.MkdirTemp(updateDirectory, "agent-firmware-update.")
	if err != nil {
		return report(stderr, "update_write_failed", 1)
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "agent-firmware-install.sh")
	if os.WriteFile(path, script, 0o600) != nil {
		return report(stderr, "update_write_failed", 1)
	}
	// The script and the installer report their own progress, result and error.
	if runUpdateScript(ctx, path, stdout, stderr) != nil {
		return 1
	}
	return 0
}
