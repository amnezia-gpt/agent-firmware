package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/installer"
)

type testCodes struct {
	mu     sync.Mutex
	held   bool
	staged chan string
}

func (s *testCodes) Put(code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.held = true
	s.staged <- code
	return nil
}
func (s *testCodes) Held() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.held
}
func (s *testCodes) HasCode() (bool, error) { return s.Held(), nil }
func (s *testCodes) Delete() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.held = false
	return nil
}

func TestEnrollmentRefusalAndRetryPreserveDeviceAndPinIssuer(t *testing.T) {
	stubStatusData(t)
	enableTestRelease(t)
	profile := fixture(t, profileFixture)
	originalDiscover, originalCheck, originalCodes := discover, checkWorkloadVolumes, openCodes
	t.Cleanup(func() {
		discover, checkWorkloadVolumes, openCodes = originalDiscover, originalCheck, originalCodes
	})
	discoveries, checks := 0, 0
	const issuer = "https://auth.example"
	discover = func(_ context.Context, got string) ([]byte, error) {
		discoveries++
		if got != issuer {
			t.Errorf("unexpected issuer lookup: %q", got)
		}
		return profile, nil
	}
	checkWorkloadVolumes = func(_ context.Context, _ *configuration.Session, _ *configuration.Input, fresh bool) (bool, error) {
		checks++
		return fresh, nil
	}
	codes := &testCodes{staged: make(chan string, 2)}
	openCodes = func(host.Layout, string, bool) (codeStore, error) { return codes, nil }

	dir := t.TempDir()
	store := configuration.Store{Path: filepath.Join(dir, "state")}
	layout := host.Layout{Lock: filepath.Join(dir, "lock"), UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
	show := func(want string, showIssuer bool) {
		t.Helper()
		var out, stderr bytes.Buffer
		if got := showStatus(store, &out, &stderr); got != 0 || !strings.Contains(out.String(), "Device "+want+"\n") ||
			!strings.Contains(out.String(), "Firmware "+firmwareVersion+"\n") ||
			strings.Contains(out.String(), issuer) != showIssuer || stderr.Len() != 0 {
			t.Fatalf("status=%q stderr=%q", &out, &stderr)
		}
	}
	show(notInstalled, false)
	device, err := configuration.ParseDevice(bytes.NewReader(fixture(t, manifestFixture)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Manufacture(device, nil); err != nil {
		t.Fatal(err)
	}
	show(manufactured, false)
	// A manufactured service becomes ready even with an unusable engine address.
	if err := os.WriteFile(filepath.Join(store.Path, host.EngineState), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if result, err := executeManaged(t.Context(), store, nil); err != nil || !result.Applied {
		t.Fatalf("manufactured service was not ready: %+v %v", result, err)
	}
	devicePath, rootPath := filepath.Join(store.Path, host.DeviceState), filepath.Join(store.Path, host.RootSecret)
	manifestBefore, keyBefore := fixture(t, devicePath), fixture(t, rootPath)

	var progressMu sync.Mutex
	var progressLines []string
	progress := func(message string) {
		progressMu.Lock()
		defer progressMu.Unlock()
		progressLines = append(progressLines, message)
	}
	attempt := func(code string) (<-chan error, context.CancelFunc) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		done := make(chan error, 1)
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			_, err := enrollDevice(ctx, store, layout, issuer, code, time.Millisecond, progress)
			done <- err
		}()
		t.Cleanup(func() { cancel(); <-finished })
		select {
		case got := <-codes.staged:
			if got != code {
				t.Fatal("wrong code staged")
			}
		case err := <-done:
			t.Fatalf("attempt ended before staging: %v", err)
		case <-ctx.Done():
			t.Fatal("attempt never staged a code")
		}
		return done, cancel
	}
	observe := func(outcome dockerengine.EnrollmentOutcome) {
		t.Helper()
		for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
			err := store.Exclusive(func(session *configuration.Session) error {
				snapshot, err := session.Snapshot()
				if err != nil {
					return err
				}
				return observeEnrollment(session, snapshot.Enrollment, outcome)
			})
			if errors.Is(err, configuration.ErrBusy) {
				time.Sleep(time.Millisecond)
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			return
		}
		t.Fatal("enrollment did not release the operation lock while waiting")
	}
	firstCode := strings.Repeat("A", 16)
	secondCode := strings.Repeat("B", 128)
	done, cancel := attempt(firstCode)
	if _, err := installer.Lock(layout); err != installer.ErrBusy {
		t.Fatalf("an overlapping installation could overwrite enrollment: %v", err)
	}
	observe(dockerengine.EnrollmentFailed)
	if err := <-done; err != errEnrollmentFailed || codes.Held() {
		t.Fatalf("refusal left a code or returned the wrong result: %v", err)
	}
	cancel()
	show(enrollmentPending, false)
	if _, err := enrollDevice(t.Context(), store, layout, "https://other.example", "", time.Millisecond, func(string) {}); err != configuration.ErrIdentityConflict {
		t.Fatalf("a second issuer was accepted: %v", err)
	}
	done, cancel = attempt(secondCode)
	observe(dockerengine.EnrollmentFailed)
	if !codes.Held() {
		t.Fatal("a stale failed status discarded the second code")
	}
	show(enrollmentPending, false)
	observe(dockerengine.EnrollmentComplete)
	if err := <-done; err != nil || codes.Held() {
		t.Fatalf("retry failed or left a code: %v", err)
	}
	cancel()
	show(enrolled, true)
	progressMu.Lock()
	shown := strings.Join(progressLines, "\n")
	progressMu.Unlock()
	for _, want := range []string{"Reading the domain configuration", "waiting for the domain to confirm the enrollment", "The domain confirmed the enrollment"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("the wait did not report %q:\n%s", want, shown)
		}
	}
	if strings.Contains(shown, secondCode) {
		t.Fatal("progress printed the enrollment code")
	}

	if _, err := store.Manufacture(device, nil); err != nil {
		t.Fatal(err)
	}
	show(enrolled, true)
	if got, err := enrollDevice(t.Context(), store, layout, issuer, "", time.Millisecond, func(string) {}); err != nil || got != "attached" {
		t.Fatalf("retry lost attachment result: %q, %v", got, err)
	}
	if discoveries != 1 || checks != 2 {
		t.Fatalf("retry repeated initial enrollment checks: discovery=%d volume checks=%d", discoveries, checks)
	}
	if !bytes.Equal(manifestBefore, fixture(t, devicePath)) || !bytes.Equal(keyBefore, fixture(t, rootPath)) {
		t.Fatal("enrollment or reinstall changed the device record or root secret")
	}
	err = filepath.WalkDir(store.Path, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw := fixture(t, path)
		if bytes.Contains(raw, []byte(firstCode)) || bytes.Contains(raw, []byte(secondCode)) {
			t.Errorf("enrollment code was persisted in %s", entry.Name())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
