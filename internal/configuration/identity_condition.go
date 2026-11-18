package configuration

import (
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/host"
)

const IdentityObservationAge = 2 * time.Minute

type IdentityReason string

const (
	IdentityConfirmed  IdentityReason = "confirmed"
	IdentityObtaining  IdentityReason = "enrollment_in_progress"
	IdentityRefused    IdentityReason = "code_refused"
	IdentityLost       IdentityReason = "identity_lost"
	IdentityRequired   IdentityReason = "enrollment_required"
	IdentityUnobserved IdentityReason = "not_observed"
)

type IdentityCondition struct {
	Status             string         `json:"status"`
	Reason             IdentityReason `json:"reason"`
	LastTransitionTime time.Time      `json:"last_transition_time,omitzero"`
	LastProbeTime      time.Time      `json:"last_probe_time,omitzero"`
}

func conditionStatus(reason IdentityReason) string {
	switch reason {
	case IdentityConfirmed:
		return "true"
	case IdentityObtaining, IdentityRefused, IdentityLost, IdentityRequired:
		return "false"
	default:
		return ""
	}
}

// Observed returns the display condition without changing the recorded value.
func (c IdentityCondition) Observed(now time.Time) IdentityCondition {
	at := c.LastProbeTime
	if at.IsZero() && c.Reason == IdentityObtaining {
		// A locally started attempt is known while it awaits its first probe.
		at = c.LastTransitionTime
	}
	if at.IsZero() || now.Before(at) || now.Sub(at) > IdentityObservationAge {
		c.Status, c.Reason = "unknown", IdentityUnobserved
	}
	return c
}

func legacyIdentity(outcome Outcome) IdentityCondition {
	reason := IdentityObtaining
	switch outcome {
	case Enrolled:
		reason = IdentityConfirmed
	case Failed:
		reason = IdentityRefused
	}
	return IdentityCondition{Status: conditionStatus(reason), Reason: reason}
}

// SetIdentity records an observation or the local start of an enrollment.
// The caller holds the state lock and passes the current enrollment snapshot.
func (s *Session) SetIdentity(existing *Enrollment, reason IdentityReason, now time.Time, probed, retryWithCode bool) error {
	status := conditionStatus(reason)
	if existing == nil || status == "" || reason == IdentityLost && existing.Outcome != Enrolled ||
		retryWithCode && reason != IdentityObtaining {
		return ErrInvalid
	}
	now = now.UTC().Truncate(time.Second)
	record := existing.record()
	if record.Identity.Reason != reason {
		record.Identity.Status = status
		record.Identity.Reason = reason
		record.Identity.LastTransitionTime = now
	}
	if probed {
		record.Identity.LastProbeTime = now
	}
	record.RetryWithCode = retryWithCode
	// Once confirmed, the historical outcome survives all later attempts.
	switch {
	case reason == IdentityConfirmed:
		record.Outcome = Enrolled
	case record.Outcome == Enrolled:
	case reason == IdentityRefused:
		record.Outcome = Failed
	default:
		record.Outcome = Pending
	}
	if record.Identity == existing.Identity && record.Outcome == existing.Outcome && retryWithCode == existing.RetryWithCode {
		return nil
	}
	return s.writeEnrollment(host.EnrollmentDir, record)
}
