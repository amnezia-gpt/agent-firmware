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
