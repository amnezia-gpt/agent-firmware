package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amnezia-gpt/agent-firmware/internal/host"
)

func TestBuiltInHelpBeforeHostChecks(t *testing.T) {
	original := defaultStateDir
	defaultStateDir = t.TempDir()
	t.Cleanup(func() { defaultStateDir = original })
	legacy := filepath.Join(defaultStateDir, host.LegacyState)
	if err := os.WriteFile(legacy, []byte("earlier state"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}, {"help", "decommission"}, {"decommission", "--help"}, {"install", "--help"}, {"serve", "-h"}} {
		var out, diagnostics bytes.Buffer
		if code := run(args, unreadableStdin{t}, &out, &diagnostics); code != 0 || out.Len() == 0 || diagnostics.Len() != 0 {
			t.Fatalf("%v: exit=%d stdout=%q stderr=%q", args, code, &out, &diagnostics)
		}
	}
	if data, err := os.ReadFile(legacy); err != nil || string(data) != "earlier state" {
		t.Fatalf("help disturbed earlier state: %v", err)
	}
}

func TestUnknownHelpTopicUsesOverviewHint(t *testing.T) {
	var out, diagnostics bytes.Buffer
	const want = "agent-firmware: run 'agent-firmware help' for the list of commands\n{\"error_code\":\"invalid_usage\"}\n"
	if code := run([]string{"help", "unknown"}, unreadableStdin{t}, &out, &diagnostics); code != 2 || out.Len() != 0 || !strings.HasSuffix(diagnostics.String(), want) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, &out, &diagnostics)
	}
}

func TestHelpOverviewText(t *testing.T) {
	const want = `agent-firmware <command> [flags]

Device lifecycle, in order:
  install        Manufacture the device on this host and install the service
  enroll         Join a domain and start the workloads
  status         Show the device, its enrollment and kept user data
  update         Install the latest firmware release
  unenroll       Leave the domain; user data is kept unless --delete-data
  decommission   Remove the device and the firmware; user data is kept
                 unless --delete-data

Checks that change nothing:
  discover-profile   Assemble a domain's backend profile from its metadata
  validate-release   Check a workload release manifest for this firmware
  validate-profile   Check a backend profile read from standard input
  validate           Check a backend profile and a release together

Run by the service and the installer, not by operators:
  serve, apply, preflight

Run 'agent-firmware help <command>' for a command's flags and effects.
`
	if got := overview(); got != want {
		t.Fatalf("unexpected overview:\n%s", got)
	}
}

func TestEveryHelpTopicAndFlagIsDispatched(t *testing.T) {
	examples := map[string][][]string{
		"install":          {{"--release", "manifest.json"}},
		"enroll":           {{"--issuer", "https://auth.example", "--enrollment-code-stdin"}, {"--enrollment-code-file", "code", "--issuer", "https://auth.example"}},
		"status":           {{}, {"--state-dir", "state"}},
		"update":           {{}},
		"unenroll":         {{"--confirm", "--delete-data"}},
		"decommission":     {{"--confirm", "--delete-data"}},
		"discover-profile": {{"--issuer", "https://auth.example"}},
		"validate-release": {{"--release", "manifest.json"}},
		"validate-profile": {{}},
		"validate":         {{"--profile", "profile.json", "--release", "manifest.json"}},
		"serve":            {{}, {"--state-dir", "state"}},
		"apply":            {{}, {"--state-dir", "state"}},
		"preflight":        {{}},
	}
	if len(firmwareCommands) != len(examples) {
		t.Fatal("command registry and dispatcher examples differ")
	}
	for i := range firmwareCommands {
		command := &firmwareCommands[i]
		t.Run(command.name, func(t *testing.T) {
			var want string
			for _, args := range [][]string{{"help", command.name}, {command.name, "--help"}, {command.name, "-h"}} {
				var out, diagnostics bytes.Buffer
				if code := run(args, unreadableStdin{t}, &out, &diagnostics); code != 0 || diagnostics.Len() != 0 {
					t.Fatalf("%v: %d %q", args, code, &diagnostics)
				}
				if want == "" {
					want = out.String()
				}
				if out.String() != want || !strings.HasSuffix(want, "\n") {
					t.Fatalf("help forms differ")
				}
			}
			if strings.Contains(want, "release bootstrap") {
				t.Error("help must use the installation script's file name")
			}
			if command.name == "decommission" &&
				(!strings.Contains(want, "sudo sh agent-firmware-install.sh --decommission --confirm\n") ||
					!strings.Contains(want, "Those volumes cannot be attached") ||
					!strings.Contains(want, "block a later install")) {
				t.Error("help must explain offline removal and its effect on reinstalling")
			}
			for _, line := range strings.Split(want, "\n") {
				if len(line) > 80 {
					t.Errorf("help line exceeds 80 columns: %q", line)
				}
			}
			if command.name == "decommission" || command.name == "unenroll" {
				if !strings.HasSuffix(want, "Data removed with --delete-data cannot be recovered.\n") {
					t.Fatal("missing final deletion warning")
				}
			}
			original := command.run
			t.Cleanup(func() { command.run = original })
			dispatched := false
			command.run = func(_ []string, _ io.Reader, _, _ io.Writer) int { dispatched = true; return 0 }
			seen := make(map[string]bool)
			invocations, ok := examples[command.name]
			if !ok {
				t.Fatal("missing dispatch examples")
			}
			for _, args := range invocations {
				dispatched = false
				if code := run(append([]string{command.name}, args...), unreadableStdin{t}, io.Discard, io.Discard); code != 0 || !dispatched {
					t.Fatalf("dispatcher refused documented flags %v", args)
				}
				for _, arg := range args {
					if strings.HasPrefix(arg, "--") {
						seen[arg] = true
					}
				}
			}
			for flag := range command.flags {
				if !seen[flag] || !strings.Contains(want, flag) {
					t.Errorf("flag %s lacks a dispatcher example or help text", flag)
				}
			}
			var diagnostics bytes.Buffer
			dispatched = false
			if code := run([]string{command.name, "--secret-unknown-flag"}, unreadableStdin{t}, io.Discard, &diagnostics); code != 2 ||
				dispatched || diagnostics.String() != "agent-firmware: run 'agent-firmware help "+command.name+"' for usage\n{\"error_code\":\"invalid_usage\"}\n" {
				t.Fatalf("wrong usage result: %q", &diagnostics)
			}
		})
	}
}
