package dockerengine

import (
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
)

func knownMount(s workload.Spec, m container.MountPoint) bool {
	for _, expected := range s.Mounts {
		if m.Type == mount.TypeVolume && m.Name == expected.Name && m.Destination == expected.Target {
			return true
		}
	}
	return false
}

func effectiveUser(s workload.Spec, imageUser string) string {
	if s.User != "" {
		return s.User
	}
	return imageUser
}
