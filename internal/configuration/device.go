package configuration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/amnezia-gpt/agent-firmware/internal/deviceidentity"
	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/strictjson"
	"github.com/amnezia-gpt/agent-firmware/internal/workloadrelease"
)

type Device struct {
	release         workloadrelease.Release
	raw             []byte
	generation      string
	rootFingerprint string
	identity        *deviceidentity.Device
	sealedIdentity  string
}

type deviceRecord struct {
	Version         int    `json:"schema_version"`
	Generation      string `json:"generation"`
	Release         []byte `json:"workload_release"`
	RootFingerprint string `json:"root_secret_sha256"`
	Identity        string `json:"idevid_key"`
}

func (d *Device) Release() workloadrelease.Release { return d.release }
func (d *Device) Generation() string               { return d.generation }
func (d *Device) Thumbprint() string {
	if d.identity == nil {
		return ""
	}
	return d.identity.Public().Thumbprint
}

func ParseDevice(reader io.Reader) (*Device, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, workloadrelease.MaxBytes+1))
	if err != nil {
		return nil, workloadrelease.ErrRead
	}
	if len(raw) > workloadrelease.MaxBytes {
		return nil, workloadrelease.ErrTooLarge
	}
	release, err := workloadrelease.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	canonical, _ := json.Marshal(release)
	hash := sha256.Sum256(canonical)
	return &Device{release: release, raw: raw, generation: "sha256:" + hex.EncodeToString(hash[:])}, nil
}

func checkCombinedState(root *os.Root) error {
	if _, err := root.Lstat(host.LegacyState); err == nil {
		return ErrLegacy
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrRead
	}
	return nil
}

func (s Store) CheckLegacy() error {
	root, err := s.root(false)
	if errors.Is(err, ErrMissing) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	if err := checkCombinedState(root); err != nil {
		return err
	}
	data, err := readRecord(root, host.DeviceState)
	if errors.Is(err, ErrMissing) {
		return nil
	}
	if err != nil {
		return err
	}
	var record deviceRecord
	if json.Unmarshal(data, &record) == nil && record.Version == 2 && earlierRelease(record.Release) {
		return ErrLegacy
	}
	return nil
}

func earlierRelease(raw []byte) bool {
	fields, ok := strictjson.Members(raw)
	return ok && string(bytes.TrimSpace(fields["schema_version"])) == "3"
}

func loadDevice(root *os.Root) (*Device, error) {
	if err := checkCombinedState(root); err != nil {
		return nil, err
	}
	data, err := readRecord(root, host.DeviceState)
	if errors.Is(err, ErrMissing) {
		if _, retained := root.Lstat(host.EnrollmentDir); !errors.Is(retained, os.ErrNotExist) {
			return nil, ErrCorrupt
		}
	}
	if err != nil {
		return nil, err
	}
	fields, ok := strictjson.Object(data, "schema_version", "generation", "workload_release", "root_secret_sha256", "idevid_key")
	if !ok {
		if old, valid := strictjson.Members(data); valid && old["idevid_key"] == nil {
			return nil, ErrRootSecret
		}
		return nil, ErrCorrupt
	}
	if string(bytes.TrimSpace(fields["schema_version"])) != "2" {
		return nil, ErrCorrupt
	}
	var r deviceRecord
	if json.Unmarshal(data, &r) != nil {
		return nil, ErrCorrupt
	}
	if earlierRelease(r.Release) {
		return nil, ErrLegacy
	}
	device, err := ParseDevice(bytes.NewReader(r.Release))
	if err != nil || device.generation != r.Generation {
		return nil, ErrCorrupt
	}
	secret, err := checkedRootSecret(root, r.RootFingerprint)
	if err != nil {
		return nil, err
	}
	defer clear(secret)
	device.identity, err = deviceidentity.OpenDevice(secret, r.Identity)
	if err != nil {
		return nil, ErrRootSecret
	}
	device.sealedIdentity = r.Identity
	device.rootFingerprint = r.RootFingerprint
	return device, nil
}

// Manufacture publishes only device state. The guard checks for retained
// engine data before an initial device identity can be initialized.
func (s Store) Manufacture(device *Device, guard func(*Session) error) (bool, error) {
	if device == nil || device.generation == "" {
		return false, ErrInvalid
	}
	var changed bool
	err := s.exclusive(true, func(session *Session) error {
		if removal, err := loadRemoval(session.root); err != nil {
			return err
		} else if removal != nil {
			return ErrRemovalPending
		}
		pending, err := session.Read()
		if err != nil {
			return err
		}
		if len(pending) > 0 {
			return ErrPending
		}
		old, err := loadDevice(session.root)
		if err != nil && !errors.Is(err, ErrMissing) {
			return err
		}
		var fingerprint, identity string
		if old != nil {
			if _, err := loadEnrollment(session.root, old); err != nil && !errors.Is(err, ErrMissing) {
				return err
			}
			if old.generation == device.generation {
				return s.synchronize(session.root)
			}
			fingerprint, identity = old.rootFingerprint, old.sealedIdentity
		} else {
			if guard != nil {
				if err := guard(session); err != nil {
					return err
				}
			}
			fingerprint, err = initializeRootSecret(session.root)
			if err != nil {
				return err
			}
			root, err := checkedRootSecret(session.root, fingerprint)
			if err != nil {
				return err
			}
			defer clear(root)
			identity, err = deviceidentity.GenerateDevice(root)
			if err != nil {
				return ErrRootSecret
			}
		}
		data, err := json.Marshal(deviceRecord{2, device.generation, device.raw, fingerprint, identity})
		if err != nil {
			return ErrWrite
		}
		if err := s.atomic(session.root, host.DeviceState, data); err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed, err
}
