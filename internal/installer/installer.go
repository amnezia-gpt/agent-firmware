// Package installer places the running firmware binary on the host as a
// host service inside one recoverable transaction: a failed or interrupted
// installation returns the previous version, unit and accepted configuration.
// Host preparation (platform, Docker, download) belongs to the bootstrap
// script; everything from here on is firmware responsibility.
package installer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	hostplatform "github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/privatefile"
)

// Error is a bounded diagnostic; it never contains input, paths or underlying errors.
type Error string

func (e Error) Error() string { return string(e) }

const (
	ErrConflict    Error = "installation_conflict"
	ErrBusy        Error = "installation_busy"
	ErrRecovery    Error = "installation_recovery_required"
	ErrInput       Error = "installation_input_too_large"
	ErrWrite       Error = "installation_write_failed"
	ErrInterrupted Error = "installation_interrupted"
	ErrDocker      Error = "docker_prerequisite_failed"
	ErrExisting    Error = "invalid_existing_configuration"
	ErrConfigure   Error = "configuration_rejected"
	ErrCode        Error = "enrollment_code_rejected"
	ErrSystemd     Error = "systemd_unavailable"
	ErrStop        Error = "service_stop_failed"
	ErrReload      Error = "service_reload_failed"
	ErrEnable      Error = "service_enable_failed"
	ErrStart       Error = "service_start_failed"
)

const (
	managedMarker = "agent-firmware-installer-v1"
	// The transaction layout is versioned so a later firmware never guesses at an older one.
	transactionFormat = "2"
	maxBinary         = 64 << 20
)

// ServiceManager supplies the platform's service definition and operations.
type ServiceManager interface {
	Definition() []byte
	Owns(definition []byte) bool
	Stop(context.Context) error
	Start(context.Context) error
	Reload(context.Context) error
	SetEnabled(context.Context, bool) error
	Active(context.Context) bool
	Enabled(context.Context) bool
	Fragment(context.Context) (string, error)
}

// Firmware is what the installer asks of the firmware's own state handling.
type Firmware interface {
	Preflight(ctx context.Context) error
	// Existing checks the retained configuration of an earlier installation, if any.
	Existing() error
	Configure(context.Context) error
	HoldCode(code string) error
	ClearCode()
}

// Request is one installation of the binary at Binary. Code may be empty.
type Request struct {
	Layout   hostplatform.Layout
	Binary   string
	Code     string
	Services ServiceManager
	Firmware Firmware
	// Progress, when set, receives short human-readable lines about the current step.
	Progress func(message string)
}

func (r *run) say(message string) {
	if r.Progress != nil {
		r.Progress(message)
	}
}

// waiting repeats a reassurance while a long step runs, so a silent start is not mistaken for a hang.
func (r *run) waiting(message string) (stop func()) {
	if r.Progress == nil {
		return func() {}
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(waitingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				r.Progress(message)
			}
		}
	}()
	return func() { close(done); <-finished }
}

var waitingInterval = 30 * time.Second

type run struct {
	Request
	transaction string
	digest      string
}

