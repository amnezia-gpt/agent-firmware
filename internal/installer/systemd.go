package installer

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Systemctl is the real service manager boundary.
type Systemctl struct{}

func systemctl(ctx context.Context, limit time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	return exec.CommandContext(ctx, "systemctl", args...).Output()
}

func (Systemctl) Do(ctx context.Context, verb string) error {
	args := []string{verb}
	if verb != "daemon-reload" {
		args = append(args, serviceName)
	}
	// A start waits for the first convergence, which may pull images.
	_, err := systemctl(ctx, 1400*time.Second, args...)
	return err
}

func (Systemctl) Active(ctx context.Context) bool {
	_, err := systemctl(ctx, 10*time.Second, "is-active", "--quiet", serviceName)
	return err == nil
}

func (Systemctl) Enabled(ctx context.Context) bool {
	_, err := systemctl(ctx, 10*time.Second, "is-enabled", "--quiet", serviceName)
	return err == nil
}

func (Systemctl) Fragment(ctx context.Context) (string, error) {
	out, err := systemctl(ctx, 10*time.Second, "show", "--property=FragmentPath", "--value", serviceName)
	return strings.TrimSpace(string(out)), err
}
