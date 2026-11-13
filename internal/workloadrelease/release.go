// Package workloadrelease resolves the fixed device workload's supplied metadata.
package workloadrelease

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strings"

	"github.com/amnezia-gpt/agent-firmware/internal/strictjson"
)

const Contract = "urn:amnezia-gpt:workload-contract:device:v1"
const DeviceAPIContract = "urn:amnezia-gpt:workload-contract:device:v2"
const MaxBytes = 64 * 1024

type Error string

func (e Error) Error() string { return string(e) }

const (
	ErrRead     Error = "release_read_failed"
	ErrTooLarge Error = "release_too_large"
	ErrInvalid  Error = "invalid_workload_release"
	ErrContract Error = "unsupported_workload_contract"
)

type Release struct {
	Kind           string
	Version        string
	SourceRevision string
	Contract       string
	Platform       string
	GatewayImage   string
	CodexImage     string
}

var revisionPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var versionPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func Parse(reader io.Reader) (Release, error) {
	data, err := io.ReadAll(io.LimitReader(reader, MaxBytes+1))
	if err != nil {
		return Release{}, ErrRead
	}
	if len(data) > MaxBytes {
		return Release{}, ErrTooLarge
	}
	return parse(data)
}

func parse(data []byte) (Release, error) {
	var selector struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(data, &selector) != nil {
		return Release{}, ErrInvalid
	}
	names := []string{"schema_version", "kind", "source_revision", "platform", "images", "workload_contract"}
	switch selector.Kind {
	case "agent-workloads-release":
		names = append(names, "release")
	case "agent-workloads-publication":
		names = append(names, "publication_attempt", "validation", "supply_chain", "workflow_run_url")
	default:
		return Release{}, ErrInvalid
	}
	f, ok := strictjson.Object(data, names...)
	if !ok {
		// Classify an otherwise exact v2 object missing only its contract.
		// Unknown/duplicate members and other missing fields remain invalid.
		withoutContract := make([]string, 0, len(names)-1)
		for _, name := range names {
			if name != "workload_contract" {
				withoutContract = append(withoutContract, name)
			}
		}
		if fields, valid := strictjson.Object(data, withoutContract...); valid && string(bytes.TrimSpace(fields["schema_version"])) == "2" {
			return Release{}, ErrContract
		}
		return Release{}, ErrInvalid
	}
	if string(bytes.TrimSpace(f["schema_version"])) != "2" {
		return Release{}, ErrInvalid
	}
	str := func(name string) string { return strictjson.StringField(f, name) }
	r := Release{Kind: str("kind"), Version: str("release"), SourceRevision: str("source_revision"), Contract: str("workload_contract"), Platform: str("platform")}
	if r.Contract != Contract && r.Contract != DeviceAPIContract {
		return Release{}, ErrContract
	}
	if !revisionPattern.MatchString(r.SourceRevision) || r.Platform != "linux/amd64" {
		return Release{}, ErrInvalid
	}
	if r.Kind == "agent-workloads-release" && !versionPattern.MatchString(r.Version) {
		return Release{}, ErrInvalid
	}
	if r.Kind == "agent-workloads-publication" {
		validation, vok := strictjson.Object(f["validation"], "level", "command", "result")
		supply, sok := strictjson.Object(f["supply_chain"], "sbom", "provenance")
		if !vok || !sok || len(str("publication_attempt")) < 1 || len(str("publication_attempt")) > 200 ||
			!strings.HasPrefix(str("workflow_run_url"), "https://") ||
			strictjson.StringField(validation, "level") != "L2" || strictjson.StringField(validation, "result") != "passed" ||
			strictjson.StringField(validation, "command") != "just artifact publication-published-contracts" ||
			strictjson.StringField(supply, "sbom") != "buildkit:spdx" || strictjson.StringField(supply, "provenance") != "buildkit:mode=max" {
			return Release{}, ErrInvalid
		}
	}
	images, ok := strictjson.Object(f["images"], "openclaw-codex", "amgpt-device-gateway")
	if !ok {
		return Release{}, ErrInvalid
	}
	for _, role := range []string{"openclaw-codex", "amgpt-device-gateway"} {
		repo := "docker.io/amneziavpn/agent-workload-openclaw-codex"
		if role == "amgpt-device-gateway" {
			repo = "docker.io/amneziavpn/agent-workload-amgpt-auth-proxy"
		}
		members := []string{"repository", "digest", "reference"}
		if r.Kind == "agent-workloads-release" {
			members = append(members, "source_tag", "release_tag")
		} else {
			members = append(members, "candidate_tag", "published_tag", "published_reference", "channel_tag", "channel_reference")
		}
		image, ok := strictjson.Object(images[role], members...)
		value := func(k string) string { return strictjson.StringField(image, k) }
		if !ok || value("repository") != repo || !digestPattern.MatchString(value("digest")) || value("reference") != repo+"@"+value("digest") {
			return Release{}, ErrInvalid
		}
		if r.Kind == "agent-workloads-release" {
			if value("source_tag") != "sha-"+r.SourceRevision || value("release_tag") != r.Version {
				return Release{}, ErrInvalid
			}
		} else if value("candidate_tag") != "candidate-dev" || value("published_tag") != "sha-"+r.SourceRevision ||
			value("published_reference") != repo+":sha-"+r.SourceRevision || value("channel_tag") != "dev" || value("channel_reference") != repo+":dev" {
			return Release{}, ErrInvalid
		}
		if role == "amgpt-device-gateway" {
			r.GatewayImage = value("reference")
		} else {
			r.CodexImage = value("reference")
		}
	}
	return r, nil
}
