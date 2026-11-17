//go:build linux

package deviceapi

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"golang.org/x/sys/unix"
)

func startWatch(path string, publish func(bool, bool)) (func(), os.FileInfo, error) {
	// No config file is opened. The pinned directory FD is the only watch target.
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, ErrWatch
	}
	dir := os.NewFile(uintptr(fd), "owned-agent-home")
	info, err := dir.Stat()
	if err != nil {
		dir.Close()
		return nil, nil, ErrWatch
	}
	in, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		dir.Close()
		return nil, nil, ErrWatch
	}
	mask := uint32(unix.IN_MODIFY | unix.IN_CLOSE_WRITE | unix.IN_ATTRIB | unix.IN_MOVED_TO | unix.IN_MOVED_FROM | unix.IN_CREATE | unix.IN_DELETE | unix.IN_DELETE_SELF | unix.IN_MOVE_SELF | unix.IN_UNMOUNT | unix.IN_ONLYDIR)
	if _, err := unix.InotifyAddWatch(in, filepath.Join(host.DefaultLayout().ProcFD, strconv.Itoa(fd)), mask); err != nil {
		unix.Close(in)
		dir.Close()
		return nil, nil, ErrWatch
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		defer unix.Close(in)
		defer dir.Close()
		buf := make([]byte, 16*1024)
		dirty := false
		var due time.Time
		for {
			select {
			case <-stop:
				return
			default:
			}
			poll := []unix.PollFd{{Fd: int32(in), Events: unix.POLLIN}}
			_, err := unix.Poll(poll, 100)
			if err == unix.EINTR {
				continue
			}
			if err != nil {
				publish(false, true)
				return
			}
			if poll[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
				publish(false, true)
				return
			}
			if poll[0].Revents&unix.POLLIN != 0 {
				n, err := unix.Read(in, buf)
				if err != nil && err != unix.EAGAIN {
					publish(false, true)
					return
				}
				for off := 0; off+16 <= n; {
					mask := binary.NativeEndian.Uint32(buf[off+4 : off+8])
					length := int(binary.NativeEndian.Uint32(buf[off+12 : off+16]))
					end := off + 16 + length
					if end > n {
						publish(false, true)
						return
					}
					if mask&(unix.IN_IGNORED|unix.IN_UNMOUNT|unix.IN_DELETE_SELF|unix.IN_MOVE_SELF) != 0 {
						publish(false, true)
						return
					}
					if mask&unix.IN_Q_OVERFLOW != 0 {
						publish(true, true)
					}
					name := strings.TrimRight(string(buf[off+16:end]), "\x00")
					if name == "config.toml" {
						if !dirty {
							due = time.Now().Add(100 * time.Millisecond)
						}
						dirty = true
					}
					off = end
				}
			}
			if dirty && !time.Now().Before(due) {
				publish(true, false)
				dirty = false
			}
		}
	}()
	return func() { once.Do(func() { close(stop); <-done }) }, info, nil
}
