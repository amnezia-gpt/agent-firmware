//go:build linux

package deviceapi

import (
	"golang.org/x/sys/unix"
	"net"
	"syscall"
)

func peerUID(c net.Conn) (uint32, error) {
	// boundedConn wraps the accepted Unix connection without changing its peer.
	if b, ok := c.(*boundedConn); ok {
		c = b.Conn
	}
	sc, ok := c.(syscall.Conn)
	if !ok {
		return 0, ErrKeystore
	}
	raw, err := sc.SyscallConn()
	if err != nil {
		return 0, ErrKeystore
	}
	var uid uint32
	var credErr error
	err = raw.Control(func(fd uintptr) {
		u, e := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		credErr = e
		if e == nil {
			uid = u.Uid
		}
	})
	if err != nil || credErr != nil {
		return 0, ErrKeystore
	}
	return uid, nil
}
