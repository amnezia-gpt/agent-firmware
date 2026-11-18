package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/deviceapi"
	"github.com/amnezia-gpt/agent-firmware/internal/discovery"
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/installer"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
)

const (
	errEnrollmentFailed       configuration.Error = "enrollment_failed"
	errEnrollmentTimeout      configuration.Error = "enrollment_timeout"
	errEnrollmentInterrupted  configuration.Error = "enrollment_interrupted"
	errAlreadyEnrolled        configuration.Error = "already_enrolled"
	errEnrollmentCodeRequired configuration.Error = "enrollment_code_required"
	errWorkloadUnavailable    configuration.Error = "workload_unavailable"
)

var errEnrollmentProbePending = errors.New("enrollment probe pending")

type enrollmentResult struct {
	State      string `json:"state"`
	Enrollment string `json:"enrollment"`
	UserData   string `json:"user_data"`
}

type codeStore interface {
	Put(string) error
	HasCode() (bool, error)
	Delete() error
}

var openCodes = func(layout host.Layout, installation string, create bool) (codeStore, error) {
	return deviceapi.EnrollmentCodes(layout, installation, create)
}

func runEnroll(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	var issuer, source, codePath string
	input := stdin
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--issuer":
			if issuer != "" || i+1 == len(args) || args[i+1] == "" {
				return report(stderr, "invalid_usage", 2)
			}
			issuer = args[i+1]
			i++
		case "--enrollment-code-stdin":
			if source != "" {
				return report(stderr, "invalid_usage", 2)
			}
			source = args[i]
		case "--enrollment-code-file":
			if source != "" || i+1 == len(args) || args[i+1] == "" {
				return report(stderr, "invalid_usage", 2)
			}
			source, codePath = args[i], args[i+1]
			i++
		default:
			return report(stderr, "invalid_usage", 2)
		}
	}
	if issuer == "" {
		return report(stderr, "invalid_usage", 2)
	}
	if os.Geteuid() != 0 {
		return report(stderr, "root_required", 1)
	}
	var code []byte
	if source != "" {
		if codePath != "" {
			file, err := openInput(codePath)
			if err != nil {
				return report(stderr, "input_read_failed", 1)
			}
			defer file.Close()
			input = file
		}
		data, err := io.ReadAll(io.LimitReader(input, deviceapi.MaxEnrollmentCode+2))
		defer clear(data)
		if err != nil {
			return report(stderr, "input_read_failed", 1)
		}
		code = bytes.TrimSuffix(data, []byte{'\n'})
		if !deviceapi.ValidEnrollmentCode(string(code)) {
			return report(stderr, string(deviceapi.ErrEnrollmentCode), 1)
		}
	}
	parent, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	ctx, cancel := context.WithTimeout(parent, 20*time.Minute)
	defer cancel()
	store := configuration.Store{Path: defaultStateDir}
	// The wait can take minutes; without these lines the terminal looks hung.
	progress := func(message string) { _, _ = io.WriteString(stderr, "agent-firmware: "+message+"\n") }
	result, err := enrollDevice(ctx, store, hostLayout, issuer, code, time.Second, progress)
	if err != nil {
		return report(stderr, err.Error(), 1)
	}
	return respond(stdout, stderr, result)
}

// enrollmentHeartbeat is how often the wait reports that it is still running.
const enrollmentHeartbeat = 15 * time.Second

