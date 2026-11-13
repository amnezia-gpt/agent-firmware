package deviceapi

import (
	"context"
	"encoding/hex"
	"os"
	"strconv"
	"sync"
	"time"
)

const ErrWatch Error = "watch_unavailable"

// Watcher retains one invalidation generation, never file contents or event history.
type Watcher struct {
	diagnostics   *diagnostics
	mu            sync.Mutex
	bindMu        sync.Mutex
	ready, closed bool
	epoch         string
	counter       uint64
	changed       chan struct{}
	stop          func()
	path          string
	inode         os.FileInfo
}

func NewWatcher() *Watcher { return &Watcher{changed: make(chan struct{})} }
func (w *Watcher) publish(ready, reset bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	if reset || w.epoch == "" {
		b, err := randomBytes(16)
		if err != nil {
			ready = false
		} else {
			w.epoch = hex.EncodeToString(b)
		}
		w.counter = 0
	}
	if w.diagnostics != nil {
		phase := "resync"
		if !ready {
			phase = "unavailable"
		} else if !w.ready {
			phase = "ready"
		}
		w.diagnostics.record("config_watch", "invalidate", phase, phase, 0)
	}
	w.ready = ready
	w.counter++
	close(w.changed)
	w.changed = make(chan struct{})
}
func (w *Watcher) State() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ready && !w.closed {
		return "ready"
	}
	return "unavailable"
}
func (w *Watcher) Wait(ctx context.Context, after string, hold time.Duration) (Change, error) {
	timer := time.NewTimer(hold)
	defer timer.Stop()
	for {
		if ctx.Err() != nil {
			return Change{}, ctx.Err()
		}
		w.mu.Lock()
		if !w.ready || w.closed {
			w.mu.Unlock()
			return Change{}, ErrWatch
		}
		cursor := "e_" + w.epoch + ":" + strconv.FormatUint(w.counter, 10)
		changed := w.changed
		w.mu.Unlock()
		if after != cursor {
			return Change{cursor, true}, nil
		}
		select {
		case <-ctx.Done():
			return Change{}, ctx.Err()
		case <-changed:
			continue
		case <-timer.C:
			return Change{cursor, false}, nil
		}
	}
}

// Bind accepts a host path only from the owned local-volume resolver, never HTTP.
func (w *Watcher) Bind(path string) error {
	w.bindMu.Lock()
	defer w.bindMu.Unlock()
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return ErrWatch
	}
	info, err := os.Lstat(path)
	if err == nil && info.IsDir() && w.path == path && w.inode != nil && os.SameFile(info, w.inode) && w.State() == "ready" {
		return nil
	}
	if w.stop != nil {
		w.stop()
		w.stop = nil
	}
	w.path = ""
	w.inode = nil
	w.publish(false, true)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrWatch
	}
	stop, opened, err := startWatch(path, w.publish)
	if err != nil {
		return ErrWatch
	}
	w.stop = stop
	w.path = path
	w.inode = opened
	// The kernel watch is already installed before exposing the initial generation.
	w.publish(true, true)
	return nil
}
func (w *Watcher) Unbind() {
	w.bindMu.Lock()
	defer w.bindMu.Unlock()
	if w.stop != nil {
		w.stop()
		w.stop = nil
	}
	w.path = ""
	w.inode = nil
	w.publish(false, true)
}
func (w *Watcher) Close() {
	w.bindMu.Lock()
	defer w.bindMu.Unlock()
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	w.ready = false
	close(w.changed)
	w.mu.Unlock()
	if w.stop != nil {
		w.stop()
		w.stop = nil
	}
}
