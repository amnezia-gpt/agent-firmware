package dockerengine

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRemoveEnrollmentRechecksOwnershipAndLeavesForeignResources(t *testing.T) {
	id := strings.Repeat("a", 32)
	prefix := "af-" + id + "-"
	own := strings.Repeat("1", 64)
	foreign := strings.Repeat("2", 64)
	netID := strings.Repeat("3", 64)
	var mu sync.Mutex
	var deleted []string
	path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.52")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var value any
		switch {
		case r.Method == http.MethodDelete:
			mu.Lock()
			deleted = append(deleted, r.URL.Path)
			mu.Unlock()
			if r.URL.Query().Get("v") != "" {
				t.Error("container deletion included other mounts")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			// The second container has changed ownership since the list.
			value = []any{
				map[string]any{"Id": own, "Labels": labels(id, "front")},
				map[string]any{"Id": foreign, "Labels": labels(id, "front")},
			}
		case strings.HasSuffix(r.URL.Path, own+"/json"):
			value = map[string]any{"Id": own, "Name": "/" + prefix + "front", "Config": map[string]any{"Labels": labels(id, "front")}}
		case strings.HasSuffix(r.URL.Path, foreign+"/json"):
			value = map[string]any{"Id": foreign, "Name": "/" + prefix + "similar", "Config": map[string]any{}}
		case strings.Contains(r.URL.Path, "/networks/"):
			value = map[string]any{"Id": netID, "Name": prefix + "net", "Driver": "bridge", "Labels": labels(id, "network")}
		case strings.HasSuffix(r.URL.Path, "/volumes"):
			value = map[string]any{"Volumes": []any{
				map[string]any{"Name": prefix + "data", "Labels": labels(id, "volume")},
				map[string]any{"Name": prefix + "foreign"},
			}}
		case strings.HasSuffix(r.URL.Path, prefix+"data"):
			value = map[string]any{"Name": prefix + "data", "Driver": "local", "Labels": labels(id, "volume")}
		default:
			t.Error("unexpected Docker call", r.Method, r.URL.Path)
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(value)
	})
	engine, err := newClient(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if err := engine.RemoveEnrollment(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"/containers/" + own, "/networks/" + netID, "/volumes/" + prefix + "data"}
	if len(deleted) != len(want) {
		t.Fatalf("unexpected deletion count: %v", deleted)
	}
	for i, path := range deleted {
		if !strings.HasSuffix(path, want[i]) {
			t.Fatalf("unexpected deletion or order: %v", deleted)
		}
	}
}
