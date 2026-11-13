package dockerengine

import (
	"context"
	"strings"

	"github.com/moby/moby/client"
)

type PrerequisiteError string

func (e PrerequisiteError) Error() string { return string(e) }

const ErrPrerequisites PrerequisiteError = "unsupported_docker_prerequisites"

// Prerequisites inspects the already prepared engine without creating resources.
func (e *Engine) Prerequisites(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, e.callTimeout)
	defer cancel()
	info, err := e.api.Info(ctx, client.InfoOptions{})
	if err != nil {
		return classify(err)
	}
	if info.Info.OSType != "linux" || (info.Info.Architecture != "amd64" && info.Info.Architecture != "x86_64") {
		return ErrPrerequisites
	}
	for _, option := range info.Info.SecurityOptions {
		if strings.Contains(option, "rootless") {
			return ErrPrerequisites
		}
	}
	return nil
}
