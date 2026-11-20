package dockerengine

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	"github.com/moby/moby/client"
)

const ErrPrerequisites = workload.ErrEnginePrerequisites

// Prerequisites inspects the already prepared engine without creating resources.
func (e *Engine) Prerequisites(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, e.callTimeout)
	defer cancel()
	info, err := e.api.Info(ctx, client.InfoOptions{})
	if err != nil {
		if client.IsErrConnectionFailed(err) && !errors.Is(err, os.ErrPermission) && ctx.Err() != context.Canceled {
			return workload.ErrEngineUnavailable
		}
		return classify(err)
	}
	architecture := info.Info.Architecture
	switch architecture {
	case "x86_64":
		architecture = "amd64"
	case "aarch64":
		architecture = "arm64"
	}
	if info.Info.OSType != e.platform.OS || architecture != e.platform.Architecture {
		return ErrPrerequisites
	}
	for _, option := range info.Info.SecurityOptions {
		if strings.Contains(option, "rootless") {
			return ErrPrerequisites
		}
	}
	return nil
}
