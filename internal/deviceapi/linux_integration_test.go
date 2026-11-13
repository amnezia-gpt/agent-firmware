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
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in only: these tests create their own Docker/systemd resources on
// the selected disposable Linux host. Ordinary go test never operates a host.
func linuxHostTest(t *testing.T) {
	t.Helper()
	if os.Getenv("AF_DEVICE_API_LINUX_TEST") != "1" || os.Geteuid() != 0 {
		t.Skip("selected disposable root Linux host required")
	}
}
func TestLinuxAPIIsolationRestartAndSystemdCredential(t *testing.T) {
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
	state := root + "/state"
	os.Mkdir(state, 0700)
	installation := strings.Repeat("3", 32)
	var key []byte
	if err := Initialize(t.Context(), state, installation, func(ctx context.Context, k []byte) ([]byte, error) { key = bytes.Clone(k); return WrapHost(ctx, k) }); err != nil {
		t.Fatal("host wrap:", err)
	}
	defer clear(key)
	// PID 1 loads the encrypted credential. The service verifies it without printing
	// key material or copying plaintext credentials into a persistent directory.
	executable, _ := os.Executable()
	unit := "af-self7-credential-" + filepath.Base(root)
	cmd := exec.CommandContext(t.Context(), "systemd-run", "--quiet", "--wait", "--collect", "--unit="+unit,
		"--property=Type=exec", "--property=LoadCredentialEncrypted=device-api-key:"+state+"/device-api/key.cred",
		"--setenv=AF_DEVICE_API_SYSTEMD_PROBE=1", "--setenv=AF_DEVICE_API_STATE="+state,
		executable, "-test.run=^TestSystemdCredentialProbe$")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("systemd credential load failed: %v (%d diagnostic bytes)", err, len(out))
	}
	rt, err := startAt(state, root+"/runtime/installation", installation, key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if rt != nil {
			rt.Close()
		}
	}()
	home := root + "/agent-home"
	os.Mkdir(home, 0700)
	if err := rt.Watch.Bind(home); err != nil {
		t.Fatal(err)
	}
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
	blob, err := rt.Vault.Seal(t.Context(), "mcp-oauth", "server", []byte("restart-example"))
	if err != nil {
		t.Fatal(err)
	}
	first, _ := rt.Watch.Wait(t.Context(), "", 0)
	if err := os.WriteFile(home+"/config.toml.new", []byte("example=true"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(home+"/config.toml.new", home+"/config.toml"); err != nil {
		t.Fatal(err)
	}
	changed, err := rt.Watch.Wait(t.Context(), first.Cursor, time.Second)
	if err != nil || !changed.Resync {
		t.Fatal("atomic replacement was lost")
	}
	// A symlink's target is never opened or watched.
	outside := root + "/outside"
	os.WriteFile(outside, []byte("private-example"), 0600)
	os.Remove(home + "/config.toml")
	os.Symlink(outside, home+"/config.toml")
	time.Sleep(250 * time.Millisecond)
	baseline, _ := rt.Watch.Wait(t.Context(), "", 0)
	os.WriteFile(outside, []byte("changed-example"), 0600)
	noChange, err := rt.Watch.Wait(t.Context(), baseline.Cursor, 200*time.Millisecond)
	if err != nil || noChange.Resync {
		t.Fatal("symlink target followed")
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	rt = nil
	reopened, err := startAt(state, root+"/runtime/installation", installation, key)
	if err != nil {
		t.Fatal("restart:", err)
	}
	rt = reopened
	plain, err := rt.Vault.Unseal("mcp-oauth", "server", blob)
	if err != nil || string(plain) != "restart-example" {
		t.Fatal("restart lost encrypted data")
	}
	clear(plain)
	runProbe("65532:65532", "200")
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	rt = nil
	// No key replacement, even after the encrypted credential disappears.
	os.Remove(state + "/device-api/key.cred")
	if err := Initialize(t.Context(), state, installation, WrapHost); err != ErrKeystore {
		t.Fatal("missing credential regenerated")
	}
}
func TestSystemdCredentialProbe(t *testing.T) {
	if os.Getenv("AF_DEVICE_API_SYSTEMD_PROBE") != "1" {
		t.Skip("service helper")
	}
	key, err := LoadCredential(os.Getenv("CREDENTIALS_DIRECTORY"))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(key)
	v, err := OpenVault(os.Getenv("AF_DEVICE_API_STATE"), strings.Repeat("3", 32), key)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if _, err := v.Seal(t.Context(), "gateway-session", "default", []byte("systemd-example")); err != nil {
		t.Fatal(err)
	}
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
	code, b := request("POST", "/v1/crypto/seal", `{"purpose":"gateway-session","record_id":"default","plaintext":"e30"}`)
	if code != 200 {
		t.Fatalf("seal %d", code)
	}
	var sealed map[string]string
	json.Unmarshal(b, &sealed)
	body, _ := json.Marshal(map[string]string{"purpose": "gateway-session", "record_id": "default", "sealed": sealed["sealed"]})
	code, b = request("POST", "/v1/crypto/unseal", string(body))
	if code != 200 || !bytes.Contains(b, []byte(`"plaintext":"e30"`)) {
		t.Fatal("UDS roundtrip failed")
	}
	code, b = request("GET", "/v1/config/agent/changes", "")
	if code == 200 {
		var initial Change
		json.Unmarshal(b, &initial)
		// Watch must outlive the unary two-second body budget.
		client.Timeout = 5 * time.Second
		transport.ResponseHeaderTimeout = 4 * time.Second
		began := time.Now()
		code, b = request("GET", "/v1/config/agent/changes?after="+initial.Cursor+"&wait_seconds=3", "")
		if code != 200 || time.Since(began) < 2800*time.Millisecond {
			t.Fatal("watch inherited unary timeout")
		}
	}
	if err := os.WriteFile("/run/firmware-device-api-auth/token", []byte("changed"), 0400); err == nil {
		t.Fatal("token mount writable")
	}
	if err := os.WriteFile("/run/firmware-device-api/injected", []byte("changed"), 0600); err == nil {
		t.Fatal("API mount writable")
	}
}
