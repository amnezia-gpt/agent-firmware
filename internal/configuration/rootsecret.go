package configuration

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"regexp"

	"github.com/amnezia-gpt/agent-firmware/internal/host"
)

const rootSecretSize = 32

var pendingRecordName = regexp.MustCompile(`^pending-[0-9a-f]{32}$`)

func secretFingerprint(key []byte) string {
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:])
}

func readRootSecret(root *os.Root) ([]byte, error) {
	file, err := openFile(root, host.RootSecret, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	secret, err := io.ReadAll(io.LimitReader(file, rootSecretSize+1))
	if err != nil || len(secret) != rootSecretSize {
		clear(secret)
		return nil, ErrRootSecret
	}
	return secret, nil
}

func checkedRootSecret(root *os.Root, fingerprint string) ([]byte, error) {
	secret, err := readRootSecret(root)
	if err != nil || secretFingerprint(secret) != fingerprint {
		clear(secret)
		return nil, ErrRootSecret
	}
	return secret, nil
}

// RootSecret returns a validated copy for the running service. The caller clears it.
func (s *Session) RootSecret(device *Device) ([]byte, error) {
	return checkedRootSecret(s.root, device.rootFingerprint)
}

// initializeRootSecret runs only with the existing state writer lock, before
// publishing the first device record.
func initializeRootSecret(root *os.Root) (string, error) {
	dir, err := root.Open(".")
	if err != nil {
		return "", ErrRootSecret
	}
	defer dir.Close()
	for {
		names, readErr := dir.Readdirnames(32)
		for _, name := range names {
			if name != host.OperationLock && name != host.RootSecret && !pendingRecordName.MatchString(name) {
				return "", ErrRootSecret
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return "", ErrRootSecret
		}
	}
	secret, err := readRootSecret(root)
	if err == nil {
		defer clear(secret)
		return secretFingerprint(secret), nil
	}
	if !errors.Is(err, ErrMissing) {
		return "", ErrRootSecret
	}
	secret = make([]byte, rootSecretSize)
	defer clear(secret)
	if _, err := rand.Read(secret); err != nil {
		return "", ErrRootSecret
	}
	// The configuration fault hooks belong to the device record. Root secret
	// publication must finish durably before the device record refers to it.
	guard := Store{beforeRename: func() error {
		if _, err := root.Lstat(host.RootSecret); !errors.Is(err, os.ErrNotExist) {
			return ErrRootSecret
		}
		return nil
	}}
	if err := guard.atomic(root, host.RootSecret, secret); err != nil {
		return "", err
	}
	return secretFingerprint(secret), nil
}
