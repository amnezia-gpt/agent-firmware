package configuration

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"

	"github.com/amnezia-gpt/agent-firmware/internal/backendprofile"
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
}

type enrollmentRecord struct {
	Version       int     `json:"schema_version"`
	Profile       []byte  `json:"backend_profile"`
	Outcome       Outcome `json:"outcome"`
	RetryWithCode bool    `json:"retry_with_code"`
}

func (e *Enrollment) Input(d *Device) *Input { return d.withProfile(e.Profile, e.raw) }

func enrollmentRoot(root *os.Root, create bool) (*os.Root, error) {
	if create {
		if err := root.Mkdir(host.EnrollmentDir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, ErrWrite
		}
		if err := privatefile.SyncDirectory(root, ".", nil); err != nil {
			return nil, ErrWrite
		}
	}
	info, err := root.Lstat(host.EnrollmentDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrMissing
	}
	owner := privatefile.EffectiveOwner()
	if err != nil || !owner.Directory(info, 0700) {
		return nil, ErrUnsafe
	}
	dir, err := root.OpenRoot(host.EnrollmentDir)
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

func loadEnrollment(root *os.Root) (*Enrollment, error) {
	dir, err := enrollmentRoot(root, false)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	data, err := readRecord(dir, enrollmentState)
	if err != nil {
		return nil, err
	}
	fields, ok := strictjson.Object(data, "schema_version", "backend_profile", "outcome", "retry_with_code")
	retry := string(bytes.TrimSpace(fields["retry_with_code"]))
	if !ok || string(bytes.TrimSpace(fields["schema_version"])) != "1" || retry != "true" && retry != "false" {
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
	return &Enrollment{Profile: profile, Outcome: record.Outcome, RetryWithCode: record.RetryWithCode, raw: record.Profile}, nil
}

func (s *Session) writeEnrollment(raw []byte, outcome Outcome, retryWithCode bool) error {
	dir, err := enrollmentRoot(s.root, true)
	if err != nil {
		return err
	}
	defer dir.Close()
	data, err := json.Marshal(enrollmentRecord{1, raw, outcome, retryWithCode})
	if err != nil {
		return ErrWrite
	}
	return s.store.atomic(dir, enrollmentState, data)
}

func (s *Session) BeginEnrollment(input *Input) error {
	device, err := loadDevice(s.root)
	if err != nil {
		return err
	}
	if input == nil || !bytes.Equal(device.raw, input.releaseBytes) {
		return ErrInvalid
	}
	existing, err := loadEnrollment(s.root)
	if err == nil {
		if existing.Profile.AuthIssuer != input.profile.AuthIssuer {
			return ErrIdentityConflict
		}
		return nil
	}
	if !errors.Is(err, ErrMissing) {
		return err
	}
	return s.writeEnrollment(input.profileBytes, Pending, false)
}

func (s *Session) SetOutcome(issuer string, outcome Outcome, retryWithCode bool) error {
	if outcome != Pending && outcome != Failed && outcome != Enrolled || outcome != Pending && retryWithCode {
		return ErrInvalid
	}
	existing, err := loadEnrollment(s.root)
	if err != nil {
		return err
	}
	if issuer != existing.Profile.AuthIssuer {
		return ErrIdentityConflict
	}
	if existing.Outcome == Enrolled || existing.Outcome == outcome && existing.RetryWithCode == retryWithCode {
		return nil
	}
	return s.writeEnrollment(existing.raw, outcome, retryWithCode)
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
	return s.writeEnrollment(raw, existing.Outcome, existing.RetryWithCode)
}
