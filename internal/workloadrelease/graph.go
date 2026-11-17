package workloadrelease

import (
	"maps"
	"path"
	"slices"
	"strings"
)

func (r Release) validReferences() bool {
	if _, exists := r.Services[r.Client.Service]; !exists {
		return false
	}
	aliases := make(map[string]string)
	usedVolumes := make(map[string]bool)
	for name, service := range r.Services {
		for _, alias := range service.Aliases {
			if _, serviceName := r.Services[alias]; serviceName && alias != name || aliases[alias] != "" {
				return false
			}
			aliases[alias] = name
		}
		mounted := make(map[string]bool)
		for i, volume := range service.Volumes {
			if mounted[volume.Source] || !slices.Contains(r.Volumes, volume.Source) || path.Clean(volume.Target) != volume.Target ||
				volume.Target == "/tmp" || strings.HasPrefix(volume.Target, "/tmp/") {
				return false
			}
			for _, previous := range service.Volumes[:i] {
				if overlaps(volume.Target, previous.Target) {
					return false
				}
			}
			mounted[volume.Source] = true
			usedVolumes[volume.Source] = true
		}
	}
	return len(usedVolumes) == len(r.Volumes) && len(r.Order()) == len(r.Services)
}

func overlaps(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// Order returns a deterministic dependency-first ordering. Unknown dependencies
// and cycles leave their services out, so the parser rejects those graphs.
func (r Release) Order() []string {
	names := slices.Sorted(maps.Keys(r.Services))
	var ordered []string
	ready := make(map[string]bool, len(names))
	for range names {
		for _, name := range names {
			if ready[name] {
				continue
			}
			satisfied := true
			for _, dependency := range r.Services[name].DependsOn {
				satisfied = satisfied && ready[dependency]
			}
			if satisfied {
				ordered = append(ordered, name)
				ready[name] = true
				break
			}
		}
	}
	return ordered
}

// ResourceScopes returns the distinct OAuth scopes requested by the manifest.
func (r Release) ResourceScopes() []string {
	scopes := map[string]bool{}
	for _, service := range r.Services {
		for _, value := range service.Environment {
			if value.From == "resource" {
				scopes[value.Scope] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(scopes))
}
