package deviceapi

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// LinkReason is a firmware-owned diagnostic code. Raw errors never reach a
// link event.
type LinkReason string

const (
	LinkPreviousExecRunning LinkReason = "previous_exec_running"
	LinkEngineUnreachable   LinkReason = "engine_unreachable"
	LinkExecInspectFailed   LinkReason = "exec_inspect_failed"
	LinkTargetAbsent        LinkReason = "target_absent"
	LinkTargetNotRunning    LinkReason = "target_not_running"
	LinkTargetUnverified    LinkReason = "target_unverified"
	LinkExecCreateFailed    LinkReason = "exec_create_failed"
	LinkExecAttachFailed    LinkReason = "exec_attach_failed"
	LinkSetupFailed         LinkReason = "link_setup_failed"

	LinkKeepaliveTimeout     LinkReason = "keepalive_timeout"
	LinkWorkloadClosed       LinkReason = "workload_closed"
	LinkEngineConnectionLost LinkReason = "engine_connection_lost"
	LinkServiceStopping      LinkReason = "service_stopping"
	LinkTargetChanged        LinkReason = "target_changed"
	LinkProtocolFailure      LinkReason = "protocol_failure"
)

func (r LinkReason) Error() string { return string(r) }

type LinkExecOutcome string

const (
	LinkExecExitedOK     LinkExecOutcome = "exited_ok"
	LinkExecExitedError  LinkExecOutcome = "exited_error"
	LinkExecStillRunning LinkExecOutcome = "still_running"
	LinkExecUnknown      LinkExecOutcome = "unknown"
)

type LinkEvent struct {
	Event               string          `json:"event"`
	State               string          `json:"state"`
	ReconnectCount      uint64          `json:"reconnect_count"`
	Reason              LinkReason      `json:"reason,omitempty"`
	DurationSeconds     *float64        `json:"duration_seconds,omitempty"`
	ExecOutcome         LinkExecOutcome `json:"exec_outcome,omitempty"`
	KeepaliveRTTSeconds *float64        `json:"keepalive_rtt_seconds,omitempty"`
}

// LinkRunner belongs to one supervisor. Run calls are serialized, including
// after the verified workload target is replaced.
type LinkRunner struct {
	attempted  bool
	reconnects uint64
}

func (r *LinkRunner) Unavailable(err error) LinkEvent {
	var reason LinkReason
	_ = errors.As(err, &reason)
	switch reason {
	case LinkPreviousExecRunning, LinkEngineUnreachable, LinkExecInspectFailed,
		LinkTargetAbsent, LinkTargetNotRunning, LinkTargetUnverified, LinkExecCreateFailed, LinkExecAttachFailed:
	default:
		reason = LinkSetupFailed
	}
	return LinkEvent{Event: "device_api_link", State: "unavailable", ReconnectCount: r.reconnects, Reason: reason}
}

