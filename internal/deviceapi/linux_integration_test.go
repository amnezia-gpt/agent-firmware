//go:build linux

package deviceapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in only: these tests create their own Docker resources on
// the selected disposable Linux host. Ordinary go test never operates a host.
func linuxHostTest(t *testing.T) {
	t.Helper()
	if os.Getenv("AF_DEVICE_API_LINUX_TEST") != "1" || os.Geteuid() != 0 {
		t.Skip("selected disposable root Linux host required")
	}
}
func TestLinuxAPIIsolationAndRestart(t *testing.T) {
	linuxHostTest(t)
	image := os.Getenv("AF_DEVICE_API_TEST_IMAGE")
	if !strings.HasPrefix(image, "sha256:") || len(image) != 71 {
		t.Fatal("existing pinned local test image required")
	}
	root, err := os.MkdirTemp("/tmp", "af-api-linux-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	installation := strings.Repeat("3", 32)
	key := bytes.Repeat([]byte{3}, 32)
	defer clear(key)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	rt, err := startAt(root+"/runtime/installation", installation, key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if rt != nil {
			rt.Close()
		}
	}()
	runProbe := func(user, want string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "docker", "run", "--rm", "--interactive", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--user="+user,
			"--mount=type=bind,source="+executable+",target=/api-probe,readonly",
			"--mount=type=bind,source="+root+"/runtime/installation/api,target=/run/firmware-device-api,readonly",
			"--mount=type=bind,source="+root+"/runtime/installation/auth,target=/run/firmware-device-api-auth,readonly",
			"--env=AF_DEVICE_API_CONTAINER_PROBE=1", "--env=AF_DEVICE_API_EXPECT="+want, "--entrypoint=/api-probe", image, "-test.run=^TestLinuxContainerProbe$", "-test.v")
		// Only the wrong-UID probe receives the test capability over stdin. It has
		// no permission to read the mounted token, despite sharing the socket group.
		if want != "200" {
			cmd.Stdin = strings.NewReader(rt.Vault.token())
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("container peer probe %s: %v (%s)", want, err, out)
		}
	}
	runProbe("65532:65532", "200")
	runProbe("0:65532", "403")
	blob, err := rt.Vault.Seal(t.Context(), "example-purpose", "server", []byte("restart-example"))
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	rt = nil
	reopened, err := startAt(root+"/runtime/installation", installation, key)
	if err != nil {
		t.Fatal("restart:", err)
	}
	rt = reopened
	plain, err := rt.Vault.Unseal("example-purpose", "server", blob)
	if err != nil || string(plain) != "restart-example" {
		t.Fatal("restart lost encrypted data")
	}
	clear(plain)
	runProbe("65532:65532", "200")
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	rt = nil
}

func TestLinuxContainerProbe(t *testing.T) {
	if os.Getenv("AF_DEVICE_API_CONTAINER_PROBE") != "1" {
		t.Skip("container helper")
	}
	expected := os.Getenv("AF_DEVICE_API_EXPECT")
	var token []byte
	var err error
	if expected == "200" {
		token, err = os.ReadFile("/run/firmware-device-api-auth/token")
	} else {
		token, err = io.ReadAll(io.LimitReader(os.Stdin, 44))
	}
	if err != nil {
		t.Fatal(err)
	}
	defer clear(token)
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		d := net.Dialer{Timeout: 250 * time.Millisecond}
		return d.DialContext(ctx, "unix", "/run/firmware-device-api/socket")
	}, MaxConnsPerHost: 2, ResponseHeaderTimeout: time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	request := func(method, path, body string) (int, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, "http://device"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+string(token))
		if method == "POST" {
			req.Header.Set("Content-Type", "application/json")
		}
		r, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(r.Body, 128*1024))
		return r.StatusCode, b
	}
	code, _ := request("GET", "/v1/capabilities", "")
	if fmt.Sprint(code) != expected {
		t.Fatalf("kernel peer status %d", code)
	}
	if expected != "200" {
		return
	}
	code, b := request("POST", "/v1/crypto/seal", `{"purpose":"example-purpose","record_id":"default","plaintext":"e30"}`)
	if code != 200 {
		t.Fatalf("seal %d", code)
	}
	var sealed map[string]string
	json.Unmarshal(b, &sealed)
	body, _ := json.Marshal(map[string]string{"purpose": "example-purpose", "record_id": "default", "sealed": sealed["sealed"]})
	code, b = request("POST", "/v1/crypto/unseal", string(body))
	if code != 200 || !bytes.Contains(b, []byte(`"plaintext":"e30"`)) {
		t.Fatal("UDS roundtrip failed")
	}
	if code, _ := request("GET", "/v1/config/agent/changes", ""); code != 404 {
		t.Fatal("configuration watch exposed")
	}
	if err := os.WriteFile("/run/firmware-device-api-auth/token", []byte("changed"), 0400); err == nil {
		t.Fatal("token mount writable")
	}
	if err := os.WriteFile("/run/firmware-device-api/injected", []byte("changed"), 0600); err == nil {
		t.Fatal("API mount writable")
	}
}
