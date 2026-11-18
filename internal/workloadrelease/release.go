// Package workloadrelease validates the firmware's bounded release manifest.
package workloadrelease

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/amnezia-gpt/agent-firmware/internal/strictjson"
)

const (
	MaxBytes    = 64 * 1024
	MaxServices = 8
	DeviceAPIV3 = "urn:agent-firmware:device-api:v3"
)

type Error string

func (e Error) Error() string { return string(e) }

const (
	ErrRead      Error = "release_read_failed"
	ErrTooLarge  Error = "release_too_large"
	ErrInvalid   Error = "invalid_workload_release"
	ErrDeviceAPI Error = "unsupported_device_api"
	ErrPlatform  Error = "unsupported_workload_platform"
)

type Release struct {
	Kind           string                       `json:"kind"`
	Version        string                       `json:"release"`
	SourceRevision string                       `json:"source_revision"`
	DeviceAPI      string                       `json:"device_api"`
	Platforms      []string                     `json:"platforms"`
	Volumes        map[string]VolumeDeclaration `json:"volumes"`
	Services       map[string]Service           `json:"services"`
	Client         APIClient                    `json:"device_api_client"`
}

type Service struct {
	Image       Image            `json:"image"`
	User        string           `json:"user,omitempty"`
	ReadOnly    bool             `json:"read_only,omitempty"`
	Aliases     []string         `json:"aliases,omitempty"`
	DependsOn   []string         `json:"depends_on,omitempty"`
	Environment map[string]Value `json:"environment,omitempty"`
	Volumes     []Volume         `json:"volumes,omitempty"`
	HealthStart int              `json:"health_start_seconds"`
	StopGrace   int              `json:"stop_grace_seconds"`
}

type Image struct {
	Repository string `json:"repository"`
	Digest     string `json:"digest"`
}

func (i Image) Reference() string { return i.Repository + "@" + i.Digest }

func IsPinnedReference(reference string) bool {
	repository, digest, ok := strings.Cut(reference, "@")
	return ok && repositoryPattern.MatchString(repository) && digestPattern.MatchString(digest)
}

type Volume struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only,omitempty"`
}

type Retention string

const (
	Enrollment Retention = "enrollment"
	UserData   Retention = "user-data"
)

type VolumeDeclaration struct {
	Retention Retention `json:"retention"`
}

type Value struct {
	Value string `json:"value,omitempty"`
	From  string `json:"from,omitempty"`
	Scope string `json:"scope,omitempty"`
}

type APIClient struct {
	Service          string   `json:"service"`
	Dial             []string `json:"dial"`
	EnrollmentStatus []string `json:"enrollment_status"`
	Deregister       []string `json:"deregister"`
}

var (
	revisionPattern   = regexp.MustCompile(`^[a-f0-9]{40}$`)
	digestPattern     = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	versionPattern    = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	namePattern       = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	aliasPattern      = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	userPattern       = regexp.MustCompile(`^[1-9][0-9]{0,9}:[1-9][0-9]{0,9}$`)
	envPattern        = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	scopePattern      = regexp.MustCompile(`^[\x21\x23-\x5B\x5D-\x7E]{1,128}$`)
	targetPattern     = regexp.MustCompile(`^/[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
	repositoryPattern = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*(:[0-9]{1,5})?(/[a-z0-9]+([._-][a-z0-9]+)*)+$`)
)

func Parse(reader io.Reader) (Release, error) {
	data, err := io.ReadAll(io.LimitReader(reader, MaxBytes+1))
	if err != nil {
		return Release{}, ErrRead
	}
	if len(data) > MaxBytes {
		return Release{}, ErrTooLarge
	}
	fields, ok := object(data, []string{
		"schema_version", "kind", "release", "source_revision", "device_api", "platforms", "volumes", "services", "device_api_client",
	})
	var release Release
	if !ok || string(bytes.TrimSpace(fields["schema_version"])) != "4" || json.Unmarshal(data, &release) != nil ||
		release.Kind != "workload-release" || release.DeviceAPI != DeviceAPIV3 ||
		!versionPattern.MatchString(release.Version) || !revisionPattern.MatchString(release.SourceRevision) ||
		len(release.Platforms) < 1 || !unique(release.Platforms, 4, validPlatform) ||
		!validVolumes(fields["volumes"], release.Volumes) ||
		!validServices(fields["services"], release.Services) || !validClient(fields["device_api_client"], release.Client) ||
		!release.validReferences() {
		return Release{}, ErrInvalid
	}
	return release, nil
}

func validVolumes(raw []byte, volumes map[string]VolumeDeclaration) bool {
	fields, ok := strictjson.Members(raw)
	if !ok || len(fields) > 16 {
		return false
	}
	for name, declaration := range fields {
		_, ok := object(declaration, []string{"retention"})
		class := volumes[name].Retention
		if !ok || !namePattern.MatchString(name) || class != Enrollment && class != UserData {
			return false
		}
	}
	return true
}

