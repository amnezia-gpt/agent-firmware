package workload

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func testStack(t *testing.T) Stack {
	t.Helper()
	p, err := os.ReadFile("../backendprofile/testdata/profile-v4.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := os.ReadFile("../workloadrelease/testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	stack, err := FromReaders(bytes.NewReader(p), bytes.NewReader(r))
	if err != nil {
		t.Fatal(err)
	}
	return stack
}

func TestManifestStackIsIsolatedAndHasOnlyResolvedImages(t *testing.T) {
	s := testStack(t)
	if len(s.Specs) != 3 || len(s.Volumes) != 4 {
		t.Fatal("wrong topology")
	}
	if s.Specs[1].Image != s.Specs[2].Image {
		t.Fatal("runtime images differ")
	}
	for i, spec := range s.Specs {
		if spec.Installation == "" || spec.Network != s.Network || spec.Name == "amnezia-openclaw-codex" {
			t.Fatal("unsafe namespace")
		}
		if spec.ReadOnly != (i > 0) {
			t.Fatal("wrong root filesystem policy")
		}
		for _, m := range spec.Mounts {
			if m.Name == "amnezia-agent-runtime-codex-home" {
				t.Fatal("client volume reused")
			}
		}
	}
	if !s.Specs[1].Mounts[1].ReadOnly || !s.Specs[2].Mounts[1].ReadOnly {
		t.Fatal("capability mount writable")
	}
	if len(s.Specs[1].Env) != 0 || len(s.Specs[2].Env) != 0 {
		t.Fatal("backend data leaked to runtime")
	}
}

type memoryJournal struct {
	data []byte
	fail bool
}

func (j *memoryJournal) Read() ([]byte, error) { return append([]byte(nil), j.data...), nil }
func (j *memoryJournal) Write(b []byte) error {
	if j.fail {
		return errors.New("disk error")
	}
	j.data = append([]byte(nil), b...)
	return nil
}

type fakeEngine struct {
	states     map[string]State
	calls      []string
	retired    []State
	failRole   string
	loseCreate bool
	conflict   bool
	failRemove bool
	next       int
}

func fake() *fakeEngine                                               { return &fakeEngine{states: map[string]State{}} }
func (f *fakeEngine) Retired(context.Context, Stack) ([]State, error) { return f.retired, nil }
func (f *fakeEngine) Preflight(context.Context, Stack) error {
	if f.conflict {
		return ErrConflict
	}
	return nil
}
func (f *fakeEngine) Prepare(context.Context, Stack) error {
	f.calls = append(f.calls, "prepare")
	return nil
}
func (f *fakeEngine) Inspect(_ context.Context, name string, spec Spec) (State, error) {
	s := f.states[name]
	s.Matches = s.Hash == spec.Hash()
	return s, nil
}
func (f *fakeEngine) Create(_ context.Context, spec Spec, op string) (State, error) {
	f.next++
	s := State{ID: fmt.Sprintf("%064x", f.next), Name: spec.Name, Installation: spec.Installation, Role: spec.Role, Hash: spec.Hash(), Operation: op, Policy: "unless-stopped", Matches: true}
	f.states[s.Name] = s
	f.calls = append(f.calls, "create:"+s.Role)
	if f.loseCreate {
		f.loseCreate = false
		return State{}, ErrEngine
	}
	return s, nil
}
func (f *fakeEngine) Mutate(_ context.Context, s State, action, value string) error {
	if action == "remove" && f.failRemove {
		f.failRemove = false
		return ErrEngine
	}
	current, ok := f.states[s.Name]
	if !ok || current.ID != s.ID {
		return ErrConflict
	}
	f.calls = append(f.calls, action+":"+s.Role)
	switch action {
	case "start":
		current.Running = true
		current.Healthy = true
	case "stop":
		current.Running = false
	case "remove":
		delete(f.states, s.Name)
		return nil
	case "rename":
		delete(f.states, s.Name)
		current.Name = value
	case "policy":
		current.Policy = value
	}
	f.states[current.Name] = current
	return nil
}
func (f *fakeEngine) Wait(ctx context.Context, spec Spec, id string) error {
	f.calls = append(f.calls, "wait:"+spec.Role)
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.failRole == spec.Role {
		return ErrHealth
	}
	s := f.states[spec.Name]
	if s.ID != id || !s.Running {
		return ErrHealth
	}
	return nil
}

func TestBootstrapThenNoOpAndStoppedRepair(t *testing.T) {
	s := testStack(t)
	e := fake()
	j := &memoryJournal{}
	result, err := Apply(context.Background(), e, j, s)
	if err != nil || !result.Changed || len(e.states) != 3 || len(j.data) != 0 {
		t.Fatalf("bootstrap: %+v %v", result, err)
	}
	ids := map[string]string{}
	for name, v := range e.states {
		ids[name] = v.ID
	}
	result, err = Apply(context.Background(), e, j, s)
	if err != nil || result.Changed {
		t.Fatalf("NoOp: %+v %v", result, err)
	}
	for name, v := range e.states {
		if v.ID != ids[name] {
			t.Fatal("NoOp replaced a container")
		}
	}
	v := e.states[s.Specs[1].Name]
	v.Running = false
	e.states[v.Name] = v
	if _, err := Apply(context.Background(), e, j, s); err != nil {
		t.Fatal(err)
	}
	if e.states[v.Name].ID != v.ID || !e.states[v.Name].Running {
		t.Fatal("stopped container not repaired in place")
	}
}

func TestFirstApplyFailureRollsBackAllContainers(t *testing.T) {
	s := testStack(t)
	e := fake()
	e.failRole = "storage"
	j := &memoryJournal{}
	if _, err := Apply(context.Background(), e, j, s); !errors.Is(err, ErrHealth) {
		t.Fatalf("error=%v", err)
	}
	if len(e.states) != 0 || len(j.data) != 0 {
		t.Fatal("failed group was not rolled back")
	}
}

func TestFailedRecreateRestoresOriginalIDsAndRunningState(t *testing.T) {
	s := testStack(t)
	e := fake()
	j := &memoryJournal{}
	if _, err := Apply(context.Background(), e, j, s); err != nil {
		t.Fatal(err)
	}
	old := map[string]State{}
	for name, v := range e.states {
		old[name] = v
		v.Hash = "drift"
		e.states[name] = v
	}
	e.failRole = "storage"
	if _, err := Apply(context.Background(), e, j, s); err == nil {
		t.Fatal("failed health accepted")
	}
	for name, v := range old {
		now := e.states[name]
		if now.ID != v.ID || now.Policy != v.Policy || now.Running != v.Running {
			t.Fatal("original container state lost")
		}
	}
	if len(e.states) != 3 {
		t.Fatal("backup or replacement left after rollback")
	}
}

func TestLostCreateResponseIsReconciledBeforeCleanup(t *testing.T) {
	s := testStack(t)
	e := fake()
	e.loseCreate = true
	j := &memoryJournal{}
	if _, err := Apply(context.Background(), e, j, s); err == nil {
		t.Fatal("lost response reported success")
	}
	if len(e.states) != 0 {
		t.Fatal("owned create with lost response leaked")
	}
}

func TestForeignConflictAndJournalFailureHaveNoContainerMutations(t *testing.T) {
	s := testStack(t)
	for _, mode := range []string{"conflict", "journal"} {
		e := fake()
		j := &memoryJournal{}
		if mode == "conflict" {
			e.conflict = true
		} else {
			j.fail = true
		}
		if _, err := Apply(context.Background(), e, j, s); err == nil {
			t.Fatal("failure accepted")
		}
		if len(e.states) != 0 {
			t.Fatal("container mutated without admission/journal")
		}
	}
}

func TestPendingJournalRecoveryBeforeNewApply(t *testing.T) {
	s := testStack(t)
	e := fake()
	j := &memoryJournal{}
	op := operation{Version: 1, ID: "11111111111111111111111111111111", Generation: s.Generation, Entries: []entry{{SpecRole: s.Specs[0].Role, Name: s.Specs[0].Name, Action: "create"}}}
	if err := save(j, &op); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Create(context.Background(), s.Specs[0], op.ID); err != nil {
		t.Fatal(err)
	}
	result, err := Apply(context.Background(), e, j, s)
	if err != nil || !result.Recovered || len(e.states) != 3 {
		t.Fatalf("recovery %+v %v", result, err)
	}
}

func TestCommittedCleanupFailureNeverRollsBackVerifiedReplacement(t *testing.T) {
	s := testStack(t)
	e := fake()
	j := &memoryJournal{}
	if _, err := Apply(context.Background(), e, j, s); err != nil {
		t.Fatal(err)
	}
	v := e.states[s.Specs[1].Name]
	oldID := v.ID
	v.Hash = "drift"
	e.states[v.Name] = v
	e.failRemove = true
	if _, err := Apply(context.Background(), e, j, s); !errors.Is(err, ErrCleanup) {
		t.Fatalf("error=%v", err)
	}
	newID := e.states[v.Name].ID
	if newID == oldID || len(j.data) == 0 {
		t.Fatal("verified replacement was rolled back")
	}
	result, err := Apply(context.Background(), e, j, s)
	if err != nil || !result.Recovered || result.Changed || e.states[v.Name].ID != newID || len(e.states) != 3 {
		t.Fatalf("committed cleanup: %+v %v", result, err)
	}
}

func TestCanceledAdmissionDoesNotMutateResources(t *testing.T) {
	e := fake()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Apply(ctx, e, &memoryJournal{}, testStack(t)); !errors.Is(err, ErrCanceled) {
		t.Fatalf("error=%v", err)
	}
	if len(e.calls) != 0 {
		t.Fatal("cancelled operation reached mutation preparation")
	}
}

func TestAmbiguousOperationJournalIsRejected(t *testing.T) {
	s := testStack(t)
	j := &memoryJournal{}
	op := operation{Version: 1, ID: strings.Repeat("1", 32), Generation: s.Generation, Entries: []entry{{SpecRole: s.Specs[0].Role, Name: s.Specs[0].Name, Action: "create"}}}
	if err := save(j, &op); err != nil {
		t.Fatal(err)
	}
	j.data = []byte(strings.Replace(string(j.data), `"Committed":false`, `"Committed":false,"Committed":true`, 1))
	if _, err := Apply(context.Background(), fake(), j, s); !errors.Is(err, ErrJournal) {
		t.Fatalf("ambiguous journal accepted: %v", err)
	}
}

func TestRemovedServiceIsRetiredOrRestoredWithTransaction(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "committed"
		if fail {
			name = "rolled back"
		}
		t.Run(name, func(t *testing.T) {
			old := testStack(t)
			e := fake()
			journal := &memoryJournal{}
			if _, err := Apply(t.Context(), e, journal, old); err != nil {
				t.Fatal(err)
			}
			removed := e.states[old.Specs[2].Name]
			next := old
			next.Specs = append([]Spec(nil), old.Specs[:2]...)
			e.retired = []State{removed}
			if fail {
				next.Specs[0].Env = map[string]string{"CHANGED": "value"}
				e.failRole = next.Specs[0].Role
			}
			result, err := Apply(t.Context(), e, journal, next)
			actual, present := e.states[removed.Name]
			if fail {
				if !errors.Is(err, ErrHealth) || !present || actual.ID != removed.ID ||
					actual.Policy != removed.Policy || actual.Running != removed.Running {
					t.Fatalf("rollback failed: %+v %v", actual, err)
				}
			} else if err != nil || !result.Applied || !result.Changed || present {
				t.Fatalf("retirement failed: %+v %v", result, err)
			}
			if len(journal.data) != 0 {
				t.Fatal("transaction left a journal")
			}
		})
	}
}
