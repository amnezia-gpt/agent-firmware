---
name: test-driven-development
description: Drives development with tests. Use when implementing any logic, fixing any bug, or changing any behavior. Use when you need to prove that code works, when a bug report arrives, or when you're about to modify existing functionality.
---

# Test-Driven Development

## Overview

Write a failing test before writing the code that makes it pass. For bug fixes, reproduce the bug with a test before attempting a fix. Tests are proof — "seems right" is not done. A codebase with good tests is an AI agent's superpower; a codebase without tests is a liability.

## When to Use

- Implementing any new logic or behavior
- Fixing any bug (the Prove-It Pattern)
- Modifying existing functionality
- Adding edge case handling
- Any change that could break existing behavior

**When NOT to use:** Pure configuration changes, documentation updates, or static content changes that have no behavioral impact.

For daemon, service, and container lifecycle changes, combine TDD with a pinned
runtime or disposable supported Linux-host check at the narrowest boundary that
proves the claim. Go tests belong beside the owning package in `*_test.go`.
Use temporary state directories, controlled clocks, and local/fake engine
boundaries; never let ordinary tests operate on the developer's workloads.

## The TDD Cycle

```
    RED                GREEN              REFACTOR
 Write a test    Write minimal code    Clean up the
 that fails  ──→  to make it pass  ──→  implementation  ──→  (repeat)
      │                  │                    │
      ▼                  ▼                    ▼
   Test FAILS        Test PASSES         Tests still PASS
```

### Step 1: RED — Write a Failing Test

Write the test first. It must fail. A test that passes immediately proves nothing.

```go
// RED: This fails because NewTarget is not implemented yet.
func TestNewTargetStartsPending(t *testing.T) {
    target := NewTarget("gateway", "release-1")
    if target.State != "pending" {
        t.Fatalf("state = %q, want pending", target.State)
    }
}
```

### Step 2: GREEN — Make It Pass

Write the minimum code to make the test pass. Don't over-engineer:

```go
// GREEN: Minimal implementation for the stated behavior.
func NewTarget(workload, release string) Target {
    return Target{Workload: workload, Release: release, State: "pending"}
}
```

### Step 3: REFACTOR — Clean Up

With tests green, improve the code without changing behavior:

- Extract shared logic
- Improve naming
- Remove duplication
- Optimize if necessary

Run tests after every refactor step to confirm nothing broke.

## The Prove-It Pattern (Bug Fixes)

When a bug is reported, **do not start by trying to fix it.** Start by writing a test that reproduces it.

```
Bug report arrives
       │
       ▼
  Write a test that demonstrates the bug
       │
       ▼
  Test FAILS (confirming the bug exists)
       │
       ▼
  Implement the fix
       │
       ▼
  Test PASSES (proving the fix works)
       │
       ▼
  Run full test suite (no regressions)
```

**Example:**

```go
// Bug: completing an operation does not record its completion time.
func TestCompleteRecordsCompletionTime(t *testing.T) {
    now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
    operation := Operation{State: "pending"}

    operation.Complete(now)

    if operation.CompletedAt.IsZero() || !operation.CompletedAt.Equal(now) {
        t.Fatalf("completion time = %v, want %v", operation.CompletedAt, now)
    }
}

// After observing the test fail, implement the fix.
func (operation *Operation) Complete(now time.Time) {
    operation.State = "completed"
    operation.CompletedAt = now
}
```

## The Test Pyramid

Invest testing effort according to the pyramid — most tests should be small and fast, with progressively fewer tests at higher levels:

```
          ╱╲
         ╱  ╲         E2E Tests (~5%)
        ╱    ╲        Full user flows, real browser
       ╱──────╲
      ╱        ╲      Integration Tests (~15%)
     ╱          ╲     Component interactions, API boundaries
    ╱────────────╲
   ╱              ╲   Unit Tests (~80%)
  ╱                ╲  Pure logic, isolated, milliseconds each
 ╱──────────────────╲
```

