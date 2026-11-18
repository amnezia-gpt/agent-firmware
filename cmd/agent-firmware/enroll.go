package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"strings"
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
	errEnrollmentFailed      configuration.Error = "enrollment_failed"
	errEnrollmentTimeout     configuration.Error = "enrollment_timeout"
	errEnrollmentInterrupted configuration.Error = "enrollment_interrupted"
)

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
	var code string
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
		if err != nil {
			clear(data)
			return report(stderr, "input_read_failed", 1)
		}
		code = strings.TrimSuffix(string(data), "\n")
		clear(data)
		if !deviceapi.ValidEnrollmentCode(code) {
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
	userData, err := enrollDevice(ctx, store, hostLayout, issuer, code, time.Second, progress)
	if err != nil {
		return report(stderr, err.Error(), 1)
	}
	return respond(stdout, stderr, struct {
		State    string `json:"state"`
		UserData string `json:"user_data"`
	}{"enrolled", userData})
}

// enrollmentHeartbeat is how often the wait reports that it is still running.
const enrollmentHeartbeat = 15 * time.Second

func enrollDevice(ctx context.Context, store configuration.Store, layout host.Layout, issuer, code string, interval time.Duration, progress func(string)) (userData string, result error) {
	unlock, err := installer.Lock(layout)
	if err != nil {
		return "", err
	}
	defer unlock()
	snapshot, err := store.Snapshot()
	if err != nil {
		return "", err
	}
	if snapshot.Removal != nil {
		return "", configuration.ErrRemovalPending
	}
	if snap := snapshot.Enrollment; snap != nil {
		if snap.Profile.AuthIssuer != issuer {
			return "", configuration.ErrIdentityConflict
		}
		if snap.Outcome == configuration.Enrolled {
			return enrollmentDataResult(snap.AttachedUserData), nil
		}
	}
	if err := checkDeployableRelease(snapshot.Device.Release()); err != nil {
		return "", err
	}
	if snapshot.Enrollment == nil {
		progress("Reading the domain configuration")
	}
	input, err := enrollmentInput(ctx, snapshot, issuer)
	if err != nil {
		return "", err
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
		if current.Enrollment != nil {
			if current.Enrollment.Profile.AuthIssuer != issuer {
				return configuration.ErrIdentityConflict
			}
			if current.Enrollment.Outcome == configuration.Enrolled {
				return nil
			}
		}
		check, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		attached, err := checkWorkloadVolumes(check, session, input, current.Enrollment == nil)
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return enrollmentWaitError(ctx)
		}
		if code != "" {
			next, err := openCodes(layout, workload.EnrollmentID(issuer), true)
			if err != nil {
				return err
			}
			if err := next.Put(code); err != nil {
				return err
			}
			codes = next
		}
		if err := session.BeginEnrollment(input, attached); err != nil {
			return err
		}
		if current.Enrollment != nil && code != "" {
			return session.SetOutcome(issuer, configuration.Pending, true)
		}
		return nil
	}
	progress("Preparing the enrollment")
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for busy := false; ; {
		err := store.Exclusive(prepare)
		if err == nil {
			break
		}
		if !errors.Is(err, configuration.ErrBusy) {
			return "", err
		}
		if !busy {
			busy = true
			progress("Waiting for the firmware service to finish its current step")
		}
		select {
		case <-ctx.Done():
			return "", enrollmentWaitError(ctx)
		case <-ticker.C:
		}
	}

	progress("Starting the workloads and waiting for the domain to confirm the enrollment")
	progress("This usually takes a few minutes. Do not interrupt")
	started := time.Now()
	reported := started
	for {
		observed, err := store.Snapshot()
		if err != nil {
			return "", err
		}
		if observed.Enrollment == nil || observed.Enrollment.Profile.AuthIssuer != issuer {
			return "", configuration.ErrIdentityConflict
		}
		switch observed.Enrollment.Outcome {
		case configuration.Enrolled:
			progress("The domain confirmed the enrollment")
			return enrollmentDataResult(observed.Enrollment.AttachedUserData), nil
		case configuration.Failed:
			return "", errEnrollmentFailed
		}
		if now := time.Now(); now.Sub(reported) >= enrollmentHeartbeat {
			reported = now
			progress("Still waiting for the enrollment (" + now.Sub(started).Round(time.Second).String() + ")")
		}
		select {
		case <-ctx.Done():
			return "", enrollmentWaitError(ctx)
		case <-ticker.C:
		}
	}
}

func enrollmentDataResult(attached bool) string {
	if attached {
		return "attached"
	}
	return "created"
}

func enrollmentInput(ctx context.Context, snapshot configuration.Snapshot, issuer string) (*configuration.Input, error) {
	if snapshot.Enrollment != nil {
		return snapshot.Enrollment.Input(snapshot.Device), nil
	}
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
	issuer := enrollment.Profile.AuthIssuer
	if result == dockerengine.EnrollmentPending {
		if enrollment.RetryWithCode {
			return session.SetOutcome(issuer, configuration.Pending, false)
		}
		return nil
	}
	codes, err := openCodes(hostLayout, workload.EnrollmentID(issuer), false)
	if err != nil {
		return err
	}
	if result == dockerengine.EnrollmentFailed && enrollment.RetryWithCode {
		// The workload may still report the previous attempt until it polls
		// the new code. A pending result or consumption makes failure fresh.
		held, err := codes.HasCode()
		if err != nil || held {
			return err
		}
	}
	if err := codes.Delete(); err != nil {
		return err
	}
	outcome := configuration.Failed
	if result == dockerengine.EnrollmentComplete {
		outcome = configuration.Enrolled
	}
	return session.SetOutcome(issuer, outcome, false)
}
