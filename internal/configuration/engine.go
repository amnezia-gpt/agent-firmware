package configuration

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/amnezia-gpt/agent-firmware/internal/host"
)

// EngineAddress uses the installed socket, or the historical default for old state.
func (s Store) EngineAddress() (string, error) {
	root, err := s.root(false)
	if errors.Is(err, ErrMissing) {
		return host.DefaultLayout().EngineSocket, nil
	}
	if err != nil {
		return "", err
	}
	defer root.Close()
	return configuredEngineAddress(root)
}

func (s *Session) EngineAddress() (string, error) {
	return configuredEngineAddress(s.root)
}

func configuredEngineAddress(root *os.Root) (string, error) {
	address, err := readEngineAddress(root)
	if errors.Is(err, ErrMissing) {
		return host.DefaultLayout().EngineSocket, nil
	}
	return address, err
}

// PersistEngineAddress is called during installation, with the service stopped.
func (s Store) PersistEngineAddress() error {
	return s.Exclusive(func(session *Session) error {
		_, err := readEngineAddress(session.root)
		if errors.Is(err, ErrMissing) {
			return s.atomic(session.root, host.EngineState, []byte(host.DefaultLayout().EngineSocket+"\n"))
		}
		if err != nil {
			return err
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
	if len(data) > host.MaxEngineAddress || !ok || !filepath.IsAbs(address) || filepath.Clean(address) != address ||
		address == string(filepath.Separator) || strings.ContainsAny(address, "\x00\r\n") {
		return "", ErrCorrupt
	}
	return address, nil
}
