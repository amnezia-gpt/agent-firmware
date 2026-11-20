package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/deviceapi"
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/host"
)

func manufacturedStore(t *testing.T) (configuration.Store, *configuration.Device) {
	t.Helper()
	store := configuration.Store{Path: filepath.Join(t.TempDir(), "state")}
	device, err := configuration.ParseDevice(bytes.NewReader(fixture(t, manifestFixture)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Manufacture(device, nil); err != nil {
		t.Fatal(err)
	}
	return store, device
}

func boundDevice(t *testing.T) configuration.Store {
	t.Helper()
	store, device := manufacturedStore(t)
	input, err := device.WithProfile(fixture(t, profileFixture))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Exclusive(func(s *configuration.Session) error { return s.BeginEnrollment(input, true) }); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestEnrollmentProbeTransitions(t *testing.T) {
	const (
		obtaining = configuration.IdentityObtaining
		confirmed = configuration.IdentityConfirmed
		lost      = configuration.IdentityLost
		refused   = configuration.IdentityRefused
		required  = configuration.IdentityRequired
	)
	for _, tc := range []struct {
		name        string
		before      configuration.IdentityReason
		result      dockerengine.EnrollmentOutcome
		held, retry bool
		want        configuration.IdentityReason
		keepsCode   bool
	}{
		{"enrolled", obtaining, dockerengine.EnrollmentComplete, true, false, confirmed, false},
		{"still enrolled", confirmed, dockerengine.EnrollmentComplete, false, false, confirmed, false},
		{"pending with code", obtaining, dockerengine.EnrollmentPending, true, true, obtaining, true},
		{"pending without code during cleanup", confirmed, dockerengine.EnrollmentPending, false, false, "", false},
		{"code refused", obtaining, dockerengine.EnrollmentFailed, true, false, refused, false},
		{"consumed code refused", obtaining, dockerengine.EnrollmentFailed, false, true, refused, false},
		{"previous code refused", obtaining, dockerengine.EnrollmentFailed, true, true, "", true},
		{"lost", confirmed, dockerengine.EnrollmentAbsent, false, false, lost, false},
		{"still lost", lost, dockerengine.EnrollmentAbsent, false, false, lost, false},
		{"lost during cleanup", lost, dockerengine.EnrollmentPending, false, false, "", false},
		{"never enrolled", obtaining, dockerengine.EnrollmentAbsent, false, false, required, false},
		{"awaiting new code pickup", obtaining, dockerengine.EnrollmentAbsent, true, true, "", true},
		{"no answer", confirmed, "", false, false, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := boundDevice(t)
			codes := &testCodes{held: tc.held}
			original := openCodes
			openCodes = func(host.Layout, string, bool) (codeStore, error) { return codes, nil }
			t.Cleanup(func() { openCodes = original })
			first := time.Now().UTC().Truncate(time.Second).Add(time.Minute)
			probe := first.Add(30 * time.Second)
			if err := store.Exclusive(func(s *configuration.Session) error {
				initial, err := s.Snapshot()
				if err != nil {
					return err
				}
				if tc.before == lost {
					if err := s.SetIdentity(initial.Enrollment, confirmed, first.Add(-time.Second), true, false); err != nil {
						return err
					}
					initial, err = s.Snapshot()
					if err != nil {
						return err
					}
				}
				return s.SetIdentity(initial.Enrollment, tc.before, first, true, tc.retry)
			}); err != nil {
				t.Fatal(err)
			}
			previous, err := store.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(store.Path, host.EnrollmentDir, host.EnrollmentRecord)
			raw := fixture(t, path)
			if err := store.Exclusive(func(s *configuration.Session) error {
				got, err := recordEnrollmentProbe(s, previous.Enrollment, host.Layout{}, tc.result, probe)
				if got != tc.want {
					t.Errorf("reason = %q, want %q", got, tc.want)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			current, err := store.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			condition := current.Enrollment.Identity
			if tc.want == "" {
				if condition != previous.Enrollment.Identity || !bytes.Equal(raw, fixture(t, path)) {
					t.Fatal("inconclusive result changed the record")
				}
			} else {
				transition := previous.Enrollment.Identity.LastTransitionTime
				if tc.want != tc.before {
					transition = probe
				}
				if condition.Reason != tc.want || condition.LastProbeTime != probe || condition.LastTransitionTime != transition ||
					(condition.Status == "true") != (tc.want == confirmed) || current.Enrollment.RetryWithCode {
					t.Fatalf("invalid recorded condition: %+v", condition)
				}
			}
			if codes.Held() != tc.keepsCode {
				t.Fatal("code disposition does not match the probe result")
			}
		})
	}
}

func TestStatusDocumentAndTextShareDevicePhase(t *testing.T) {
	store := boundDevice(t)
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	path := filepath.Join(store.Path, host.EnrollmentDir, host.EnrollmentRecord)
	raw := fixture(t, path)
	type phaseCase struct {
		phase, heading, step, hint string
		snapshot                   configuration.Snapshot
	}
	cases := []phaseCase{
		{"not_installed", "not installed", "install", "Run the firmware installation command.", configuration.Snapshot{}},
		{"manufactured", "manufactured and not enrolled", "enroll", "Run the installation command from your account to enroll this device.", configuration.Snapshot{Device: snapshot.Device}},
		{"removing", "removing", "repeat_removal_command", "Repeat the removal command to finish removing the device.", configuration.Snapshot{Decommissioning: true}},
	}
	for _, tc := range []struct {
		phase, heading, step, hint string
		condition                  configuration.IdentityCondition
	}{
		{"enrolling", "enrolling", "wait", "Wait for the domain to confirm the enrollment.", configuration.IdentityCondition{Status: "false", Reason: configuration.IdentityObtaining, LastProbeTime: now}},
		{"enrollment_failed", "enrollment failed", "new_enrollment_command", newEnrollmentCommand, configuration.IdentityCondition{Status: "false", Reason: configuration.IdentityRefused, LastProbeTime: now}},
		{"enrollment_failed", "enrollment failed", "new_enrollment_command", newEnrollmentCommand, configuration.IdentityCondition{Status: "false", Reason: configuration.IdentityRequired, LastProbeTime: now}},
		{"identity_lost", "identity lost", "new_enrollment_command", newEnrollmentCommand, configuration.IdentityCondition{Status: "false", Reason: configuration.IdentityLost, LastProbeTime: now}},
		{"enrolled", "enrolled", "none", "", configuration.IdentityCondition{Status: "true", Reason: configuration.IdentityConfirmed, LastProbeTime: now}},
		{"enrolled_unverified", "enrolled (identity unverified)", "check_service", "Check the firmware service with 'systemctl status agent-firmware'.", configuration.IdentityCondition{Status: "false", Reason: configuration.IdentityLost, LastProbeTime: now.Add(-121 * time.Second)}},
	} {
		e := *snapshot.Enrollment
		e.Identity = tc.condition
		if hostLayout.UserService && tc.step == "check_service" {
			tc.hint = "Check the firmware LaunchAgent in your login session and make sure Docker Desktop is running."
		}
		cases = append(cases, phaseCase{tc.phase, tc.heading, tc.step, tc.hint, configuration.Snapshot{Device: snapshot.Device, Enrollment: &e}})
	}
	for _, tc := range cases {
		var observed runtimeStatus
		if tc.snapshot.Enrollment != nil {
			observed = *observedRuntime(tc.snapshot, deviceapi.LinkEvent{State: "connected"}, nil, now)
		}
		doc := describeStatus(tc.snapshot, observed, []dockerengine.VolumeInfo{}, now)
		wire, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{`"schema_version":1`, `"phase":"` + tc.phase + `"`, `"next_step":"` + tc.step + `"`, `"user_data":[]`} {
			if !bytes.Contains(wire, []byte(field)) {
				t.Fatalf("status missing %s: %s", field, wire)
			}
		}
		if tc.snapshot.Enrollment != nil && (doc.Issuer != tc.snapshot.Enrollment.Profile.AuthIssuer || len(doc.Conditions) != 5 ||
			doc.Conditions[3].Reason != "converged" || doc.Conditions[4].Reason != "connected") {
			t.Fatalf("bound conditions missing: %s", wire)
		}
		if tc.phase == "enrolled_unverified" && (doc.Conditions[2].Status != "unknown" || doc.Conditions[2].Reason != "not_observed") {
			t.Fatalf("stale condition appears current: %s", wire)
		}
		if text := doc.text(); !strings.HasPrefix(text, "Device "+tc.heading+"\n") || tc.hint != "" && !strings.HasSuffix(text, tc.hint+"\n") {
			t.Fatalf("text and JSON diverge for %s: %q", tc.phase, text)
		}
	}
	if !bytes.Equal(raw, fixture(t, path)) {
		t.Fatal("read-only status changed the persisted condition")
	}
}

func TestStatusJSONFlagPrintsVersionedDocument(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("status requires root")
	}
	stubStatusData(t)
	store := boundDevice(t)
	if err := store.Exclusive(func(s *configuration.Session) error {
		return s.SetOutcome("https://auth.example", configuration.Enrolled, false)
	}); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if exit := run([]string{"status", "--state-dir", store.Path, "--json"}, strings.NewReader(""), &out, &stderr); exit != 0 || stderr.Len() != 0 {
		t.Fatalf("status exit %d: %s", exit, stderr.String())
	}
	var doc deviceStatus
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil || doc.SchemaVersion != 1 || doc.Phase != "enrolled" ||
		doc.IDevID == "" || doc.WorkloadRelease == "" || len(doc.Conditions) != 5 || doc.UserData == nil {
		t.Fatalf("unexpected versioned status %s, error %v", out.Bytes(), err)
	}
}
