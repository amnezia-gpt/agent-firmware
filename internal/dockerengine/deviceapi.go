package dockerengine

import (
	"context"

	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

func knownMount(s workload.Spec, m container.MountPoint) bool {
	for _, expected := range s.Mounts {
		if m.Type == mount.TypeVolume && m.Name == expected.Name && m.Destination == expected.Target {
			return true
		}
	}
	return false
}

// AnyVolumesExist is read-only; any retained identity or data forbids implicit
// key initialization, even when a different volume has been removed.
func (e *Engine) AnyVolumesExist(parent context.Context, s workload.Stack) (bool, error) {
	// A previous manifest may have used other volume names. Look for any
	// enrollment-owned data before checking collisions at the new names.
	ctx, cancel := context.WithTimeout(parent, e.callTimeout)
	list, err := e.api.VolumeList(ctx, client.VolumeListOptions{
		Filters: make(client.Filters).Add("label", workload.LabelPrefix+"installation="+s.Installation),
	})
	cancel()
	if err != nil {
		return false, classify(err)
	}
	if len(list.Warnings) != 0 {
		return false, workload.ErrEngine
	}
	for _, v := range list.Items {
		if !owned(v.Labels, s.Installation, "volume") || v.Driver != "local" || len(v.Options) > 0 {
			return false, workload.ErrConflict
		}
	}
	if len(list.Items) > 0 {
		return true, nil
	}
	for _, name := range s.Volumes {
		ctx, cancel := context.WithTimeout(parent, e.callTimeout)
		v, err := e.api.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
		cancel()
		if errdefs.IsNotFound(err) {
			continue
		}
		if err != nil {
			return false, classify(err)
		}
		if v.Volume.Name != name || !owned(v.Volume.Labels, s.Installation, "volume") || v.Volume.Driver != "local" || len(v.Volume.Options) > 0 {
			return false, workload.ErrConflict
		}
		return true, nil
	}
	return false, nil
}

func effectiveUser(s workload.Spec, imageUser string) string {
	if s.User != "" {
		return s.User
	}
	return imageUser
}