// Install runs the transaction. ctx carries interruption only: recovery after a
// failure always runs to its own end.
func Install(ctx context.Context, request Request) (err error) {
	r := &run{Request: request, transaction: filepath.Join(request.Layout.Base, ".transaction")}
	l := r.Layout
	for _, directory := range l.Parents {
		if !r.safeDir(directory, 0o755) {
			return ErrConflict
		}
	}
	unlock, err := r.lock()
	if err != nil {
		return err
	}
	defer unlock()

	// Recover a previous owned transaction before beginning a new one.
	if lexists(l.Base) {
		if !r.safeDir(l.Base, 0o755) || !r.safeFile(filepath.Join(l.Base, ".managed"), 0o644) || !hasLine(filepath.Join(l.Base, ".managed"), managedMarker) {
			return ErrConflict
		}
		if r.recover() != nil {
			return ErrRecovery
		}
	}
	binary, err := r.readBinary()
	if err != nil {
		return err
	}
	r.say("Checking the host and Docker")
	if r.Firmware.Preflight(ctx) != nil {
		return ErrDocker
	}
	if err := r.conflicts(ctx); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ErrInterrupted
	}

	if !lexists(l.Base) {
		if os.Mkdir(l.Base, 0o755) != nil || os.Chmod(l.Base, 0o755) != nil || write(filepath.Join(l.Base, ".managed"), []byte(managedMarker+"\n"), 0o644) != nil {
			return ErrWrite
		}
	}
	releases := filepath.Join(l.Base, "releases")
	if !lexists(releases) {
		if os.Mkdir(releases, 0o755) != nil || os.Chmod(releases, 0o755) != nil {
			return ErrWrite
		}
	}
	if !r.safeDir(releases, 0o755) {
		return ErrConflict
	}
	if err := r.placeVersion(releases, binary); err != nil {
		return err
	}

	if err := r.prepare(ctx); err != nil {
		return err
	}
	// From here every failure rolls back to the recorded previous installation.
	codeHeld := false
	defer func() {
		if err == nil {
			return
		}
		r.say("Installation failed, restoring the previous state")
		if codeHeld {
			// A failed installation must not leave a bearer secret behind.
			r.Firmware.ClearCode()
		}
		if r.recover() != nil {
			err = ErrRecovery
		}
	}()
	step := func(failure Error, action func() error) error {
		if ctx.Err() != nil {
			return ErrInterrupted
		}
		if action() != nil {
			return failure
		}
		return nil
	}
	r.say("Installing the firmware service")
	if exists(filepath.Join(r.transaction, "was-active")) {
		if err = step(ErrStop, func() error { return r.Services.Stop(ctx) }); err != nil {
			return err
		}
	}
	if err = step(ErrConfigure, func() error { return r.Firmware.Configure(ctx) }); err != nil {
		return err
	}
	if err = step(ErrWrite, func() error {
		if r.pointer("releases/"+r.digest) != nil {
			return ErrWrite
		}
		if !isLink(l.Wrapper) {
			if os.Symlink(filepath.Join(l.Base, "current", "agent-firmware"), l.Wrapper) != nil {
				return ErrWrite
			}
		}
		if replace(l.Unit, r.Services.Definition(), 0o644) != nil {
			return ErrWrite
		}
		return syncPath(l.Base)
	}); err != nil {
		return err
	}
	if err = step(ErrReload, func() error { return r.Services.Reload(ctx) }); err != nil {
		return err
	}
	if err = step(ErrEnable, func() error { return r.Services.SetEnabled(ctx, true) }); err != nil {
		return err
	}
	if r.Code != "" {
		// Held before the service starts so Gateway finds it on its first start.
		codeHeld = true
		if err = step(ErrCode, func() error { return r.Firmware.HoldCode(r.Code) }); err != nil {
			return err
		}
	}
	r.say("Starting the service and downloading the workload images, this can take several minutes")
	stopWaiting := r.waiting("Still starting, please wait")
	defer stopWaiting()
	if err = step(ErrStart, func() error {
		if r.Services.Start(ctx) != nil || !r.Services.Active(ctx) {
			return ErrStart
		}
		return nil
	}); err != nil {
		return err
	}
	if write(filepath.Join(r.transaction, "committed"), nil, 0o600) != nil || syncPath(r.transaction) != nil {
		return ErrWrite
	}
	_ = os.RemoveAll(r.transaction)
	return nil
}

func (r *run) lock() (func(), error) {
	l := r.Layout
	if !lexists(l.Lock) {
		if os.Mkdir(l.Lock, 0o700) != nil {
			return nil, ErrConflict
		}
	}
	file := filepath.Join(l.Lock, "lock")
	if !r.safeDir(l.Lock, 0o700) || lexists(file) && !r.safeFile(file, 0o600) {
		return nil, ErrConflict
	}
	f, err := privatefile.OpenRegular(nil, file, os.O_WRONLY|os.O_CREATE, 0o600, r.owner(), false)
	if err != nil {
		return nil, ErrConflict
	}
	if privatefile.TryLock(f) != nil {
		f.Close()
		return nil, ErrBusy
	}
	return func() { f.Close() }, nil
}

func (r *run) readBinary() ([]byte, error) {
	f, err := os.Open(r.Binary)
	if err != nil {
		return nil, ErrWrite
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBinary+1))
	if err != nil {
		return nil, ErrWrite
	}
	if len(data) == 0 || len(data) > maxBinary {
		return nil, ErrInput
	}
	sum := sha256.Sum256(data)
	r.digest = hex.EncodeToString(sum[:])
	return data, nil
}

// conflicts refuses host resources this installer does not recognize as its own.
func (r *run) conflicts(ctx context.Context) error {
	l := r.Layout
	for _, directory := range l.DropIns {
		if lexists(directory) {
			return ErrConflict
		}
	}
	fragment, err := r.Services.Fragment(ctx)
	if err != nil {
		return ErrSystemd
	}
	if fragment != "" && fragment != l.Unit {
		return ErrConflict
	}
	if lexists(l.Unit) && (!r.safeFile(l.Unit, 0o644) || !checkDefinition(l.Unit, r.Services.Owns)) {
		return ErrConflict
	}
	if lexists(l.Wrapper) && !r.ownWrapper() {
		return ErrConflict
	}
	current := filepath.Join(l.Base, "current")
	if lexists(current) {
		target, err := os.Readlink(current)
		if err != nil || !r.validPointer(target) {
			return ErrConflict
		}
	}
	if lexists(l.State) {
		if !r.safeDir(l.State, 0o700) {
			return ErrConflict
		}
		if r.Firmware.Existing() != nil {
			return ErrExisting
		}
	}
	return nil
}

