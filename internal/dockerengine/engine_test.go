package dockerengine

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/workload"
)

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
	if err := e.volumes(context.Background(), workload.Stack{Installation: "test", Volumes: []string{"test-volume"}}, true); !errors.Is(err, workload.ErrConflict) {
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
