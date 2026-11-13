package deviceapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Synthetic value with the contract's shape; it is not a credential.
var exampleCode = "agpt_enr_" + strings.Repeat("A", 43)

func testCodes(t *testing.T) (*CodeStore, *time.Time) {
	t.Helper()
	now := time.Unix(1_800_000_000, 0)
	return &CodeStore{dir: t.TempDir(), now: func() time.Time { return now }}, &now
}

func TestEnrollmentCodeBounds(t *testing.T) {
	for code, valid := range map[string]bool{
		exampleCode: true,
		"agpt_enr_" + strings.Repeat("-", 21) + strings.Repeat("_", 22): true,
		"":                                    false,
		exampleCode + "A":                     false,
		exampleCode[:len(exampleCode)-1]:      false,
		"agpt_xxx_" + strings.Repeat("A", 43): false,
		"agpt_enr_" + strings.Repeat("A", 42) + "=": false,
		exampleCode + "\n":                          false,
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
	other := "agpt_enr_" + strings.Repeat("B", 43)
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
	v, _, _ := testVault(t)
	h := NewHandler(v, nil)
	if callAPI(h, GatewayUID, v.token(), "GET", "/v1/enrollment/code", "").Code != 404 {
		t.Fatal("route exists without a code store")
	}
	s, _ := testCodes(t)
	h.enrollment = s
	caps := callAPI(h, GatewayUID, v.token(), "GET", "/v1/capabilities", "")
	if !strings.Contains(caps.Body.String(), EnrollmentCapability) {
		t.Fatal("capability not advertised")
	}
	if w := callAPI(h, GatewayUID, v.token(), "GET", "/v1/enrollment/code", ""); w.Code != 404 || !strings.Contains(w.Body.String(), `"not_found"`) {
		t.Fatal("absent code is not 404")
	}
	if callAPI(h, GatewayUID, v.token(), "DELETE", "/v1/enrollment/code", "").Code != 204 {
		t.Fatal("delete of an absent code is not idempotent")
	}
	s.Put(exampleCode)
	for _, c := range []struct {
		uid    uint32
		token  string
		method string
		path   string
		body   string
		status int
	}{
		{GatewayUID, "wrong", "GET", "/v1/enrollment/code", "", 401},
		{0, v.token(), "GET", "/v1/enrollment/code", "", 403},
		{0, v.token(), "DELETE", "/v1/enrollment/code", "", 403},
		{GatewayUID, v.token(), "POST", "/v1/enrollment/code", "", 405},
		{GatewayUID, v.token(), "PUT", "/v1/enrollment/code", "", 405},
		{GatewayUID, v.token(), "GET", "/v1/enrollment/code?x=1", "", 400},
		{GatewayUID, v.token(), "DELETE", "/v1/enrollment/code", "{}", 400},
		{GatewayUID, v.token(), "GET", "/v1/enrollment/code/", "", 404},
	} {
		w := callAPI(h, c.uid, c.token, c.method, c.path, c.body)
		if w.Code != c.status || strings.Contains(w.Body.String(), exampleCode) {
			t.Fatalf("%s %s: %d", c.method, c.path, w.Code)
		}
	}
	if !s.Held() {
		t.Fatal("rejected request removed the code")
	}
	w := callAPI(h, GatewayUID, v.token(), "GET", "/v1/enrollment/code", "")
	var body map[string]string
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil || len(body) != 1 || body["code"] != exampleCode || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("held code not served")
	}
	if w := callAPI(h, GatewayUID, v.token(), "DELETE", "/v1/enrollment/code", ""); w.Code != 204 || w.Body.Len() != 0 {
		t.Fatal("delete failed")
	}
	if callAPI(h, GatewayUID, v.token(), "GET", "/v1/enrollment/code", "").Code != 404 || s.Held() {
		t.Fatal("code survived delete")
	}
	// Diagnostics carry fixed labels only.
	s.Put(exampleCode)
	callAPI(h, GatewayUID, v.token(), "GET", "/v1/enrollment/code", "")
	for len(h.diagnostics.events) > 0 {
		b, _ := json.Marshal(<-h.diagnostics.events)
		if strings.Contains(string(b), "agpt_enr_") {
			t.Fatal("code in diagnostics")
		}
	}
}
