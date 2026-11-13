package workload

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/strictjson"
)

type State struct {
	ID, Name, Installation, Role, Hash, Operation, Policy string
	Running, Healthy, Matches                             bool
}
type Engine interface {
	Preflight(context.Context, Stack) error
	Prepare(context.Context, Stack) error
	Inspect(context.Context, string, Spec) (State, error)
	Create(context.Context, Spec, string) (State, error)
	Mutate(context.Context, State, string, string) error
	Wait(context.Context, Spec, string) error
}
type Journal interface {
	Read() ([]byte, error)
	Write([]byte) error
}
type Result struct {
	Applied   bool `json:"applied"`
	Changed   bool `json:"changed"`
	Recovered bool `json:"recovered"`
}
type entry struct {
	SpecRole, Name, Action, NewID string
	Old                           State
}
type operation struct {
	Version        int
	ID, Generation string
	Committed      bool
	Entries        []entry
}

var fullID = regexp.MustCompile(`^[a-f0-9]{64}$`)
var operationID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var generationID = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func save(j Journal, op *operation) error {
	b, err := json.Marshal(op)
	if err != nil || j.Write(b) != nil {
		return ErrJournal
	}
	return nil
}
func specFor(stack Stack, role string) (Spec, bool) {
	for _, s := range stack.Specs {
		if s.Role == role {
			return s, true
		}
	}
	return Spec{}, false
}
func read(j Journal, stack Stack) (*operation, error) {
	b, err := j.Read()
	if err != nil {
		return nil, ErrJournal
	}
	if len(b) == 0 {
		return nil, nil
	}
	if len(b) > 32*1024 {
		return nil, ErrJournal
	}
	var op operation
	fields, ok := strictjson.Object(b, "Version", "ID", "Generation", "Committed", "Entries")
	if !ok || string(bytes.TrimSpace(fields["Version"])) != "1" {
		return nil, ErrJournal
	}
	var entries []json.RawMessage
	if json.Unmarshal(fields["Entries"], &entries) != nil {
		return nil, ErrJournal
	}
	for _, raw := range entries {
		entryFields, ok := strictjson.Object(raw, "SpecRole", "Name", "Action", "NewID", "Old")
		if !ok {
			return nil, ErrJournal
		}
		if _, ok := strictjson.Object(entryFields["Old"], "ID", "Name", "Installation", "Role", "Hash", "Operation", "Policy", "Running", "Healthy", "Matches"); !ok {
			return nil, ErrJournal
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&op) != nil || op.Version != 1 || !operationID.MatchString(op.ID) || !generationID.MatchString(op.Generation) || len(op.Entries) < 1 || len(op.Entries) > 3 {
		return nil, ErrJournal
	}
	seen := map[string]bool{}
	for _, v := range op.Entries {
		s, ok := specFor(stack, v.SpecRole)
		if !ok || seen[v.SpecRole] || v.Name != s.Name || (v.NewID != "" && !fullID.MatchString(v.NewID)) {
			return nil, ErrJournal
		}
		seen[v.SpecRole] = true
		switch v.Action {
		case "create":
			if v.Old.ID != "" {
				return nil, ErrJournal
			}
		case "noop", "start", "recreate", "restart":
			if !fullID.MatchString(v.Old.ID) || v.Old.Name != s.Name || v.Old.Installation != s.Installation || v.Old.Role != s.Role {
				return nil, ErrJournal
			}
		default:
			return nil, ErrJournal
		}
	}
	return &op, nil
}

// Apply recovers an unfinished transaction before starting any new generation.
func Apply(parent context.Context, e Engine, j Journal, stack Stack) (Result, error) {
	result := Result{}
	if err := parent.Err(); err != nil {
		return result, bounded(err)
	}
	if previous, err := read(j, stack); err != nil {
		return result, err
	} else if previous != nil {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 2*time.Minute)
		if previous.Committed {
			err = cleanup(ctx, e, stack, previous)
		} else {
			err = rollback(ctx, e, stack, previous)
		}
		cancel()
		if err != nil {
			return result, ErrRecovery
		}
		if j.Write(nil) != nil {
			return result, ErrJournal
		}
		result.Recovered = true
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Minute)
	defer cancel()
	if err := e.Preflight(ctx, stack); err != nil {
		return result, bounded(err)
	}
	if err := e.Prepare(ctx, stack); err != nil {
		return result, bounded(err)
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return result, ErrJournal
	}
	op := operation{Version: 1, ID: hex.EncodeToString(nonce[:]), Generation: stack.Generation}
	gatewayChanged := false
	for _, s := range stack.Specs {
		old, err := e.Inspect(ctx, s.Name, s)
		if err != nil {
			return result, bounded(err)
		}
		if old.ID != "" && (old.Installation != s.Installation || old.Role != s.Role) {
			return result, ErrConflict
		}
		action := "noop"
		switch {
		case old.ID == "":
			action = "create"
		case !old.Matches || old.Running && !old.Healthy:
			action = "recreate"
		case !old.Running:
			action = "start"
		case gatewayChanged:
			action = "restart"
		}
		if s.Role == "gateway" {
			gatewayChanged = action != "noop"
		}
		if action != "noop" {
			result.Changed = true
		}
		op.Entries = append(op.Entries, entry{SpecRole: s.Role, Name: s.Name, Action: action, Old: old})
	}
	if !result.Changed {
		result.Applied = true
		return result, nil
	}
	if err := save(j, &op); err != nil {
		return result, err
	}
	fail := func(cause error) (Result, error) {
		recovery, stop := context.WithTimeout(context.WithoutCancel(parent), 2*time.Minute)
		defer stop()
		if err := rollback(recovery, e, stack, &op); err != nil {
			return result, ErrRecovery
		}
		if j.Write(nil) != nil {
			return result, ErrJournal
		}
		return result, bounded(cause)
	}
	for i := range op.Entries {
		v := &op.Entries[i]
		s, _ := specFor(stack, v.SpecRole)
		if v.Action == "noop" {
			continue
		}
		if v.Action == "recreate" {
			if err := e.Mutate(ctx, v.Old, "policy", "no"); err != nil {
				return fail(err)
			}
			if err := e.Mutate(ctx, v.Old, "stop", ""); err != nil {
				return fail(err)
			}
			if err := e.Mutate(ctx, v.Old, "rename", backup(v.Name, op.ID)); err != nil {
				return fail(err)
			}
		}
		current := v.Old
		if v.Action == "create" || v.Action == "recreate" {
			var err error
			current, err = e.Create(ctx, s, op.ID)
			if err != nil {
				return fail(err)
			}
			v.NewID = current.ID
			if err := save(j, &op); err != nil {
				return result, err
			}
		}
		if v.Action == "restart" {
			if err := e.Mutate(ctx, current, "stop", ""); err != nil {
				return fail(err)
			}
		}
		if err := e.Mutate(ctx, current, "start", ""); err != nil {
			return fail(err)
		}
		if err := e.Wait(ctx, s, current.ID); err != nil {
			return fail(err)
		}
	}
	for _, s := range stack.Specs {
		actual, err := e.Inspect(ctx, s.Name, s)
		if err != nil {
			return fail(err)
		}
		if !actual.Running || !actual.Healthy || !actual.Matches {
			return fail(ErrHealth)
		}
	}
	op.Committed = true
	if err := save(j, &op); err != nil {
		return result, err
	}
	cleanupCtx, stop := context.WithTimeout(context.WithoutCancel(parent), 2*time.Minute)
	defer stop()
	if err := cleanup(cleanupCtx, e, stack, &op); err != nil {
		return result, ErrCleanup
	}
	if j.Write(nil) != nil {
		return result, ErrJournal
	}
	result.Applied = true
	return result, nil
}