func (r *LinkRunner) Run(ctx context.Context, open func(context.Context) (net.Conn, error), handler http.Handler, emit func(LinkEvent)) {
	const minDelay = 250 * time.Millisecond
	const maxDelay = 10 * time.Second
	delay := minDelay
	for ctx.Err() == nil {
		if r.attempted {
			r.reconnects++
		}
		r.attempted = true
		conn, err := open(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			emit(r.Unavailable(err))
		} else {
			emit(LinkEvent{Event: "device_api_link", State: "connected", ReconnectCount: r.reconnects})
			at := time.Now()
			reason, rtt := serveLink(ctx, conn, handler, 30*time.Second, 5*time.Second)
			elapsed := time.Since(at)
			outcome := LinkExecUnknown
			if probe, ok := conn.(interface {
				ExecExit(context.Context) (LinkExecOutcome, bool)
			}); ok {
				var reachable bool
				outcome, reachable = probe.ExecExit(ctx)
				// An orderly EOF can also come from a disappearing Docker engine.
				if reason == LinkWorkloadClosed && !reachable {
					reason = LinkEngineConnectionLost
				}
			}
			if elapsed >= 5*time.Second {
				delay = minDelay
			}
			state := "reconnecting"
			if ctx.Err() != nil {
				state = "stopped"
				reason = stopReason(ctx)
			}
			seconds := elapsed.Seconds()
			emit(LinkEvent{Event: "device_api_link", State: state, ReconnectCount: r.reconnects, Reason: reason, DurationSeconds: &seconds, ExecOutcome: outcome, KeepaliveRTTSeconds: rtt})
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(delay*2, maxDelay)
	}
}

func stopReason(ctx context.Context) LinkReason {
	if errors.Is(context.Cause(ctx), LinkTargetChanged) {
		return LinkTargetChanged
	}
	return LinkServiceStopping
}

// serveLink presents the already-open exec to net/http. It never binds a host
// listener, and closing the HTTP/2 session also closes the Docker attachment.
func serveLink(ctx context.Context, conn net.Conn, handler http.Handler, pingInterval, pingTimeout time.Duration) (LinkReason, *float64) {
	const handshakeTimeout = 5 * time.Second
	listener := &singleLink{conn: conn, done: make(chan struct{})}
	listener.ping.in.preface = h2ClientPrefaceLength
	defer listener.Close()
	stop := context.AfterFunc(ctx, func() { listener.Close() })
	defer stop()
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	server := &http.Server{
		Handler: handler, Protocols: protocols,
		ReadHeaderTimeout: handshakeTimeout, MaxHeaderBytes: 8 * 1024,
		ErrorLog:    log.New(io.Discard, "", 0),
		BaseContext: func(net.Listener) context.Context { return ctx },
		HTTP2: &http.HTTP2Config{
			MaxConcurrentStreams: 8, SendPingTimeout: pingInterval, PingTimeout: pingTimeout,
			WriteByteTimeout: 5 * time.Second, MaxReadFrameSize: 16 * 1024,
			MaxReceiveBufferPerConnection: 256 * 1024, MaxReceiveBufferPerStream: 128 * 1024,
			CountError: func(code string) {
				// Go's HTTP/2 server calls this before closing a session
				// whose health-check PING went unacknowledged.
				if code == "conn_close_lost_ping" {
					listener.end.Store(endKeepaliveTimeout)
				}
			},
		},
	}
	_ = server.Serve(listener)
	_ = server.Close()
	rtt := listener.ping.seconds()
	if ctx.Err() != nil {
		return stopReason(ctx), rtt
	}
	switch listener.end.Load() {
	case endKeepaliveTimeout:
		return LinkKeepaliveTimeout, rtt
	case endConnectionLost:
		return LinkEngineConnectionLost, rtt
	case endPeerClosed:
		return LinkWorkloadClosed, rtt
	default:
		return LinkProtocolFailure, rtt
	}
}

const (
	endPeerClosed uint32 = iota + 1
	endConnectionLost
	endKeepaliveTimeout
)

type singleLink struct {
	conn     net.Conn
	done     chan struct{}
	once     sync.Once
	accepted bool
	end      atomic.Uint32
	ping     pingMonitor
}

type acceptedLink struct {
	net.Conn
	link *singleLink
}

func (c acceptedLink) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.link.ping.observe(p[:n], false)
	}
	c.link.observe(err)
	return n, err
}

func (c acceptedLink) Write(p []byte) (int, error) {
	// Record the send before the peer can acknowledge the frame.
	c.link.ping.observe(p, true)
	n, err := c.Conn.Write(p)
	c.link.observe(err)
	return n, err
}

func (c acceptedLink) Close() error { return c.link.Close() }

func (l *singleLink) observe(err error) {
	if err == nil {
		return
	}
	// Ignore errors induced by our own Close or protocol deadlines.
	select {
	case <-l.done:
		return
	default:
	}
	var timer net.Error
	if errors.As(err, &timer) && timer.Timeout() {
		return
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, syscall.EPIPE) {
		l.end.CompareAndSwap(0, endPeerClosed)
		return
	}
	if !l.end.CompareAndSwap(0, endConnectionLost) {
		l.end.CompareAndSwap(endPeerClosed, endConnectionLost)
	}
}

func (l *singleLink) Accept() (net.Conn, error) {
	if !l.accepted {
		l.accepted = true
		return acceptedLink{l.conn, l}, nil
	}
	<-l.done
	return nil, net.ErrClosed
}

func (l *singleLink) Close() error {
	l.once.Do(func() {
		close(l.done)
		_ = l.conn.Close()
	})
	return nil
}

func (l *singleLink) Addr() net.Addr { return l.conn.LocalAddr() }