// object rejects case aliases, duplicates, unknown members and null fields.
// Struct decoding then checks the exact types of the supplied values.
func object(raw []byte, required []string, optional ...string) (map[string]json.RawMessage, bool) {
	fields, ok := strictjson.Members(raw)
	if !ok {
		return nil, false
	}
	for _, name := range required {
		if fields[name] == nil {
			return nil, false
		}
	}
	for name, value := range fields {
		if (!slices.Contains(required, name) && !slices.Contains(optional, name)) ||
			bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, false
		}
	}
	return fields, true
}

func validServices(data []byte, services map[string]Service) bool {
	fields, ok := strictjson.Members(data)
	if !ok || len(fields) < 1 || len(fields) > MaxServices {
		return false
	}
	for name, raw := range fields {
		if !namePattern.MatchString(name) || !validService(raw, services[name]) {
			return false
		}
	}
	return true
}

func validService(raw []byte, service Service) bool {
	fields, ok := object(raw, []string{"image", "health_start_seconds", "stop_grace_seconds"},
		"user", "read_only", "aliases", "depends_on", "environment", "volumes")
	if !ok || service.HealthStart < 1 || service.HealthStart > 600 || service.StopGrace < 1 || service.StopGrace > 120 {
		return false
	}
	if _, ok := object(fields["image"], []string{"repository", "digest"}); !ok ||
		len(service.Image.Repository) > 255 || !repositoryPattern.MatchString(service.Image.Repository) ||
		!digestPattern.MatchString(service.Image.Digest) {
		return false
	}
	if fields["user"] != nil && !ValidUser(service.User) ||
		!unique(service.Aliases, 4, aliasPattern.MatchString) || !unique(service.DependsOn, 7, namePattern.MatchString) {
		return false
	}
	if fields["environment"] != nil && !validEnvironment(fields["environment"], service.Environment) {
		return false
	}
	var volumes []json.RawMessage
	if fields["volumes"] != nil && (json.Unmarshal(fields["volumes"], &volumes) != nil || len(volumes) > 16) {
		return false
	}
	for i, raw := range volumes {
		volume := service.Volumes[i]
		if _, ok := object(raw, []string{"source", "target"}, "read_only"); !ok ||
			!namePattern.MatchString(volume.Source) || len(volume.Target) > 255 || !targetPattern.MatchString(volume.Target) {
			return false
		}
	}
	return true
}

func validEnvironment(data []byte, values map[string]Value) bool {
	fields, ok := strictjson.Members(data)
	if !ok || len(fields) > 32 {
		return false
	}
	for name, raw := range fields {
		if !envPattern.MatchString(name) {
			return false
		}
		v := values[name]
		switch v.From {
		case "":
			if _, ok := object(raw, []string{"value"}); !ok || utf8.RuneCountInString(v.Value) > 4096 || strings.ContainsRune(v.Value, 0) {
				return false
			}
		case "issuer", "trust_bundle", "trust_bundle_sha256":
			if _, ok := object(raw, []string{"from"}); !ok {
				return false
			}
		case "resource":
			if _, ok := object(raw, []string{"from", "scope"}); !ok || !ValidScope(v.Scope) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func validClient(raw []byte, client APIClient) bool {
	_, ok := object(raw, []string{"service", "dial", "enrollment_status", "deregister"})
	return ok && namePattern.MatchString(client.Service) && validCommand(client.Dial) &&
		validCommand(client.EnrollmentStatus) && validCommand(client.Deregister)
}

func validCommand(command []string) bool {
	if len(command) < 1 || len(command) > 8 {
		return false
	}
	for _, arg := range command {
		if arg == "" || utf8.RuneCountInString(arg) > 256 || strings.ContainsFunc(arg, func(r rune) bool { return r < 32 }) {
			return false
		}
	}
	return true
}

func validPlatform(platform string) bool {
	return platform == "linux/amd64" || platform == "linux/arm64"
}

func unique(values []string, maximum int, valid func(string) bool) bool {
	if len(values) > maximum {
		return false
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] || !valid(value) {
			return false
		}
		seen[value] = true
	}
	return true
}

// ValidUser requires an explicit non-root numeric UID and GID within the
// engine's Unix credential range.
func ValidUser(user string) bool {
	if !userPattern.MatchString(user) {
		return false
	}
	uid, gid, _ := strings.Cut(user, ":")
	u, err := strconv.ParseUint(uid, 10, 32)
	g, groupErr := strconv.ParseUint(gid, 10, 32)
	return err == nil && groupErr == nil && u < 1<<32-1 && g < 1<<32-1
}

func ValidScope(scope string) bool { return scopePattern.MatchString(scope) }

func (r Release) CheckPlatform(platform string) error {
	if !slices.Contains(r.Platforms, platform) {
		return ErrPlatform
	}
	return nil
}

func (r Release) CheckDeviceAPI(served string) error {
	if r.DeviceAPI != served {
		return ErrDeviceAPI
	}
	return nil
}
