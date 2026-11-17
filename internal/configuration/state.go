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

	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/privatefile"
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

func load(root *os.Root) (*Input, error) {
	f, err := openFile(root, host.AcceptedState, os.O_RDONLY)
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
	lock, err := operationLock(root)
	if err != nil {
		return false, err
	}
	defer lock.Close()
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
	if err := s.atomic(root, host.AcceptedState, data); err != nil {
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