func enrollDevice(ctx context.Context, store configuration.Store, layout host.Layout, issuer string, code []byte, interval time.Duration, progress func(string)) (reply enrollmentResult, result error) {
	defer clear(code)
	unlock, err := installer.Lock(layout)
	if err != nil {
		return reply, err
	}
	defer unlock()
	snapshot, err := store.Snapshot()
	if err != nil {
		return reply, err
	}
	if snapshot.Removal != nil {
		return reply, configuration.ErrRemovalPending
	}
	if snap := snapshot.Enrollment; snap != nil {
		if snap.Profile.AuthIssuer != issuer {
			return reply, configuration.ErrIdentityConflict
		}
	}
	if err := checkDeployableRelease(snapshot.Device.Release()); err != nil {
		return reply, err
	}
	mode := "created"
	var input *configuration.Input
	prepareCtx := ctx
	if snapshot.Enrollment != nil {
		mode = "renewed"
		var cancel context.CancelFunc
		prepareCtx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		progress("Checking the device's current enrollment")
	} else {
		progress("Reading the domain configuration")
		input, err = enrollmentInput(ctx, snapshot, issuer)
		if err != nil {
			return reply, err
		}
	}
	deadline, bounded := prepareCtx.Deadline()
	preparationExpired := func() bool {
		return prepareCtx.Err() != nil || bounded && !time.Now().Before(deadline)
	}
	var codes codeStore
	defer func() {
		if codes != nil {
			if err := codes.Delete(); err != nil {
				result = err
			}
		}
	}()
	prepare := func(session *configuration.Session) error {
		current, err := session.Snapshot()
		if err != nil {
			return err
		}
		existing := current.Enrollment
		if existing != nil {
			if existing.Profile.AuthIssuer != issuer {
				return configuration.ErrIdentityConflict
			}
			if err := checkRenewal(prepareCtx, session, current, layout, len(code) != 0); err != nil {
				if preparationExpired() {
					return enrollmentPreparationError(ctx)
				}
				return err
			}
			if len(code) == 0 {
				return nil
			}
			// The probe may have changed the condition. Preserve that record's
			// timestamps and the original binding when marking the new attempt.
			current, err = session.Snapshot()
			if err != nil {
				return err
			}
			existing = current.Enrollment
		}
		var attached bool
		if existing == nil {
			check, cancel := context.WithTimeout(ctx, 30*time.Second)
			attached, err = checkWorkloadVolumes(check, session, input, true)
			cancel()
			if err != nil {
				return err
			}
		}
		if preparationExpired() {
			return enrollmentPreparationError(ctx)
		}
		if len(code) != 0 {
			next, err := openCodes(layout, workload.EnrollmentID(issuer), true)
			if err != nil {
				return err
			}
			if err := next.Put(string(code)); err != nil {
				return err
			}
			codes = next
		}
		if existing == nil {
			return session.BeginEnrollment(input, attached)
		}
		return session.SetIdentity(existing, configuration.IdentityObtaining, time.Now(), false, true)
	}
	progress("Preparing the enrollment")
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var busy, awaitingStatus bool
	for {
		if preparationExpired() {
			return reply, enrollmentPreparationError(ctx)
		}
		err := store.Exclusive(prepare)
		if err == nil {
			break
		}
		switch {
		case errors.Is(err, errEnrollmentProbePending):
			if !awaitingStatus {
				awaitingStatus = true
				progress("Waiting for the workload to confirm its enrollment status")
			}
		case errors.Is(err, configuration.ErrBusy):
			if !busy {
				busy = true
				progress("Waiting for the firmware service to finish its current step")
			}
		default:
			return reply, err
		}
		select {
		case <-prepareCtx.Done():
			return reply, enrollmentPreparationError(ctx)
		case <-ticker.C:
		}
	}
	clear(code)

	if mode == "created" {
		progress("Starting the workloads and waiting for the domain to confirm the enrollment")
	} else {
		progress("Waiting for the domain to confirm the renewed enrollment")
	}
	progress("This usually takes a few minutes. Do not interrupt")
	started := time.Now()
	reported := started
	for {
		observed, err := store.Snapshot()
		if err != nil {
			return reply, err
		}
		if observed.Enrollment == nil || observed.Enrollment.Profile.AuthIssuer != issuer {
			return reply, configuration.ErrIdentityConflict
		}
		switch observed.Enrollment.Identity.Reason {
		case configuration.IdentityConfirmed:
			progress("The domain confirmed the enrollment")
			return enrollmentResult{"enrolled", mode, enrollmentDataResult(observed.Enrollment.AttachedUserData)}, nil
		case configuration.IdentityRefused:
			return reply, errEnrollmentFailed
		}
		if now := time.Now(); now.Sub(reported) >= enrollmentHeartbeat {
			reported = now
			progress("Still waiting for the enrollment (" + now.Sub(started).Round(time.Second).String() + ")")
		}
		select {
		case <-ctx.Done():
			return reply, enrollmentWaitError(ctx)
		case <-ticker.C:
		}
	}
}

func enrollmentPreparationError(ctx context.Context) error {
	if ctx.Err() != nil {
		return enrollmentWaitError(ctx)
	}
	return errWorkloadUnavailable
}

