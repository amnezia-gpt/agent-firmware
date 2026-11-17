package dockerengine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/deviceapi"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
)

func TestExecLinkReadsOnlyBoundedStdout(t *testing.T) {
	wire := append(statusFrame(1, "one"), statusFrame(2, "private stderr")...)
	wire = append(wire, statusFrame(1, "two")...)
	conn := &execConn{reader: bytes.NewReader(wire)}
	got, err := io.ReadAll(conn)
	if err != nil || string(got) != "onetwo" {
		t.Fatalf("stdout=%q, err=%v", got, err)
	}
	for _, rejected := range [][]byte{
		{1, 0, 0, 0, 255, 255, 255, 255},
		{1, 0, 0, 0, 0, 0, 0, 2, 'x'},
		{3, 0, 0, 0, 0, 0, 0, 0},
	} {
		conn := &execConn{reader: bytes.NewReader(rejected)}
		if _, err := io.ReadAll(conn); err == nil {
			t.Fatal("invalid Docker frame accepted")
		}
	}
}

func TestDialReportsBoundedOpenFailureReasons(t *testing.T) {
	id, execID := strings.Repeat("a", 64), strings.Repeat("b", 64)
	target := ClientTarget{ID: id, Name: "test-service", User: "65532:65532", Installation: "installation", Role: "service", Hash: "spec"}
	for _, tc := range []struct {
		name string
		want deviceapi.LinkReason
	}{
		{"running exec", deviceapi.LinkPreviousExecRunning},
		{"absent target", deviceapi.LinkTargetAbsent},
		{"not running", deviceapi.LinkTargetNotRunning},
		{"unverified target", deviceapi.LinkTargetUnverified},
		{"create failed", deviceapi.LinkExecCreateFailed},
		{"attach failed", deviceapi.LinkExecAttachFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == "/_ping":
					w.Header().Set("API-Version", "1.52")
				case strings.HasSuffix(r.URL.Path, "/exec/"+execID+"/json"):
					fmt.Fprint(w, `{"Running":true}`)
				case strings.HasSuffix(r.URL.Path, "/containers/"+id+"/json"):
					if tc.name == "absent target" {
						w.WriteHeader(404)
						fmt.Fprint(w, `{"message":"private engine response"}`)
						return
					}
					configured := labels(target.Installation, target.Role)
					configured[workload.LabelPrefix+"spec"] = target.Hash
					if tc.name == "unverified target" {
						delete(configured, workload.LabelPrefix+"spec")
					}
					json.NewEncoder(w).Encode(map[string]any{
						"Id": id, "Name": "/" + target.Name,
						"Config": map[string]any{"User": target.User, "Labels": configured},
						"State":  map[string]bool{"Running": tc.name != "not running"},
					})
				case strings.HasSuffix(r.URL.Path, "/containers/"+id+"/exec") && tc.name == "attach failed":
					fmt.Fprintf(w, `{"Id":%q}`, execID)
				default:
					w.WriteHeader(500)
					fmt.Fprint(w, `{"message":"private engine response"}`)
				}
			})
			engine, err := newClient(path, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close()
			var dial DialCommand
			if tc.name == "running exec" {
				dial.active = execID
			}
			conn, err := dial.Attach(t.Context(), engine, target, []string{"apictl", "dial"})
			if conn != nil || err != tc.want {
				t.Fatalf("want %q, got conn=%v err=%v", tc.want, conn != nil, err)
			}
		})
	}
	dir, err := os.MkdirTemp("/tmp", "af-link-offline-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	engine, err := newClient(filepath.Join(dir, "missing.sock"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if _, err := (&DialCommand{}).Attach(t.Context(), engine, target, []string{"apictl", "dial"}); err != deviceapi.LinkEngineUnreachable {
		t.Fatalf("want unreachable engine, got %v", err)
	}
}

func TestExecExitDistinguishesBoundedOutcomes(t *testing.T) {
	execID, containerID := strings.Repeat("a", 64), strings.Repeat("b", 64)
	for _, tc := range []struct {
		name    string
		running bool
		exit    int
		missing bool
		want    deviceapi.LinkExecOutcome
	}{
		{"clean", false, 0, false, deviceapi.LinkExecExitedOK},
		{"error", false, 2, false, deviceapi.LinkExecExitedError},
		{"survived attachment", true, 0, false, deviceapi.LinkExecStillRunning},
		{"removed exec", false, 0, true, deviceapi.LinkExecUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/_ping" {
					w.Header().Set("API-Version", "1.52")
					return
				}
				if tc.missing {
					w.WriteHeader(http.StatusNotFound)
					fmt.Fprint(w, `{"message":"private engine response"}`)
					return
				}
				fmt.Fprintf(w, `{"ID":%q,"ContainerID":%q,"Running":%t,"ExitCode":%d}`, execID, containerID, tc.running, tc.exit)
			})
			engine, err := newClient(path, 250*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close()
			conn := execConn{engine: engine, execID: execID, containerID: containerID}
			outcome, reachable := conn.ExecExit(t.Context())
			if !reachable || outcome != tc.want {
				t.Fatalf("want %q, got %q with reachability %v", tc.want, outcome, reachable)
			}
		})
	}
}
