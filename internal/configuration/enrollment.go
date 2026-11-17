package configuration

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"

	"github.com/amnezia-gpt/agent-firmware/internal/backendprofile"
	"github.com/amnezia-gpt/agent-firmware/internal/deviceidentity"
	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/privatefile"
	"github.com/amnezia-gpt/agent-firmware/internal/strictjson"
)

const enrollmentState = "profile.json"

type Outcome string

const (
	Pending  Outcome = "pending"
	Failed   Outcome = "failed"
	Enrolled Outcome = "enrolled"
)

type Enrollment struct {
	Profile       backendprofile.Profile
	Outcome       Outcome
	RetryWithCode bool
	raw           []byte
	keys          *deviceidentity.Operational
	sealedKeys    string
}

type enrollmentRecord struct {
	Version       int     `json:"schema_version"`
	Profile       []byte  `json:"backend_profile"`
	Outcome       Outcome `json:"outcome"`
	RetryWithCode bool    `json:"retry_with_code"`
	Keys          string  `json:"operational_keys"`
}

func (e *Enrollment) Input(d *Device) *Input { return d.withProfile(e.Profile, e.raw) }

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
	fields, ok := strictjson.Object(data, "schema_version", "backend_profile", "outcome", "retry_with_code", "operational_keys")
	if !ok {
		if old, valid := strictjson.Members(data); valid && old["operational_keys"] == nil {
			return nil, ErrRootSecret
		}
		return nil, ErrCorrupt
	}
	retry := string(bytes.TrimSpace(fields["retry_with_code"]))
	if string(bytes.TrimSpace(fields["schema_version"])) != "2" || retry != "true" && retry != "false" {
		return nil, ErrCorrupt
	}
	var record enrollmentRecord
	if json.Unmarshal(data, &record) != nil ||
		record.Outcome != Pending && record.Outcome != Failed && record.Outcome != Enrolled ||
		record.Outcome != Pending && record.RetryWithCode {
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
		raw: record.Profile, keys: keys, sealedKeys: record.Keys}, nil
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

func (s *Session) BeginEnrollment(input *Input) error {
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
	return s.createEnrollment(enrollmentRecord{2, input.profileBytes, Pending, false, keys})
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
	return s.writeEnrollment(host.EnrollmentDir, enrollmentRecord{2, existing.raw, outcome, retryWithCode, existing.sealedKeys})
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
	return s.writeEnrollment(host.EnrollmentDir, enrollmentRecord{2, raw, existing.Outcome, existing.RetryWithCode, existing.sealedKeys})
}
