package dockerengine

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

var installationID = regexp.MustCompile(`^[a-f0-9]{32}$`)

func validBindingProfile(s workload.Spec) bool {
	if len(s.Bindings) == 0 {
		return true
	}
	if s.Role != "gateway" || !installationID.MatchString(s.Installation) || len(s.Bindings) != 2 {
		return false
	}
	base := workload.APIRuntimeDir(s.Installation)
	return s.Bindings[0] == (workload.Binding{Source: base + "/api", Target: "/run/firmware-device-api", ReadOnly: true}) && s.Bindings[1] == (workload.Binding{Source: base + "/auth", Target: "/run/firmware-device-api-auth", ReadOnly: true})
}
func knownMount(s workload.Spec, m container.MountPoint) bool {
	for _, expected := range s.Mounts {
		if m.Type == mount.TypeVolume && m.Name == expected.Name && m.Destination == expected.Target {
			return true
		}
	}
	if !validBindingProfile(s) {
		return false
	}
	for _, expected := range s.Bindings {
		if m.Type == mount.TypeBind && m.Source == expected.Source && m.Destination == expected.Target && m.RW != expected.ReadOnly {
			return true
		}
	}
	return false
}
func (e *Engine) apiPrerequisites(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, e.callTimeout)
	defer cancel()
	info, err := e.api.Info(ctx, client.InfoOptions{})
	if err != nil {
		return classify(err)
	}
	for _, o := range info.Info.SecurityOptions {
		if strings.Contains(o, "userns") || strings.Contains(o, "rootless") {
			return ErrPrerequisites
		}
	}
	return nil
}

// AnyVolumesExist is read-only; any retained identity or data forbids implicit
// key initialization, even when a different volume has been removed.
func (e *Engine) AnyVolumesExist(parent context.Context, s workload.Stack) (bool, error) {
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

func (e *Engine) AgentHome(parent context.Context, s workload.Stack) (string, error) {
	ctx, cancel := context.WithTimeout(parent, e.callTimeout)
	defer cancel()
	name := "af-" + s.Installation + "-agent-home"
	v, err := e.api.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
	if err != nil {
		return "", classify(err)
	}
	if v.Volume.Name != name || !owned(v.Volume.Labels, s.Installation, "volume") || v.Volume.Driver != "local" || len(v.Volume.Options) > 0 || !filepath.IsAbs(v.Volume.Mountpoint) || filepath.Clean(v.Volume.Mountpoint) != v.Volume.Mountpoint {
		return "", workload.ErrConflict
	}
	return v.Volume.Mountpoint, nil
}

func creationUser(s workload.Spec) string {
	if len(s.Bindings) > 0 {
		return "65532:65532"
	}
	return ""
}
func effectiveUser(s workload.Spec, imageUser string) string {
	if u := creationUser(s); u != "" {
		return u
	}
	return imageUser
}
