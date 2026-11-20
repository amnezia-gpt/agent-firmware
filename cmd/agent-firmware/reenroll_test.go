package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/host"
)

func TestFirstEnrollmentReportsCreated(t *testing.T) {
	enableTestRelease(t)
	const issuer = "https://auth.example"
	store, _ := manufacturedStore(t)
	layout := host.Layout{Lock: filepath.Join(t.TempDir(), "install.lock"), UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
	oldDiscover, oldCheck, oldProbe, oldCodes := discover, checkWorkloadVolumes, probeExistingEnrollment, openCodes
	t.Cleanup(func() {
		discover, checkWorkloadVolumes, probeExistingEnrollment, openCodes = oldDiscover, oldCheck, oldProbe, oldCodes
	})
	discover = func(context.Context, string) ([]byte, error) { return fixture(t, profileFixture), nil }
	checkWorkloadVolumes = func(_ context.Context, _ *configuration.Session, _ *configuration.Input, fresh bool) (bool, error) {
		if !fresh {
			t.Error("first enrollment was treated as an existing binding")
		}
		return false, nil
	}
	probeExistingEnrollment = func(context.Context, *configuration.Session, configuration.Snapshot) (dockerengine.EnrollmentOutcome, error) {
		t.Error("first enrollment tried to probe a nonexistent client")
		return "", dockerengine.ErrEnrollmentStatus
	}
	codes := &testCodes{staged: make(chan string, 1)}
	openCodes = func(host.Layout, string, bool) (codeStore, error) { return codes, nil }
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	progress := func(line string) {
		if !strings.HasPrefix(line, "Starting the workloads") {
			return
		}
		if !codes.Held() {
			t.Fatal("no code was handed over for first enrollment")
		}
		if err := store.Exclusive(func(s *configuration.Session) error {
			snapshot, err := s.Snapshot()
			if err != nil {
				return err
			}
			return observeEnrollment(s, snapshot.Enrollment, dockerengine.EnrollmentComplete)
		}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := enrollDevice(ctx, store, layout, issuer, []byte("sample_enrollment_code"), time.Millisecond, progress)
	if err != nil || result != (enrollmentResult{"enrolled", "created", "created"}) || codes.Held() {
		t.Fatalf("first enrollment result %+v, error %v", result, err)
	}
}

func TestEnrollUsesFreshProbeAndPreservesExistingDevice(t *testing.T) {
	enableTestRelease(t)
	const issuer = "https://auth.example"
	for _, tc := range []struct {
		name           string
		outcome        dockerengine.EnrollmentOutcome
		prior          configuration.IdentityReason
		withCode, held bool
		want           error
	}{
		{"working with code", dockerengine.EnrollmentComplete, configuration.IdentityConfirmed, true, false, errAlreadyEnrolled},
		{"working without code", dockerengine.EnrollmentComplete, configuration.IdentityConfirmed, false, false, errAlreadyEnrolled},
		{"lost", dockerengine.EnrollmentAbsent, configuration.IdentityConfirmed, true, false, nil},
		{"lost without code", dockerengine.EnrollmentAbsent, configuration.IdentityConfirmed, false, false, errEnrollmentCodeRequired},
		{"existing binding has never been confirmed", dockerengine.EnrollmentAbsent, configuration.IdentityObtaining, true, false, nil},
		{"code required", dockerengine.EnrollmentAbsent, configuration.IdentityObtaining, false, false, errEnrollmentCodeRequired},
		{"previous attempt refused", dockerengine.EnrollmentFailed, configuration.IdentityRefused, true, false, nil},
		{"refused without replacement code", dockerengine.EnrollmentFailed, configuration.IdentityRefused, false, false, errEnrollmentCodeRequired},
		{"replaces a held code", dockerengine.EnrollmentPending, configuration.IdentityObtaining, true, true, nil},
		{"unavailable with code", "", configuration.IdentityConfirmed, true, false, errWorkloadUnavailable},
		{"unavailable without code", "", configuration.IdentityConfirmed, false, false, errWorkloadUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := boundDevice(t)
			if err := store.Exclusive(func(s *configuration.Session) error {
				snapshot, err := s.Snapshot()
				if err != nil {
					return err
				}
				return s.SetIdentity(snapshot.Enrollment, tc.prior, time.Now(), true, tc.held)
			}); err != nil {
				t.Fatal(err)
			}
			layout := host.Layout{Lock: filepath.Join(t.TempDir(), "install.lock"), UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
			before, err := store.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			devicePath := filepath.Join(store.Path, host.DeviceState)
			device := fixture(t, devicePath)
			now := time.Now()
			keys, err := before.Identity().Identity(now)
			if err != nil {
				t.Fatal(err)
			}
			oldDiscover, oldCheck, oldProbe, oldCodes := discover, checkWorkloadVolumes, probeExistingEnrollment, openCodes
			t.Cleanup(func() {
				discover, checkWorkloadVolumes, probeExistingEnrollment, openCodes = oldDiscover, oldCheck, oldProbe, oldCodes
			})
			discover = func(context.Context, string) ([]byte, error) {
				t.Error("existing binding tried to discover a new profile")
				return nil, configuration.ErrInvalid
			}
			checkWorkloadVolumes = func(context.Context, *configuration.Session, *configuration.Input, bool) (bool, error) {
				t.Error("existing binding tried to provision volumes")
				return false, configuration.ErrInvalid
			}
			probes := 0
			probeExistingEnrollment = func(ctx context.Context, _ *configuration.Session, _ configuration.Snapshot) (dockerengine.EnrollmentOutcome, error) {
				probes++
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 30*time.Second || time.Until(deadline) < 25*time.Second {
					t.Error("existing identity probe is not bounded")
				}
				if tc.outcome == "" {
					return "", dockerengine.ErrEnrollmentStatus
				}
				return tc.outcome, nil
			}
			codes := &testCodes{held: tc.held, staged: make(chan string, 1)}
			openCodes = func(_ host.Layout, _ string, create bool) (codeStore, error) {
				if create && tc.want != nil {
					t.Error("refusal opened the code store for writing")
				}
				return codes, nil
			}
			var input []byte
			const code = "sample_enrollment_code"
			if tc.withCode {
				input = []byte(code)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			if tc.want != nil {
				if _, err := enrollDevice(ctx, store, layout, issuer, input, time.Millisecond, func(string) {}); err != tc.want || codes.Held() {
					t.Fatalf("want refusal %v; got %v, code held %v", tc.want, err, codes.Held())
				}
			} else {
				type completion struct {
					result enrollmentResult
					err    error
				}
				done := make(chan completion, 1)
				go func() {
					result, err := enrollDevice(ctx, store, layout, issuer, input, time.Millisecond, func(string) {})
					done <- completion{result, err}
				}()
				var received bool
				defer func() {
					cancel()
					if !received {
						<-done
					}
				}()
				select {
				case staged := <-codes.staged:
					if staged != code {
						t.Fatal("replacement code was not staged")
					}
				case result := <-done:
					received = true
					t.Fatalf("enrollment returned before staging: %v", result.err)
				case <-time.After(3 * time.Second):
					t.Fatal("enrollment did not hand over the code")
				}
				for {
					err := store.Exclusive(func(s *configuration.Session) error {
						current, err := s.Snapshot()
						if err != nil {
							return err
						}
						if current.Enrollment.Identity.Reason != configuration.IdentityObtaining || !current.Enrollment.RetryWithCode {
							t.Error("replacement did not start a renewed attempt")
						}
						return observeEnrollment(s, current.Enrollment, dockerengine.EnrollmentComplete)
					})
					if errors.Is(err, configuration.ErrBusy) && ctx.Err() == nil {
						time.Sleep(time.Millisecond)
						continue
					}
					if err != nil {
						t.Fatal(err)
					}
					break
				}
				result := <-done
				received = true
				if result.err != nil || result.result != (enrollmentResult{"enrolled", "renewed", "attached"}) || codes.Held() {
					t.Fatalf("renewal result: %+v", result)
				}
			}
			if probes != 1 || !bytes.Equal(input, make([]byte, len(input))) {
				t.Fatal("command skipped the fresh probe or retained the code in its input buffer")
			}
			after, err := store.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			currentKeys, err := after.Identity().Identity(now)
			if err != nil || keys.SigningKey != currentKeys.SigningKey || keys.EncryptionKey != currentKeys.EncryptionKey ||
				before.Enrollment.BoundAt != after.Enrollment.BoundAt || !bytes.Equal(device, fixture(t, devicePath)) {
				t.Fatal("attempt changed the device, binding or operational keys")
			}
		})
	}
}

func TestEnrollWaitsForGatewayAfterRestart(t *testing.T) {
	enableTestRelease(t)
	const issuer = "https://auth.example"
	for _, tc := range []struct {
		name       string
		ready      dockerengine.EnrollmentOutcome
		want       error
		wait       time.Duration
		wantProbes int
	}{
		{"identity lost after startup", dockerengine.EnrollmentAbsent, nil, time.Second, 2},
		{"session restored after startup", dockerengine.EnrollmentComplete, errAlreadyEnrolled, time.Second, 2},
		{"status still pending at deadline", dockerengine.EnrollmentPending, errWorkloadUnavailable, 30 * time.Second, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				store := boundDevice(t)
				if err := store.Exclusive(func(s *configuration.Session) error {
					return s.SetOutcome(issuer, configuration.Enrolled, false)
				}); err != nil {
					t.Fatal(err)
				}
				layout := host.Layout{Lock: filepath.Join(t.TempDir(), "install.lock"), UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
				codes := &testCodes{staged: make(chan string, 1)}
				oldProbe, oldCodes := probeExistingEnrollment, openCodes
				t.Cleanup(func() { probeExistingEnrollment, openCodes = oldProbe, oldCodes })
				writes, probes, notices := 0, 0, 0
				openCodes = func(_ host.Layout, _ string, create bool) (codeStore, error) {
					if create {
						writes++
					}
					return codes, nil
				}
				probeExistingEnrollment = func(context.Context, *configuration.Session, configuration.Snapshot) (dockerengine.EnrollmentOutcome, error) {
					probes++
					if writes != 0 || codes.Held() {
						t.Fatal("the code reached the workload before its status was known")
					}
					if probes == 1 {
						return dockerengine.EnrollmentPending, nil
					}
					return tc.ready, nil
				}
				progress := func(line string) {
					switch line {
					case "Waiting for the workload to confirm its enrollment status":
						notices++
						if err := store.Exclusive(func(*configuration.Session) error { return nil }); err != nil {
							t.Fatalf("waiting for Gateway held the configuration lock: %v", err)
						}
					case "Waiting for the domain to confirm the renewed enrollment":
						if !codes.Held() {
							t.Fatal("no code was staged for renewal")
						}
						if err := store.Exclusive(func(s *configuration.Session) error {
							current, err := s.Snapshot()
							if err != nil {
								return err
							}
							return observeEnrollment(s, current.Enrollment, dockerengine.EnrollmentComplete)
						}); err != nil {
							t.Fatal(err)
						}
					}
				}
				started := time.Now()
				code := []byte("sample_enrollment_code")
				result, err := enrollDevice(t.Context(), store, layout, issuer, code, time.Second, progress)
				if err != tc.want || time.Since(started) != tc.wait || probes != tc.wantProbes || notices != 1 {
					t.Fatalf("result=%+v err=%v wait=%v probes=%d notices=%d", result, err, time.Since(started), probes, notices)
				}
				if tc.want == nil {
					if result != (enrollmentResult{"enrolled", "renewed", "attached"}) || writes != 1 {
						t.Fatalf("renewal result=%+v writes=%d", result, writes)
					}
				} else if writes != 0 {
					t.Fatal("a refusal wrote the code")
				}
				if codes.Held() || !bytes.Equal(code, make([]byte, len(code))) {
					t.Fatal("enrollment retained the code")
				}
			})
		})
	}
}

func TestEnrollmentRefusalHintsPrecedeJSON(t *testing.T) {
	for failure, hint := range map[error]string{
		errAlreadyEnrolled:        "The device is enrolled and working. The code was not used.",
		errEnrollmentCodeRequired: newEnrollmentCommand,
		errWorkloadUnavailable:    checkService,
	} {
		var stderr bytes.Buffer
		want := "agent-firmware: " + operatorText(hint) + "\n" + `{"error_code":"` + failure.Error() + `"}` + "\n"
		if report(&stderr, failure.Error(), 1) != 1 || strings.TrimSpace(stderr.String()) != strings.TrimSpace(want) {
			t.Fatalf("invalid hint for %v: %q", failure, &stderr)
		}
	}
}

func TestEnrollmentFailureOffersRetryWithExistingInstallation(t *testing.T) {
	for _, failure := range []error{errEnrollmentFailed, errEnrollmentTimeout} {
		var stderr bytes.Buffer
		want := "agent-firmware: If the enrollment code expired, get a fresh code from your device page.\n" +
			"agent-firmware: Retry only the enroll command on the installed firmware.\n" +
			`{"error_code":"` + failure.Error() + `"}` + "\n"
		if report(&stderr, failure.Error(), 1) != 1 || stderr.String() != want {
			t.Fatalf("unexpected enrollment diagnostic: %q", &stderr)
		}
	}
}
