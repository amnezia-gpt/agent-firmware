package deviceapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Synthetic value with the contract's shape; it is not a credential.
var exampleCode = "enrc_" + strings.Repeat("A", 43)

func testCodes(t *testing.T) (*CodeStore, *time.Time) {
	t.Helper()
	now := time.Unix(1_800_000_000, 0)
	directory := filepath.Join(t.TempDir(), "codes")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	return &CodeStore{dir: directory, now: func() time.Time { return now }}, &now
}

func TestEnrollmentCodeBounds(t *testing.T) {
	for code, valid := range map[string]bool{
		exampleCode: true,
		"enrc_" + strings.Repeat("-", 21) + strings.Repeat("_", 22): true,
		// The prefix and exact length are the authorization server's; any token of the alphabet in bounds passes.
		"other_" + strings.Repeat("A", 43):      true,
		strings.Repeat("A", 16):                 true,
		strings.Repeat("A", 128):                true,
		"":                                      false,
		strings.Repeat("A", 15):                 false,
		strings.Repeat("A", 129):                false,
		"enrc_" + strings.Repeat("A", 42) + "=": false,
		"enrc_" + strings.Repeat("A", 42) + " ": false,
		exampleCode + "\n":                      false,
	} {
		if ValidEnrollmentCode(code) != valid {
			t.Fatalf("bounds check of a %d-byte value: want %v", len(code), valid)
		}
	}
}

func TestEnrollmentCodeHoldReplaceDelete(t *testing.T) {
	s, _ := testCodes(t)
	if _, held := s.Get(); held || s.Held() {
		t.Fatal("empty store holds a code")
	}
	if s.Put("not-a-code") != ErrEnrollmentCode || s.Held() {
		t.Fatal("invalid code accepted")
	}
	if err := s.Put(exampleCode); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(s.dir, enrollmentFile))
	if err != nil || info.Mode().Perm() != 0600 || !info.Mode().IsRegular() {
		t.Fatal("code file is not a private regular file")
	}
	// Reading does not spend the code: Gateway removes it after Auth answered.
	for i := 0; i < 2; i++ {
		if got, held := s.Get(); !held || got != exampleCode {
			t.Fatal("held code not returned")
		}
	}
	other := "enrc_" + strings.Repeat("B", 43)
	if err := s.Put(other); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(); got != other {
		t.Fatal("new installation command did not replace the code")
	}
	if s.Delete() != nil || s.Held() || s.Delete() != nil {
		t.Fatal("delete is not idempotent")
	}
	entries, _ := os.ReadDir(s.dir)
	if len(entries) != 0 {
		t.Fatal("files left behind")
	}
}

func TestEnrollmentCodeExpiresAndRejectsUnsafeFiles(t *testing.T) {
	s, now := testCodes(t)
	s.Put(exampleCode)
	*now = now.Add(time.Hour - time.Second)
	if !s.Held() {
		t.Fatal("expired early")
	}
	*now = now.Add(time.Second)
	if s.Held() {
		t.Fatal("held past one hour")
	}
	if _, err := os.Lstat(filepath.Join(s.dir, enrollmentFile)); !os.IsNotExist(err) {
		t.Fatal("expired code left on disk")
	}
	// A clock set back must not extend the lifetime.
	s.Put(exampleCode)
	*now = now.Add(-2 * time.Minute)
	if s.Held() {
		t.Fatal("code from the future accepted")
	}
	path := filepath.Join(s.dir, enrollmentFile)
	for _, prepare := range []func(){
		func() { os.WriteFile(path, []byte(exampleCode), 0644) },
		func() { os.WriteFile(path, []byte("garbage"), 0600) },
		func() {
			target := filepath.Join(t.TempDir(), "target")
			os.WriteFile(target, []byte(exampleCode), 0600)
			os.Symlink(target, path)
		},
	} {
		os.Remove(path)
		prepare()
		os.Chtimes(path, s.now(), s.now())
		if _, held := s.Get(); held {
			t.Fatal("unsafe code file accepted")
		}
	}
}

