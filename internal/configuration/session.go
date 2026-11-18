//go:build linux || darwin

package configuration

import (
	"errors"
	"io"
	"os"

	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/privatefile"
)

// Session retains the configuration writer lock for one lifecycle operation.
// Its methods are valid only inside the Exclusive callback.
type Session struct {
	store Store
	root  *os.Root
}

func (s Store) Exclusive(fn func(*Session) error) error {
	return s.exclusive(false, fn)
}

func (s Store) exclusive(create bool, fn func(*Session) error) error {
	root, err := s.root(create)
	if err != nil {
		return err
	}
	defer root.Close()
	lock, err := operationLock(root)
	if err != nil {
		return err
	}
	defer lock.Close()
	return fn(&Session{s, root})
}

func operationLock(root *os.Root) (*os.File, error) {
	lock, err := openFile(root, host.OperationLock, os.O_RDWR|os.O_CREATE)
	if err != nil {
		return nil, err
	}
	if err := privatefile.TryLock(lock); err != nil {
		lock.Close()
		if errors.Is(err, privatefile.ErrBusy) {
			return nil, ErrBusy
		}
		return nil, ErrWrite
	}
	return lock, nil
}

func (s *Session) Snapshot() (Snapshot, error) {
	if err := checkDecommission(s.root); err != nil {
		return Snapshot{}, err
	}
	return loadSnapshot(s.root)
}
func (s *Session) Read() ([]byte, error) {
	f, err := openFile(s.root, host.OperationJournal, os.O_RDONLY)
	if errors.Is(err, ErrMissing) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 32*1024+1))
	if err != nil {
		return nil, ErrRead
	}
	if len(data) > 32*1024 {
		return nil, ErrCorrupt
	}
	return data, nil
}
func (s *Session) Write(data []byte) error {
	if len(data) > 32*1024 {
		return ErrInvalid
	}
	if len(data) > 0 {
		return s.store.atomic(s.root, host.OperationJournal, data)
	}
	f, err := openFile(s.root, host.OperationJournal, os.O_RDONLY)
	if errors.Is(err, ErrMissing) {
		return nil
	}
	if err != nil {
		return err
	}
	f.Close()
	if s.root.Remove(host.OperationJournal) != nil {
		return ErrWrite
	}
	return s.store.synchronize(s.root)
}
