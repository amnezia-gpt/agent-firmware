package dockerengine

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/workload"
)

func TestOwnedBridgeAcceptsOnlyDockerIPFlagMetadata(t *testing.T) {
	installation := strings.Repeat("a", 32)
	name := "af-" + installation + "-net"
	networkID := strings.Repeat("b", 64)
	for _, test := range []struct {
		name    string
		options map[string]string
		want    error
	}{
		{name: "older bridge response"},
		{name: "Docker 29 bridge response", options: map[string]string{
			"com.docker.network.enable_ipv4": "true", "com.docker.network.enable_ipv6": "false",
		}},
		{name: "inconsistent IP flag", options: map[string]string{"com.docker.network.enable_ipv4": "false"}, want: workload.ErrConflict},
		{name: "custom bridge option", options: map[string]string{"com.docker.network.bridge.enable_icc": "false"}, want: workload.ErrConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			network := map[string]any{
				"Name": name, "Id": networkID, "Driver": "bridge", "EnableIPv4": true, "EnableIPv6": false,
				"Labels": labels(installation, "network"), "Options": test.options,
			}
			path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/_ping" {
					w.Header().Set("API-Version", "1.53")
					return
				}
				w.Header().Set("Content-Type", "application/json")
				var body any
				switch {
				case strings.HasSuffix(r.URL.Path, "/containers/json"):
					body = []any{}
				case strings.HasSuffix(r.URL.Path, "/networks"):
					body = []any{network}
				case strings.HasSuffix(r.URL.Path, "/networks/"+name), strings.HasSuffix(r.URL.Path, "/networks/"+networkID):
					body = network
				case strings.HasSuffix(r.URL.Path, "/volumes"):
					body = map[string]any{"Volumes": []any{}}
				default:
					t.Error("unexpected engine call", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				_ = json.NewEncoder(w).Encode(body)
			})
			engine, err := newClient(path, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close()
			if err := engine.Preflight(t.Context(), workload.Stack{Installation: installation, Network: name}); err != test.want {
				t.Errorf("workload preflight = %v, want %v", err, test.want)
			}
			if err := engine.CheckRemoval(t.Context(), installation); err != test.want {
				t.Errorf("removal preflight = %v, want %v", err, test.want)
			}
		})
	}
}

func TestDecommissionRejectsConflictingResourceBeforeDeletion(t *testing.T) {
	id := strings.Repeat("a", 32)
	prefix := "af-" + id + "-"
	path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.52")
			return
		}
		if r.Method == http.MethodDelete {
			t.Error("preflight sent a delete request")
		}
		w.Header().Set("Content-Type", "application/json")
		var body any
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			body = []any{}
		case strings.HasSuffix(r.URL.Path, "/volumes"):
			body = map[string]any{"Volumes": []any{map[string]any{"Name": prefix + "foreign", "Driver": "local"}}}
		case strings.HasSuffix(r.URL.Path, "/networks"):
			body = []any{}
		default:
			t.Error("unexpected engine call", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	})
	engine, err := newClient(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if err := engine.CheckRemoval(t.Context(), id); err != workload.ErrConflict {
		t.Fatalf("unexpected preflight result: %v", err)
	}
}

func TestDecommissionRemovesUnusedPinnedImagesWithoutForce(t *testing.T) {
	used := "example.test/workloads/used@sha256:" + strings.Repeat("1", 64)
	unused := "example.test/workloads/unused@sha256:" + strings.Repeat("2", 64)
	usedID, unusedID := "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64)
	var mu sync.Mutex
	var deleted []string
	path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.52")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var body any
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			if r.URL.Query().Get("all") != "1" {
				t.Error("image check did not include stopped containers")
			}
			body = []any{map[string]any{"ImageID": usedID, "Image": "example.test/workloads/used:stable", "State": "exited"}}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, used+"/json"):
			body = map[string]any{"Id": usedID}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, unused+"/json"):
			body = map[string]any{"Id": unusedID}
		case r.Method == http.MethodDelete:
			mu.Lock()
			deleted = append(deleted, r.URL.Path)
			mu.Unlock()
			if r.URL.Query().Get("force") != "" || r.URL.Query().Get("noprune") != "1" {
				t.Error("image removal changed other references or pruned parent images")
			}
			w.WriteHeader(http.StatusConflict)
			body = map[string]string{"message": "image in use"}
		default:
			t.Error("unexpected engine call", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	})
	engine, err := newClient(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if err := engine.RemoveReleaseImages(t.Context(), []string{used, unused}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(deleted) != 1 || !strings.HasSuffix(deleted[0], "/images/"+unused) {
		t.Fatalf("unexpected image removal: %v", deleted)
	}
}
