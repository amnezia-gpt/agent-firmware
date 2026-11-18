package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/dockerengine"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
)

func stubStatusData(t *testing.T) {
	t.Helper()
	original := listStatusData
	listStatusData = func(context.Context, configuration.Store) ([]dockerengine.VolumeInfo, error) {
		return nil, nil
	}
	t.Cleanup(func() { listStatusData = original })
}

func TestStatusShowsRetainedDataAndHandlesUnavailableEngine(t *testing.T) {
	_, store := decommissionTestHost(t)
	device, err := configuration.ParseDevice(bytes.NewReader(fixture(t, manifestFixture)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Manufacture(device, nil); err != nil {
		t.Fatal(err)
	}
	original := listStatusData
	t.Cleanup(func() { listStatusData = original })
	for _, test := range []struct {
		title     string
		volumes   []dockerengine.VolumeInfo
		engineErr error
		want      []string
	}{
		{"kept", []dockerengine.VolumeInfo{{Name: "known-files", Issuer: "https://auth.example", CreatedAt: "2026-01-01T00:00:00Z"}, {Name: "old-files"}}, nil, []string{"https://auth.example  known-files  2026-01-01T00:00:00Z", "unknown  old-files  unknown"}},
		{"empty", nil, nil, []string{"User data: []"}},
		{"unavailable", nil, workload.ErrEngine, []string{"User data: unknown"}},
	} {
		t.Run(test.title, func(t *testing.T) {
			listStatusData = func(ctx context.Context, _ configuration.Store) ([]dockerengine.VolumeInfo, error) {
				if _, bounded := ctx.Deadline(); !bounded {
					t.Error("unbounded inventory")
				}
				return test.volumes, test.engineErr
			}
			var out, stderr bytes.Buffer
			if code := showStatus(store, &out, &stderr); code != 0 || stderr.Len() != 0 || !strings.Contains(out.String(), "Device "+manufactured) {
				t.Fatalf("device status lost: %q %q", &out, &stderr)
			}
			for _, line := range test.want {
				if !strings.Contains(out.String(), line) {
					t.Fatalf("missing %q in %q", line, &out)
				}
			}
		})
	}
}
