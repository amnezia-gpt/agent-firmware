package configuration

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"

	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/strictjson"
)

const ErrRemovalPending Error = "enrollment_removal_pending"
const removalFile = "enrollment-removal.json"
const detachedEnrollment = "removed-enrollment"

const (
	DeregistrationPending     = "pending"
	Deregistered              = "deregistered"
	DeregistrationFailed      = "failed"
	DeregistrationUnavailable = "unavailable"
)

var enrollmentIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type Removal struct {
	Version        int    `json:"schema_version"`
	Installation   string `json:"installation"`
	Deregistration string `json:"deregistration"`
}

func loadRemoval(root *os.Root) (*Removal, error) {
	raw, err := readRecord(root, removalFile)
	if errors.Is(err, ErrMissing) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	fields, ok := strictjson.Object(raw, "schema_version", "installation", "deregistration")
	var removal Removal
	if !ok || string(fields["schema_version"]) != "1" || json.Unmarshal(raw, &removal) != nil ||
		!enrollmentIDPattern.MatchString(removal.Installation) || !validDeregistration(removal.Deregistration) {
		return nil, ErrCorrupt
	}
	return &removal, nil
}

func validDeregistration(outcome string) bool {
	switch outcome {
	case DeregistrationPending, Deregistered, DeregistrationFailed, DeregistrationUnavailable:
		return true
	default:
		return false
	}
}

func (s *Session) BeginRemoval(id string) (*Removal, error) {
	if !enrollmentIDPattern.MatchString(id) {
		return nil, ErrInvalid
	}
	if pending, err := loadRemoval(s.root); err != nil || pending != nil {
		return pending, err
	}
	removal := &Removal{1, id, DeregistrationPending}
	return removal, s.writeRemoval(removal)
}

func (s *Session) SetDeregistration(removal *Removal, outcome string) error {
	if !validDeregistration(outcome) || outcome == DeregistrationPending {
		return ErrInvalid
	}
	next := *removal
	next.Deregistration = outcome
	if err := s.writeRemoval(&next); err != nil {
		return err
	}
	*removal = next
	return nil
}

func (s *Session) writeRemoval(removal *Removal) error {
	raw, err := json.Marshal(removal)
	if err != nil {
		return ErrWrite
	}
	return s.store.atomic(s.root, removalFile, raw)
}

// FinishRemoval is called only after owned engine and runtime resources are
// gone. The marker stays present until both the journal and the detached keys
// have been durably removed.
func (s *Session) FinishRemoval(installation string) error {
	removal, err := loadRemoval(s.root)
	if err != nil {
		return err
	}
	if removal == nil || removal.Installation != installation || removal.Deregistration == DeregistrationPending {
		return ErrRemovalPending
	}
	dir, err := enrollmentRoot(s.root, host.EnrollmentDir)
	if err == nil {
		dir.Close()
		if _, err := s.root.Lstat(detachedEnrollment); !errors.Is(err, os.ErrNotExist) {
			return ErrCorrupt
		}
		if s.root.Rename(host.EnrollmentDir, detachedEnrollment) != nil {
			return ErrWrite
		}
		if err := s.store.synchronize(s.root); err != nil {
			return err
		}
	} else if !errors.Is(err, ErrMissing) {
		return err
	}
	if s.root.RemoveAll(detachedEnrollment) != nil {
		return ErrWrite
	}
	if err := s.Write(nil); err != nil {
		return err
	}
	if err := s.store.synchronize(s.root); err != nil {
		return err
	}
	if s.root.Remove(removalFile) != nil {
		return ErrWrite
	}
	return s.store.synchronize(s.root)
}
