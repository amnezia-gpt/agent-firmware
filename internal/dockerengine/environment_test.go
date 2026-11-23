package dockerengine

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestCreateMasksMissingOptionalResourceImageDefault(t *testing.T) {
	imageID, containerID := "sha256:"+strings.Repeat("a", 64), strings.Repeat("b", 64)
	test := []string{"CMD", "check"}
	spec := workload.Spec{
		Name: "owned-agent", Installation: "owned", Role: "agent", Network: "owned-net",
		Image: "registry.example/agent@" + imageID, User: "65532:65532",
		HealthStart: 5, StopSeconds: 15, Env: map[string]string{"EMPTY": ""},
		UnsetEnv: []string{"OPTIONAL_URL"},
	}
	var leak atomic.Bool
	created := make(chan container.InspectResponse, 1)
	path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.52")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
			var request struct {
				container.Config
				HostConfig *container.HostConfig
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("create request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if want := []string{"EMPTY=", "OPTIONAL_URL"}; !slices.Equal(request.Env, want) {
				t.Errorf("create Env = %v, want %v", request.Env, want)
			}
			request.Healthcheck.Test = test
			request.Env = append(request.Env, "KEPT=image")
			created <- container.InspectResponse{
				ID: containerID, Name: "/" + spec.Name, Image: imageID,
				HostConfig: request.HostConfig, Config: &request.Config,
				NetworkSettings: &container.NetworkSettings{Networks: map[string]*network.EndpointSettings{spec.Network: {}}},
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"Id": containerID})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/"+spec.Name+"/json"):
			var actual container.InspectResponse
			select {
			case actual = <-created:
			default:
				w.WriteHeader(http.StatusNotFound)
				return
			}
			created <- actual
			if leak.Load() {
				config := *actual.Config
				config.Env = []string{"OPTIONAL_URL=from-image", "KEPT=image", "EMPTY="}
				actual.Config = &config
			}
			json.NewEncoder(w).Encode(actual)
		default:
			t.Errorf("unexpected engine request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	engine, err := newClient(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	engine.images[spec.Image] = client.ImageInspectResult{
		ID: imageID,
		Config: &dockerspec.DockerOCIImageConfig{
			ImageConfig: ocispec.ImageConfig{User: spec.User, Env: []string{"OPTIONAL_URL=from-image", "KEPT=image"}},
			DockerOCIImageConfigExt: dockerspec.DockerOCIImageConfigExt{
				Healthcheck: &dockerspec.HealthcheckConfig{Test: test},
			},
		},
	}
	if state, err := engine.Create(t.Context(), spec, "operation"); err != nil || !state.Matches {
		t.Fatalf("create with missing optional resource: matches=%t, %v", state.Matches, err)
	}
	leak.Store(true)
	if state, err := engine.Inspect(t.Context(), spec.Name, spec); err != nil || state.Matches {
		t.Fatalf("inspect with inherited optional variable: matches=%t, %v", state.Matches, err)
	}
}
