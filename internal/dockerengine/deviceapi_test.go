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

func TestOnlyExactGatewayReadOnlyAPIBindsAreRecognized(t *testing.T) {
	id := strings.Repeat("1", 32)
	s := workload.Spec{Role: "gateway", Installation: id, Bindings: []workload.Binding{{Source: workload.APIRuntimeDir(id) + "/api", Target: "/run/firmware-device-api", ReadOnly: true}, {Source: workload.APIRuntimeDir(id) + "/auth", Target: "/run/firmware-device-api-auth", ReadOnly: true}}}
	if !validBindingProfile(s) {
		t.Fatal("valid Gateway profile rejected")
	}
	good := container.MountPoint{Type: mount.TypeBind, Source: s.Bindings[0].Source, Destination: s.Bindings[0].Target, RW: false}
	if !knownMount(s, good) {
		t.Fatal("API bind rejected")
	}
	for _, c := range []container.MountPoint{{Type: mount.TypeBind, Source: "/tmp/foreign", Destination: good.Destination}, {Type: mount.TypeBind, Source: good.Source, Destination: good.Destination, RW: true}, {Type: mount.TypeBind, Source: good.Source, Destination: "/different"}} {
		if knownMount(s, c) {
			t.Fatal("foreign/writable bind accepted")
		}
	}
	s.Role = "agent"
	if validBindingProfile(s) {
		t.Fatal("Codex received API")
	}
}

func TestAPIGatewayUserIsNumericForNamedImageUsers(t *testing.T) {
	id := strings.Repeat("1", 32)
	spec := workload.Spec{Role: "gateway", Installation: id, Name: "test", Image: "image@sha256:test", Bindings: []workload.Binding{{Source: workload.APIRuntimeDir(id) + "/api", Target: "/run/firmware-device-api", ReadOnly: true}, {Source: workload.APIRuntimeDir(id) + "/auth", Target: "/run/firmware-device-api-auth", ReadOnly: true}}}
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
			if json.NewDecoder(r.Body).Decode(&request) != nil || request.User != "65532:65532" || len(request.HostConfig.Mounts) != 2 {
				t.Error("API Gateway UID/binds not enforced")
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
		if strings.HasSuffix(r.URL.Path, "-identity") {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"message":"not found"}`)
			return
		}
		fmt.Fprintf(w, `{"Name":%q,"Driver":"local","Labels":{"org.amnezia.firmware.managed-by":"agent-firmware","org.amnezia.firmware.installation":%q,"org.amnezia.firmware.role":"volume"}}`, s.Volumes[1], id)
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
