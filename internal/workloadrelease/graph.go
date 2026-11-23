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
			if _, declared := r.Volumes[volume.Source]; mounted[volume.Source] || !declared || path.Clean(volume.Target) != volume.Target ||
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

type ResourceScope struct {
	Scope    string
	Required bool
}

// ResourceScopes returns each scope once. Any required binding makes its scope required.
func (r Release) ResourceScopes() []ResourceScope {
	scopes := map[string]bool{}
	for _, service := range r.Services {
		for _, value := range service.Environment {
			if value.From == "resource" {
				scopes[value.Scope] = scopes[value.Scope] || !value.Optional
			}
		}
	}
	var result []ResourceScope
	for _, scope := range slices.Sorted(maps.Keys(scopes)) {
		result = append(result, ResourceScope{Scope: scope, Required: scopes[scope]})
	}
	return result
}
