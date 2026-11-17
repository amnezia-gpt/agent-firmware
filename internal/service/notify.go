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
	path := n.Socket
	if strings.HasPrefix(path, "@") {
		path = "\x00" + path[1:]
	} else if !strings.HasPrefix(path, "/") {
		return ErrNotify
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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
	if size, err := connection.Write([]byte("READY=1")); err != nil || size != 7 {
		return ErrNotify
	}
	n.sent = true
	return nil
}
