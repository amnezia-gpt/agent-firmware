package dockerengine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	"github.com/amnezia-gpt/agent-firmware/internal/workloadrelease"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func newClient(socket string, timeout time.Duration) (*Engine, error) {
	return newEngine(socket, timeout, ocispec.Platform{OS: "linux", Architecture: "amd64"})
}

func socketServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "af-sdk-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	path := filepath.Join(directory, "engine.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	s := &http.Server{Handler: handler}
	go s.Serve(l)
	t.Cleanup(func() { s.Close(); l.Close() })
	return path
}

func TestForeignResourceIsRejectedBeforeAnyMutation(t *testing.T) {
	mutations := 0
	path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.52")
			return
		}
		if r.Method != "GET" {
			mutations++
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"Name":"test","Driver":"bridge","Labels":{"org.amnezia.firmware.managed-by":"other"}}`))
	})
	e, err := newClient(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	err = e.Preflight(context.Background(), workload.Stack{Installation: "test", Network: "test"})
	if !errors.Is(err, workload.ErrConflict) || mutations != 0 {
		t.Fatalf("error=%v mutations=%d", err, mutations)
	}
}

func TestUnixRequestHonorsCallerCancellation(t *testing.T) {
	path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.52")
			return
		}
		<-r.Context().Done()
	})
	e, err := newClient(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := e.Preflight(ctx, workload.Stack{Network: "test"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func TestOversizedMetadataIsRejectedAndRedacted(t *testing.T) {
	path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.52")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"Name":"` + strings.Repeat("sensitive-example", 150000) + `"}`))
	})
	e, err := newClient(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	err = e.Preflight(context.Background(), workload.Stack{Network: "test"})
	if err == nil || strings.Contains(err.Error(), "sensitive-example") {
		t.Fatalf("unbounded diagnostic: %v", err)
	}
}

func TestUnexpectedHostMountIsAHardConflict(t *testing.T) {
	path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.52")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/networks/") {
			w.Write([]byte(`{"Name":"test","Driver":"bridge","Labels":{"org.amnezia.firmware.managed-by":"agent-firmware","org.amnezia.firmware.installation":"test","org.amnezia.firmware.role":"network"}}`))
			return
		}
		w.Write([]byte(`{"Id":"` + strings.Repeat("1", 64) + `","Name":"/test-gateway","Config":{"Image":"docker.io/amneziavpn/agent-workload-amgpt-auth-proxy@sha256:abc","Labels":{"org.amnezia.firmware.managed-by":"agent-firmware","org.amnezia.firmware.installation":"test","org.amnezia.firmware.role":"gateway"}},"HostConfig":{"Binds":["/private-example:/data"]},"NetworkSettings":{"Networks":{"test":{}}}}`))
	})
	e, err := newClient(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	s := workload.Stack{Installation: "test", Network: "test", Specs: []workload.Spec{{Installation: "test", Role: "gateway", Name: "test-gateway", Image: "docker.io/amneziavpn/agent-workload-amgpt-auth-proxy@sha256:abc"}}}
	if err := e.Preflight(context.Background(), s); !errors.Is(err, workload.ErrConflict) {
		t.Fatalf("unexpected host bind was accepted: %v", err)
	}
}

func TestInFlightUnixReadHasFiniteDeadline(t *testing.T) {
	path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.52")
			return
		}
		<-r.Context().Done()
	})
	e, err := newClient(path, 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.Preflight(context.Background(), workload.Stack{Network: "test"}); !errors.Is(err, workload.ErrTimeout) {
		t.Fatalf("deadline classification=%v", err)
	}
}

func TestPullErrorIsConsumedClosedAndRedacted(t *testing.T) {
	path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.52")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" {
			w.Write([]byte("{\"errorDetail\":{\"message\":\"sensitive-example\"},\"error\":\"sensitive-example\"}\n"))
			return
		}
		w.WriteHeader(404)
		w.Write([]byte(`{"message":"not found"}`))
	})
	e, err := newClient(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	s := workload.Stack{Specs: []workload.Spec{{Image: "docker.io/amneziavpn/agent-workload-openclaw-codex@sha256:" + strings.Repeat("a", 64)}}}
	if err := e.Prepare(context.Background(), s); err != workload.ErrEngine {
		t.Fatalf("pull error=%v", err)
	}
}

func TestVolumeCreateResponseMustStillProveOwnership(t *testing.T) {
	path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.52")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" {
			w.Write([]byte(`{"Name":"test-volume","Driver":"local","Labels":{"org.amnezia.firmware.managed-by":"other"}}`))
			return
		}
		w.WriteHeader(404)
		w.Write([]byte(`{"message":"not found"}`))
	})
	e, err := newClient(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.volumes(context.Background(), workload.Stack{Installation: "test", Volumes: map[string]workloadrelease.Retention{"test-volume": workloadrelease.Enrollment}}, true); !errors.Is(err, workload.ErrConflict) {
		t.Fatalf("foreign create result accepted: %v", err)
	}
}

