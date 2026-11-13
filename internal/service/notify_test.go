package service

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadinessUsesOnlyOneBoundedLocalDatagram(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "af-notify-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "notify.sock")
	listener, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	n := Notifier{Socket: path}
	if err := n.Ready(); err != nil {
		t.Fatal(err)
	}
	if err := n.Ready(); err != nil {
		t.Fatal(err)
	}
	listener.SetReadDeadline(time.Now().Add(time.Second))
	buffer := make([]byte, 64)
	size, _, err := listener.ReadFromUnix(buffer)
	if err != nil || string(buffer[:size]) != "READY=1" {
		t.Fatalf("notification=%q error=%v", buffer[:size], err)
	}
	listener.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	if _, _, err := listener.ReadFromUnix(buffer); err == nil {
		t.Fatal("duplicate readiness notification")
	}
}

func TestAbsentNotifySocketPreservesForegroundBehavior(t *testing.T) {
	n := Notifier{}
	if err := n.Ready(); err != nil {
		t.Fatal(err)
	}
}

func TestNotifyFailureDoesNotExposeSocketPath(t *testing.T) {
	n := Notifier{Socket: filepath.Join(t.TempDir(), "private-example")}
	if err := n.Ready(); err != ErrNotify {
		t.Fatalf("error=%v", err)
	}
}
