package supervisor

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
)

func TestTransitionsAreBoundedAndDuplicateSuccessIsSilent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := 0
	var events []Event
	cycle := func(context.Context) (bool, error) {
		n++
		switch n {
		case 1:
			return false, configuration.ErrMissing
		case 2:
			return false, configuration.ErrBusy
		case 3:
			return true, nil
		case 4:
			return false, nil
		case 5:
			return false, workload.ErrConflict
		default:
			cancel()
			return false, context.Canceled
		}
	}
	emit := func(e Event) error { events = append(events, e); return nil }
	if err := Run(ctx, cycle, emit, time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	want := []Event{{State: "awaiting_configuration"}, {State: "busy"}, {State: "repaired"}, {State: "converged"}, {State: "failed", ErrorCode: "resource_conflict"}, {State: "stopped"}}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events=%+v", events)
	}
}

func TestCancellationReachesActiveCycleAndExitsWithoutRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	finished := make(chan error, 1)
	calls := 0
	go func() {
		finished <- Run(ctx, func(c context.Context) (bool, error) { calls++; close(entered); <-c.Done(); return false, c.Err() }, func(Event) error { return nil }, time.Hour)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("cycle did not start")
	}
	cancel()
	select {
	case err := <-finished:
		if err != nil || calls != 1 {
			t.Fatal("cancelled supervisor retried")
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not stop supervisor")
	}
}

func TestOutputFailureStopsBeforeAnotherCycleAndRedactsUnknownErrors(t *testing.T) {
	calls := 0
	err := Run(context.Background(), func(context.Context) (bool, error) { calls++; return false, errors.New("sensitive-example") }, func(e Event) error {
		if e.ErrorCode != "supervision_failed" {
			t.Fatal("raw cycle error leaked")
		}
		return errors.New("sensitive output error")
	}, time.Nanosecond)
	if err != ErrOutput || calls != 1 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
}
