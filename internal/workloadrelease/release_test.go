package workloadrelease

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

func encoded(t *testing.T, fields map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseReleaseShapes(t *testing.T) {
	for _, name := range []string{"manifest", "publication"} {
		t.Run(name, func(t *testing.T) {
			got, err := Parse(bytes.NewReader(encoded(t, fixture(t, name))))
			if err != nil {
				t.Fatal(err)
			}
			if got.Platform != "linux/amd64" || got.SourceRevision != strings.Repeat("1", 40) ||
				!strings.HasSuffix(got.GatewayImage, "@sha256:"+strings.Repeat("b", 64)) ||
				!strings.HasSuffix(got.CodexImage, "@sha256:"+strings.Repeat("a", 64)) {
				t.Fatalf("unexpected resolved release: %#v", got)
			}
		})
	}
}

func TestParseRejectsEarlierContractsAndOtherSchemas(t *testing.T) {
	for _, name := range []string{"manifest", "publication"} {
		for _, test := range []struct {
			name string
			edit func(map[string]any)
			want error
		}{
			{"old manifest version", func(p map[string]any) { p["schema_version"] = 1 }, ErrInvalid},
			{"old selector v1", func(p map[string]any) { p["workload_contract"] = "urn:amnezia-gpt:workload-contract:device:v1" }, ErrInvalid},
			{"old selector v2", func(p map[string]any) { p["workload_contract"] = "urn:amnezia-gpt:workload-contract:device:v2" }, ErrInvalid},
			{"unknown version", func(p map[string]any) { p["schema_version"] = 3 }, ErrInvalid},
			{"zero version", func(p map[string]any) { p["schema_version"] = 0 }, ErrInvalid},
			{"string version", func(p map[string]any) { p["schema_version"] = "2" }, ErrInvalid},
			{"null version", func(p map[string]any) { p["schema_version"] = nil }, ErrInvalid},
			{"missing version", func(p map[string]any) { delete(p, "schema_version") }, ErrInvalid},
		} {
			t.Run(name+"/"+test.name, func(t *testing.T) {
				fields := fixture(t, name)
				test.edit(fields)
				got, err := Parse(bytes.NewReader(encoded(t, fields)))
				if !errors.Is(err, test.want) || got != (Release{}) {
					t.Fatalf("got %#v, error=%v; want empty release, error=%v", got, err, test.want)
				}
			})
		}
	}
}

func TestParseRejectsIncompatibleOrAmbiguousRelease(t *testing.T) {
	for _, name := range []string{"manifest", "publication"} {
		for _, test := range []struct {
			name string
			edit func(map[string]any)
		}{
			{"candidate", func(p map[string]any) { p["kind"] = "agent-workloads-publication-candidate" }},
			{"wrong platform", func(p map[string]any) { p["platform"] = "linux/arm64" }},
			{"abbreviated revision", func(p map[string]any) { p["source_revision"] = "1111111" }},
			{"extra role", func(p map[string]any) { p["images"].(map[string]any)["other"] = map[string]any{} }},
			{"missing role", func(p map[string]any) { delete(p["images"].(map[string]any), "openclaw-codex") }},
			{"unknown executable member", func(p map[string]any) { p["command"] = "do-not-execute" }},
			{"unapproved repository", func(p map[string]any) {
				p["images"].(map[string]any)["openclaw-codex"].(map[string]any)["repository"] = "other.example/image"
			}},
			{"moving image", func(p map[string]any) {
				p["images"].(map[string]any)["openclaw-codex"].(map[string]any)["reference"] = "docker.io/amneziavpn/agent-workload-openclaw-codex:dev"
			}},
			{"digest mismatch", func(p map[string]any) {
				p["images"].(map[string]any)["openclaw-codex"].(map[string]any)["digest"] = "sha256:" + strings.Repeat("c", 64)
			}},
		} {
			t.Run(name+"/"+test.name, func(t *testing.T) {
				fields := fixture(t, name)
				test.edit(fields)
				got, err := Parse(bytes.NewReader(encoded(t, fields)))
				if err == nil || got != (Release{}) {
					t.Fatal("invalid release accepted or partial result returned")
				}
			})
		}
	}
}

func TestParseRejectsMixedSourceTagsAndFalseValidation(t *testing.T) {
	for _, name := range []string{"manifest", "publication"} {
		p := fixture(t, name)
		key := "source_tag"
		if name == "publication" {
			key = "published_tag"
		}
		p["images"].(map[string]any)["openclaw-codex"].(map[string]any)[key] = "sha-" + strings.Repeat("2", 40)
		if _, err := Parse(bytes.NewReader(encoded(t, p))); err == nil {
			t.Fatal("mixed source revision accepted")
		}
	}
	p := fixture(t, "publication")
	p["validation"].(map[string]any)["result"] = "failed"
	if _, err := Parse(bytes.NewReader(encoded(t, p))); err == nil {
		t.Fatal("failed publication accepted")
	}
}

func TestParseRejectsDuplicateAndTrailingMetadata(t *testing.T) {
	p := string(encoded(t, fixture(t, "manifest")))
	for _, data := range []string{
		p + `{}`, strings.Replace(p, `"schema_version":2`, `"schema_version":2,"schema_version":2`, 1),
		strings.Replace(p, `"digest":`, `"digest":"sha256:duplicate","digest":`, 1),
		strings.Replace(p, `"source_revision":`, `"source_revision":"\ud800","source_revision":`, 1),
	} {
		if _, err := Parse(strings.NewReader(data)); err == nil {
			t.Fatal("ambiguous metadata accepted")
		}
	}
}

func TestParseReleaseReadLimit(t *testing.T) {
	data := encoded(t, fixture(t, "manifest"))
	data = append(data, bytes.Repeat([]byte(" "), 64*1024-len(data))...)
	if _, err := Parse(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(bytes.NewReader(append(data, ' '))); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("error=%v", err)
	}
}
