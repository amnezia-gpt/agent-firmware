package dockerengine

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	"github.com/amnezia-gpt/agent-firmware/internal/workloadrelease"
	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// CheckRemoval checks the entire installation namespace before deregistration
// or deletion. This includes conflicting resources that label filters hide.
func (e *Engine) CheckRemoval(ctx context.Context, installation string) error {
	_, err := e.removalInventory(ctx, installation, false)
	return err
}

// RemoveReleaseImages only removes pinned release references. A stopped
// container can still depend on the image, including through a different tag.
func (e *Engine) RemoveReleaseImages(ctx context.Context, images []string) error {
	if len(images) == 0 {
		return nil
	}
	call, cancel := context.WithTimeout(ctx, e.callTimeout)
	containers, err := e.api.ContainerList(call, client.ContainerListOptions{All: true})
	cancel()
	if err != nil {
		return classify(err)
	}
	for _, reference := range images {
		if !workloadrelease.IsPinnedReference(reference) {
			return workload.ErrConflict
		}
		call, cancel = context.WithTimeout(ctx, e.callTimeout)
		image, err := e.api.ImageInspect(call, reference)
		cancel()
		if errdefs.IsNotFound(err) {
			continue
		}
		if err != nil {
			return classify(err)
		}
		id, ok := strings.CutPrefix(image.ID, "sha256:")
		if !ok || !containerID.MatchString(id) {
			return workload.ErrEngine
		}
		if slices.ContainsFunc(containers.Items, func(item container.Summary) bool {
			return item.ImageID == image.ID || item.Image == reference
		}) {
			continue
		}
		call, cancel = context.WithTimeout(ctx, e.callTimeout)
		_, err = e.api.ImageRemove(call, reference, client.ImageRemoveOptions{})
		cancel()
		if err != nil && !errdefs.IsNotFound(err) && !errdefs.IsConflict(err) {
			return classify(err)
		}
	}
	return nil
}

func (e *Engine) removalInventory(ctx context.Context, installation string, keptOK bool) (bool, error) {
	if !enrollmentID.MatchString(installation) {
		return false, workload.ErrConflict
	}
	prefix := "af-" + installation + "-"
	call, cancel := context.WithTimeout(ctx, e.callTimeout)
	containers, err := e.api.ContainerList(call, client.ContainerListOptions{All: true})
	cancel()
	if err != nil {
		return false, classify(err)
	}
	found := false
	ownedContainers := make(map[string]bool)
	for _, item := range containers.Items {
		role := item.Labels[workload.LabelPrefix+"role"]
		ownedName := slices.ContainsFunc(item.Names, func(name string) bool { return strings.HasPrefix(name, "/"+prefix) })
		if ownedName || item.Labels[workload.LabelPrefix+"installation"] == installation {
			if !owned(item.Labels, installation, role) || !containerID.MatchString(item.ID) ||
				!resourceName.MatchString(role) || len(item.Names) != 1 ||
				!strings.HasPrefix(item.Names[0], "/"+prefix) || !resourceName.MatchString(strings.TrimPrefix(item.Names[0], "/"+prefix)) {
				return false, workload.ErrConflict
			}
			found = true
			ownedContainers[item.ID] = true
			continue
		}
		if item.NetworkSettings != nil && item.NetworkSettings.Networks[prefix+"net"] != nil {
			return false, workload.ErrConflict
		}
		for _, mount := range item.Mounts {
			if strings.HasPrefix(mount.Name, prefix) {
				return false, workload.ErrConflict
			}
		}
	}
	call, cancel = context.WithTimeout(ctx, e.callTimeout)
	networks, err := e.api.NetworkList(call, client.NetworkListOptions{})
	cancel()
	if err != nil {
		return false, classify(err)
	}
	for _, network := range networks.Items {
		if strings.HasPrefix(network.Name, prefix) || network.Labels[workload.LabelPrefix+"installation"] == installation {
			if !owned(network.Labels, installation, "network") || network.Name != prefix+"net" ||
				!containerID.MatchString(network.ID) || network.Driver != "bridge" || network.Ingress ||
				network.Internal || len(network.Options) != 0 {
				return false, workload.ErrConflict
			}
			call, cancel = context.WithTimeout(ctx, e.callTimeout)
			details, err := e.api.NetworkInspect(call, network.ID, client.NetworkInspectOptions{})
			cancel()
			if err != nil && !errdefs.IsNotFound(err) {
				return false, classify(err)
			}
			for id := range details.Network.Containers {
				if !ownedContainers[id] {
					return false, workload.ErrConflict
				}
			}
			found = true
		}
	}
	call, cancel = context.WithTimeout(ctx, e.callTimeout)
	volumes, err := e.api.VolumeList(call, client.VolumeListOptions{})
	cancel()
	if err != nil {
		return false, classify(err)
	}
	if len(volumes.Warnings) > 0 {
		return false, workload.ErrEngine
	}
	for _, volume := range volumes.Items {
		suffix, named := strings.CutPrefix(volume.Name, prefix)
		if named || volume.Labels[workload.LabelPrefix+"installation"] == installation {
			info, err := volumeInfo(volume)
			if err != nil {
				return false, err
			}
			if volume.Labels[installationLabel] != installation || !named || !resourceName.MatchString(suffix) {
				return false, workload.ErrConflict
			}
			found = found || !keptOK || info.Retention == workloadrelease.Enrollment
		}
	}
	return found, nil
}

