package configuration

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
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
