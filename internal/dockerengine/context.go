package dockerengine

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
)

const ErrContext workload.Error = "docker_context_unavailable"
const ErrContextEndpoint workload.Error = "unsupported_docker_context_endpoint"

// ContextSocket uses the interactive user's active Docker context exactly once.
func ContextSocket(ctx context.Context) (string, error) {
	cli, err := exec.LookPath("docker")
	if err != nil {
		home, err := os.UserHomeDir()
		if err != nil || !filepath.IsAbs(home) {
			return "", ErrContext
		}
		cli = filepath.Join(home, ".docker", "bin", "docker")
	}
	call, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := host.CommandOutput(call, host.MaxEngineAddress*6, cli, "context", "inspect", "--format", "{{json .Endpoints.docker.Host}}")
	if err != nil {
		return "", ErrContext
	}
	return parseContextSocket(output)
}

func parseContextSocket(output []byte) (string, error) {
	var endpoint string
	if json.Unmarshal(output, &endpoint) != nil {
		return "", ErrContextEndpoint
	}
	path, ok := strings.CutPrefix(endpoint, "unix://")
	if !ok || len(path) >= host.MaxEngineAddress || !filepath.IsAbs(path) || filepath.Clean(path) != path ||
		path == string(filepath.Separator) || strings.ContainsAny(path, "\x00\r\n") {
		return "", ErrContextEndpoint
	}
	return path, nil
}
