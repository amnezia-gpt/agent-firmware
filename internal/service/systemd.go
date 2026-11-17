package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/host"
)

// Systemd manages the firmware service and its unit.
type Systemd struct{ Layout host.Layout }

const UnitMarker = "# Managed by agent-firmware installer v1"

func (s Systemd) Available() bool {
	info, err := os.Stat(s.Layout.SystemdRuntime)
	return err == nil && info.IsDir()
}

func (Systemd) Owns(definition []byte) bool { return hasLine(definition, UnitMarker) }

func hasLine(data []byte, line string) bool {
	for candidate := range strings.SplitSeq(string(data), "\n") {
		if candidate == line {
			return true
		}
	}
	return false
}

func (s Systemd) Stop(ctx context.Context) error   { return s.do(ctx, "stop") }
func (s Systemd) Start(ctx context.Context) error  { return s.do(ctx, "start") }
func (s Systemd) Reload(ctx context.Context) error { return s.do(ctx, "daemon-reload") }
func (s Systemd) SetEnabled(ctx context.Context, enabled bool) error {
	if enabled {
		return s.do(ctx, "enable")
	}
	return s.do(ctx, "disable")
}

func systemctl(ctx context.Context, limit time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	return exec.CommandContext(ctx, "systemctl", args...).Output()
}

func (Systemd) do(ctx context.Context, verb string) error {
	args := []string{verb}
	if verb != "daemon-reload" {
		args = append(args, host.ServiceName)
	}
	// A start waits for the first convergence, which may pull images.
	_, err := systemctl(ctx, 1400*time.Second, args...)
	return err
}

func (Systemd) Active(ctx context.Context) bool {
	_, err := systemctl(ctx, 10*time.Second, "is-active", "--quiet", host.ServiceName)
	return err == nil
}

func (Systemd) Enabled(ctx context.Context) bool {
	_, err := systemctl(ctx, 10*time.Second, "is-enabled", "--quiet", host.ServiceName)
	return err == nil
}

func (Systemd) Fragment(ctx context.Context) (string, error) {
	out, err := systemctl(ctx, 10*time.Second, "show", "--property=FragmentPath", "--value", host.ServiceName)
	return strings.TrimSpace(string(out)), err
}

// Definition builds the systemd unit.
func (s Systemd) Definition() []byte {
	return []byte(UnitMarker + `
[Unit]
Description=Agent firmware host supervisor
After=network-online.target docker.service
Wants=network-online.target
Requires=docker.service
StartLimitIntervalSec=60s
StartLimitBurst=3

[Service]
Type=notify
NotifyAccess=main
User=root
Group=root
ExecStart=` + s.Layout.Wrapper + ` serve
UMask=0077
Restart=on-failure
RestartSec=5s
TimeoutStartSec=20min
TimeoutStopSec=150s
KillMode=control-group
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
RuntimeDirectory=` + filepath.Base(s.Layout.Runtime) + `
RuntimeDirectoryMode=0700
RuntimeDirectoryPreserve=yes
ReadWritePaths=` + s.Layout.State + " " + s.Layout.Runtime + `
RestrictAddressFamilies=AF_UNIX
LockPersonality=true
LimitNOFILE=4096

[Install]
WantedBy=multi-user.target
`)
}