**The Beyonce Rule:** If you liked it, you should have put a test on it. Infrastructure changes, refactoring, and migrations are not responsible for catching your bugs — your tests are. If a change breaks your code and you didn't have a test for it, that's on you.

### Test Sizes (Resource Model)

Beyond the pyramid levels, classify tests by what resources they consume:

| Size | Constraints | Speed | Example |
|------|------------|-------|---------|
| **Small** | Single process, no I/O, no network, no database | Milliseconds | Pure function tests, data transforms |
| **Medium** | Multi-process OK, localhost only, no external services | Seconds | API tests with test DB, component tests |
| **Large** | Multi-machine OK, external services allowed | Minutes | E2E tests, performance benchmarks, staging integration |

Small tests should make up the vast majority of your suite. They're fast, reliable, and easy to debug when they fail.

### Decision Guide

```
Is it pure logic with no side effects?
  → Unit test (small)

Does it cross a boundary (API, database, file system)?
  → Integration test (medium)

Is it a critical user flow that must work end-to-end?
  → E2E test (large) — limit these to critical paths
```

## Writing Good Tests

### Test State, Not Interactions

Assert on the *outcome* of an operation, not on which methods were called internally. Tests that verify method call sequences break when you refactor, even if the behavior is unchanged.

```go
// Good: test the persisted result after reopening the store.
func TestSavedTargetSurvivesReopen(t *testing.T) {
    path := filepath.Join(t.TempDir(), "state.json")
    want := NewTarget("gateway", "release-1")
    store := NewStore(path)
    if err := store.Save(want); err != nil {
        t.Fatal(err)
    }
    got, err := NewStore(path).Load()
    if err != nil {
        t.Fatal(err)
    }
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("target = %#v, want %#v", got, want)
    }
}

// Avoid asserting internal helper-call sequences when persisted state is
// the contract. Refactoring should not break a behaviorally equivalent test.
```

### DAMP Over DRY in Tests