func backup(name, id string) string { return name + "-backup-" + id[:12] }
func rollback(ctx context.Context, e Engine, stack Stack, op *operation) error {
	for i := len(op.Entries) - 1; i >= 0; i-- {
		v := op.Entries[i]
		if v.Action == "noop" {
			continue
		}
		s, _ := specFor(stack, v.SpecRole)
		current, err := e.Inspect(ctx, v.Name, s)
		if err != nil {
			return err
		}
		if v.Action == "create" || v.Action == "recreate" {
			if current.ID != "" && current.ID != v.Old.ID {
				if current.Operation != op.ID || v.NewID != "" && current.ID != v.NewID {
					return ErrRecovery
				}
				if err := e.Mutate(ctx, current, "remove", ""); err != nil {
					return err
				}
			}
		}
		if v.Action == "create" {
			continue
		}
		if v.Action == "recreate" {
			kept, err := e.Inspect(ctx, backup(v.Name, op.ID), s)
			if err != nil {
				return err
			}
			if kept.ID != "" {
				if kept.ID != v.Old.ID {
					return ErrRecovery
				}
				if err := e.Mutate(ctx, kept, "rename", v.Name); err != nil {
					return err
				}
			}
		}
		current, err = e.Inspect(ctx, v.Name, s)
		if err != nil {
			return err
		}
		if current.ID != v.Old.ID {
			return ErrRecovery
		}
		if err := e.Mutate(ctx, current, "policy", v.Old.Policy); err != nil {
			return err
		}
		if current.Running != v.Old.Running {
			action := "stop"
			if v.Old.Running {
				action = "start"
			}
			if err := e.Mutate(ctx, current, action, ""); err != nil {
				return err
			}
		}
		check, err := e.Inspect(ctx, v.Name, s)
		if err != nil || check.ID != v.Old.ID || check.Running != v.Old.Running || check.Policy != v.Old.Policy {
			return ErrRecovery
		}
	}
	return nil
}

func cleanup(ctx context.Context, e Engine, stack Stack, op *operation) error {
	for _, v := range op.Entries {
		if v.Action != "recreate" {
			continue
		}
		s, _ := specFor(stack, v.SpecRole)
		current, err := e.Inspect(ctx, v.Name, s)
		if err != nil || current.ID != v.NewID {
			return ErrRecovery
		}
		kept, err := e.Inspect(ctx, backup(v.Name, op.ID), s)
		if err != nil {
			return err
		}
		if kept.ID == "" {
			continue
		}
		if kept.ID != v.Old.ID {
			return ErrRecovery
		}
		if err := e.Mutate(ctx, kept, "remove", ""); err != nil {
			return err
		}
	}
	return nil
}
func bounded(err error) error {
	if errors.Is(err, context.Canceled) {
		return ErrCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrDeadline
	}
	var code Error
	if errors.As(err, &code) {
		return code
	}
	return ErrEngine
}
