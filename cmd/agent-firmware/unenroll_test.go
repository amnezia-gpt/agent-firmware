package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
)

type fakeRemoval struct {
	deregistrations int
	removals        int
	purges          int
	installation    string
}

func (f *fakeRemoval) Deregister(_ context.Context, stack workload.Stack) (string, error) {
	f.deregistrations++
	if stack.Installation != f.installation || len(stack.Client.Deregister) == 0 {
		return "", workload.ErrConflict
	}
	return "", errors.New("domain unavailable")
}

func (f *fakeRemoval) RemoveEnrollment(_ context.Context, id string, deleteData bool) error {
	f.removals++
	if deleteData || id != f.installation || f.removals == 1 {
		return workload.ErrEngine
	}
	return nil
}
func (f *fakeRemoval) Close()                                     {}
func (f *fakeRemoval) CheckRemoval(context.Context, string) error { return nil }
func (f *fakeRemoval) RetainedData(_ context.Context, id string) ([]dockerengine.VolumeInfo, error) {
	return []dockerengine.VolumeInfo{{Name: "af-" + id + "-work", Issuer: "https://auth.example"}}, nil
}

func TestUnenrollResumesLocalRemovalWithoutDomain(t *testing.T) {
	temp := t.TempDir()
	store := configuration.Store{Path: filepath.Join(temp, "state")}
	layout := host.Layout{Lock: filepath.Join(temp, "lock"), UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
	device, err := configuration.ParseDevice(bytes.NewReader(fixture(t, manifestFixture)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Manufacture(device, nil); err != nil {
		t.Fatal(err)
	}
	input, err := device.WithProfile(fixture(t, profileFixture))
	if err != nil {
		t.Fatal(err)
	}
	issuer := input.Profile().AuthIssuer
	prepare := func(s *configuration.Session) error {
		if err := s.BeginEnrollment(input, false); err != nil {
			return err
		}
		return s.SetOutcome(issuer, configuration.Enrolled, false)
	}
	if err := store.Exclusive(prepare); err != nil {
		t.Fatal(err)
	}
	before, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	rawRoot := fixture(t, filepath.Join(store.Path, host.RootSecret))
	fake := &fakeRemoval{installation: workload.EnrollmentID(issuer)}
	originalEngine, originalPurge := openRemovalEngine, purgeEnrollmentCode
	t.Cleanup(func() { openRemovalEngine, purgeEnrollmentCode = originalEngine, originalPurge })
	openRemovalEngine = func(string) (removalEngine, error) { return fake, nil }
	purgeEnrollmentCode = func(_ host.Layout, id string) error {
		if id != fake.installation {
			t.Fatal("wrong runtime scope")
		}
		fake.purges++
		return nil
	}

	if _, err := unenrollDevice(t.Context(), store, layout, false); err != workload.ErrEngine {
		t.Fatalf("unexpected interrupted removal: %v", err)
	}
	pending, err := store.Snapshot()
	if err != nil || pending.Removal == nil || pending.Removal.Deregistration != configuration.DeregistrationUnavailable {
		t.Fatalf("missing durable checkpoint: %v", err)
	}
	if got, err := executeManaged(t.Context(), store, nil); err != nil || !got.Applied {
		t.Fatalf("supervisor tried to recreate a removing enrollment: %+v %v", got, err)
	}
	if _, err := unenrollDevice(t.Context(), store, layout, true); err != configuration.ErrRemovalIntent || fake.removals != 1 {
		t.Fatalf("a retry widened the removal: %v", err)
	}
	outcome, err := unenrollDevice(t.Context(), store, layout, false)
	if err != nil || outcome.Deregistration != configuration.DeregistrationUnavailable || len(outcome.KeptVolumes) != 1 || outcome.DeleteDataCommand == "" || fake.deregistrations != 1 ||
		fake.removals != 2 || fake.purges != 1 {
		t.Fatalf("wrong resume: outcome=%+v, calls=%+v, err=%v", outcome, fake, err)
	}
	after, err := store.Snapshot()
	if phase, _ := phaseOf(after, time.Now()); err != nil || phase != "manufactured" || after.Removal != nil || after.Enrollment != nil {
		t.Fatalf("device did not return to manufactured state: %v", err)
	}
	if after.Device.Thumbprint() != before.Device.Thumbprint() ||
		!bytes.Equal(rawRoot, fixture(t, filepath.Join(store.Path, host.RootSecret))) {
		t.Fatal("unenroll replaced the permanent identity")
	}
	if _, err := os.Lstat(filepath.Join(store.Path, host.EnrollmentDir)); !os.IsNotExist(err) {
		t.Fatalf("operational keys remain: %v", err)
	}
	if err := store.Exclusive(prepare); err != nil {
		t.Fatalf("could not enroll again without install: %v", err)
	}
	joined, err := store.Snapshot()
	if err != nil || joined.Device.Thumbprint() != before.Device.Thumbprint() || joined.Identity() == nil {
		t.Fatalf("new enrollment did not reuse the device identity: %v", err)
	}
}

func TestUnenrollRequiresExplicitConfirmation(t *testing.T) {
	var output bytes.Buffer
	if got := run([]string{"unenroll"}, nil, io.Discard, &output); got != 2 ||
		!strings.Contains(output.String(), "--delete-data") || !strings.Contains(output.String(), "confirmation_required") {
		t.Fatalf("unexpected result %d %q", got, &output)
	}
}
