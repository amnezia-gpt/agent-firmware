package deviceapi

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/deviceidentity"
)

func callAPI(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestHTTPStrictRequestsAndRoundTrip(t *testing.T) {
	v, _ := testVault(t)
	h := NewHandler(v, nil, nil)
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
		w := callAPI(h, "POST", "/v3/crypto/seal", c.body)
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
			u := callAPI(h, "POST", "/v3/crypto/unseal", string(b))
			if u.Code != 200 || !strings.Contains(u.Body.String(), `"plaintext":"e30"`) {
				t.Fatal("HTTP roundtrip failed")
			}
		}
	}
}
func TestHTTPNoGenericSurfaceAndBoundedSlots(t *testing.T) {
	v, _ := testVault(t)
	h := NewHandler(v, nil, nil)
	for _, c := range []struct {
		method, path string
		code         int
	}{{"GET", "/v3/crypto/seal", 405}, {"POST", "/v3/crypto/seal/", 404}, {"GET", "/v3/capabilities?x=1", 400}, {"GET", "/v3/config/agent/changes", 404}, {"OPTIONS", "/v3/capabilities", 405}} {
		if got := callAPI(h, c.method, c.path, "").Code; got != c.code {
			t.Fatalf("%s %s: %d", c.method, c.path, got)
		}
	}
	h.unary <- struct{}{}
	h.unary <- struct{}{}
	w := callAPI(h, "GET", "/v3/capabilities", "")
	if w.Code != 429 || w.Header().Get("Retry-After") != "1" {
		t.Fatal("unbounded admission")
	}
	<-h.unary
	<-h.unary
}

func TestDiagnosticsAreBoundedAndContainNoCallerData(t *testing.T) {
	v, _ := testVault(t)
	h := NewHandler(v, nil, nil)
	for i := 0; i < 200; i++ {
		callAPI(h, "GET", "/private-example", "")
	}
	if len(h.diagnostics.events) > 2 {
		t.Fatal("request diagnostics not throttled")
	}
	for len(h.diagnostics.events) > 0 {
		b, _ := json.Marshal(<-h.diagnostics.events)
		if strings.Contains(string(b), "private-example") {
			t.Fatal("caller data logged")
		}
	}
	h.diagnostics.mu.Lock()
	defer h.diagnostics.mu.Unlock()
	if len(h.diagnostics.values) != 1 {
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
	h := NewHandler(v, nil, nil)
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

func TestIdentityRoutesUseOnlyOperationalKeys(t *testing.T) {
	vault, root := testVault(t)
	sealedDevice, err := deviceidentity.GenerateDevice(root)
	if err != nil {
		t.Fatal(err)
	}
	const issuer = "https://issuer.example"
	device, err := deviceidentity.OpenDevice(root, sealedDevice)
	if err != nil {
		t.Fatal(err)
	}
	sealedKeys, err := deviceidentity.GenerateOperational(root, issuer)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := deviceidentity.OpenOperational(root, issuer, sealedKeys)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(vault, deviceidentity.NewAccess(device, keys, issuer), nil)
	response := callAPI(handler, "GET", "/v3/identity", "")
	var identity deviceidentity.Identity
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &identity) != nil ||
		identity.IDevID.Thumbprint != device.Public().Thumbprint || !strings.Contains(identity.Statement, ".") {
		t.Fatal("identity response disagrees with the public device key")
	}
	for _, endpoint := range []string{"/v1/capabilities", "/v3/keys/idevid/sign", "/v3/keys/signing/export"} {
		if got := callAPI(handler, "GET", endpoint, "").Code; got != 404 {
			t.Fatalf("unexpected route %s (%d)", endpoint, got)
		}
	}
	if got := callAPI(handler, "POST", "/v3/keys/signing/sign", `{"algorithm":"ES256","input":"YWJj","key":"idevid"}`).Code; got != 400 {
		t.Fatalf("request selected a key: %d", got)
	}
	sign := callAPI(handler, "POST", "/v3/keys/signing/sign", `{"algorithm":"ES256","input":"YWJj"}`)
	var signature map[string]string
	if sign.Code != 200 || json.Unmarshal(sign.Body.Bytes(), &signature) != nil {
		t.Fatal("valid signing request failed")
	}
	raw, err := decode(signature["signature"])
	if err != nil || len(raw) != 64 {
		t.Fatal("unexpected ES256 response")
	}
	verify := func(key deviceidentity.JWK) bool {
		x, _ := decode(key.X)
		y, _ := decode(key.Y)
		point := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		digest := sha256.Sum256([]byte("abc"))
		return ecdsa.Verify(point, digest[:], new(big.Int).SetBytes(raw[:32]), new(big.Int).SetBytes(raw[32:]))
	}
	if !verify(identity.SigningKey.JWK) || verify(identity.IDevID.JWK) {
		t.Fatal("signing endpoint used the wrong key")
	}
}
