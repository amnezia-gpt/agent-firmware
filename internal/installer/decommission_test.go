package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemovalHostChecksEntireInstallationBeforeStopping(t *testing.T) {
	h := newHost(t)
	if err := h.install("version one"); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(h.layout.Base, "releases", "not-ours")
	if err := os.Mkdir(foreign, 0755); err != nil {
		t.Fatal(err)
	}
	h.calls = nil
	if _, err := InspectRemovalHost(t.Context(), h.layout, h, false); err != ErrConflict {
		t.Fatalf("unexpected result: %v", err)
	}
	if len(h.calls) != 0 || !h.active || !lexists(foreign) || !lexists(h.layout.Wrapper) {
		t.Fatal("a conflicting installation was changed")
	}
}

func TestRemovalHostResumesAfterBinaryAndUnitRemoval(t *testing.T) {
	h := newHost(t)
	if err := h.install("version one"); err != nil {
		t.Fatal(err)
	}
	h.calls = nil
	removal, err := InspectRemovalHost(t.Context(), h.layout, h, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := removal.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.fail["daemon-reload"] = true
	if err := removal.Remove(t.Context()); err != ErrReload {
		t.Fatalf("expected reload failure, got %v", err)
	}
	delete(h.fail, "daemon-reload")
	removal, err = InspectRemovalHost(t.Context(), h.layout, h, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := removal.Remove(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{h.layout.Base, h.layout.Unit, h.layout.Wrapper} {
		if lexists(path) {
			t.Fatalf("remaining host artifact: %s", path)
		}
	}
	if h.active || h.enabled || !lexists(h.layout.State) {
		t.Fatal("bad service or state directory after host removal")
	}
	h.order("systemd stop", "systemd disable", "systemd daemon-reload")
}

func TestRemovalHostAcceptsEachPartiallyRemovedVersion(t *testing.T) {
	for _, step := range []string{"current", "binary", "marker"} {
		t.Run(step, func(t *testing.T) {
			h := newHost(t)
			if err := h.install("version one"); err != nil {
				t.Fatal(err)
			}
			target, err := InspectRemovalHost(t.Context(), h.layout, h, false)
			if err != nil {
				t.Fatal(err)
			}
			last := filepath.Join(h.layout.Base, "current")
			switch step {
			case "binary":
				pointer, err := os.Readlink(last)
				if err != nil {
					t.Fatal(err)
				}
				last = filepath.Join(h.layout.Base, pointer, "agent-firmware")
			case "marker":
				last = filepath.Join(h.layout.Base, ".managed")
			}
			if err := target.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
			for _, path := range target.paths {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if path == last {
					break
				}
			}
			target, err = InspectRemovalHost(t.Context(), h.layout, h, true)
			if err != nil {
				t.Fatal(err)
			}
			if err := target.Remove(t.Context()); err != nil {
				t.Fatal(err)
			}
			if lexists(h.layout.Base) || lexists(h.layout.Unit) || lexists(h.layout.Wrapper) {
				t.Fatal("resume left host artifacts")
			}
		})
	}
}
