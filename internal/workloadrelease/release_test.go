package workloadrelease

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestAcceptedManifestVectors(t *testing.T) {
	for _, set := range []struct{ name, path string }{
		{"v4", "../../.agents/plans/SELF-24/workload-release-v4.vectors.json"},
		{"v5", "../../.agents/plans/SELF-37/workload-release-v5.vectors.json"},
	} {
		t.Run(set.name, func(t *testing.T) { testManifestVectors(t, set.path) })
	}
}

func testManifestVectors(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Valid  json.RawMessage
		Reject []struct {
			Reason string
			Set    map[string]any
			Remove string
		}
	}
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(bytes.NewReader(vectors.Valid)); err != nil {
		t.Fatalf("valid vector: %v", err)
	}
	if len(vectors.Reject) == 0 {
		t.Fatal("missing rejection vectors")
	}
	for _, probe := range vectors.Reject {
		t.Run(probe.Reason, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(vectors.Valid, &value); err != nil {
				t.Fatal(err)
			}
			for pointer, replacement := range probe.Set {
				patch(t, value, pointer, replacement, false)
			}
			if probe.Remove != "" {
				patch(t, value, probe.Remove, nil, true)
			}
			input, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Parse(bytes.NewReader(input)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid vector accepted: %v", err)
			}
		})
	}
}

func patch(t *testing.T, object any, pointer string, replacement any, remove bool) {
	t.Helper()
	head, tail, nested := strings.Cut(strings.TrimPrefix(pointer, "/"), "/")
	head = strings.NewReplacer("~1", "/", "~0", "~").Replace(head)
	switch node := object.(type) {
	case map[string]any:
		switch {
		case nested:
			patch(t, node[head], tail, replacement, remove)
		case remove:
			delete(node, head)
		default:
			node[head] = replacement
		}
	case []any:
		i, err := strconv.Atoi(head)
		if err != nil || i < 0 || i >= len(node) || remove && !nested {
			t.Fatalf("invalid vector array edit %q", pointer)
		}
		if nested {
			patch(t, node[i], tail, replacement, remove)
		} else {
			node[i] = replacement
		}
	default:
		t.Fatalf("invalid vector edit %q", pointer)
	}
}

func TestManifestBoundsAndStrictNestedFields(t *testing.T) {
	valid, err := os.ReadFile("testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range [][]byte{
		bytes.Replace(valid, []byte(`"health_start_seconds": 5`), []byte(`"health_start_seconds": 5, "health_start_seconds": 5`), 1),
		bytes.Replace(valid, []byte(`"value":`), []byte(`"VALUE":`), 1),
		bytes.Replace(valid, []byte(`"read_only": true`), []byte(`"read_only": null`), 1),
		append(bytes.Clone(valid), []byte("{}")...),
	} {
		if _, err := Parse(bytes.NewReader(input)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected strict rejection: %v", err)
		}
	}
	if _, err := Parse(strings.NewReader(strings.Repeat(" ", MaxBytes+1))); err != ErrTooLarge {
		t.Fatalf("oversized manifest: %v", err)
	}
}