func TestEnrollmentCodeRoutes(t *testing.T) {
	v, _ := testVault(t)
	h := NewHandler(v, nil, nil)
	if callAPI(h, "GET", "/v3/enrollment/code", "").Code != 404 {
		t.Fatal("route exists without a code store")
	}
	s, _ := testCodes(t)
	h.enrollment = s
	caps := callAPI(h, "GET", "/v3/capabilities", "")
	var advertised struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(caps.Body.Bytes(), &advertised); caps.Code != 200 || err != nil {
		t.Fatalf("invalid capabilities response: status %d, decode error %v", caps.Code, err)
	}
	const enrollmentCodeService = "urn:agent-firmware:device-service:enrollment-code:v1"
	if !slices.Contains(advertised.Capabilities, enrollmentCodeService) {
		t.Fatalf("capabilities %q must include %q", advertised.Capabilities, enrollmentCodeService)
	}
	if w := callAPI(h, "GET", "/v3/enrollment/code", ""); w.Code != 404 || !strings.Contains(w.Body.String(), `"not_found"`) {
		t.Fatal("absent code is not 404")
	}
	if callAPI(h, "DELETE", "/v3/enrollment/code", "").Code != 204 {
		t.Fatal("delete of an absent code is not idempotent")
	}
	s.Put(exampleCode)
	for _, c := range []struct {
		method string
		path   string
		body   string
		status int
	}{
		{"POST", "/v3/enrollment/code", "", 405},
		{"PUT", "/v3/enrollment/code", "", 405},
		{"GET", "/v3/enrollment/code?x=1", "", 400},
		{"DELETE", "/v3/enrollment/code", "{}", 400},
		{"GET", "/v3/enrollment/code/", "", 404},
	} {
		w := callAPI(h, c.method, c.path, c.body)
		if w.Code != c.status || strings.Contains(w.Body.String(), exampleCode) {
			t.Fatalf("%s %s: %d", c.method, c.path, w.Code)
		}
	}
	if !s.Held() {
		t.Fatal("rejected request removed the code")
	}
	w := callAPI(h, "GET", "/v3/enrollment/code", "")
	var body map[string]string
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil || len(body) != 1 || body["code"] != exampleCode || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("held code not served")
	}
	if w := callAPI(h, "DELETE", "/v3/enrollment/code", ""); w.Code != 204 || w.Body.Len() != 0 {
		t.Fatal("delete failed")
	}
	if callAPI(h, "GET", "/v3/enrollment/code", "").Code != 404 || s.Held() {
		t.Fatal("code survived delete")
	}
	// Diagnostics carry fixed labels only.
	s.Put(exampleCode)
	callAPI(h, "GET", "/v3/enrollment/code", "")
	for len(h.diagnostics.events) > 0 {
		b, _ := json.Marshal(<-h.diagnostics.events)
		if strings.Contains(string(b), "enrc_") {
			t.Fatal("code in diagnostics")
		}
	}
}

func TestLateWorkloadDeletePreservesNewEnrollmentCode(t *testing.T) {
	host, _ := testCodes(t)
	workload := &CodeStore{dir: host.dir, now: host.now}
	if err := host.Put(exampleCode); err != nil {
		t.Fatal(err)
	}
	if got, ok := workload.getForWorkload(); !ok || got != exampleCode {
		t.Fatal("first code was not delivered")
	}
	if err := host.Delete(); err != nil {
		t.Fatal(err)
	}
	replacement := strings.Repeat("B", MaxEnrollmentCode)
	if err := host.Put(replacement); err != nil {
		t.Fatal(err)
	}
	if err := workload.deleteForWorkload(); err != nil {
		t.Fatal(err)
	}
	if got, ok := workload.getForWorkload(); !ok || got != replacement {
		t.Fatal("a delayed delete spent the replacement code")
	}
	if err := workload.deleteForWorkload(); err != nil || host.Held() {
		t.Fatalf("the workload could not delete the replacement after reading it: %v", err)
	}
}
