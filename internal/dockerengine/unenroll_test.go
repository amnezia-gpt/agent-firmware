package dockerengine

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	"github.com/amnezia-gpt/agent-firmware/internal/workloadrelease"
	"github.com/moby/moby/api/types/volume"
)

func TestVolumeRemovalRespectsRetentionAndDomain(t *testing.T) {
	for _, test := range []struct {
		name    string
		purge   bool
		all     bool
		removed int
	}{
		{"default keeps user and unclassified volumes", false, false, 1},
		{"purge only this domain", true, false, 3},
		{"purge every domain", true, true, 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			const issuer = "https://auth.example"
			id := workload.EnrollmentID(issuer)
			prefix := "af-" + id + "-"
			makeVolume := func(name, issuer string, class workloadrelease.Retention) volume.Volume {
				installation := workload.EnrollmentID(issuer)
				tags := labels(installation, "volume")
				if class != "" {
					tags[volumeRetentionLabel] = string(class)
					tags[volumeIssuerLabel] = issuer
				}
				return volume.Volume{Name: "af-" + installation + "-" + name, Driver: "local", Labels: tags}
			}
			volumes := map[string]volume.Volume{}
			for _, v := range []volume.Volume{
				makeVolume("auth", issuer, workloadrelease.Enrollment),
				makeVolume("files", issuer, workloadrelease.UserData),
				makeVolume("unknown", issuer, ""),
				makeVolume("other", "https://other.example", workloadrelease.UserData),
				{Name: "foreign", Driver: "local"},
			} {
				volumes[v.Name] = v
			}
			var mu sync.Mutex
			path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/_ping" {
					w.Header().Set("API-Version", "1.52")
					return
				}
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/volumes"):
					items := []volume.Volume{}
					for _, v := range volumes {
						items = append(items, v)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"Volumes": items})
				case strings.Contains(r.URL.Path, "/volumes/"):
					name := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
					if r.Method == http.MethodDelete {
						if r.URL.Query().Get("force") != "" {
							t.Error("force deletion")
						}
						delete(volumes, name)
						w.WriteHeader(http.StatusNoContent)
						return
					}
					_ = json.NewEncoder(w).Encode(volumes[name])
				case strings.HasSuffix(r.URL.Path, "/containers/json"), strings.HasSuffix(r.URL.Path, "/networks"):
					_, _ = w.Write([]byte("[]"))
				case strings.HasSuffix(r.URL.Path, "/networks/"+prefix+"net"):
					w.WriteHeader(http.StatusNotFound)
				default:
					t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			})
			engine, err := newClient(path, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close()
			if test.all {
				err = engine.RemoveOwnedVolumes(t.Context(), "", test.purge)
			} else {
				err = engine.RemoveEnrollment(t.Context(), id, test.purge)
			}
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(volumes) != 5-test.removed || test.removed < 4 && volumes["af-"+workload.EnrollmentID("https://other.example")+"-other"].Name == "" || volumes["foreign"].Name == "" {
				t.Fatal("unexpected volumes after removal")
			}
			if !test.purge && (volumes[prefix+"files"].Name == "" || volumes[prefix+"unknown"].Name == "") {
				t.Fatal("default removal deleted data")
			}
		})
	}
}