// OwnedInstallations discovers namespaces for offline cleanup when the
// installed manifest is unreadable. CheckRemoval verifies every candidate.
func (e *Engine) OwnedInstallations(ctx context.Context) ([]string, error) {
	filters := client.Filters{}.Add("label", managerLabel+"="+workload.Manager)
	ids := make(map[string]bool)
	call, cancel := context.WithTimeout(ctx, e.callTimeout)
	containers, err := e.api.ContainerList(call, client.ContainerListOptions{All: true, Filters: filters})
	cancel()
	if err != nil {
		return nil, classify(err)
	}
	for _, item := range containers.Items {
		if item.Labels[managerLabel] == workload.Manager {
			ids[item.Labels[installationLabel]] = true
		}
	}
	call, cancel = context.WithTimeout(ctx, e.callTimeout)
	networks, err := e.api.NetworkList(call, client.NetworkListOptions{Filters: filters})
	cancel()
	if err != nil {
		return nil, classify(err)
	}
	for _, item := range networks.Items {
		if item.Labels[managerLabel] == workload.Manager {
			ids[item.Labels[installationLabel]] = true
		}
	}
	volumes, err := e.managedVolumeInfo(ctx, "")
	if err != nil {
		return nil, err
	}
	for _, item := range volumes {
		ids[item.installation] = true
	}
	return slices.Sorted(maps.Keys(ids)), nil
}

func (e *Engine) CheckAllVolumeRemoval(ctx context.Context, removing []string) error {
	volumes, err := e.managedVolumeInfo(ctx, "")
	if err != nil || len(volumes) == 0 {
		return err
	}
	ownedNames := make(map[string]bool, len(volumes))
	for _, v := range volumes {
		ownedNames[v.Name] = true
	}
	call, cancel := context.WithTimeout(ctx, e.callTimeout)
	defer cancel()
	containers, err := e.api.ContainerList(call, client.ContainerListOptions{All: true})
	if err != nil {
		return classify(err)
	}
	for _, item := range containers.Items {
		id := item.Labels[installationLabel]
		if item.Labels[managerLabel] == workload.Manager && slices.Contains(removing, id) {
			continue
		}
		for _, mount := range item.Mounts {
			if ownedNames[mount.Name] {
				return workload.ErrConflict
			}
		}
	}
	return nil
}