func (r *run) placeVersion(releases string, binary []byte) error {
	version := filepath.Join(releases, r.digest)
	target := filepath.Join(version, "agent-firmware")
	if lexists(version) {
		if !r.safeDir(version, 0o755) || !r.safeFile(target, 0o755) {
			return ErrConflict
		}
		if digestOf(target) != r.digest {
			return ErrConflict
		}
		return nil
	}
	staging, err := os.MkdirTemp(releases, ".new.")
	if err != nil {
		return ErrWrite
	}
	if os.Chmod(staging, 0o755) != nil || write(filepath.Join(staging, "agent-firmware"), binary, 0o755) != nil ||
		syncPath(staging) != nil || os.Rename(staging, version) != nil {
		_ = os.RemoveAll(staging)
		return ErrWrite
	}
	return nil
}

// prepare records what the host looked like, so recover can return to it.
func (r *run) prepare(ctx context.Context) error {
	l, t := r.Layout, r.transaction
	if os.Mkdir(t, 0o700) != nil {
		return ErrWrite
	}
	record := func(name string, data []byte) bool { return write(filepath.Join(t, name), data, 0o600) == nil }
	ok := record("format", []byte(transactionFormat+"\n"))
	if target, err := os.Readlink(filepath.Join(l.Base, "current")); err == nil {
		ok = ok && record("had-pointer", nil) && record("previous-pointer", []byte(target+"\n"))
	}
	if isLink(l.Wrapper) {
		ok = ok && record("had-wrapper", nil)
	}
	if isFile(l.Unit) {
		data, err := os.ReadFile(l.Unit)
		ok = ok && err == nil && record("unit", data) && record("had-unit", nil)
	}
	accepted := filepath.Join(l.State, hostplatform.AcceptedState)
	if isFile(accepted) {
		if !r.safeFile(accepted, 0o600) {
			_ = os.RemoveAll(t)
			return ErrConflict
		}
		data, err := os.ReadFile(accepted)
		ok = ok && err == nil && record("config", data) && record("had-config", nil)
	}
	// Existing engine records are immutable. Only a newly published record
	// needs to be removed during rollback.
	if !lexists(filepath.Join(l.State, hostplatform.EngineState)) {
		ok = ok && record("engine-absent", nil)
	}
	if r.Services.Active(ctx) {
		ok = ok && record("was-active", nil)
	}
	if r.Services.Enabled(ctx) {
		ok = ok && record("was-enabled", nil)
	}
	if !ok || !record("prepared", nil) || syncPath(t) != nil {
		_ = os.RemoveAll(t)
		return ErrWrite
	}
	return nil
}

// recover returns the host to the state a prepared, uncommitted transaction
// recorded. It never removes or rewrites firmware's own operation journal.
func (r *run) recover() error {
	l, t := r.Layout, r.transaction
	if !lexists(t) {
		return nil
	}
	if !r.safeDir(t, 0o700) {
		return ErrRecovery
	}
	has := func(name string) bool { return exists(filepath.Join(t, name)) }
	if has("committed") || !has("prepared") {
		return os.RemoveAll(t)
	}
	if !hasLine(filepath.Join(t, "format"), transactionFormat) {
		return ErrRecovery
	}
	ctx := context.Background()
	if isFile(l.Unit) {
		if !r.safeFile(l.Unit, 0o644) || !checkDefinition(l.Unit, r.Services.Owns) || r.Services.Stop(ctx) != nil {
			return ErrRecovery
		}
	}
	current := filepath.Join(l.Base, "current")
	if has("had-pointer") {
		previous, err := os.ReadFile(filepath.Join(t, "previous-pointer"))
		target := strings.TrimSuffix(string(previous), "\n")
		if err != nil || !r.validPointer(target) || r.pointer(target) != nil {
			return ErrRecovery
		}
	} else if isLink(current) && os.Remove(current) != nil {
		return ErrRecovery
	}
	if has("had-wrapper") {
		if !r.ownWrapper() {
			return ErrRecovery
		}
	} else if r.ownWrapper() && os.Remove(l.Wrapper) != nil {
		return ErrRecovery
	}
	if has("had-unit") {
		data, err := os.ReadFile(filepath.Join(t, "unit"))
		if err != nil || replace(l.Unit, data, 0o644) != nil {
			return ErrRecovery
		}
	} else if isFile(l.Unit) {
		if !checkDefinition(l.Unit, r.Services.Owns) || os.Remove(l.Unit) != nil {
			return ErrRecovery
		}
	}
	accepted := filepath.Join(l.State, hostplatform.AcceptedState)
	switch {
	case isFile(filepath.Join(l.State, hostplatform.OperationJournal)):
		if !has("had-config") || !sameContent(filepath.Join(t, "config"), accepted) {
			return ErrRecovery
		}
	case has("had-config"):
		data, err := os.ReadFile(filepath.Join(t, "config"))
		if err != nil || replace(accepted, data, 0o600) != nil {
			return ErrRecovery
		}
	case isFile(accepted):
		if !r.safeFile(accepted, 0o600) || os.Remove(accepted) != nil {
			return ErrRecovery
		}
	}
	if has("engine-absent") && r.removeNewEngineState() != nil {
		return ErrRecovery
	}
	if r.Services.Reload(ctx) != nil {
		return ErrRecovery
	}
	if r.Services.SetEnabled(ctx, has("was-enabled")) != nil {
		return ErrRecovery
	}
	if has("was-active") && r.Services.Start(ctx) != nil {
		return ErrRecovery
	}
	if syncPath(l.Base) != nil {
		return ErrRecovery
	}
	return os.RemoveAll(t)
}

