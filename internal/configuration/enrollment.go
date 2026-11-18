package configuration

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/backendprofile"
	"github.com/amnezia-gpt/agent-firmware/internal/deviceidentity"
	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/privatefile"
	"github.com/amnezia-gpt/agent-firmware/internal/strictjson"
)

const enrollmentState = host.EnrollmentRecord

type Outcome string

const (
	Pending  Outcome = "pending"
	Failed   Outcome = "failed"
	Enrolled Outcome = "enrolled"
)

type Enrollment struct {
	Profile          backendprofile.Profile
	Outcome          Outcome
	RetryWithCode    bool
	AttachedUserData bool
	Identity         IdentityCondition
	BoundAt          time.Time
	raw              []byte
	keys             *deviceidentity.Operational
	sealedKeys       string
}

type enrollmentRecord struct {
	Version          int               `json:"schema_version"`
	Profile          []byte            `json:"backend_profile"`
	Outcome          Outcome           `json:"outcome"`
	RetryWithCode    bool              `json:"retry_with_code"`
	Keys             string            `json:"operational_keys"`
	AttachedUserData bool              `json:"attached_user_data"`
	Identity         IdentityCondition `json:"identity_condition"`
	BoundAt          time.Time         `json:"bound_at"`
}

func (e *Enrollment) Input(d *Device) *Input { return d.withProfile(e.Profile, e.raw) }

func (e *Enrollment) record() enrollmentRecord {
	return enrollmentRecord{Version: 3, Profile: e.raw, Outcome: e.Outcome, RetryWithCode: e.RetryWithCode,
		Keys: e.sealedKeys, AttachedUserData: e.AttachedUserData, Identity: e.Identity, BoundAt: e.BoundAt}
}

func enrollmentRoot(root *os.Root, name string) (*os.Root, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrMissing
	}
	owner := privatefile.EffectiveOwner()
	if err != nil || !owner.Directory(info, 0700) {
		return nil, ErrUnsafe
	}
	dir, err := root.OpenRoot(name)
	if err != nil {
		return nil, ErrUnsafe
	}
	opened, err := dir.Stat(".")
	if err != nil || !os.SameFile(info, opened) || !owner.Directory(opened, 0700) {
		dir.Close()
		return nil, ErrUnsafe
	}
	return dir, nil
}

func loadEnrollment(root *os.Root, device *Device) (*Enrollment, error) {
	dir, err := enrollmentRoot(root, host.EnrollmentDir)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	data, err := readRecord(dir, enrollmentState)
	if errors.Is(err, ErrMissing) {
		return nil, ErrRootSecret
	}
	if err != nil {
		return nil, err
	}
	fields, ok := strictjson.Members(data)
	if !ok {
		return nil, ErrCorrupt
	}
	if fields["operational_keys"] == nil {
		return nil, ErrRootSecret
	}
	names := []string{"schema_version", "backend_profile", "outcome", "retry_with_code", "operational_keys", "attached_user_data"}
	version := string(bytes.TrimSpace(fields["schema_version"]))
	if version == "3" {
		names = append(names, "identity_condition", "bound_at")
	}
	if len(fields) != len(names) || version != "2" && version != "3" {
		return nil, ErrCorrupt
	}
	for _, name := range names {
		if fields[name] == nil {
			return nil, ErrCorrupt
		}
	}
	retry := string(bytes.TrimSpace(fields["retry_with_code"]))
	attached := string(bytes.TrimSpace(fields["attached_user_data"]))
	if retry != "true" && retry != "false" || attached != "true" && attached != "false" {
		return nil, ErrCorrupt
	}
	var record enrollmentRecord
	if json.Unmarshal(data, &record) != nil ||
		record.Outcome != Pending && record.Outcome != Failed && record.Outcome != Enrolled {
		return nil, ErrCorrupt
	}
	if record.Version == 2 {
		record.Identity = legacyIdentity(record.Outcome)
	} else if !validIdentityRecord(fields["identity_condition"], record) {
		return nil, ErrCorrupt
	}
	if record.RetryWithCode && record.Identity.Reason != IdentityObtaining {
		return nil, ErrCorrupt
	}
	profile, err := backendprofile.Parse(bytes.NewReader(record.Profile))
	if err != nil {
		return nil, ErrCorrupt
	}
	secret, err := checkedRootSecret(root, device.rootFingerprint)
	if err != nil {
		return nil, err
	}
	defer clear(secret)
	keys, err := deviceidentity.OpenOperational(secret, profile.AuthIssuer, record.Keys)
	if err != nil {
		return nil, ErrRootSecret
	}
	return &Enrollment{Profile: profile, Outcome: record.Outcome, RetryWithCode: record.RetryWithCode,
		raw: record.Profile, keys: keys, sealedKeys: record.Keys, AttachedUserData: record.AttachedUserData,
		Identity: record.Identity, BoundAt: record.BoundAt}, nil
}

