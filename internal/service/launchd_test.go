package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/host"
)

func TestLaunchAgentStartsOwnedDefinitionWithFreshPID(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	unit := filepath.Join(dir, "agent with spaces.plist")
	layout := host.Layout{UID: 501, Unit: unit, Runtime: dir, Wrapper: filepath.Join(dir, "path & tools", "agent-firmware")}
	var calls []string
	loaded := false
	manager := Launchd{Layout: layout, command: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		switch args[0] {
		case "print":
			if !loaded {
				return nil, errNotLoaded
			}
			return []byte("\tpath = " + unit + "\n"), nil
		case "bootstrap":
			loaded = true
		case "kickstart":
			stamp := fmt.Sprintf(`{"pid":321,"ready_at":%q}`, time.Now().UTC().Format(time.RFC3339Nano))
			if err := os.WriteFile(filepath.Join(dir, host.RuntimeStatus), []byte(stamp), 0600); err != nil {
				t.Fatal(err)
			}
			return []byte("321\n"), nil
		}
		return nil, nil
	}}
	definition := manager.Definition()
	if !manager.Owns(definition) || !strings.Contains(string(definition), "path &amp; tools") {
		t.Fatal("invalid owned plist definition")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(calls, "bootstrap gui/501 "+unit) || !slices.Contains(calls, "kickstart -p gui/501/"+host.LaunchAgentLabel) {
		t.Fatalf("unexpected launchctl calls: %v", calls)
	}
}
