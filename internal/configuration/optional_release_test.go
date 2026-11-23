//go:build linux || darwin

package configuration

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSavedV4ReleaseGeneration(t *testing.T) {
	manifest, err := os.ReadFile("../workloadrelease/testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := os.ReadFile("../backendprofile/testdata/profile-v4.json")
	if err != nil {
		t.Fatal(err)
	}
	device, err := ParseDevice(bytes.NewReader(manifest))
	if err != nil {
		t.Fatal(err)
	}
	store := Store{Path: filepath.Join(t.TempDir(), "state")}
	if _, err := store.Manufacture(device, nil); err != nil {
		t.Fatal(err)
	}
	device, err = store.LoadDevice()
	if err != nil {
		t.Fatal(err)
	}
	input, err := device.WithProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	const deviceGeneration = "sha256:2f79883453606eb0dd5646aa3e20061ade63bff5dd007b8687bd528a3026c520"
	const stackGeneration = "sha256:fefded07f5f3e39143768a7282ce27a7312b5ea0d05e6d776a8a46e8f632638a"
	if device.Generation() != deviceGeneration || input.Generation() != stackGeneration {
		t.Fatalf("v4 device = %s, input = %s", device.Generation(), input.Generation())
	}
}
