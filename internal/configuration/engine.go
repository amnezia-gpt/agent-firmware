package configuration

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/amnezia-gpt/agent-firmware/internal/host"
)

const ErrEngineChanged Error = "docker_context_changed"

// EngineAddress uses the installed socket, or the historical default for old state.
func (s Store) EngineAddress() (string, error) {
	root, err := s.root(false)
	if errors.Is(err, ErrMissing) {
		return s.engineFallback()
	}
	if err != nil {
		return "", err
	}
	defer root.Close()
	return s.configuredEngineAddress(root)
}

func (s *Session) EngineAddress() (string, error) {
	return s.store.configuredEngineAddress(s.root)
}

func (s Store) configuredEngineAddress(root *os.Root) (string, error) {
	address, err := readEngineAddress(root)
	if errors.Is(err, ErrMissing) {
		return s.engineFallback()
	}
	return address, err
}

func (s Store) engineFallback() (string, error) {
	address := s.EngineSocket
	if address == "" {
		address = host.DefaultLayout().EngineSocket
	}
	if address == "" {
		return "", ErrMissing
	}
	if !validEngineAddress(address) {
		return "", ErrCorrupt
	}
	return address, nil
}

// PersistEngineAddress is called during installation, with the service stopped.
func (s Store) PersistEngineAddress() error {
	return s.Exclusive(func(session *Session) error {
		address, err := readEngineAddress(session.root)
		if errors.Is(err, ErrMissing) {
			address, err := s.engineFallback()
			if err != nil {
				return err
			}
			return s.atomic(session.root, host.EngineState, []byte(address+"\n"))
		}
		if err != nil {
			return err
		}
		if s.EngineSocket != "" && address != s.EngineSocket {
			return ErrEngineChanged
		}
		return s.synchronize(session.root)
	})
}

func readEngineAddress(root *os.Root) (string, error) {
	file, err := openFile(root, host.EngineState, os.O_RDONLY)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, host.MaxEngineAddress+1))
	if err != nil {
		return "", ErrRead
	}
	address, ok := strings.CutSuffix(string(data), "\n")
	if len(data) > host.MaxEngineAddress || !ok || !validEngineAddress(address) {
		return "", ErrCorrupt
	}
	return address, nil
}

func validEngineAddress(address string) bool {
	return len(address) < host.MaxEngineAddress && filepath.IsAbs(address) && filepath.Clean(address) == address &&
		address != string(filepath.Separator) && !strings.ContainsAny(address, "\x00\r\n")
}
