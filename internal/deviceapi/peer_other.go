//go:build !linux

package deviceapi

import "net"

func peerUID(net.Conn) (uint32, error) { return 0, ErrKeystore }
