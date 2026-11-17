package configuration

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRootSecretIsBoundToAcceptedState(t *testing.T) {
	for _, damage := range []string{"missing", "short", "different", "mode", "symlink"} {
		t.Run(damage, func(t *testing.T) {
			store := testStore(t)
			input := testInput(t)
			if _, err := store.Save(input); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(store.Path, "root-secret")
			key, err := os.ReadFile(path)
			if err != nil || len(key) != 32 {
				t.Fatalf("root secret was not created: %v", err)
			}
			if _, err := store.Save(input); err != nil {
				t.Fatal(err)
			}
			if again, err := os.ReadFile(path); err != nil || !bytes.Equal(key, again) {
				t.Fatal("unchanged configuration replaced the root secret")
			}
			switch damage {
			case "missing":
				err = os.Remove(path)
			case "short":
				err = os.WriteFile(path, key[:31], 0600)
			case "different":
				key[0] ^= 1
				err = os.WriteFile(path, key, 0600)
			case "mode":
				err = os.Chmod(path, 0644)
			case "symlink":
				if err = os.Rename(path, path+".moved"); err == nil {
					err = os.Symlink(path+".moved", path)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Load(); err == nil {
				t.Fatal("accepted state with damaged root secret")
			}
			if _, err := store.Save(input); err == nil {
				t.Fatal("saved over damaged root secret")
			}
			if damage == "missing" {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatal("missing root secret was replaced")
				}
			}
		})
	}
}

func TestBootstrapCheckRunsUnderStateLockBeforeCreatingSecret(t *testing.T) {
	store := testStore(t)
	input := testInput(t)
	store.BootstrapCheck = func(*Session, *Input) error {
		if err := store.Exclusive(func(*Session) error { return nil }); err != ErrBusy {
			t.Fatalf("bootstrap check outside state lock: %v", err)
		}
		return ErrRootSecret
	}
	if _, err := store.Save(input); err != ErrRootSecret {
		t.Fatalf("bootstrap refusal: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(store.Path, "root-secret")); !os.IsNotExist(err) {
		t.Fatal("secret created before bootstrap check")
	}
}
