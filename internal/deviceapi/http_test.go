package deviceapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func callAPI(h http.Handler, uid uint32, token, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), peerKey{}, uid))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestHTTPAuthenticationStrictRequestsAndRoundTrip(t *testing.T) {
	v, _ := testVault(t)
	h := NewHandler(v)
	for _, c := range []struct {
		uid   uint32
		token string
		code  int
	}{{65532, "wrong", 401}, {0, v.token(), 403}, {65532, v.token(), 200}} {
		w := callAPI(h, c.uid, c.token, "GET", "/v1/capabilities", "")
		if w.Code != c.code {
			t.Fatalf("auth status %d", w.Code)
		}
	}
	for _, c := range []struct {
		body string
		code int
	}{
		{`{"purpose":"example-purpose","record_id":"default","plaintext":"e30"}`, 200},
		{`{"purpose":"example-purpose","record_id":"default","plaintext":"e30="}`, 400},
		{`{"purpose":"future-purpose","record_id":"default","plaintext":"e30"}`, 200},
		{`{"purpose":"example-purpose","record_id":"default","plaintext":"e30","plaintext":"e30"}`, 400},
		{`{"Purpose":"example-purpose","record_id":"default","plaintext":"e30"}`, 400},
		{`{"purpose":"example-purpose","record_id":"default","plaintext":"e30","path":"/tmp/file"}`, 400},
		{string(bytes.Repeat([]byte("x"), 128*1024+1)), 413},
	} {
		w := callAPI(h, 65532, v.token(), "POST", "/v1/crypto/seal", c.body)
		if w.Code != c.code {
			t.Fatalf("seal status %d want %d", w.Code, c.code)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cache allowed")
		}
		if c.code == 200 {
			var record map[string]string
			json.Unmarshal(w.Body.Bytes(), &record)
			request := map[string]string{}
			if err := json.Unmarshal([]byte(c.body), &request); err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(map[string]string{"purpose": request["purpose"], "record_id": "default", "sealed": record["sealed"]})
			u := callAPI(h, 65532, v.token(), "POST", "/v1/crypto/unseal", string(b))
			if u.Code != 200 || !strings.Contains(u.Body.String(), `"plaintext":"e30"`) {
				t.Fatal("HTTP roundtrip failed")
			}
		}
	}
}
func TestHTTPNoGenericSurfaceAndBoundedSlots(t *testing.T) {
	v, _ := testVault(t)
	h := NewHandler(v)
	for _, c := range []struct {
		method, path string
		code         int
	}{{"GET", "/v1/crypto/seal", 405}, {"POST", "/v1/crypto/seal/", 404}, {"GET", "/v1/capabilities?x=1", 400}, {"GET", "/v1/config/agent/changes", 404}, {"OPTIONS", "/v1/capabilities", 405}} {
		if got := callAPI(h, 65532, v.token(), c.method, c.path, "").Code; got != c.code {
			t.Fatalf("%s %s: %d", c.method, c.path, got)
		}
	}
	h.unary <- struct{}{}
	h.unary <- struct{}{}
	w := callAPI(h, 65532, v.token(), "GET", "/v1/capabilities", "")
	if w.Code != 429 || w.Header().Get("Retry-After") != "1" {
		t.Fatal("unbounded admission")
	}
	<-h.unary
	<-h.unary
}

func TestDiagnosticsAreBoundedAndContainNoCallerData(t *testing.T) {
	v, _ := testVault(t)
	h := NewHandler(v)
	for i := 0; i < 200; i++ {
		callAPI(h, 65532, "private-example", "GET", "/private-example", "")
	}
	if len(h.diagnostics.events) > 2 {
		t.Fatal("auth diagnostics not throttled")
	}
	for len(h.diagnostics.events) > 0 {
		b, _ := json.Marshal(<-h.diagnostics.events)
		if strings.Contains(string(b), "private-example") {
			t.Fatal("caller data logged")
		}
	}
	h.diagnostics.mu.Lock()
	defer h.diagnostics.mu.Unlock()
	if len(h.diagnostics.values) != 2 {
		t.Fatal("unbounded diagnostic labels")
	}
	for _, a := range h.diagnostics.values {
		if a.count != 200 {
			t.Fatal("throttling lost counters")
		}
	}
}

func TestCryptoAdmissionBoundsBurstAndRefills(t *testing.T) {
	v, _ := testVault(t)
	h := NewHandler(v)
	began := time.Now()
	admitted := 0
	for i := 0; i < 100; i++ {
		if h.cryptoAllowed() {
			admitted++
		}
	}
	// Include elapsed refill allowance rather than assuming an unloaded scheduler.
	if admitted > 40+int(time.Since(began).Seconds()*20) {
		t.Fatal("crypto rate budget exceeded")
	}
	time.Sleep(60 * time.Millisecond)
	if !h.cryptoAllowed() {
		t.Fatal("rate budget did not refill")
	}
}
