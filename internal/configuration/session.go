//go:build linux || darwin

package configuration

import (
	"errors"
	"io"
	"os"
	"syscall"
)

// Session retains the configuration writer lock for one lifecycle operation.
// Its methods are valid only inside the Exclusive callback.
type Session struct {
	store Store
	root  *os.Root
}

func (s Store) Exclusive(fn func(*Session) error) error {
	root, err := s.root(false)
	if err != nil {
		return err
	}
	defer root.Close()
	lock, err := openFile(root, "operation.lock", os.O_RDWR|os.O_CREATE)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return ErrBusy
		}
		return ErrWrite
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return fn(&Session{s, root})
}
func (s *Session) Load() (*Input, error) { return load(s.root) }
func (s *Session) Read() ([]byte, error) {
	f, err := openFile(s.root, "operation.json", os.O_RDONLY)
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
		return s.store.atomic(s.root, "operation.json", data)
	}
	f, err := openFile(s.root, "operation.json", os.O_RDONLY)
	if errors.Is(err, ErrMissing) {
		return nil
	}
	if err != nil {
		return err
	}
	f.Close()
	if s.root.Remove("operation.json") != nil {
		return ErrWrite
	}
	return s.store.synchronize(s.root)
}
