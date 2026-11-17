package deviceapi

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestHTTP2SessionAnswersPingAndClosesOnCancellation(t *testing.T) {
	vault, _ := testVault(t)
	server, peer := net.Pipe()
	defer peer.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan LinkReason, 1)
	go func() {
		reason, _ := serveLink(ctx, server, NewHandler(vault, nil, nil), 10*time.Second, time.Second)
		done <- reason
	}()
	if err := peer.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	preface := []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
	settings := []byte{0, 0, 0, 4, 0, 0, 0, 0, 0}
	ping := []byte{0, 0, 8, 6, 0, 0, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8}
	// net.Pipe has no buffers and HTTP/2 permits both peers to write at once.
	go func() {
		_, _ = peer.Write(append(append(preface, settings...), ping...))
	}()
	var header [9]byte
	for {
		if _, err := io.ReadFull(peer, header[:]); err != nil {
			t.Fatal(err)
		}
		n := binary.BigEndian.Uint32([]byte{0, header[0], header[1], header[2]})
		if n > 16384 {
			t.Fatal("unexpected frame length")
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(peer, payload); err != nil {
			t.Fatal(err)
		}
		if header[3] == 6 && header[4]&1 != 0 {
			if !bytes.Equal(payload, ping[9:]) {
				t.Fatal("PING acknowledgment changed the opaque bytes")
			}
			break
		}
	}
	cancel()
	select {
	case reason := <-done:
		if reason != LinkServiceStopping {
			t.Fatalf("wrong end reason: %s", reason)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled link did not terminate")
	}
}

func TestHTTP2ReconnectsAfterPeerDisconnects(t *testing.T) {
	vault, _ := testVault(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	peers := make(chan net.Conn, 2)
	events := make(chan LinkEvent, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runner := LinkRunner{}
		runner.Run(ctx, func(context.Context) (net.Conn, error) {
			server, peer := net.Pipe()
			peers <- peer
			return server, nil
		}, NewHandler(vault, nil, nil), func(event LinkEvent) { events <- event })
	}()
	for round := range 2 {
		var peer net.Conn
		select {
		case peer = <-peers:
		case <-ctx.Done():
			t.Fatal("link did not reconnect")
		}
		protocols := new(http.Protocols)
		protocols.SetUnencryptedHTTP2(true)
		transport := &http.Transport{
			Protocols:   protocols,
			DialContext: func(context.Context, string, string) (net.Conn, error) { return peer, nil },
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://device-api/v3/capabilities", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := transport.RoundTrip(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || response.ProtoMajor != 2 || !bytes.Contains(body, []byte(Protocol)) {
			t.Fatalf("round %d did not serve HTTP/2: %v", round, err)
		}
		peer.Close()
		transport.CloseIdleConnections()
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runner did not stop")
	}
	first, dropped, second := <-events, <-events, <-events
	if first.State != "connected" || dropped.State != "reconnecting" || dropped.Reason != LinkWorkloadClosed || dropped.DurationSeconds == nil ||
		second.State != "connected" || second.ReconnectCount != 1 {
		t.Fatalf("wrong transitions: %+v %+v %+v", first, dropped, second)
	}
}

func TestHTTP2ReportsKeepaliveTimeout(t *testing.T) {
	server, peer := net.Pipe()
	defer peer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	done := make(chan LinkReason, 1)
	go func() {
		reason, _ := serveLink(ctx, server, http.NotFoundHandler(), 20*time.Millisecond, 30*time.Millisecond)
		done <- reason
	}()
	_ = peer.SetDeadline(time.Now().Add(2 * time.Second))
	go func() {
		_, _ = io.WriteString(peer, "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"+string([]byte{0, 0, 0, 4, 0, 0, 0, 0, 0}))
	}()
	// Drain server frames without acknowledging its PING.
	_, _ = io.Copy(io.Discard, peer)
	select {
	case reason := <-done:
		if reason != LinkKeepaliveTimeout {
			t.Fatalf("want keepalive timeout, got %s", reason)
		}
	case <-ctx.Done():
		t.Fatal("link did not terminate")
	}
}

func TestLinkReasonsAndReconnectCountSurviveReplacement(t *testing.T) {
	runner := LinkRunner{}
	for round := range 2 {
		ctx, cancel := context.WithCancel(t.Context())
		var events []LinkEvent
		runner.Run(ctx, func(ctx context.Context) (net.Conn, error) {
			if round == 0 {
				return nil, LinkPreviousExecRunning
			}
			return nil, errors.New("private engine response")
		}, http.NotFoundHandler(), func(event LinkEvent) {
			events = append(events, event)
			cancel()
		})
		want := LinkPreviousExecRunning
		if round == 1 {
			want = LinkSetupFailed
		}
		if len(events) != 1 || events[0].ReconnectCount != uint64(round) || events[0].Reason != want {
			t.Fatalf("round %d: %+v", round, events)
		}
	}
}

type failedEngineAttachment struct {
	net.Conn
}

func (failedEngineAttachment) ExecExit(context.Context) (LinkExecOutcome, bool) {
	return LinkExecUnknown, false
}

func TestLinkDistinguishesEngineLossFromWorkloadEOF(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	server, peer := net.Pipe()
	_ = peer.Close()
	var events []LinkEvent
	runner := LinkRunner{}
	runner.Run(ctx, func(context.Context) (net.Conn, error) {
		return failedEngineAttachment{server}, nil
	}, http.NotFoundHandler(), func(event LinkEvent) {
		events = append(events, event)
		if event.State == "reconnecting" {
			cancel()
		}
	})
	if len(events) != 2 || events[1].Reason != LinkEngineConnectionLost || events[1].DurationSeconds == nil {
		t.Fatalf("engine loss was not attributed: %+v", events)
	}
}

func TestHTTP2MeasuresMatchedKeepaliveRTT(t *testing.T) {
	server, peer := net.Pipe()
	defer peer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	done := make(chan *float64, 1)
	go func() {
		_, rtt := serveLink(ctx, server, http.NotFoundHandler(), 50*time.Millisecond, time.Second)
		done <- rtt
	}()
	_ = peer.SetDeadline(time.Now().Add(2 * time.Second))
	go func() {
		_, _ = io.WriteString(peer, "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"+string([]byte{0, 0, 0, 4, 0, 0, 0, 0, 0}))
	}()
	var header [9]byte
	for {
		if _, err := io.ReadFull(peer, header[:]); err != nil {
			t.Fatal(err)
		}
		length := int(header[0])<<16 | int(header[1])<<8 | int(header[2])
		payload := make([]byte, length)
		if _, err := io.ReadFull(peer, payload); err != nil {
			t.Fatal(err)
		}
		if header[3] == 6 && header[4] == 0 {
			// Split the ACK across reads to exercise partial frame tracking.
			header[4] = 1
			for _, part := range [][]byte{header[:4], header[4:], payload[:3], payload[3:]} {
				if _, err := peer.Write(part); err != nil {
					t.Fatal(err)
				}
			}
			break
		}
	}
	peer.Close()
	select {
	case rtt := <-done:
		if rtt == nil || *rtt <= 0 || *rtt >= 2 {
			t.Fatalf("invalid keepalive sample %v", rtt)
		}
	case <-ctx.Done():
		t.Fatal("RTT was not available after disconnect")
	}
}
