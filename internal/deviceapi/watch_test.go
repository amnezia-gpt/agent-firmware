package deviceapi

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWatchResyncTimeoutResetAndCancellation(t *testing.T) {
	w := NewWatcher()
	defer w.Close()
	if _, err := w.Wait(context.Background(), "", 0); !errors.Is(err, ErrWatch) {
		t.Fatal("unbound watch available")
	}
	w.publish(true, true)
	initial, err := w.Wait(context.Background(), "", 0)
	if err != nil || !initial.Resync || initial.Cursor == "" {
		t.Fatal("no startup resync")
	}
	unchanged, err := w.Wait(context.Background(), initial.Cursor, time.Millisecond)
	if err != nil || unchanged.Resync || unchanged.Cursor != initial.Cursor {
		t.Fatal("timeout lied")
	}
	w.publish(true, false)
	next, err := w.Wait(context.Background(), initial.Cursor, 0)
	if err != nil || !next.Resync || next.Cursor == initial.Cursor {
		t.Fatal("event lost")
	}
	w.publish(true, true)
	reset, _ := w.Wait(context.Background(), next.Cursor, 0)
	if !reset.Resync || reset.Cursor == next.Cursor {
		t.Fatal("epoch not reset")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := w.Wait(ctx, reset.Cursor, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel ignored")
	}
	w.publish(false, true)
	if _, err := w.Wait(context.Background(), reset.Cursor, 0); !errors.Is(err, ErrWatch) {
		t.Fatal("loss hidden")
	}
}