func checkRenewal(ctx context.Context, session *configuration.Session, snapshot configuration.Snapshot, layout host.Layout, hasCode bool) error {
	outcome, err := probeExistingEnrollment(ctx, session, snapshot)
	if err != nil || ctx.Err() != nil {
		return errWorkloadUnavailable
	}
	reason, err := recordEnrollmentProbe(session, snapshot.Enrollment, layout, outcome, time.Now())
	if err != nil {
		return err
	}
	switch reason {
	case configuration.IdentityConfirmed:
		return errAlreadyEnrolled
	case configuration.IdentityObtaining:
		return nil
	case configuration.IdentityLost, configuration.IdentityRequired, configuration.IdentityRefused:
		if !hasCode {
			return errEnrollmentCodeRequired
		}
		return nil
	default:
		if outcome == dockerengine.EnrollmentPending {
			// The workload may still be restoring its previous session. Retry
			// outside the configuration lock before deciding whether to stage a code.
			return errEnrollmentProbePending
		}
		return errWorkloadUnavailable
	}
}

var probeExistingEnrollment = func(ctx context.Context, session *configuration.Session, snapshot configuration.Snapshot) (dockerengine.EnrollmentOutcome, error) {
	stack, err := workload.FromInput(snapshot.Enrollment.Input(snapshot.Device))
	if err != nil {
		return "", err
	}
	address, err := session.EngineAddress()
	if err != nil {
		return "", err
	}
	engine, err := dockerengine.New(address)
	if err != nil {
		return "", err
	}
	defer engine.Close()
	target, err := engine.ClientTarget(ctx, stack)
	if err != nil {
		return "", err
	}
	var command dockerengine.StatusCommand
	return command.Run(ctx, engine, target, stack.Client.EnrollmentStatus)
}

func enrollmentDataResult(attached bool) string {
	if attached {
		return "attached"
	}
	return "created"
}

func enrollmentInput(ctx context.Context, snapshot configuration.Snapshot, issuer string) (*configuration.Input, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	raw, err := discover(ctx, issuer)
	if err != nil {
		return nil, err
	}
	input, err := snapshot.Device.WithProfile(raw)
	if err != nil {
		return nil, err
	}
	if input.Profile().AuthIssuer != issuer {
		return nil, discovery.ErrIssuer
	}
	if _, err := workload.FromInput(input); err != nil {
		return nil, err
	}
	return input, nil
}

func enrollmentWaitError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errEnrollmentTimeout
	}
	return errEnrollmentInterrupted
}

func observeEnrollment(session *configuration.Session, enrollment *configuration.Enrollment, result dockerengine.EnrollmentOutcome) error {
	_, err := recordEnrollmentProbe(session, enrollment, hostLayout, result, time.Now())
	return err
}

func recordEnrollmentProbe(session *configuration.Session, enrollment *configuration.Enrollment, layout host.Layout, result dockerengine.EnrollmentOutcome, now time.Time) (configuration.IdentityReason, error) {
	codes, err := openCodes(layout, workload.EnrollmentID(enrollment.Profile.AuthIssuer), false)
	if err != nil {
		return "", err
	}
	held, err := codes.HasCode()
	if err != nil {
		return "", err
	}
	reason := enrollmentProbeReason(enrollment, result, held)
	if reason == "" {
		return "", nil
	}
	if held && (reason == configuration.IdentityConfirmed || reason == configuration.IdentityRefused) {
		if err := codes.Delete(); err != nil {
			return "", err
		}
	}
	return reason, session.SetIdentity(enrollment, reason, now, true, false)
}

func enrollmentProbeReason(enrollment *configuration.Enrollment, result dockerengine.EnrollmentOutcome, held bool) configuration.IdentityReason {
	switch result {
	case dockerengine.EnrollmentComplete:
		return configuration.IdentityConfirmed
	case dockerengine.EnrollmentPending:
		if held {
			return configuration.IdentityObtaining
		}
	case dockerengine.EnrollmentFailed:
		// A held replacement code may not have been seen yet. Wait for pending
		// or consumption before accepting a refusal of that attempt.
		if held && !enrollment.RetryWithCode || !held &&
			(enrollment.Identity.Reason == configuration.IdentityObtaining || enrollment.Identity.Reason == configuration.IdentityRefused) {
			return configuration.IdentityRefused
		}
	case dockerengine.EnrollmentAbsent:
		if held {
			return ""
		}
		if enrollment.Outcome == configuration.Enrolled {
			return configuration.IdentityLost
		}
		return configuration.IdentityRequired
	}
	return ""
}
