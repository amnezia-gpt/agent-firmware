package dockerengine

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/deviceapi"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

const maxExecFrame = 1 << 20

type ClientTarget struct {
	ID           string
	Name         string
	User         string
	Installation string
	Role         string
	Hash         string
}

func (e *Engine) ClientTarget(parent context.Context, stack workload.Stack) (ClientTarget, error) {
	return e.clientTarget(parent, stack, true)
}

func (e *Engine) clientTarget(parent context.Context, stack workload.Stack, requireImage bool) (ClientTarget, error) {
	ctx, cancel := context.WithTimeout(parent, e.callTimeout)
	defer cancel()
	for _, spec := range stack.Specs {
		if spec.Role != stack.Client.Service {
			continue
		}
		got, err := e.api.ContainerInspect(ctx, spec.Name, client.ContainerInspectOptions{})
		if err != nil {
			return ClientTarget{}, linkOpenFailure(err, deviceapi.LinkTargetUnverified)
		}
		c := got.Container
		if !containerID.MatchString(c.ID) || c.Config == nil {
			return ClientTarget{}, deviceapi.LinkTargetUnverified
		}
		target := ClientTarget{c.ID, spec.Name, c.Config.User, spec.Installation, spec.Role, spec.Hash()}
		if reason := targetFailure(c, target); reason != "" {
			return ClientTarget{}, reason
		}
		if requireImage {
			image, ok := e.images[spec.Image]
			if !ok || !matches(spec, c, image) {
				return ClientTarget{}, deviceapi.LinkTargetUnverified
			}
		}
		return target, nil
	}
	return ClientTarget{}, deviceapi.LinkTargetUnverified
}

func targetFailure(c container.InspectResponse, target ClientTarget) deviceapi.LinkReason {
	if c.ID != target.ID || c.Name != "/"+target.Name || c.Config == nil ||
		c.Config.User != target.User || !validEffectiveUser(target.User) ||
		!owned(c.Config.Labels, target.Installation, target.Role) ||
		c.Config.Labels[workload.LabelPrefix+"spec"] != target.Hash {
		return deviceapi.LinkTargetUnverified
	}
	if !state(c).Running {
		return deviceapi.LinkTargetNotRunning
	}
	return ""
}

func linkOpenFailure(err error, fallback deviceapi.LinkReason) deviceapi.LinkReason {
	var networkError *net.OpError
	if client.IsErrConnectionFailed(err) || errors.As(err, &networkError) ||
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return deviceapi.LinkEngineUnreachable
	}
	if fallback == deviceapi.LinkTargetUnverified && errdefs.IsNotFound(err) {
		return deviceapi.LinkTargetAbsent
	}
	return fallback
}

// DialCommand remembers unfinished execs across transport retries. A dropped
// attachment alone does not establish that Docker stopped its exec process.
type DialCommand struct {
	active string
}

func (d *DialCommand) Attach(parent context.Context, e *Engine, target ClientTarget, argv []string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(parent, e.callTimeout)
	defer cancel()
	if d.active != "" {
		exec, err := e.api.ExecInspect(ctx, d.active, client.ExecInspectOptions{})
		if err == nil && exec.Running {
			return nil, deviceapi.LinkPreviousExecRunning
		}
		if err != nil && !errdefs.IsNotFound(err) {
			return nil, linkOpenFailure(err, deviceapi.LinkExecInspectFailed)
		}
		d.active = ""
	}
	got, err := e.api.ContainerInspect(ctx, target.ID, client.ContainerInspectOptions{})
	if err != nil {
		return nil, linkOpenFailure(err, deviceapi.LinkTargetUnverified)
	}
	c := got.Container
	if reason := targetFailure(c, target); reason != "" {
		return nil, reason
	}
	exec, err := e.api.ExecCreate(ctx, target.ID, client.ExecCreateOptions{
		User: target.User, Cmd: argv, AttachStdin: true, AttachStdout: true, AttachStderr: true,
	})
	if err != nil || !containerID.MatchString(exec.ID) {
		return nil, linkOpenFailure(err, deviceapi.LinkExecCreateFailed)
	}
	d.active = exec.ID
	attached, err := e.api.ExecAttach(ctx, exec.ID, client.ExecAttachOptions{})
	if err != nil {
		return nil, linkOpenFailure(err, deviceapi.LinkExecAttachFailed)
	}
	return &execConn{Conn: attached.Conn, reader: attached.Reader, engine: e, execID: exec.ID, containerID: target.ID}, nil
}

// execConn writes raw stdin and reads only the bounded Docker stdout frames.
// Stderr can contain workload data and is discarded before reaching HTTP/2.
type execConn struct {
	net.Conn
	reader      io.Reader
	remaining   int
	engine      *Engine
	execID      string
	containerID string
}

// ExecExit allows a closing dial process a bounded interval to exit. The
// observation never removes the outstanding exec or permits a duplicate attach.
func (c *execConn) ExecExit(parent context.Context) (deviceapi.LinkExecOutcome, bool) {
	wait := parent.Err() == nil
	budget := min(2*time.Second, c.engine.callTimeout)
	if !wait {
		// On shutdown make one short best-effort observation.
		budget = min(budget, 250*time.Millisecond)
		parent = context.WithoutCancel(parent)
	}
	ctx, cancel := context.WithTimeout(parent, budget)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	until, _ := ctx.Deadline()
	for {
		got, err := c.engine.api.ExecInspect(ctx, c.execID, client.ExecInspectOptions{})
		if err != nil {
			return deviceapi.LinkExecUnknown, linkOpenFailure(err, deviceapi.LinkExecInspectFailed) != deviceapi.LinkEngineUnreachable
		}
		if got.ID != c.execID || got.ContainerID != c.containerID {
			return deviceapi.LinkExecUnknown, true
		}
		if !got.Running {
			if got.ExitCode == 0 {
				return deviceapi.LinkExecExitedOK, true
			}
			return deviceapi.LinkExecExitedError, true
		}
		if !wait || time.Until(until) < 100*time.Millisecond {
			return deviceapi.LinkExecStillRunning, true
		}
		select {
		case <-ctx.Done():
			return deviceapi.LinkExecStillRunning, true
		case <-ticker.C:
		}
	}
}

func (c *execConn) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	var header [8]byte
	for c.remaining == 0 {
		if _, err := io.ReadFull(c.reader, header[:]); err != nil {
			return 0, err
		}
		size := binary.BigEndian.Uint32(header[4:])
		if header[1] != 0 || header[2] != 0 || header[3] != 0 || size > maxExecFrame {
			return 0, workload.ErrEngine
		}
		switch header[0] {
		case 1:
			c.remaining = int(size)
		case 2:
			if _, err := io.CopyN(io.Discard, c.reader, int64(size)); err != nil {
				return 0, err
			}
		default:
			return 0, workload.ErrEngine
		}
	}
	n, err := c.reader.Read(dst[:min(len(dst), c.remaining)])
	c.remaining -= n
	if err == io.EOF && c.remaining > 0 {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}
