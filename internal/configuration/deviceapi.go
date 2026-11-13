package configuration

import (
	"bytes"
	"errors"
	"io"
	"os"
)

const apiRequiredFile = "device-api-required"

func apiRequired(root *os.Root) (bool, error) {
	f, err := openFile(root, apiRequiredFile, os.O_RDONLY)
	if errors.Is(err, ErrMissing) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 32))
	if err != nil || !bytes.Equal(b, []byte("required\n")) {
		return false, ErrCorrupt
	}
	return true, nil
}
func (s *Session) DeviceAPIRequired() (bool, error) { return apiRequired(s.root) }

// RequireDeviceAPI is monotonic. Installer rollback never rewinds this floor,
// credential metadata or volumes, even when it restores accepted.json.
func (s *Session) RequireDeviceAPI() error {
	required, err := apiRequired(s.root)
	if err != nil {
		return err
	}
	if required {
		return nil
	}
	return s.store.atomic(s.root, apiRequiredFile, []byte("required\n"))
}
