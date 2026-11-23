package workloadrelease

import (
	"bytes"
	"errors"
	"os"
	"testing"
)

func TestOptionalResourceBindingVersions(t *testing.T) {
	manifest, err := os.ReadFile("testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, version, binding string
		valid                  bool
	}{
		{"v5 required", "5", `{"from":"resource","scope":"example:use"}`, true},
		{"v5 alias", "5", `{"from":"resource","scope":"example:use","Optional":true}`, false},
		{"v5 duplicate", "5", `{"from":"resource","scope":"example:use","optional":true,"optional":true}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := bytes.Replace(manifest, []byte(`"schema_version": 4`), []byte(`"schema_version": `+test.version), 1)
			input = bytes.Replace(input, []byte(`{
          "from": "resource",
          "scope": "example:use"
        }`), []byte(test.binding), 1)
			_, err := Parse(bytes.NewReader(input))
			if test.valid && err != nil || !test.valid && !errors.Is(err, ErrInvalid) {
				t.Fatalf("valid=%t, parse: %v", test.valid, err)
			}
		})
	}
}
