package dockerengine

import (
	"encoding/json"
	"fmt"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestOnlyDeclaredNamedVolumesAreRecognized(t *testing.T) {
	spec := workload.Spec{Mounts: []workload.Mount{{Name: "owned-volume", Target: "/data"}}}
	good := container.MountPoint{Type: mount.TypeVolume, Name: "owned-volume", Destination: "/data"}
	if !knownMount(spec, good) {
		t.Fatal("declared mount rejected")
	}
	for _, invalid := range []container.MountPoint{
		{Type: mount.TypeBind, Name: good.Name, Destination: good.Destination, Source: "/private"},
		{Type: mount.TypeVolume, Name: "other-volume", Destination: good.Destination},
	} {
		if knownMount(spec, invalid) {
			t.Fatal("foreign mount accepted")
		}
	}
}

func TestCreateUsesManifestUserAndNamedVolumes(t *testing.T) {
	id := strings.Repeat("1", 32)
	spec := workload.Spec{Role: "service", Installation: id, Name: "test", Image: "image@sha256:test", User: "65532:65532",
		Mounts: []workload.Mount{{Name: "owned", Target: "/data", ReadOnly: true}}}

	received := false
	path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.52")
			return
		}
		if r.Method == "POST" {
			var request struct {
				User       string
				HostConfig struct{ Mounts []mount.Mount }
			}
			if json.NewDecoder(r.Body).Decode(&request) != nil || request.User != "65532:65532" || len(request.HostConfig.Mounts) != 1 || request.HostConfig.Mounts[0].Type != mount.TypeVolume || !request.HostConfig.Mounts[0].ReadOnly {
				t.Error("manifest user and volume requirements not enforced")
			}
			received = true
			fmt.Fprint(w, `{"Id":"example"}`)
			return
		}
		fmt.Fprint(w, `{"Id":"example","Name":"/test"}`)
	})
	e, err := newClient(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if _, err := e.Create(t.Context(), spec, "operation"); err != nil {
		t.Fatal(err)
	}
	if !received {
		t.Fatal("create missing")
	}
}

func TestDataVolumeAlonePreventsFreshKeyBootstrap(t *testing.T) {
	id := strings.Repeat("1", 32)
	s := workload.Stack{Installation: id, Volumes: []string{"af-" + id + "-identity", "af-" + id + "-gateway-state"}}
	path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.52")
			return
		}
		if r.Method != "GET" {
			t.Error("preparation mutated engine")
		}
		w.Header().Set("Content-Type", "application/json")
		if !strings.HasSuffix(r.URL.Path, "/volumes") {
			t.Error("unexpected inspection after finding retained data")
		}
		fmt.Fprintf(w, `{"Volumes":[{"Name":%q,"Driver":"local","Labels":{"org.amnezia.firmware.managed-by":"agent-firmware","org.amnezia.firmware.installation":%q,"org.amnezia.firmware.role":"volume"}}]}`, "af-"+id+"-old-state", id)
	})
	e, err := newClient(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	found, err := e.AnyVolumesExist(t.Context(), s)
	if err != nil || !found {
		t.Fatal("missing identity allowed reset despite credential data")
	}
}

func TestPrepareChecksSharedImagesForEveryServiceBeforeCreatingResources(t *testing.T) {
	const image = "registry.example/work/same@sha256:aaaa"
	mutations := 0
	path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.52")
			return
		}
		if r.Method != "GET" {
			mutations++
			t.Error("invalid image constraints allowed a host mutation")
		}
		fmt.Fprint(w, `{"Id":"example","Os":"linux","Architecture":"amd64","Config":{"User":"1000:1000","Volumes":{"/data":{}},"Healthcheck":{"Test":["CMD","true"]}}}`)
	})
	engine, err := newClient(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	stack := workload.Stack{Specs: []workload.Spec{
		{Role: "with-volume", Image: image, Mounts: []workload.Mount{{Name: "owned", Target: "/data"}}},
		{Role: "without-volume", Image: image},
	}}
	if err := engine.Prepare(t.Context(), stack); err != workload.ErrConflict || mutations != 0 {
		t.Fatalf("same-image validation: %v, %d mutations", err, mutations)
	}
}

func TestEffectiveImageUserRejectsRootAndUnprovenNames(t *testing.T) {
	for _, user := range []string{"", "root", "nonroot", "0", "0000:1000", "1000:0", "4294967295:101"} {
		if validEffectiveUser(user) {
			t.Fatalf("unsafe image user %q accepted", user)
		}
	}
	for _, user := range []string{"101", "101:102"} {
		if !validEffectiveUser(user) {
			t.Fatalf("numeric non-root user %q rejected", user)
		}
	}
}

func TestRetiredListRechecksOwnershipBeforeReturningAContainer(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		label := "owned"
		if foreign {
			label = "foreign"
		}
		t.Run(label, func(t *testing.T) {
			id := strings.Repeat("a", 32)
			containerID := strings.Repeat("b", 64)
			name := "af-" + id + "-retired"
			stack := workload.Stack{Installation: id, Network: "af-" + id + "-net"}
			path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/_ping" {
					w.Header().Set("API-Version", "1.52")
					return
				}
				if r.Method != "GET" {
					t.Error("retirement discovery mutated Docker")
				}
				if strings.HasSuffix(r.URL.Path, "/containers/json") {
					if r.URL.Query().Get("all") != "1" || !strings.Contains(r.URL.Query().Get("filters"), id) {
						t.Error("retirement listing was not scoped to the installation")
					}
					fmt.Fprintf(w, `[{"Id":%q,"Names":[%q],"Labels":{"org.amnezia.firmware.managed-by":"agent-firmware","org.amnezia.firmware.installation":%q,"org.amnezia.firmware.role":"retired"}}]`, containerID, "/"+name, id)
					return
				}
				if foreign {
					fmt.Fprintf(w, `{"Id":%q,"Name":%q,"Config":{"Labels":{"org.amnezia.firmware.installation":%q,"org.amnezia.firmware.role":"retired"}}}`, containerID, "/"+name, id)
					return
				}
				fmt.Fprintf(w, `{"Id":%q,"Name":%q,"Config":{"Labels":{"org.amnezia.firmware.managed-by":"agent-firmware","org.amnezia.firmware.installation":%q,"org.amnezia.firmware.role":"retired"}}}`, containerID, "/"+name, id)
			})
			engine, err := newClient(path, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close()
			retired, err := engine.Retired(t.Context(), stack)
			if foreign {
				if err != workload.ErrConflict || len(retired) != 0 {
					t.Fatalf("foreign container selected: %v", err)
				}
			} else if err != nil || len(retired) != 1 || retired[0].ID != containerID {
				t.Fatalf("owned container missing: %v", err)
			}
		})
	}
}
