package dockerengine

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	"github.com/amnezia-gpt/agent-firmware/internal/workloadrelease"
	"github.com/moby/moby/api/types/volume"
)

func fixtureVolume(issuer, name string, retention workloadrelease.Retention) volume.Volume {
	id := workload.EnrollmentID(issuer)
	tags := labels(id, "volume")
	if retention != "" {
		tags[volumeRetentionLabel], tags[volumeIssuerLabel] = string(retention), issuer
	}
	return volume.Volume{Name: "af-" + id + "-" + name, Driver: "local", Labels: tags}
}

func TestHeldVolumeVectors(t *testing.T) {
	raw, err := os.ReadFile("../../.agents/plans/SELF-24/workload-release-v4.vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	type scenario struct {
		Reason    string
		Operation string
		Held      map[string]workloadrelease.Retention
		Volumes   map[string]workloadrelease.VolumeDeclaration
	}
	var vectors struct {
		Valid workloadrelease.Release
		Held  struct {
			Accept []scenario
			Refuse []scenario
		} `json:"held_volumes"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors.Held.Accept) == 0 || len(vectors.Held.Refuse) == 0 {
		t.Fatal("missing held-volume vectors")
	}
	const issuer = "https://auth.example"
	for _, accept := range []bool{true, false} {
		cases := vectors.Held.Refuse
		if accept {
			cases = vectors.Held.Accept
		}
		for _, probe := range cases {
			t.Run(probe.Reason, func(t *testing.T) {
				release := vectors.Valid
				if probe.Volumes != nil {
					release.Volumes = probe.Volumes
				}
				stack := workload.VolumeStack(release, issuer)
				volumes := []volume.Volume{fixtureVolume("https://other.example", "different-domain-data", workloadrelease.UserData)}
				wantAttached := false
				for name, class := range probe.Held {
					volumes = append(volumes, fixtureVolume(issuer, name, class))
					wantAttached = wantAttached || class == workloadrelease.UserData
				}
				socket := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/_ping" {
						w.Header().Set("API-Version", "1.52")
						return
					}
					if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/volumes") {
						t.Errorf("unexpected engine mutation: %s %s", r.Method, r.URL.Path)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"Volumes": volumes})
				})
				engine, err := newClient(socket, time.Second)
				if err != nil {
					t.Fatal(err)
				}
				defer engine.Close()
				fresh := len(probe.Held) <= 1 && probe.Operation != "update"
				attached, err := engine.CheckEnrollmentVolumes(t.Context(), stack, fresh)
				if (err == nil) != accept || accept && attached != wantAttached {
					t.Fatalf("attached=%v err=%v", attached, err)
				}
			})
		}
	}
}

func TestVolumeCreationCarriesIssuerAndRetention(t *testing.T) {
	const issuer = "https://auth.example"
	v := fixtureVolume(issuer, "files", workloadrelease.UserData)
	socket := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.52")
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/volumes/create") {
			var actual volume.Volume
			if json.NewDecoder(r.Body).Decode(&actual) != nil || actual.Name != v.Name ||
				actual.Labels[volumeIssuerLabel] != issuer || actual.Labels[volumeRetentionLabel] != "user-data" {
				t.Error("missing volume labels")
			}
			_ = json.NewEncoder(w).Encode(v)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	engine, err := newClient(socket, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	stack := workload.VolumeStack(workloadrelease.Release{
		Volumes: map[string]workloadrelease.VolumeDeclaration{"files": {Retention: workloadrelease.UserData}},
	}, issuer)
	if err := engine.volumes(t.Context(), stack, true); err != nil {
		t.Fatal(err)
	}
}

func TestManufactureAcceptsOnlyRetainedUserVolumes(t *testing.T) {
	for _, class := range []workloadrelease.Retention{workloadrelease.UserData, workloadrelease.Enrollment, ""} {
		t.Run(string(class), func(t *testing.T) {
			v := fixtureVolume("https://auth.example", "workspace", class)
			socket := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/_ping" {
					w.Header().Set("API-Version", "1.52")
					return
				}
				if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/volumes") {
					t.Errorf("unexpected engine operation: %s %s", r.Method, r.URL.Path)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"Volumes": []volume.Volume{v}})
			})
			engine, err := newClient(socket, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close()
			err = engine.CheckManufactureVolumes(t.Context())
			if class == workloadrelease.UserData && err != nil || class != workloadrelease.UserData && err != configuration.ErrRetainedVolumes {
				t.Fatalf("manufacture preflight returned %v", err)
			}
		})
	}
}