In production code, DRY (Don't Repeat Yourself) is usually right. In tests, **DAMP (Descriptive And Meaningful Phrases)** is better. A test should read like a specification — each test should tell a complete story without requiring the reader to trace through shared helpers.

```go
func TestValidateTargetRejectsEmptyWorkload(t *testing.T) {
    err := ValidateTarget(Target{Release: "release-1"})
    if !errors.Is(err, ErrInvalidTarget) {
        t.Fatalf("error = %v, want ErrInvalidTarget", err)
    }
}

func TestValidateTargetRejectsEmptyRelease(t *testing.T) {
    err := ValidateTarget(Target{Workload: "gateway"})
    if !errors.Is(err, ErrInvalidTarget) {
        t.Fatalf("error = %v, want ErrInvalidTarget", err)
    }
}

// Each test states its input and expected result without hidden shared state.
```

Duplication in tests is acceptable when it makes each test independently understandable.

### Prefer Real Implementations Over Mocks

Use the simplest test double that gets the job done. The more your tests use real code, the more confidence they provide.

```
Preference order (most to least preferred):
1. Real implementation  → Highest confidence, catches real bugs
2. Fake                 → In-memory version of a dependency (e.g., fake DB)
3. Stub                 → Returns canned data, no behavior
4. Mock (interaction)   → Verifies method calls — use sparingly
```

**Use mocks only when:** the real implementation is too slow, non-deterministic, or has side effects you can't control (external APIs, email sending). Over-mocking creates tests that pass while production breaks.

### Use the Arrange-Act-Assert Pattern

```go
func TestOperationIsExpiredAtDeadline(t *testing.T) {
    // Arrange: a fixed time avoids sleeping or depending on the wall clock.
    deadline := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
    operation := Operation{Deadline: deadline}

    // Act.
    expired := operation.IsExpired(deadline)

    // Assert.
    if !expired {
        t.Fatal("operation must expire at its deadline")
    }
}
```

### One Assertion Per Concept

```go
// Separate concepts so a failure identifies the broken behavior.
func TestValidateTargetRejectsUnknownWorkload(t *testing.T) { /* ... */ }
func TestValidateTargetRejectsInvalidRelease(t *testing.T) { /* ... */ }

// A table-driven test is appropriate when cases exercise one contract.
// Avoid combining unrelated lifecycle behaviors into one large test.
```

### Name Tests Descriptively

```go
// Good: names describe the observable behavior.
func TestApplyKeepsIdentityVolume(t *testing.T) { /* ... */ }
func TestInterruptedApplyRecoversOnRestart(t *testing.T) { /* ... */ }
func TestCancelReleasesEngineRequest(t *testing.T) { /* ... */ }

// Bad: names do not identify the contract.
func TestWorks(t *testing.T) { /* ... */ }
func TestCase3(t *testing.T) { /* ... */ }
```

## Test Anti-Patterns to Avoid

| Anti-Pattern | Problem | Fix |
|---|---|---|
| Testing implementation details | Tests break when refactoring even if behavior is unchanged | Test inputs and outputs, not internal structure |
| Flaky tests (timing, order-dependent) | Erode trust in the test suite | Use deterministic assertions, isolate test state |
| Testing framework code | Wastes time testing third-party behavior | Only test YOUR code |
| Snapshot abuse | Large snapshots nobody reviews, break on any change | Use snapshots sparingly and review every change |
| No test isolation | Tests pass individually but fail together | Each test sets up and tears down its own state |
| Mocking everything | Tests pass but production breaks | Prefer real implementations > fakes > stubs > mocks. Mock only at boundaries where real deps are slow or non-deterministic |

## Firmware And Workload Runtime Testing

Container and CLI compatibility claims need runtime proof in addition to unit
tests. Use the narrowest relevant layer:

- configuration validation for service units, install/uninstall scripts, and generated runtime config;
- Go component tests for reconciliation, local framing/permissions, cancellation, and redaction;
- interrupted apply, restart recovery, rollback, and persistent identity/data tests;
- disposable Linux-host checks for service lifecycle and the selected real engine/API;
- pinned workload releases when claiming Gateway/Codex integration compatibility;
- deterministic rejection and recovery fixtures for verification, update, and removal changes.

Apply `validation-and-evidence`: a passing Go test, cross-compiled binary, or
Docker API fake does not prove systemd installation or another engine/platform.
Go race detection uses a supported native CGO-enabled test toolchain; the
`CGO_ENABLED=0` requirement applies to the release binary.

Mocks prove this repository's behavior, not compatibility with an external
runtime. Never commit live credentials or captured sensitive callback data.

## Common Rationalizations

| Rationalization | Reality |
|---|---|
| "I'll write tests after the code works" | You won't. And tests written after the fact test implementation, not behavior. |
| "This is too simple to test" | Simple code gets complicated. The test documents the expected behavior. |
| "Tests slow me down" | Tests slow you down now. They speed you up every time you change the code later. |
| "I tested it manually" | Manual testing doesn't persist. Tomorrow's change might break it with no way to know. |
| "The code is self-explanatory" | Tests ARE the specification. They document what the code should do, not what it does. |
| "It's just a prototype" | Prototypes become production code. Tests from day one prevent the "test debt" crisis. |
| "Let me run the tests again just to be extra sure" | After a clean test run, repeating the same command adds nothing unless the code has changed since. Run again after subsequent edits, not as reassurance. |

## Red Flags

- Writing code without any corresponding tests
- Tests that pass on the first run (they may not be testing what you think)
- "All tests pass" but no tests were actually run
- Bug fixes without reproduction tests
- Tests that test framework behavior instead of application behavior
- Test names that don't describe the expected behavior
- Skipping tests to make the suite pass
- Running the same test command twice in a row without any intervening code change

## Verification

After completing any implementation:

- [ ] Every new behavior has a corresponding test
- [ ] The repository's documented relevant test commands pass
- [ ] Bug fixes include a reproduction test that failed before the fix
- [ ] Test names describe the behavior being verified
- [ ] No tests were skipped or disabled
- [ ] Coverage hasn't decreased (if tracked)

**Note:** Run each test command after a change that could affect the result. After a clean run, don't repeat the same command unless the code has changed since — re-running on unchanged code adds no confidence.