func (r *run) removeNewEngineState() error {
	state := r.Layout.State
	if !lexists(state) {
		return nil
	}
	if !r.safeDir(state, 0700) {
		return ErrRecovery
	}
	path := filepath.Join(state, hostplatform.EngineState)
	if lexists(path) && (!r.safeFile(path, 0600) || os.Remove(path) != nil) {
		return ErrRecovery
	}
	return syncPath(state)
}

// pointer atomically points Base/current at a version directory.
func (r *run) pointer(target string) error {
	current := filepath.Join(r.Layout.Base, "current")
	staged := current + ".install-new"
	// A killed rename can leave this owned temporary link. Never follow it as a directory.
	if isLink(staged) {
		if err := os.Remove(staged); err != nil {
			return err
		}
	}
	if err := os.Symlink(target, staged); err != nil {
		return err
	}
	return os.Rename(staged, current)
}

func (r *run) validPointer(target string) bool {
	digest, ok := strings.CutPrefix(target, "releases/")
	if !ok || len(digest) != 64 || strings.Trim(digest, "0123456789abcdef") != "" {
		return false
	}
	version := filepath.Join(r.Layout.Base, "releases", digest)
	binary := filepath.Join(version, "agent-firmware")
	return r.safeDir(version, 0o755) && r.safeFile(binary, 0o755) && digestOf(binary) == digest
}

func (r *run) ownWrapper() bool {
	target, err := os.Readlink(r.Layout.Wrapper)
	return err == nil && target == filepath.Join(r.Layout.Base, "current", "agent-firmware")
}

func (r *run) owner() privatefile.Owner {
	return privatefile.Owner{UID: r.Layout.UID, GID: r.Layout.GID}
}

func (r *run) safeDir(path string, mode os.FileMode) bool {
	info, err := os.Lstat(path)
	return err == nil && r.owner().Directory(info, mode)
}

func (r *run) safeFile(path string, mode os.FileMode) bool {
	info, err := os.Lstat(path)
	return err == nil && r.owner().Regular(info, mode, false)
}

func lexists(path string) bool { _, err := os.Lstat(path); return err == nil }
func exists(path string) bool  { _, err := os.Stat(path); return err == nil }
func isLink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}
func isFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func checkDefinition(path string, check func([]byte) bool) bool {
	data, err := os.ReadFile(path)
	return err == nil && check(data)
}

func hasLine(path, line string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, candidate := range strings.Split(string(data), "\n") {
		if candidate == line {
			return true
		}
	}
	return false
}

func sameContent(a, b string) bool {
	first, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	second, err := os.ReadFile(b)
	return err == nil && bytes.Equal(first, second)
}

func digestOf(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(f, maxBinary+1)); err != nil {
		return ""
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func write(path string, data []byte, mode os.FileMode) error {
	return privatefile.WriteNew(nil, path, data, mode, true)
}

func replace(path string, data []byte, mode os.FileMode) error {
	staged := path + ".install-new"
	if lexists(staged) {
		if err := os.Remove(staged); err != nil {
			return err
		}
	}
	_, err := privatefile.Replace(nil, path, staged, data, mode, true, nil, nil)
	return err
}

func syncPath(path string) error { return privatefile.SyncDirectory(nil, path, nil) }
