package service

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/privatefile"
)

const launchAgentMarker = "<!-- Managed by agent-firmware installer v1 -->"

var errNotLoaded = errors.New("launch agent is not loaded")
var errUnexpectedJob = errors.New("launch agent has an unexpected definition")

// Launchd manages the firmware in the user's GUI domain.
type Launchd struct {
	Layout  host.Layout
	command func(context.Context, ...string) ([]byte, error)
}

// Readiness is written by the running service in its private status file.
type Readiness struct {
	PID     int       `json:"pid,omitempty"`
	ReadyAt time.Time `json:"ready_at,omitzero"`
}

func (l Launchd) domain() string { return "gui/" + strconv.FormatUint(uint64(l.Layout.UID), 10) }
func (l Launchd) target() string { return l.domain() + "/" + host.LaunchAgentLabel }

func (l Launchd) call(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if l.command != nil {
		return l.command(ctx, args...)
	}
	out, err := host.CommandOutput(ctx, 64*1024, "/bin/launchctl", args...)
	var status *exec.ExitError
	if args[0] == "print" && errors.As(err, &status) && status.ExitCode() == 113 {
		return nil, errNotLoaded
	}
	return out, err
}

func (l Launchd) Available() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "/bin/launchctl", "print", l.domain()).Run() == nil
}

func (l Launchd) Fragment(ctx context.Context) (string, error) {
	job, err := l.inspect(ctx)
	return job.path, err
}

type launchJob struct {
	path string
	pid  int
}

func (l Launchd) inspect(ctx context.Context) (launchJob, error) {
	out, err := l.call(ctx, "print", l.target())
	if errors.Is(err, errNotLoaded) {
		return launchJob{}, nil
	}
	if err != nil {
		return launchJob{}, err
	}
	var job launchJob
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if path, ok := strings.CutPrefix(line, "path = "); ok && job.path == "" {
			job.path = path
		}
		if pid, ok := strings.CutPrefix(line, "pid = "); ok && job.pid == 0 {
			job.pid, _ = strconv.Atoi(pid)
		}
	}
	if job.path == "" {
		return launchJob{}, errUnexpectedJob
	}
	return job, nil
}

func (l Launchd) Active(ctx context.Context) bool {
	path, err := l.Fragment(ctx)
	return err == nil && path == l.Layout.Unit
}

func (l Launchd) Enabled(ctx context.Context) bool {
	if _, err := os.Lstat(l.Layout.Unit); err != nil {
		return false
	}
	out, err := l.call(ctx, "print-disabled", l.domain())
	if err != nil {
		return false
	}
	prefix := strconv.Quote(host.LaunchAgentLabel) + " => "
	for line := range strings.SplitSeq(string(out), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), prefix); ok {
			return value == "enabled" || value == "false"
		}
	}
	return true
}

func (l Launchd) SetEnabled(ctx context.Context, enabled bool) error {
	verb := "disable"
	if enabled {
		verb = "enable"
	}
	_, err := l.call(ctx, verb, l.target())
	return err
}

func (l Launchd) Stop(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 160*time.Second)
	defer cancel()
	job, err := l.inspect(ctx)
	if err != nil || job.path == "" {
		return err
	}
	if job.path != l.Layout.Unit {
		return errUnexpectedJob
	}
	if _, err := l.call(ctx, "bootout", l.target()); err != nil {
		if path, check := l.Fragment(ctx); check != nil || path != "" {
			return err
		}
	}
	// Removing the definition and reaping the old process may finish at different times.
	if job.pid > 0 {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for !errors.Is(syscall.Kill(job.pid, 0), syscall.ESRCH) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
	return nil
}

// launchd loads each definition at bootstrap and needs no global reload.
func (Launchd) Reload(context.Context) error { return nil }
func (Launchd) Notifier() Notifier           { return Notifier{} }

func (l Launchd) Start(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 1400*time.Second)
	defer cancel()
	started := time.Now()
	path, err := l.Fragment(ctx)
	if err != nil {
		return err
	}
	if path == "" {
		if _, err := l.call(ctx, "bootstrap", l.domain(), l.Layout.Unit); err != nil {
			return err
		}
	} else if path != l.Layout.Unit {
		return errUnexpectedJob
	}
	out, err := l.call(ctx, "kickstart", "-p", l.target())
	if err != nil {
		return err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || pid <= 0 {
		return errUnexpectedJob
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if l.ready(pid, started) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (l Launchd) ready(pid int, started time.Time) bool {
	owner := privatefile.EffectiveOwner()
	root, err := privatefile.OpenDirectory(l.Layout.Runtime, owner, 0700)
	if err != nil {
		return false
	}
	defer root.Close()
	file, err := privatefile.OpenRegular(root, host.RuntimeStatus, os.O_RDONLY, 0600, owner, true)
	if err != nil {
		return false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	var status Readiness
	return err == nil && len(data) <= 4096 && json.Unmarshal(data, &status) == nil &&
		status.PID == pid && !status.ReadyAt.Before(started) && !status.ReadyAt.After(time.Now())
}

func (l Launchd) arguments() string {
	var wrapper bytes.Buffer
	_ = xml.EscapeText(&wrapper, []byte(l.Layout.Wrapper))
	return "<array><string>" + wrapper.String() + "</string><string>serve</string></array>"
}

func (l Launchd) Owns(definition []byte) bool {
	// Recognize the complete owned definition to reject a foreign or edited job.
	return bytes.Equal(definition, l.Definition())
}

func (l Launchd) Definition() []byte {
	var home bytes.Buffer
	_ = xml.EscapeText(&home, []byte(l.Layout.Home))
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
` + launchAgentMarker + `
<plist version="1.0"><dict>
<key>Label</key><string>` + host.LaunchAgentLabel + `</string>
<key>ProgramArguments</key>` + l.arguments() + `
<key>EnvironmentVariables</key><dict><key>HOME</key><string>` + home.String() + `</string></dict>
<key>LimitLoadToSessionType</key><string>Aqua</string>
<key>RunAtLoad</key><true/>
<key>KeepAlive</key><true/>
<key>ThrottleInterval</key><integer>5</integer>
<key>ExitTimeOut</key><integer>150</integer>
<key>Umask</key><integer>63</integer>
</dict></plist>
`)
}
