// Package service supplies the optional systemd readiness boundary.
package service

import (
	"context"
	"net"
	"os"
	"strings"
	"time"
)

type Error string

func (e Error) Error() string { return string(e) }

const ErrNotify Error = "service_notify_failed"

type Notifier struct {
	Socket string
	sent   bool
}

func (Systemd) Notifier() Notifier {
	return Notifier{Socket: os.Getenv("NOTIFY_SOCKET")}
}

// Ready sends one fixed message. Foreground operation needs no notification.
func (n *Notifier) Ready() error {
	if n.Socket == "" || n.sent {
		return nil
	}
	if err := n.send("READY=1", 2*time.Second); err != nil {
		return err
	}
	n.sent = true
	return nil
}

// Status publishes one display line without affecting readiness.
func (n Notifier) Status(line string) error {
	if n.Socket == "" {
		return nil
	}
	if line == "" || len(line) > 128 || strings.ContainsAny(line, "\r\n\x00") {
		return ErrNotify
	}
	return n.send("STATUS="+line, 100*time.Millisecond)
}

func (n Notifier) send(message string, budget time.Duration) error {
	path := n.Socket
	if strings.HasPrefix(path, "@") {
		path = "\x00" + path[1:]
	} else if !strings.HasPrefix(path, "/") {
		return ErrNotify
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(ctx, "unixgram", path)
	if err != nil {
		return ErrNotify
	}
	defer connection.Close()
	deadline, _ := ctx.Deadline()
	if connection.SetWriteDeadline(deadline) != nil {
		return ErrNotify
	}
	if size, err := connection.Write([]byte(message)); err != nil || size != len(message) {
		return ErrNotify
	}
	return nil
}
