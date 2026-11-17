package dockerengine

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

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
	path := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/_ping":
			w.Header().Set("API-Version", "1.52")
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
			buf.Write(statusFrame(1, `{"state":"enrolled"}`))
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
	status := &StatusCommand{}
	got, err := status.run(t.Context(), engine, id, "65532:65532", command)
	if err != nil || got != EnrollmentComplete || status.active != "" {
		t.Fatalf("outcome=%q err=%v outstanding exec=%q", got, err, status.active)
	}
}
