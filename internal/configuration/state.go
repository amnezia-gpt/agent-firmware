//go:build linux || darwin

package configuration

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/amnezia-gpt/agent-firmware/internal/deviceidentity"
	"github.com/amnezia-gpt/agent-firmware/internal/privatefile"
)

const MaxStateBytes = 132 * 1024

// Store operates in a caller-selected private directory. CLI callers must be root.
type Store struct {
	Path          string
	beforeRename  func() error
	syncDirectory func(*os.File) error
}

func (s Store) root(create bool) (*os.Root, error) {
	if s.Path == "" {
		return nil, ErrUnsafe
	}
	// Lstat follows a directory symlink when its name has a trailing slash.
	s.Path = filepath.Clean(s.Path)
	if create {
		if err := os.Mkdir(s.Path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, ErrWrite
		}
	}
	root, err := privatefile.OpenDirectory(s.Path, privatefile.EffectiveOwner(), 0700)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrMissing
	}
	if errors.Is(err, privatefile.ErrUnsafe) {
		return nil, ErrUnsafe
	}
	if err != nil {
		return nil, ErrRead
	}
	if create {
		// Persist the directory entry as well as records written inside it.
		parent, err := os.Open(filepath.Dir(s.Path))
		if err != nil {
			root.Close()
			return nil, ErrWrite
		}
		err = parent.Sync()
		closeErr := parent.Close()
		if err != nil || closeErr != nil {
			root.Close()
			return nil, ErrWrite
		}
	}
	return root, nil
}

func openFile(root *os.Root, name string, flags int) (*os.File, error) {
	owner := privatefile.EffectiveOwner()
	if info, err := root.Lstat(name); err == nil {
		if !owner.Regular(info, 0600, true) {
			return nil, ErrUnsafe
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, ErrRead
	}
	file, err := privatefile.OpenRegular(root, name, flags, 0600, owner, flags&(os.O_WRONLY|os.O_RDWR) == 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrMissing
	}
	if err != nil {
		return nil, ErrUnsafe
	}
	return file, nil
}

type Snapshot struct {
	Device     *Device
	Enrollment *Enrollment
	Removal    *Removal
}

func (s Snapshot) Identity() *deviceidentity.Access {
	if s.Device == nil || s.Enrollment == nil {
		return nil
	}
	return deviceidentity.NewAccess(s.Device.identity, s.Enrollment.keys, s.Enrollment.Profile.AuthIssuer)
}

func readRecord(root *os.Root, name string) ([]byte, error) {
	f, err := openFile(root, name, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxStateBytes+1))
	if err != nil {
		return nil, ErrRead
	}
	if len(data) > MaxStateBytes {
		return nil, ErrCorrupt
	}
	return data, nil
}

func loadSnapshot(root *os.Root) (Snapshot, error) {
	device, err := loadDevice(root)
	if err != nil {
		return Snapshot{}, err
	}
	enrollment, err := loadEnrollment(root, device)
	if err != nil && !errors.Is(err, ErrMissing) {
		return Snapshot{}, err
	}
	removal, err := loadRemoval(root)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Device: device, Enrollment: enrollment, Removal: removal}, nil
}

func (s Store) Snapshot() (Snapshot, error) {
	root, err := s.root(false)
	if err != nil {
		return Snapshot{}, err
	}
	defer root.Close()
	if err := checkDecommission(root); err != nil {
		return Snapshot{}, err
	}
	return loadSnapshot(root)
}

func (s Store) LoadDevice() (*Device, error) {
	root, err := s.root(false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if err := checkDecommission(root); err != nil {
		return nil, err
	}
	return loadDevice(root)
}

func (s Store) atomic(root *os.Root, target string, data []byte) error {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return ErrWrite
	}
	name := "pending-" + hex.EncodeToString(nonce[:])
	committed, err := privatefile.Replace(root, target, name, data, 0600, false, s.beforeRename, s.syncDirectory)
	if err == nil {
		return nil
	}
	if committed {
		return ErrUncertain
	}
	return ErrWrite
}

func (s Store) synchronize(root *os.Root) error {
	if privatefile.SyncDirectory(root, ".", s.syncDirectory) != nil {
		return ErrUncertain
	}
	return nil
}
