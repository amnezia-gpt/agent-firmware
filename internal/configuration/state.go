//go:build linux || darwin

package configuration

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/amnezia-gpt/agent-firmware/internal/strictjson"
	"github.com/amnezia-gpt/agent-firmware/internal/workloadrelease"
)

const MaxStateBytes = 132 * 1024

// Store operates in a caller-selected private directory. CLI callers must be root.
type Store struct {
	Path          string
	beforeRename  func() error
	syncDirectory func(*os.File) error
}

type record struct {
	SchemaVersion int    `json:"schema_version"`
	Generation    string `json:"generation"`
	Profile       []byte `json:"backend_profile"`
	Release       []byte `json:"workload_release"`
}

func safe(info os.FileInfo, mode os.FileMode) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode().Perm() == mode && info.Mode()&(os.ModeSymlink|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0 &&
		stat.Uid == uint32(os.Geteuid()) && stat.Gid == uint32(os.Getegid()) && (info.IsDir() || info.Mode().IsRegular() && stat.Nlink <= 1)
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
	info, err := os.Lstat(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrMissing
	}
	if err != nil {
		return nil, ErrRead
	}
	if !info.IsDir() || !safe(info, 0700) {
		return nil, ErrUnsafe
	}
	root, err := os.OpenRoot(s.Path)
	if err != nil {
		return nil, ErrUnsafe
	}
	dir, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, ErrUnsafe
	}
	defer dir.Close()
	opened, err := dir.Stat()
	if err != nil || !os.SameFile(info, opened) || !safe(opened, 0700) {
		root.Close()
		return nil, ErrUnsafe
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
	if info, err := root.Lstat(name); err == nil {
		if !info.Mode().IsRegular() || !safe(info, 0600) {
			return nil, ErrUnsafe
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, ErrRead
	}
	f, err := root.OpenFile(name, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrMissing
	}
	if err != nil {
		return nil, ErrUnsafe
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !safe(info, 0600) {
		f.Close()
		return nil, ErrUnsafe
	}
	// A reader may hold the complete old snapshot after rename unlinks it.
	// Writable files, especially the process lock, must still have one link.
	if flags&(os.O_WRONLY|os.O_RDWR) != 0 && info.Sys().(*syscall.Stat_t).Nlink != 1 {
		f.Close()
		return nil, ErrUnsafe
	}
	return f, nil
}

func load(root *os.Root) (*Input, error) {
	f, err := openFile(root, "accepted.json", os.O_RDONLY)
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
	fields, ok := strictjson.Object(data, "schema_version", "generation", "backend_profile", "workload_release")
	if !ok || string(bytes.TrimSpace(fields["schema_version"])) != "1" {
		return nil, ErrCorrupt
	}
	var stored record
	if json.Unmarshal(data, &stored) != nil {
		return nil, ErrCorrupt
	}
	input, err := Parse(bytes.NewReader(stored.Profile), bytes.NewReader(stored.Release))
	if err != nil || input.Generation() != stored.Generation {
		return nil, ErrCorrupt
	}
	return input, nil
}

// Load never creates storage and validates the complete accepted snapshot.
func (s Store) Load() (*Input, error) {
	root, err := s.root(false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return load(root)
}

// Save returns changed only after the complete state is durably acknowledged.
// ErrUncertain means the accepted generation could not be durably acknowledged.
func (s Store) Save(input *Input) (bool, error) {
	if input == nil || input.generation == "" {
		return false, ErrInvalid
	}
	root, err := s.root(true)
	if err != nil {
		return false, err
	}
	defer root.Close()
	lock, err := openFile(root, "operation.lock", os.O_RDWR|os.O_CREATE)
	if err != nil {
		return false, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return false, ErrBusy
		}
		return false, ErrWrite
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if pending, err := (&Session{store: s, root: root}).Read(); err != nil {
		return false, err
	} else if len(pending) > 0 {
		return false, ErrPending
	}
	required, e := apiRequired(root)
	if e != nil {
		return false, e
	}
	if required && input.release.Contract != workloadrelease.DeviceAPIContract {
		return false, ErrContractDowngrade
	}
	old, err := load(root)
	if err != nil && !errors.Is(err, ErrMissing) {
		return false, err
	}
	if old != nil {
		if old.release.Contract == workloadrelease.DeviceAPIContract && input.release.Contract != workloadrelease.DeviceAPIContract {
			return false, ErrContractDowngrade
		}
		if old.profile.ID != input.profile.ID || old.profile.AuthIssuer != input.profile.AuthIssuer {
			return false, ErrIdentityConflict
		}
		if old.generation == input.generation {
			return false, s.synchronize(root)
		}
	}
	data, err := json.Marshal(record{1, input.generation, input.profileBytes, input.releaseBytes})
	if err != nil {
		return false, ErrWrite
	}
	if err := s.atomic(root, "accepted.json", data); err != nil {
		return false, err
	}
	return true, nil
}

func (s Store) atomic(root *os.Root, target string, data []byte) error {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return ErrWrite
	}
	name := "pending-" + hex.EncodeToString(nonce[:])
	f, err := openFile(root, name, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return ErrWrite
	}
	defer root.Remove(name)
	n, writeErr := f.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return ErrWrite
	}
	if s.beforeRename != nil {
		if err := s.beforeRename(); err != nil {
			return ErrWrite
		}
	}
	if err := root.Rename(name, target); err != nil {
		return ErrWrite
	}
	if err := s.synchronize(root); err != nil {
		return err
	}
	return nil
}

func (s Store) synchronize(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return ErrUncertain
	}
	defer dir.Close()
	if s.syncDirectory != nil {
		err = s.syncDirectory(dir)
	} else {
		err = dir.Sync()
	}
	if err != nil {
		return ErrUncertain
	}
	return nil
}
