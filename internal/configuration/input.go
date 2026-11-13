// Package configuration validates and retains a complete supplied installation input.
package configuration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"

	"github.com/amnezia-gpt/agent-firmware/internal/backendprofile"
	"github.com/amnezia-gpt/agent-firmware/internal/workloadrelease"
)

type Error string

func (e Error) Error() string { return string(e) }

const (
	ErrInvalid           Error = "invalid_configuration"
	ErrMissing           Error = "not_configured"
	ErrUnsafe            Error = "unsafe_state_storage"
	ErrRead              Error = "state_read_failed"
	ErrWrite             Error = "state_write_failed"
	ErrCorrupt           Error = "corrupt_state"
	ErrBusy              Error = "operation_busy"
	ErrPending           Error = "operation_recovery_required"
	ErrUncertain         Error = "state_commit_uncertain"
	ErrContractDowngrade Error = "workload_contract_downgrade"
	ErrIdentityConflict  Error = "identity_configuration_conflict"
)

// Input can only be constructed by validating both supplied documents.
type Input struct {
	profile      backendprofile.Profile
	release      workloadrelease.Release
	profileBytes []byte
	releaseBytes []byte
	generation   string
}

func (i *Input) Generation() string               { return i.generation }
func (i *Input) Profile() backendprofile.Profile  { return i.profile }
func (i *Input) Release() workloadrelease.Release { return i.release }

func Parse(profile, release io.Reader) (*Input, error) {
	p, err := io.ReadAll(io.LimitReader(profile, backendprofile.MaxProfileBytes+1))
	if err != nil {
		return nil, backendprofile.ErrRead
	}
	if len(p) > backendprofile.MaxProfileBytes {
		return nil, backendprofile.ErrTooLarge
	}
	r, err := io.ReadAll(io.LimitReader(release, workloadrelease.MaxBytes+1))
	if err != nil {
		return nil, workloadrelease.ErrRead
	}
	if len(r) > workloadrelease.MaxBytes {
		return nil, workloadrelease.ErrTooLarge
	}
	parsedProfile, err := backendprofile.Parse(bytes.NewReader(p))
	if err != nil {
		return nil, err
	}
	parsedRelease, err := workloadrelease.Parse(bytes.NewReader(r))
	if err != nil {
		return nil, err
	}
	canonical, _ := json.Marshal(struct {
		Profile backendprofile.Profile
		Release workloadrelease.Release
	}{parsedProfile, parsedRelease})
	digest := sha256.Sum256(append([]byte("agent-firmware-config-v1\x00"), canonical...))
	return &Input{parsedProfile, parsedRelease, p, r, "sha256:" + hex.EncodeToString(digest[:])}, nil
}
