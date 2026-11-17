package dockerengine

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"regexp"

	"github.com/amnezia-gpt/agent-firmware/internal/strictjson"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	"github.com/containerd/errdefs"
	"github.com/moby/moby/client"
)

type EnrollmentOutcome string

const (
	EnrollmentPending   EnrollmentOutcome = "pending"
	EnrollmentFailed    EnrollmentOutcome = "failed"
	EnrollmentComplete  EnrollmentOutcome = "enrolled"
	ErrEnrollmentStatus workload.Error    = "enrollment_status_unavailable"
)

const maxStatusBytes = 4 * 1024

var controlReason = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var containerID = regexp.MustCompile(`^[0-9a-f]{64}$`)

// StatusCommand owns at most one outstanding exec in the daemon. Closing an
// attach socket does not necessarily stop Docker's exec process.
type StatusCommand struct {
	active string
}

func (s *StatusCommand) Run(parent context.Context, e *Engine, stack workload.Stack) (EnrollmentOutcome, error) {
	ctx, cancel := context.WithTimeout(parent, e.callTimeout)
	defer cancel()
	if s.active != "" {
		active, err := e.api.ExecInspect(ctx, s.active, client.ExecInspectOptions{})
		if err == nil && active.Running || err != nil && !errdefs.IsNotFound(err) {
			return "", ErrEnrollmentStatus
		}
		s.active = ""
	}
	target, err := e.ClientTarget(ctx, stack)
	if err != nil {
		return "", ErrEnrollmentStatus
	}
	return s.run(ctx, e, target.ID, target.User, stack.Client.EnrollmentStatus)
}

func (s *StatusCommand) run(ctx context.Context, e *Engine, id, user string, command []string) (EnrollmentOutcome, error) {
	return s.control(ctx, e, id, user, command, EnrollmentComplete, EnrollmentPending, EnrollmentFailed)
}

func (s *StatusCommand) control(ctx context.Context, e *Engine, id, user string, command []string, allowed ...EnrollmentOutcome) (EnrollmentOutcome, error) {
	exec, err := e.api.ExecCreate(ctx, id, client.ExecCreateOptions{
		User: user, Cmd: command, AttachStdout: true, AttachStderr: true,
	})
	if err != nil || !containerID.MatchString(exec.ID) {
		return "", ErrEnrollmentStatus
	}
	s.active = exec.ID
	attached, err := e.api.ExecAttach(ctx, exec.ID, client.ExecAttachOptions{})
	if err != nil {
		return "", ErrEnrollmentStatus
	}
	defer attached.Close()
	stop := context.AfterFunc(ctx, attached.Close)
	defer stop()
	outcome, err := readControlOutcome(attached.Reader, allowed...)
	if err != nil {
		return "", err
	}
	finished, err := e.api.ExecInspect(ctx, exec.ID, client.ExecInspectOptions{})
	if err != nil || finished.Running {
		return "", ErrEnrollmentStatus
	}
	s.active = ""
	if finished.ID != exec.ID || finished.ContainerID != id || finished.ExitCode != 0 {
		return "", ErrEnrollmentStatus
	}
	return outcome, nil
}

func readControlOutcome(src io.Reader, allowed ...EnrollmentOutcome) (EnrollmentOutcome, error) {
	// The SDK's unbounded demultiplexer allocates the advertised frame length.
	// Read bounded frames instead and keep stderr separate from the payload.
	var header [8]byte
	var stdout bytes.Buffer
	stderr := 0
	for count := 0; ; count++ {
		_, err := io.ReadFull(src, header[:])
		if err == io.EOF {
			break
		}
		if err != nil || count == 512 || header[1] != 0 || header[2] != 0 || header[3] != 0 {
			return "", ErrEnrollmentStatus
		}
		size := int64(binary.BigEndian.Uint32(header[4:]))
		var dst io.Writer = &stdout
		switch header[0] {
		case 1:
			if size > int64(maxStatusBytes-stdout.Len()) {
				return "", ErrEnrollmentStatus
			}
		case 2:
			if size > int64(maxStatusBytes-stderr) {
				return "", ErrEnrollmentStatus
			}
			stderr += int(size)
			dst = io.Discard
		default:
			return "", ErrEnrollmentStatus
		}
		if _, err := io.CopyN(dst, src, size); err != nil {
			return "", ErrEnrollmentStatus
		}
	}
	fields, ok := strictjson.Members(stdout.Bytes())
	if !ok || fields["state"] == nil {
		return "", ErrEnrollmentStatus
	}
	if reason, present := fields["reason"]; len(fields) > 1 &&
		(len(fields) != 2 || !present || reason == nil || !controlReason.MatchString(strictjson.StringField(fields, "reason"))) {
		return "", ErrEnrollmentStatus
	}
	outcome := EnrollmentOutcome(strictjson.StringField(fields, "state"))
	for _, value := range allowed {
		if outcome == value {
			return outcome, nil
		}
	}
	return "", ErrEnrollmentStatus
}
