package dockerengine

import (
	"context"
	"regexp"
	"strings"

	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	"github.com/containerd/errdefs"
	"github.com/moby/moby/client"
)

var enrollmentID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var resourceName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,127}$`)

func (e *Engine) Deregister(parent context.Context, stack workload.Stack) (string, error) {
	ctx, cancel := context.WithTimeout(parent, e.callTimeout)
	defer cancel()
	target, err := e.clientTarget(ctx, stack, false)
	if err != nil {
		return "", err
	}
	var command StatusCommand
	outcome, err := command.control(ctx, e, target.ID, target.User, stack.Client.Deregister, "deregistered", EnrollmentFailed)
	return string(outcome), err
}

func (e *Engine) RemoveEnrollment(ctx context.Context, installation string) error {
	if !enrollmentID.MatchString(installation) {
		return workload.ErrConflict
	}
	prefix := "af-" + installation + "-"
	filters := make(client.Filters).Add("label", workload.LabelPrefix+"managed-by="+workload.Manager).
		Add("label", workload.LabelPrefix+"installation="+installation)
	if err := e.removeContainers(ctx, installation, prefix, filters); err != nil {
		return err
	}
	if err := e.removeNetwork(ctx, installation, prefix+"net"); err != nil {
		return err
	}
	return e.removeVolumes(ctx, installation, prefix, filters)
}

func (e *Engine) removeContainers(ctx context.Context, installation, prefix string, filters client.Filters) error {
	call, cancel := context.WithTimeout(ctx, e.callTimeout)
	list, err := e.api.ContainerList(call, client.ContainerListOptions{All: true, Filters: filters})
	cancel()
	if err != nil {
		return classify(err)
	}
	for _, item := range list.Items {
		if item.Labels[workload.LabelPrefix+"managed-by"] != workload.Manager ||
			item.Labels[workload.LabelPrefix+"installation"] != installation {
			continue
		}
		if !containerID.MatchString(item.ID) {
			return workload.ErrConflict
		}
		if err := e.removeContainer(ctx, installation, prefix, item.ID); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) removeContainer(ctx context.Context, installation, prefix, id string) error {
	call, cancel := context.WithTimeout(ctx, e.callTimeout)
	defer cancel()
	result, err := e.api.ContainerInspect(call, id, client.ContainerInspectOptions{})
	if errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return classify(err)
	}
	c := result.Container
	if c.ID != id || c.Config == nil {
		return workload.ErrConflict
	}
	role := c.Config.Labels[workload.LabelPrefix+"role"]
	if !owned(c.Config.Labels, installation, role) {
		return nil
	}
	suffix, ok := strings.CutPrefix(c.Name, "/"+prefix)
	if !ok || !resourceName.MatchString(role) || !resourceName.MatchString(suffix) {
		return workload.ErrConflict
	}
	// Omit RemoveVolumes so unrelated mounts are outside this operation.
	_, err = e.api.ContainerRemove(call, c.ID, client.ContainerRemoveOptions{Force: true})
	if errdefs.IsNotFound(err) {
		return nil
	}
	return classify(err)
}

func (e *Engine) removeNetwork(ctx context.Context, installation, name string) error {
	call, cancel := context.WithTimeout(ctx, e.callTimeout)
	defer cancel()
	result, err := e.api.NetworkInspect(call, name, client.NetworkInspectOptions{})
	if errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return classify(err)
	}
	n := result.Network
	if !owned(n.Labels, installation, "network") {
		return nil
	}
	if !containerID.MatchString(n.ID) || n.Name != name || n.Driver != "bridge" {
		return workload.ErrConflict
	}
	_, err = e.api.NetworkRemove(call, n.ID, client.NetworkRemoveOptions{})
	if errdefs.IsNotFound(err) {
		return nil
	}
	return classify(err)
}

func (e *Engine) removeVolumes(ctx context.Context, installation, prefix string, filters client.Filters) error {
	call, cancel := context.WithTimeout(ctx, e.callTimeout)
	list, err := e.api.VolumeList(call, client.VolumeListOptions{Filters: filters})
	cancel()
	if err != nil {
		return classify(err)
	}
	if len(list.Warnings) > 0 {
		return workload.ErrEngine
	}
	for _, item := range list.Items {
		if !owned(item.Labels, installation, "volume") {
			continue
		}
		suffix, ok := strings.CutPrefix(item.Name, prefix)
		if !ok || !resourceName.MatchString(suffix) {
			return workload.ErrConflict
		}
		if err := e.removeVolume(ctx, installation, item.Name); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) removeVolume(ctx context.Context, installation, name string) error {
	call, cancel := context.WithTimeout(ctx, e.callTimeout)
	defer cancel()
	result, err := e.api.VolumeInspect(call, name, client.VolumeInspectOptions{})
	if errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return classify(err)
	}
	v := result.Volume
	if !owned(v.Labels, installation, "volume") {
		return nil
	}
	if v.Name != name || v.Driver != "local" || len(v.Options) != 0 {
		return workload.ErrConflict
	}
	_, err = e.api.VolumeRemove(call, name, client.VolumeRemoveOptions{})
	if errdefs.IsNotFound(err) {
		return nil
	}
	return classify(err)
}
