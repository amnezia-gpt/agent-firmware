package workload

import (
	"bytes"
	"os"
	"reflect"
	"testing"
)

func TestDeviceAPIProfilePreservesInstallationAndVolumes(t *testing.T) {
	legacy := testStack(t)
	if legacy.Installation != "718b4bcec873094b25c27059bbf48cae" {
		t.Fatal("legacy fixture namespace changed")
	}
	p, _ := os.ReadFile("../backendprofile/testdata/profile-v3.json")
	r, _ := os.ReadFile("../workloadrelease/testdata/manifest.json")
	r = bytes.ReplaceAll(r, []byte("workload-contract:device:v1"), []byte("workload-contract:device:v2"))
	next, err := FromReaders(bytes.NewReader(p), bytes.NewReader(r))
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Installation != next.Installation || legacy.Network != next.Network || !reflect.DeepEqual(legacy.Volumes, next.Volumes) {
		t.Fatal("device upgrade changed persistent namespace")
	}
	for i := range legacy.Specs {
		if legacy.Specs[i].Name != next.Specs[i].Name {
			t.Fatal("role renamed")
		}
	}
	if next.Specs[0].Env["FIRMWARE_DEVICE_API_MODE"] != "required" || len(next.Specs[0].Bindings) != 2 {
		t.Fatal("Gateway API contract missing")
	}
	if len(legacy.Specs[0].Bindings) != 0 || len(next.Specs[1].Bindings) != 0 || len(next.Specs[2].Bindings) != 0 {
		t.Fatal("API exposed outside v2 Gateway")
	}
	for _, m := range next.Specs[0].Bindings {
		if !m.ReadOnly {
			t.Fatal("API mount writable")
		}
	}
}
