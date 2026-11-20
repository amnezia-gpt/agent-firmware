package installer

import (
	"os"
	"path/filepath"
	"testing"

	hostplatform "github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/privatefile"
)

func TestUserParentsValidateWithoutCreationAndRejectSymlinks(t *testing.T) {
	home := t.TempDir()
	if err := os.Chmod(home, 0700); err != nil {
		t.Fatal(err)
	}
	library := filepath.Join(home, "Library")
	support := filepath.Join(library, "Application Support")
	layout := hostplatform.Layout{UserService: true, UID: uint32(os.Geteuid()), GID: uint32(os.Getegid()), Base: filepath.Join(support, "firmware"),
		Parents: []string{home, library, support}, CreateParents: []string{library, support}}
	if err := CheckParents(layout, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(library); !os.IsNotExist(err) {
		t.Fatal("read-only host check created directories")
	}
	if err := CheckParents(layout, true); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(support); err != nil || !privatefile.EffectiveOwner().Directory(info, 0700) {
		t.Fatalf("application directory is not private: %v", err)
	}
	if err := os.Remove(support); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), support); err != nil {
		t.Fatal(err)
	}
	if err := CheckParents(layout, true); err != ErrConflict {
		t.Fatalf("symlink refusal: %v", err)
	}
}
