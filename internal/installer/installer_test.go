package installer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	hostplatform "github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/service"
)

const testCode = "enrc_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopq"

// host is a private tree standing in for the system paths, with recording fakes.
type host struct {
	t                  *testing.T
	layout             hostplatform.Layout
	calls              []string
	active             bool
	enabled            bool
	fragment           string
	fail               map[string]bool
	code               string
	writeEngineAddress bool
}

func newHost(t *testing.T) *host {
	t.Helper()
	root := t.TempDir()
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	h := &host{t: t, fail: map[string]bool{}}
	h.layout = hostplatform.Layout{
		Base:    filepath.Join(root, "lib", "agent-firmware"),
		Wrapper: filepath.Join(root, "bin", "agent-firmware"),
		Unit:    filepath.Join(root, "systemd", "agent-firmware.service"),
		State:   filepath.Join(root, "state"),
		Runtime: filepath.Join(root, "run", "agent-firmware"),
		Lock:    filepath.Join(root, "lock"),
		DropIns: []string{filepath.Join(root, "systemd", "agent-firmware.service.d")},
		Parents: []string{filepath.Join(root, "lib"), filepath.Join(root, "bin"), filepath.Join(root, "systemd")},
		UID:     stat.Uid,
		GID:     stat.Gid,
	}
	for _, directory := range h.layout.Parents {
		if err := os.Mkdir(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

func (h *host) Definition() []byte               { return (service.Systemd{Layout: h.layout}).Definition() }
func (h *host) Owns(data []byte) bool            { return (service.Systemd{Layout: h.layout}).Owns(data) }
func (h *host) Stop(ctx context.Context) error   { return h.do(ctx, "stop") }
func (h *host) Start(ctx context.Context) error  { return h.do(ctx, "start") }
func (h *host) Reload(ctx context.Context) error { return h.do(ctx, "daemon-reload") }
func (h *host) SetEnabled(ctx context.Context, enabled bool) error {
	if enabled {
		return h.do(ctx, "enable")
	}
	return h.do(ctx, "disable")
}
func (h *host) do(_ context.Context, verb string) error {
	h.calls = append(h.calls, "systemd "+verb)
	if h.fail[verb] {
		return errors.New("failed")
	}
	switch verb {
	case "start":
		// A service that starts and dies at once: the first start of a run leaves it inactive.
		h.active = !h.fail["dies"]
		delete(h.fail, "dies")
	case "stop":
		h.active = false
	case "enable":
		h.enabled = true
	case "disable":
		h.enabled = false
	}
	return nil
}
func (h *host) Active(context.Context) bool  { return h.active }
func (h *host) Enabled(context.Context) bool { return h.enabled }
func (h *host) Fragment(context.Context) (string, error) {
	if h.fail["fragment"] {
		return "", errors.New("failed")
	}
	return h.fragment, nil
}
func (h *host) Preflight(context.Context) error { return h.result("preflight") }
func (h *host) Existing() error                 { return h.result("existing") }
func (h *host) Configure(context.Context) error {
	if err := h.result("configure"); err != nil {
		return err
	}
	// The firmware replaces its accepted configuration; the installer only records the previous one.
	if err := os.MkdirAll(h.layout.State, 0o700); err != nil {
		return err
	}
	if h.writeEngineAddress {
		path := filepath.Join(h.layout.State, hostplatform.EngineState)
		if !lexists(path) {
			if err := write(path, []byte("test engine\n"), 0600); err != nil {
				return err
			}
		}
	}
	return replace(filepath.Join(h.layout.State, "accepted.json"), []byte("new\n"), 0o600)
}
func (h *host) HoldCode(code string) error {
	if err := h.result("hold-code"); err != nil {
		return err
	}
	h.code = code
	return nil
}
func (h *host) ClearCode() { h.calls = append(h.calls, "clear-code"); h.code = "" }
func (h *host) result(name string) error {
	h.calls = append(h.calls, name)
	if h.fail[name] {
		return errors.New("failed")
	}
	return nil
}

func (h *host) install(content, code string) error {
	h.t.Helper()
	binary := filepath.Join(h.t.TempDir(), "agent-firmware-linux-amd64")
	if err := os.WriteFile(binary, []byte(content), 0o700); err != nil {
		h.t.Fatal(err)
	}
	h.calls = nil
	return Install(context.Background(), Request{Layout: h.layout, Binary: binary, Code: code, Services: h, Firmware: h})
}

func (h *host) read(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
func (h *host) current() string {
	return h.read(filepath.Join(h.layout.Base, "current", "agent-firmware"))
}
func (h *host) noTransaction() {
	h.t.Helper()
	if lexists(filepath.Join(h.layout.Base, ".transaction")) {
		h.t.Fatal("transaction left behind")
	}
}
func (h *host) order(want ...string) {
	h.t.Helper()
	joined := strings.Join(h.calls, ",")
	position := 0
	for _, call := range want {
		index := strings.Index(joined[position:], call)
		if index < 0 {
			h.t.Fatalf("%q missing or out of order in %s", call, joined)
		}
		position += index + len(call)
	}
}

func TestFreshInstallation(t *testing.T) {
	h := newHost(t)
	if err := h.install("version one", testCode); err != nil {
		t.Fatal(err)
	}
	if h.current() != "version one" || !h.active || !h.enabled || h.code != testCode {
		t.Fatalf("current=%q active=%v enabled=%v", h.current(), h.active, h.enabled)
	}
	if target, _ := os.Readlink(h.layout.Wrapper); target != filepath.Join(h.layout.Base, "current", "agent-firmware") {
		t.Fatalf("wrapper -> %q", target)
	}
	unit := h.read(h.layout.Unit)
	if !strings.HasPrefix(unit, service.UnitMarker+"\n") || strings.Contains(unit, "LoadCredential") || !strings.Contains(unit, "ExecStart=") {
		t.Fatalf("unit:\n%s", unit)
	}
	if info, _ := os.Stat(h.layout.Unit); info.Mode().Perm() != 0o644 {
		t.Fatal("unit mode")
	}
	// The code is held after enable and before start, and is in no installer-owned file.
	h.order("preflight", "configure", "systemd daemon-reload", "systemd enable", "hold-code", "systemd start")
	h.noTransaction()
	_ = filepath.WalkDir(h.layout.Base, func(path string, entry os.DirEntry, _ error) error {
		if entry != nil && entry.Type().IsRegular() && strings.Contains(h.read(path), testCode) {
			t.Fatalf("code stored in %s", path)
		}
		return nil
	})
	if strings.Contains(unit, testCode) {
		t.Fatal("code in unit")
	}
}

func TestReinstallationAndUpgrade(t *testing.T) {
	h := newHost(t)
	if err := h.install("version one", ""); err != nil {
		t.Fatal(err)
	}
	if err := h.install("version one", ""); err != nil {
		t.Fatal(err)
	}
	h.order("systemd stop", "configure", "systemd start")
	if err := h.install("version two", ""); err != nil {
		t.Fatal(err)
	}
	if h.current() != "version two" || !h.active {
		t.Fatal(h.current())
	}
	entries, _ := os.ReadDir(filepath.Join(h.layout.Base, "releases"))
	if len(entries) != 2 {
		t.Fatalf("%d versions kept", len(entries))
	}
	h.noTransaction()
}

func TestFailedUpgradeReturnsThePreviousInstallation(t *testing.T) {
	for _, failure := range []struct {
		name string
		want error
	}{
		{"configure", ErrConfigure}, {"daemon-reload", ErrReload},
		{"enable", ErrEnable}, {"hold-code", ErrCode}, {"dies", ErrStart},
	} {
		t.Run(failure.name, func(t *testing.T) {
			h := newHost(t)
			if err := h.install("version one", ""); err != nil {
				t.Fatal(err)
			}
			unit := h.read(h.layout.Unit)
			if err := os.WriteFile(filepath.Join(h.layout.State, "accepted.json"), []byte("old\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			h.fail[failure.name] = true
			err := h.install("version two", testCode)
			// The first failing call is the installation step; recovery repeats daemon-reload and enable.
			if failure.name == "daemon-reload" || failure.name == "enable" {
				if err != ErrRecovery {
					t.Fatal(err)
				}
				return
			}
			if err != failure.want {
				t.Fatal(err)
			}
			if h.current() != "version one" || h.read(h.layout.Unit) != unit || !h.active || !h.enabled || h.code != "" {
				t.Fatalf("not restored: current=%q active=%v code=%q", h.current(), h.active, h.code)
			}
			if h.read(filepath.Join(h.layout.State, "accepted.json")) != "old\n" {
				t.Fatal("configuration not restored")
			}
			h.noTransaction()
		})
	}
}

func TestInterruptedTransactionIsRecoveredByTheNextRun(t *testing.T) {
	h := newHost(t)
	if err := h.install("version one", ""); err != nil {
		t.Fatal(err)
	}
	// A killed installer leaves a prepared transaction and a half-switched host.
	r := &run{Request: Request{Layout: h.layout, Services: h, Firmware: h}, transaction: filepath.Join(h.layout.Base, ".transaction")}
	if err := r.prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.layout.Unit, []byte(service.UnitMarker+"\nbroken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.active = false
	if err := h.install("version two", ""); err != nil {
		t.Fatal(err)
	}
	h.order("systemd stop", "systemd daemon-reload", "systemd enable", "systemd start", "preflight")
	if h.current() != "version two" {
		t.Fatal(h.current())
	}
	// A transaction of an unknown format is never guessed at.
	if err := r.prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.transaction, "format"), []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.install("version three", ""); err != ErrRecovery {
		t.Fatal(err)
	}
}

func TestForeignResourcesAreRefusedBeforeAnyChange(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(h *host)
		want  error
	}{
		{"foreign unit", func(h *host) { _ = os.WriteFile(h.layout.Unit, []byte("[Unit]\n"), 0o644) }, ErrConflict},
		{"unit loaded from elsewhere", func(h *host) { h.fragment = "/usr/lib/systemd/system/agent-firmware.service" }, ErrConflict},
		{"unit override", func(h *host) { _ = os.Mkdir(h.layout.DropIns[0], 0o755) }, ErrConflict},
		{"foreign command", func(h *host) { _ = os.WriteFile(h.layout.Wrapper, []byte("x"), 0o755) }, ErrConflict},
		{"unmanaged directory", func(h *host) { _ = os.Mkdir(h.layout.Base, 0o755) }, ErrConflict},
		{"writable parent", func(h *host) { _ = os.Chmod(h.layout.Parents[0], 0o777) }, ErrConflict},
		{"open state directory", func(h *host) { _ = os.Mkdir(h.layout.State, 0o755); _ = os.Chmod(h.layout.State, 0o755) }, ErrConflict},
		{"invalid retained configuration", func(h *host) { _ = os.Mkdir(h.layout.State, 0o700); h.fail["existing"] = true }, ErrExisting},
		{"docker missing", func(h *host) { h.fail["preflight"] = true }, ErrDocker},
		{"systemd unavailable", func(h *host) { h.fail["fragment"] = true }, ErrSystemd},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHost(t)
			test.setup(h)
			if err := h.install("version one", testCode); err != test.want {
				t.Fatal(err)
			}
			for _, call := range h.calls {
				if strings.HasPrefix(call, "systemd ") || call == "configure" || call == "hold-code" {
					t.Fatalf("changed the host: %v", h.calls)
				}
			}
			if lexists(filepath.Join(h.layout.Base, "releases")) {
				t.Fatal("version placed")
			}
		})
	}
}

func TestConcurrentInstallationIsRefused(t *testing.T) {
	h := newHost(t)
	r := &run{Request: Request{Layout: h.layout}}
	unlock, err := r.lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := h.install("version one", ""); err != ErrBusy {
		t.Fatal(err)
	}
}

func TestInterruptionRollsBack(t *testing.T) {
	h := newHost(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	binary := filepath.Join(t.TempDir(), "binary")
	if err := os.WriteFile(binary, []byte("version one"), 0o700); err != nil {
		t.Fatal(err)
	}
	err := Install(ctx, Request{Layout: h.layout, Binary: binary, Services: h, Firmware: h})
	if err != ErrInterrupted || lexists(h.layout.Unit) || h.active {
		t.Fatal(err)
	}
}

func TestProgressNamesTheStepsAndNeverTheCode(t *testing.T) {
	h := newHost(t)
	binary := filepath.Join(t.TempDir(), "binary")
	if err := os.WriteFile(binary, []byte("version one"), 0o700); err != nil {
		t.Fatal(err)
	}
	var lines []string
	request := Request{Layout: h.layout, Binary: binary, Code: testCode, Services: h, Firmware: h, Progress: func(m string) { lines = append(lines, m) }}
	if err := Install(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(lines, "|")
	if !strings.HasPrefix(got, "Checking the host and Docker|Installing the firmware service|Starting the service") || strings.Contains(got, testCode) || strings.Contains(got, "failed") {
		t.Fatal(got)
	}
	// A failed start says that the previous state is being restored.
	h2 := newHost(t)
	h2.fail["dies"] = true
	lines = nil
	request.Layout, request.Services, request.Firmware = h2.layout, h2, h2
	if err := Install(context.Background(), request); err != ErrStart {
		t.Fatal(err)
	}
	if last := lines[len(lines)-1]; last != "Installation failed, restoring the previous state" {
		t.Fatal(last)
	}
}

func TestFailedInstallRollsBackOnlyNewEngineAddress(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "new record"
		if existing {
			name = "existing record"
		}
		t.Run(name, func(t *testing.T) {
			h := newHost(t)
			h.writeEngineAddress = true
			path := filepath.Join(h.layout.State, hostplatform.EngineState)
			if existing {
				if err := h.install("version one", ""); err != nil {
					t.Fatal(err)
				}
			}
			h.fail["dies"] = true
			if err := h.install("version two", ""); err != ErrStart {
				t.Fatalf("installation error = %v", err)
			}
			if existing {
				if got := h.read(path); got != "test engine\n" {
					t.Fatalf("engine record = %q", got)
				}
			} else if lexists(path) {
				t.Fatal("rolled back installation left an engine record")
			}
		})
	}
}
