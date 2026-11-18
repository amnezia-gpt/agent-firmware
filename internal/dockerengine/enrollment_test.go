package dockerengine

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	"github.com/amnezia-gpt/agent-firmware/internal/workloadrelease"
	"github.com/moby/moby/api/types/container"
)

func statusFrame(stream byte, payload string) []byte {
	header := []byte{stream, 0, 0, 0}
	header = binary.BigEndian.AppendUint32(header, uint32(len(payload)))
	return append(header, payload...)
}

func TestReadEnrollmentStatus(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
		want string
	}{
		{"split streams", append(append(statusFrame(1, `{"state":`), statusFrame(2, "private detail")...), statusFrame(1, `"failed","reason":"denied"}`)...), "failed"},
		{"enrolled", statusFrame(1, `{"state":"enrolled"}`), "enrolled"},
		{"pending", statusFrame(1, `{"state":"pending"}`), "pending"},
		{"unexpected field", statusFrame(1, `{"state":"failed","bad":"secret"}`), ""},
		{"repeated field", statusFrame(1, `{"state":"failed","state":"enrolled"}`), ""},
		{"trailing data", statusFrame(1, `{"state":"enrolled"}{}`), ""},
		{"bad reason", statusFrame(1, `{"state":"failed","reason":"Not a code"}`), ""},
		{"null reason", statusFrame(1, `{"state":"failed","reason":null}`), ""},
		{"oversized", statusFrame(1, strings.Repeat(" ", 4097)), ""},
		{"oversized advertised frame", []byte{1, 0, 0, 0, 255, 255, 255, 255}, ""},
		{"system error frame", append(statusFrame(3, "secret"), statusFrame(1, `{"state":"enrolled"}`)...), ""},
		{"truncated", statusFrame(1, `{"state":"enrolled"}`)[:16], ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := readControlOutcome(bytes.NewReader(tc.body), EnrollmentPending, EnrollmentFailed, EnrollmentComplete)
			if string(result) != tc.want || (tc.want == "") != (err != nil) {
				t.Fatalf("outcome=%q err=%v", result, err)
			}
		})
	}
}

func TestEnrollmentStatusExecUsesOneBoundedUnprivilegedCommand(t *testing.T) {
	id, execID := strings.Repeat("a", 64), strings.Repeat("b", 64)
	command := []string{"control", "enrollment", "status"}
	imageID := "sha256:" + strings.Repeat("c", 64)
	spec := workload.Spec{Role: "gateway", Installation: strings.Repeat("0", 32), Name: "gateway", Network: "test-net",
		Image: "registry.example/gateway@sha256:" + strings.Repeat("d", 64), User: "65532:65532", StopSeconds: 10}
	stack := workload.Stack{Specs: []workload.Spec{spec}, Client: workloadrelease.APIClient{Service: spec.Role, EnrollmentStatus: command}}
	configured := labels(spec.Installation, spec.Role)
	configured[workload.LabelPrefix+"spec"] = spec.Hash()
	path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/_ping":
			w.Header().Set("API-Version", "1.52")
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/containers/"+spec.Name+"/json"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Id": id, "Name": "/" + spec.Name, "Image": imageID,
				"State": map[string]bool{"Running": true},
				"Config": map[string]any{"Image": spec.Image, "User": spec.User, "Labels": configured, "StopTimeout": spec.StopSeconds,
					"Healthcheck": map[string]any{"Test": []string{"CMD", "status"}, "Interval": 10 * time.Second, "Timeout": 5 * time.Second, "Retries": 3}},
				"HostConfig": map[string]any{"CapDrop": []string{"ALL"}, "SecurityOpt": []string{"no-new-privileges"}, "NetworkMode": spec.Network,
					"RestartPolicy": map[string]string{"Name": "unless-stopped"}, "Tmpfs": map[string]string{"/tmp": "rw,noexec,nosuid,nodev"}},
				"NetworkSettings": map[string]any{"Networks": map[string]any{spec.Network: map[string]any{}}},
			})
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/images/"+spec.Image+"/json"):
			fmt.Fprintf(w, `{"Id":%q,"Config":{"User":%q,"Healthcheck":{"Test":["CMD","status"]}}}`, imageID, spec.User)
		case strings.HasSuffix(r.URL.Path, "/containers/"+id+"/exec"):
			var input container.ExecCreateRequest
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.User != "65532:65532" ||
				input.Privileged || input.Tty || input.AttachStdin || !input.AttachStdout || !input.AttachStderr ||
				len(input.Env) != 0 || !slices.Equal(input.Cmd, command) {
				t.Errorf("invalid exec request: %+v, %v", input, err)
				w.WriteHeader(400)
				return
			}
			fmt.Fprintf(w, `{"Id":%q}`, execID)
		case strings.HasSuffix(r.URL.Path, "/exec/"+execID+"/start"):
			if _, err := io.Copy(io.Discard, r.Body); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			conn, buf, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			buf.WriteString("HTTP/1.1 101 UPGRADED\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
			buf.Write(statusFrame(2, "redacted stderr"))
			buf.Write(statusFrame(1, `{"state":"unenrolled","reason":"sign_in_required"}`))
			buf.Flush()
		case strings.HasSuffix(r.URL.Path, "/exec/"+execID+"/json"):
			fmt.Fprintf(w, `{"ID":%q,"ContainerID":%q,"Running":false,"ExitCode":0}`, execID, id)
		default:
			t.Error("unexpected Docker endpoint:", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	engine, err := newClient(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	target, err := engine.ClientTarget(t.Context(), stack)
	if err != nil {
		t.Fatalf("could not verify installed client without a cached image: %v", err)
	}
	status := &StatusCommand{}
	got, err := status.Run(t.Context(), engine, target, command)
	if err != nil || got != "unenrolled" || status.active != "" {
		t.Fatalf("outcome=%q err=%v outstanding exec=%q", got, err, status.active)
	}
}

func TestSchema4ControlResultVectors(t *testing.T) {
	raw, err := os.ReadFile("../../.agents/plans/SELF-24/workload-release-v4.vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Control struct {
			Accept []json.RawMessage
			Refuse []json.RawMessage `json:"treat_as_unavailable"`
		} `json:"control_results"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors.Control.Accept) == 0 || len(vectors.Control.Refuse) == 0 {
		t.Fatal("missing control vectors")
	}
	for _, accept := range []bool{true, false} {
		cases := vectors.Control.Refuse
		if accept {
			cases = vectors.Control.Accept
		}
		for _, value := range cases {
			payload := string(value)
			var malformed string
			if json.Unmarshal(value, &malformed) == nil {
				payload = malformed
			}
			_, err := readControlOutcome(bytes.NewReader(statusFrame(1, payload)), EnrollmentComplete, EnrollmentPending, EnrollmentFailed, "unenrolled", "deregistered")
			if (err == nil) != accept {
				t.Fatalf("control result %s: %v", value, err)
			}
		}
	}
}
