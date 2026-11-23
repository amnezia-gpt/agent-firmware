package workload

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/amnezia-gpt/agent-firmware/internal/backendprofile"
)

func TestReleaseVectorsBuildAndApplyAnotherStack(t *testing.T) {
	raw, err := os.ReadFile("../../.agents/plans/SELF-24/workload-release-v4.vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct{ Valid json.RawMessage }
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	// Exercise an implicit service name and a distinct explicit alias.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(vectors.Valid, &fields); err != nil {
		t.Fatal(err)
	}
	var services map[string]map[string]json.RawMessage
	if err := json.Unmarshal(fields["services"], &services); err != nil {
		t.Fatal(err)
	}
	delete(services["worker"], "aliases")
	services["front"]["aliases"] = json.RawMessage(`["front","ingress"]`)
	fields["services"], err = json.Marshal(services)
	if err != nil {
		t.Fatal(err)
	}
	release, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := os.ReadFile("../backendprofile/testdata/profile-v4.json")
	if err != nil {
		t.Fatal(err)
	}
	stack, err := FromReaders(bytes.NewReader(profile), bytes.NewReader(release))
	if err != nil {
		t.Fatal(err)
	}
	if len(stack.Specs) != 2 || stack.Specs[0].Role != "front" || stack.Specs[1].Role != "worker" ||
		stack.Specs[0].Env["API_BASE_URL"] != "https://resource.example/api" || stack.Client.Service != "front" ||
		stack.Installation != EnrollmentID("https://auth.example") {
		t.Fatal("manifest was not resolved into the expected stack")
	}
	if !slices.Equal(stack.Specs[0].Aliases, []string{"front", "ingress"}) ||
		!slices.Equal(stack.Specs[1].Aliases, []string{"worker"}) {
		t.Fatalf("wrong network aliases: front=%v worker=%v", stack.Specs[0].Aliases, stack.Specs[1].Aliases)
	}

	engine := fake()
	if result, err := Apply(t.Context(), engine, &memoryJournal{}, stack); err != nil || !result.Applied {
		t.Fatalf("alternative stack convergence: %+v, %v", result, err)
	}
	expect := []string{"prepare", "create:front", "start:front", "wait:front", "create:worker", "start:worker", "wait:worker"}
	if !slices.Equal(engine.calls, expect) {
		t.Fatalf("wrong health order: %v", engine.calls)
	}

	for _, resources := range []struct {
		replacement string
		want        error
	}{
		{`"example:other"`, backendprofile.ErrScopeMissing},
		{`"example:use"`, nil},
	} {
		modified := bytes.Replace(profile, []byte(`"example:use"`), []byte(resources.replacement), 1)
		if _, err := FromReaders(bytes.NewReader(modified), bytes.NewReader(vectors.Valid)); !errors.Is(err, resources.want) {
			t.Fatalf("scope resolution: %v", err)
		}
	}
}

func TestOptionalResourceBindingResolution(t *testing.T) {
	manifest, err := os.ReadFile("../workloadrelease/testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	rawProfile, err := os.ReadFile("../backendprofile/testdata/profile-v4.json")
	if err != nil {
		t.Fatal(err)
	}
	manifest = bytes.Replace(manifest, []byte(`"schema_version": 4`), []byte(`"schema_version": 5`), 1)
	required := []byte(`"scope": "example:use"`)
	optional := bytes.Replace(manifest, required, []byte(`"scope": "example:use", "optional": true`), 1)
	mixed := bytes.Replace(optional, []byte(`"AUTH_ISSUER": {`), []byte(`"REQUIRED_URL":{"from":"resource","scope":"example:use"},"AUTH_ISSUER": {`), 1)
	for _, test := range []struct {
		name     string
		manifest []byte
		urls     []string
		wantErr  error
	}{
		{name: "available", manifest: optional, urls: []string{"https://resource.example/api"}},
		{name: "absent", manifest: optional},
		{name: "ambiguous", manifest: optional, urls: []string{"https://resource.example/api", "https://other.example/api"}, wantErr: backendprofile.ErrScopeAmbiguous},
		{name: "required", manifest: manifest, wantErr: backendprofile.ErrScopeMissing},
		{name: "mixed", manifest: mixed, wantErr: backendprofile.ErrScopeMissing},
	} {
		t.Run(test.name, func(t *testing.T) {
			profile, err := backendprofile.Parse(bytes.NewReader(rawProfile))
			if err != nil {
				t.Fatal(err)
			}
			profile.Resources = []backendprofile.Resource{}
			for _, url := range test.urls {
				profile.Resources = append(profile.Resources, backendprofile.Resource{Identifier: url, Scopes: []string{"example:use"}})
			}
			raw, err := backendprofile.Encode(profile)
			if err != nil {
				t.Fatal(err)
			}
			stack, err := FromReaders(bytes.NewReader(raw), bytes.NewReader(test.manifest))
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("stack: %v, want %v", err, test.wantErr)
			}
			if err != nil {
				return
			}
			spec := stack.Specs[0]
			value, found := spec.Env["API_BASE_URL"]
			if len(test.urls) == 0 {
				if found || !slices.Contains(spec.UnsetEnv, "API_BASE_URL") {
					t.Fatalf("missing binding did not suppress the variable: set=%t, unset=%v", found, spec.UnsetEnv)
				}
			} else if !found || value != test.urls[0] || len(spec.UnsetEnv) != 0 {
				t.Fatalf("unexpected resolved binding: %q, unset=%v", value, spec.UnsetEnv)
			}
			if result, err := Apply(t.Context(), fake(), &memoryJournal{}, stack); err != nil || !result.Applied {
				t.Fatalf("stack apply: %+v, %v", result, err)
			}
		})
	}
}