func validIdentityRecord(raw []byte, record enrollmentRecord) bool {
	fields, ok := strictjson.Members(raw)
	condition := record.Identity
	if !ok || fields["status"] == nil || fields["reason"] == nil || condition.Status == "" ||
		conditionStatus(condition.Reason) != condition.Status ||
		(condition.Reason == IdentityConfirmed || condition.Reason == IdentityLost) && record.Outcome != Enrolled ||
		condition.Reason == IdentityRequired && record.Outcome == Enrolled {
		return false
	}
	for name := range fields {
		switch name {
		case "status", "reason", "last_transition_time", "last_probe_time":
		default:
			return false
		}
	}
	for _, at := range []time.Time{record.BoundAt, condition.LastTransitionTime, condition.LastProbeTime} {
		if !at.IsZero() && (at.Location() != time.UTC || at.Nanosecond() != 0) {
			return false
		}
	}
	return true
}

func (s *Session) writeEnrollment(dirName string, record enrollmentRecord) error {
	dir, err := enrollmentRoot(s.root, dirName)
	if err != nil {
		return err
	}
	defer dir.Close()
	data, err := json.Marshal(record)
	if err != nil {
		return ErrWrite
	}
	return s.store.atomic(dir, enrollmentState, data)
}

func (s *Session) createEnrollment(record enrollmentRecord) error {
	// Publish the directory and its first key record together. An existing
	// directory without its record is damaged state, never a new enrollment.
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return ErrWrite
	}
	staged := "pending-" + hex.EncodeToString(nonce[:])
	if err := s.root.Mkdir(staged, 0700); err != nil {
		return ErrWrite
	}
	defer s.root.RemoveAll(staged)
	if err := s.writeEnrollment(staged, record); err != nil {
		return err
	}
	if err := s.root.Rename(staged, host.EnrollmentDir); err != nil {
		return ErrWrite
	}
	return s.store.synchronize(s.root)
}

func (s *Session) BeginEnrollment(input *Input, attachedUserData bool) error {
	if removal, err := loadRemoval(s.root); err != nil {
		return err
	} else if removal != nil {
		return ErrRemovalPending
	}
	device, err := loadDevice(s.root)
	if err != nil {
		return err
	}
	if input == nil || !bytes.Equal(device.raw, input.releaseBytes) {
		return ErrInvalid
	}
	existing, err := loadEnrollment(s.root, device)
	if err == nil {
		if existing.Profile.AuthIssuer != input.profile.AuthIssuer {
			return ErrIdentityConflict
		}
		return nil
	}
	if !errors.Is(err, ErrMissing) {
		return err
	}
	secret, err := checkedRootSecret(s.root, device.rootFingerprint)
	if err != nil {
		return err
	}
	defer clear(secret)
	keys, err := deviceidentity.GenerateOperational(secret, input.profile.AuthIssuer)
	if err != nil {
		return ErrRootSecret
	}
	now := time.Now().UTC().Truncate(time.Second)
	identity := legacyIdentity(Pending)
	identity.LastTransitionTime = now
	return s.createEnrollment(enrollmentRecord{
		Version: 3, Profile: input.profileBytes, Outcome: Pending, Keys: keys, AttachedUserData: attachedUserData,
		Identity: identity, BoundAt: now,
	})
}

func (s *Session) SetOutcome(issuer string, outcome Outcome, retryWithCode bool) error {
	if outcome != Pending && outcome != Failed && outcome != Enrolled || outcome != Pending && retryWithCode {
		return ErrInvalid
	}
	snapshot, err := s.Snapshot()
	if err != nil {
		return err
	}
	existing := snapshot.Enrollment
	if existing == nil || issuer != existing.Profile.AuthIssuer {
		return ErrIdentityConflict
	}
	if existing.Outcome == Enrolled || existing.Outcome == outcome && existing.RetryWithCode == retryWithCode {
		return nil
	}
	return s.SetIdentity(existing, legacyIdentity(outcome).Reason, time.Now(), outcome != Pending, retryWithCode)
}

// RefreshEnrollment updates only the discovered resources. The identity and
// trust bundle selected at enrollment remain authoritative.
func (s *Session) RefreshEnrollment(existing *Enrollment, raw []byte) error {
	profile, err := backendprofile.Parse(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	previous := existing.Profile
	if profile.AuthIssuer != previous.AuthIssuer || profile.ID != previous.ID ||
		profile.IngressTrustBundle != previous.IngressTrustBundle || profile.IngressTrustSHA256 != previous.IngressTrustSHA256 {
		return ErrIdentityConflict
	}
	record := existing.record()
	record.Profile = raw
	return s.writeEnrollment(host.EnrollmentDir, record)
}
