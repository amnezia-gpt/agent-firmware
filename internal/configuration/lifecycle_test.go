package configuration

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManufactureAndEnrollmentKeepSeparateState(t *testing.T) {
	input := testInput(t)
	store := testStore(t)
	device, err := ParseDevice(bytes.NewReader(input.releaseBytes))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Manufacture(device, nil); err != nil {
		t.Fatal(err)
	}
	first, err := store.Snapshot()
	if err != nil || first.Device == nil || first.Enrollment != nil {
		t.Fatalf("manufactured state: %+v, %v", first, err)
	}
	root := filepath.Join(store.Path, "root-secret")
	original, err := os.ReadFile(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Exclusive(func(s *Session) error {
		if err := s.BeginEnrollment(input); err != nil {
			return err
		}
		return s.SetOutcome(input.Profile().AuthIssuer, Enrolled, false)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Manufacture(device, nil); err != nil {
		t.Fatal(err)
	}
	last, err := store.Snapshot()
	if err != nil || last.Enrollment == nil || last.Enrollment.Outcome != Enrolled {
		t.Fatalf("reinstall lost enrollment: %+v, %v", last, err)
	}
	actual, err := os.ReadFile(root)
	if err != nil || !bytes.Equal(original, actual) {
		t.Fatal("the root secret changed")
	}
}

func TestManufactureDistinguishesLegacyAndDamagedState(t *testing.T) {
	store := testStore(t)
	input := testInput(t)
	device, err := ParseDevice(bytes.NewReader(input.releaseBytes))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(store.Path, 0700); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(store.Path, "accepted.json")
	if err := os.WriteFile(legacy, []byte("prior state"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Manufacture(device, nil); !errors.Is(err, ErrLegacy) {
		t.Fatalf("legacy error: %v", err)
	}
	if err := os.Remove(legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Manufacture(device, nil); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(store.Path, "root-secret")
	if err := os.Remove(secret); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Manufacture(device, nil); !errors.Is(err, ErrRootSecret) {
		t.Fatalf("damaged secret error: %v", err)
	}
	if _, err := os.Stat(secret); !os.IsNotExist(err) {
		t.Fatalf("root secret re-created: %v", err)
	}
}

func TestInstalledIdentityAndOperationalKeysFailClosed(t *testing.T) {
	store := testStore(t)
	input := testInput(t)
	device := testDevice(t)
	if _, err := store.Manufacture(device, nil); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Snapshot()
	if err != nil || snapshot.Device.Thumbprint() == "" || snapshot.Identity() != nil {
		t.Fatalf("manufactured identity unavailable: %v", err)
	}
	original := snapshot.Device.Thumbprint()
	if err := store.Exclusive(func(s *Session) error { return s.BeginEnrollment(input) }); err != nil {
		t.Fatal(err)
	}
	after, err := store.Snapshot()
	if err != nil || after.Device.Thumbprint() != original || after.Identity() == nil {
		t.Fatalf("enrollment replaced device identity: %v", err)
	}
	first, err := after.Identity().Identity(time.Unix(1_800_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Exclusive(func(s *Session) error {
		if err := s.BeginEnrollment(input); err != nil {
			return err
		}
		return s.SetOutcome(input.Profile().AuthIssuer, Failed, false)
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err = store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	retry, err := snapshot.Identity().Identity(time.Unix(1_800_000_000, 0))
	if err != nil || retry.SigningKey != first.SigningKey || retry.EncryptionKey != first.EncryptionKey {
		t.Fatalf("retry replaced the enrolled keys: %v", err)
	}

	path := filepath.Join(store.Path, "enrollment", "profile.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]json.RawMessage
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	delete(record, "operational_keys")
	damaged, err := json.Marshal(record)
	if err != nil || os.WriteFile(path, damaged, 0600) != nil {
		t.Fatal("could not remove encrypted key field")
	}
	if _, err := store.Snapshot(); !errors.Is(err, ErrRootSecret) {
		t.Fatalf("missing keys were accepted: %v", err)
	}
	if err := store.Exclusive(func(s *Session) error { return s.BeginEnrollment(input) }); !errors.Is(err, ErrRootSecret) {
		t.Fatalf("missing keys were silently replaced: %v", err)
	}
	if _, err := store.Manufacture(device, nil); !errors.Is(err, ErrRootSecret) {
		t.Fatalf("reinstall ignored missing operational custody: %v", err)
	}
}
