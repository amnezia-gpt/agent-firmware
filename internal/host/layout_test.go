package host

import (
	"path/filepath"
	"testing"
)

func TestUserLayoutKeepsStateAndExecutableInApplicationSupport(t *testing.T) {
	home := filepath.Join(t.TempDir(), "Example User")
	layout := userLayout(home, 501, 20)
	base := filepath.Join(home, "Library", "Application Support", "agent-firmware")
	if layout.UpdateDirectory != base || layout.State != filepath.Join(base, "state") ||
		layout.Wrapper != filepath.Join(base, "agent-firmware") || layout.UID != 501 || layout.GID != 20 ||
		layout.Unit != filepath.Join(home, "Library", "LaunchAgents", LaunchAgentLabel+".plist") {
		t.Fatalf("unexpected user layout: %+v", layout)
	}
}