func TestPrerequisitesRejectRootlessOrWrongPlatformWithoutMutations(t *testing.T) {
	for _, response := range []string{
		`{"OSType":"linux","Architecture":"x86_64","SecurityOptions":["name=rootless"]}`,
		`{"OSType":"linux","Architecture":"aarch64"}`,
		`{"OSType":"windows","Architecture":"x86_64"}`,
	} {
		path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/_ping" {
				w.Header().Set("API-Version", "1.52")
				return
			}
			if r.Method != "GET" {
				t.Error("preflight mutated engine")
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(response))
		})
		e, err := newClient(path, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.Prerequisites(t.Context()); err != ErrPrerequisites {
			t.Fatalf("unsupported engine accepted: %v", err)
		}
		e.Close()
	}
}

func TestPrerequisitesAcceptNativeARM64Engine(t *testing.T) {
	for _, architecture := range []string{"aarch64", "arm64", "amd64"} {
		t.Run(architecture, func(t *testing.T) {
			path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/_ping" {
					w.Header().Set("API-Version", "1.52")
					return
				}
				json.NewEncoder(w).Encode(map[string]string{"OSType": "linux", "Architecture": architecture})
			})
			e, err := newEngine(path, time.Second, ocispec.Platform{OS: "linux", Architecture: "arm64"})
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			want := error(nil)
			if architecture == "amd64" {
				want = ErrPrerequisites
			}
			if err := e.Prerequisites(t.Context()); err != want {
				t.Fatalf("engine prerequisite: got %v, want %v", err, want)
			}
		})
	}
}

func TestPrepareRejectsImageForAnotherArchitecture(t *testing.T) {
	path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.52")
			return
		}
		if r.Method != "GET" || !strings.Contains(r.URL.Path, "/images/") {
			t.Errorf("unexpected request with incompatible image: %s %s", r.Method, r.URL.Path)
		}
		io.WriteString(w, `{"Id":"image","Os":"linux","Architecture":"amd64","Config":{"User":"65532:65532","Healthcheck":{"Test":["CMD","check"]}}}`)
	})
	e, err := newEngine(path, time.Second, ocispec.Platform{OS: "linux", Architecture: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	stack := workload.Stack{Specs: []workload.Spec{{Image: "example"}}}
	for range 2 {
		if err := e.Prepare(t.Context(), stack); !errors.Is(err, workload.ErrConflict) {
			t.Fatalf("incompatible image: %v", err)
		}
	}
}

func TestManufacturePullsImagesForEnrollmentAndRetry(t *testing.T) {
	manifest, err := os.Open("../workloadrelease/testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	defer manifest.Close()
	release, err := workloadrelease.Parse(manifest)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := os.Open("../backendprofile/testdata/profile-v4.json")
	if err != nil {
		t.Fatal(err)
	}
	defer profile.Close()
	if _, err := manifest.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	stack, err := workload.FromReaders(profile, manifest)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]int)
	var mu sync.Mutex
	manufacturing := true
	var pulls int
	path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/_ping":
			w.Header().Set("API-Version", "1.52")
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/images/create"):
			if got := r.URL.Query().Get("platform"); got != "linux/arm64" {
				t.Errorf("pull platform: %q", got)
			}
			pulls++
			io.WriteString(w, "{\"status\":\"done\"}\n")
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/images/"):
			seen[r.URL.Path]++
			if seen[r.URL.Path] == 1 {
				w.WriteHeader(http.StatusNotFound)
				io.WriteString(w, `{"message":"missing"}`)
			} else {
				io.WriteString(w, `{"Id":"image","Os":"linux","Architecture":"arm64","Config":{"User":"65532:65532","Healthcheck":{"Test":["CMD","check"]}}}`)
			}
		case !manufacturing && r.Method == "GET" && strings.Contains(r.URL.Path, "/networks/"):
			json.NewEncoder(w).Encode(map[string]any{"Name": stack.Network, "Driver": "bridge", "Labels": labels(stack.Installation, "network")})
		case !manufacturing && r.Method == "GET" && strings.Contains(r.URL.Path, "/volumes/"):
			name := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
			tags := labels(stack.Installation, "volume")
			tags[volumeRetentionLabel] = string(stack.Volumes[name])
			tags[volumeIssuerLabel] = stack.Issuer
			json.NewEncoder(w).Encode(map[string]any{"Name": name, "Driver": "local", "Labels": tags})
		default:
			t.Error("unexpected engine request:", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
		}
	})
	for phase := range 3 {
		e, err := newEngine(path, time.Second, ocispec.Platform{OS: "linux", Architecture: "arm64"})
		if err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		manufacturing = phase == 0
		mu.Unlock()
		if phase == 0 {
			err = e.PrepareImages(t.Context(), release)
		} else {
			err = e.Prepare(t.Context(), stack)
		}
		e.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if pulls != 2 || len(seen) != 2 {
		t.Fatalf("expected two distinct digest pulls, got pulls=%d images=%d", pulls, len(seen))
	}
}
