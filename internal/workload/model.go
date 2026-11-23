// Package workload resolves the manifest and owns the container transaction.
package workload

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/amnezia-gpt/agent-firmware/internal/backendprofile"
	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/workloadrelease"
)

const Manager = "agent-firmware"
const LabelPrefix = "org.amnezia.firmware."

type Error string

func (e Error) Error() string { return string(e) }

const (
	ErrEngine             Error = "engine_failed"
	ErrConflict           Error = "resource_conflict"
	ErrHealth             Error = "workload_health_failed"
	ErrTimeout            Error = "engine_timeout"
	ErrCanceled           Error = "operation_cancelled"
	ErrDeadline           Error = "operation_deadline"
	ErrJournal            Error = "operation_journal_failed"
	ErrRecovery           Error = "recovery_required"
	ErrCleanup            Error = "backup_cleanup_pending"
	ErrVolumeUnclassified Error = "volume_retention_missing"
	ErrVolumeClass        Error = "volume_retention_conflict"
	ErrUserVolumeMissing  Error = "held_user_data_not_declared"
)

const ErrEnginePrerequisites Error = "unsupported_docker_prerequisites"
const ErrEngineUnavailable Error = "engine_unavailable"

type Mount struct {
	Name, Target string
	ReadOnly     bool
}
type Spec struct {
	Installation, Role, Name, Image, Network, User string
	Aliases, DependsOn                             []string
	Env                                            map[string]string
	UnsetEnv                                       []string `json:",omitempty"`
	Mounts                                         []Mount
	ReadOnly                                       bool
	HealthStart, StopSeconds                       int
}

func (s Spec) Hash() string {
	b, _ := json.Marshal(s)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

type Stack struct {
	Installation, Generation, Network string
	Issuer                            string
	Volumes                           map[string]workloadrelease.Retention
	Specs                             []Spec
	DeviceAPI                         string
	Client                            workloadrelease.APIClient
	Retired                           []State
}

func EnrollmentID(issuer string) string {
	sum := sha256.Sum256([]byte("urn:agent-firmware:enrollment:v1\n" + issuer))
	return hex.EncodeToString(sum[:16])
}

func VolumeStack(release workloadrelease.Release, issuer string) Stack {
	id := EnrollmentID(issuer)
	prefix := "af-" + id + "-"
	stack := Stack{Installation: id, Issuer: issuer, Network: prefix + "net",
		Volumes: make(map[string]workloadrelease.Retention, len(release.Volumes))}
	for name, declaration := range release.Volumes {
		stack.Volumes[prefix+name] = declaration.Retention
	}
	return stack
}

func FromReaders(p, r io.Reader) (Stack, error) {
	input, err := configuration.Parse(p, r)
	if err != nil {
		return Stack{}, err
	}
	return FromInput(input)
}

func FromInput(input *configuration.Input) (Stack, error) {
	profile, release := input.Profile(), input.Release()
	resolved, err := resolveScopes(profile, release.ResourceScopes())
	if err != nil {
		return Stack{}, err
	}
	stack := VolumeStack(release, profile.AuthIssuer)
	id := stack.Installation
	prefix := "af-" + id + "-"
	stack.Generation, stack.DeviceAPI, stack.Client = input.Generation(), release.DeviceAPI, release.Client
	for _, name := range release.Order() {
		service := release.Services[name]
		spec := Spec{
			Installation: id, Role: name, Name: prefix + name, Image: service.Image.Reference(),
			Network: stack.Network, User: service.User, ReadOnly: service.ReadOnly,
			HealthStart: service.HealthStart, StopSeconds: service.StopGrace,
			Aliases: slices.Clone(service.Aliases), DependsOn: slices.Clone(service.DependsOn),
			Env: make(map[string]string, len(service.Environment)),
		}
		if !slices.Contains(spec.Aliases, name) {
			spec.Aliases = append(spec.Aliases, name)
		}
		slices.Sort(spec.Aliases)
		slices.Sort(spec.DependsOn)
		for key, value := range service.Environment {
			switch value.From {
			case "issuer":
				spec.Env[key] = profile.AuthIssuer
			case "trust_bundle":
				spec.Env[key] = profile.IngressTrustBundle
			case "trust_bundle_sha256":
				spec.Env[key] = profile.IngressTrustSHA256
			case "resource":
				if url, ok := resolved[value.Scope]; ok {
					spec.Env[key] = url
				} else {
					spec.UnsetEnv = append(spec.UnsetEnv, key)
				}
			default:
				spec.Env[key] = value.Value
			}
		}
		slices.Sort(spec.UnsetEnv)
		for _, volume := range service.Volumes {
			spec.Mounts = append(spec.Mounts, Mount{prefix + volume.Source, volume.Target, volume.ReadOnly})
		}
		slices.SortFunc(spec.Mounts, func(a, b Mount) int { return strings.Compare(a.Target, b.Target) })
		stack.Specs = append(stack.Specs, spec)
	}
	return stack, nil
}

// A known ambiguity takes precedence over a missing scope. An update must
// never use a resource refresh to choose between competing identifiers.
func resolveScopes(profile backendprofile.Profile, scopes []workloadrelease.ResourceScope) (map[string]string, error) {
	resolved := make(map[string]string, len(scopes))
	missing := false
	for _, scope := range scopes {
		resource, err := profile.ResolveScope(scope.Scope)
		if errors.Is(err, backendprofile.ErrScopeMissing) {
			missing = missing || scope.Required
			continue
		}
		if err != nil {
			return nil, err
		}
		resolved[scope.Scope] = resource
	}
	if missing {
		return nil, backendprofile.ErrScopeMissing
	}
	return resolved, nil
}
