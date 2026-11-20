package configuration

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/amnezia-gpt/agent-firmware/internal/host"
)

func TestEngineAddressPersistsAndReadsOldState(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "state")}
	if err := os.Mkdir(store.Path, 0700); err != nil {
		t.Fatal(err)
	}
	fallback := filepath.Join(store.Path, "engine.sock")
	store.EngineSocket = fallback
	if address, err := store.EngineAddress(); err != nil || address != fallback {
		t.Fatalf("old state: address=%q, error=%v", address, err)
	}
	if err := store.PersistEngineAddress(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Path, host.EngineState)
	if data, err := os.ReadFile(path); err != nil || string(data) != fallback+"\n" {
		t.Fatalf("installed engine record: %q, %v", data, err)
	}

	alternate := filepath.Join(store.Path, "alternate.sock")
	if err := os.WriteFile(path, []byte(alternate+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.PersistEngineAddress(); err != ErrEngineChanged {
		t.Fatalf("changed context was accepted: %v", err)
	}
	store.EngineSocket = ""
	if err := store.PersistEngineAddress(); err != nil {
		t.Fatal(err)
	}
	if err := store.Exclusive(func(session *Session) error {
		address, err := session.EngineAddress()
		if err != nil || address != alternate {
			t.Fatalf("retained address=%q, error=%v", address, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestEngineAddressRejectsUnsafeOrCorruptState(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "state")}
	if err := os.Mkdir(store.Path, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Path, host.EngineState)
	if err := os.WriteFile(path, []byte("tcp://remote.invalid:2375\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.PersistEngineAddress(); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("bad address: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EngineAddress(); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("symlink: %v", err)
	}
}
