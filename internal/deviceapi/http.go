package deviceapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/deviceidentity"
	"github.com/amnezia-gpt/agent-firmware/internal/strictjson"
)

const Protocol = "urn:agent-firmware:device-api:v3"
const SealCapability = "urn:agent-firmware:device-service:credential-seal"
const identityCapability = "urn:agent-firmware:device-service:device-identity"
const signingCapability = "urn:agent-firmware:device-service:signing"
const deriveCapability = "urn:agent-firmware:device-service:key-agreement"
const maxBody = 128 * 1024

type Handler struct {
	diagnostics *diagnostics
	vault       *Vault
	identity    *deviceidentity.Access
	enrollment  *CodeStore
	unary       chan struct{}
	mu          sync.Mutex
	tokens      float64
	last        time.Time
}

func NewHandler(v *Vault, identity *deviceidentity.Access, codes *CodeStore) *Handler {
	return &Handler{vault: v, identity: identity, enrollment: codes, unary: make(chan struct{}, 2), tokens: 40, last: time.Now(), diagnostics: newDiagnostics()}
}
func (h *Handler) Events() <-chan Event { return h.diagnostics.events }

func (h *Handler) cryptoAllowed() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	h.tokens = min(40, h.tokens+now.Sub(h.last).Seconds()*20)
	h.last = now
	if h.tokens < 1 {
		return false
	}
	h.tokens--
	return true
}
func reply(w http.ResponseWriter, status int, v any) {
	if status == 204 {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, status int, code string, retry bool) {
	if status == 429 {
		w.Header().Set("Retry-After", "1")
	}
	reply(w, status, struct {
		Error struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}{struct {
		Code      string `json:"code"`
		Retryable bool   `json:"retryable"`
	}{code, retry}})
}
func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Scheme != "" || r.URL.Host != "" {
		failure(w, 400, "invalid_request", false)
		return
	}
	method, allow := "", ""
	switch r.URL.Path {
	case "/v3/capabilities", "/v3/identity":
		method = "GET"
	case "/v3/crypto/seal", "/v3/crypto/unseal", "/v3/keys/signing/sign", "/v3/keys/encryption/derive":
		method = "POST"
	case "/v3/enrollment/code":
		if h.enrollment == nil {
			failure(w, 404, "not_found", false)
			return
		}
		allow, method = "GET, DELETE", "GET"
		if r.Method == "DELETE" {
			method = "DELETE"
		}
	default:
		failure(w, 404, "not_found", false)
		return
	}
	if r.URL.RawPath != "" {
		failure(w, 400, "invalid_request", false)
		return
	}
	if r.Method != method {
		if allow == "" {
			allow = method
		}
		w.Header().Set("Allow", allow)
		failure(w, 405, "method_not_allowed", false)
		return
	}
	const budget = 2 * time.Second
	select {
	case h.unary <- struct{}{}:
		defer func() { <-h.unary }()
	default:
		failure(w, 429, "resource_exhausted", true)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), budget)
	defer cancel()
	rc := http.NewResponseController(w)
	rc.SetReadDeadline(time.Now().Add(budget))
	rc.SetWriteDeadline(time.Now().Add(budget))
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		failure(w, 400, "invalid_request", false)
		return
	}
	if method != "POST" {
		if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			failure(w, 400, "invalid_request", false)
			return
		}
		switch {
		case r.URL.Path == "/v3/capabilities":
			h.capabilities(w)
		case r.URL.Path == "/v3/identity":
			if !h.cryptoAllowed() {
				failure(w, 429, "resource_exhausted", true)
				return
			}
			if h.identity == nil {
				cryptoFailure(w, ErrKeystore)
				return
			}
			value, err := h.identity.Identity(time.Now())
			if err != nil {
				cryptoFailure(w, err)
				return
			}
			reply(w, 200, value)
		case method == "DELETE":
			// The workload acknowledges the code it received.
			if h.enrollment.deleteForWorkload() != nil {
				failure(w, 500, "internal_error", false)
				return
			}
			reply(w, 204, nil)
		default:
			code, held := h.enrollment.getForWorkload()
			if !held {
				failure(w, 404, "not_found", false)
				return
			}
			reply(w, 200, map[string]string{"code": code})
		}
		return
	}
	if !h.cryptoAllowed() {
		failure(w, 429, "resource_exhausted", true)
		return
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(params) > 1 || len(params) == 1 && params["charset"] != "utf-8" {
		failure(w, 415, "unsupported_media_type", false)
		return
	}
	if r.Header.Get("Content-Encoding") != "" {
		failure(w, 415, "unsupported_media_type", false)
		return
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			failure(w, 413, "payload_too_large", false)
		} else {
			failure(w, 400, "invalid_request", false)
		}
		return
	}
	if r.URL.Path == "/v3/keys/signing/sign" || r.URL.Path == "/v3/keys/encryption/derive" {
		h.keyOperation(w, r.URL.Path, b)
		return
	}
	field := "plaintext"
	if r.URL.Path == "/v3/crypto/unseal" {
		field = "sealed"
	}
	fields, valid := strictjson.Object(b, "purpose", "record_id", field)
	if !valid {
		failure(w, 400, "invalid_request", false)
		return
	}
	purpose, id, value := strictjson.StringField(fields, "purpose"), strictjson.StringField(fields, "record_id"), strictjson.StringField(fields, field)
	if !purposePattern.MatchString(purpose) || !idPattern.MatchString(id) {
		failure(w, 400, "invalid_request", false)
		return
	}
	if field == "plaintext" {
		plain, err := decode(value)
		if err != nil || len(plain) == 0 || len(plain) > MaxPlaintext {
			failure(w, 400, "invalid_request", false)
			return
		}
		defer clear(plain)
		sealed, err := h.vault.Seal(ctx, purpose, id, plain)
		if err != nil {
			cryptoFailure(w, err)
			return
		}
		reply(w, 200, map[string]string{"sealed": sealed})
	} else {
		plain, err := h.vault.Unseal(purpose, id, value)
		if err != nil {
			cryptoFailure(w, err)
			return
		}
		defer clear(plain)
		reply(w, 200, map[string]string{"plaintext": base64.RawURLEncoding.EncodeToString(plain)})
	}
}

func cryptoFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrCiphertext):
		failure(w, 422, string(ErrCiphertext), false)
	case errors.Is(err, ErrRequest) || errors.Is(err, deviceidentity.ErrInput):
		failure(w, 400, string(ErrRequest), false)
	default:
		failure(w, 503, string(ErrKeystore), true)
	}
}
func (h *Handler) capabilities(w http.ResponseWriter) {
	capabilities := []string{SealCapability, identityCapability, signingCapability, deriveCapability}
	if h.enrollment != nil {
		capabilities = append(capabilities, EnrollmentCapability)
	}
	reply(w, 200, map[string]any{
		"protocol":              Protocol,
		"capabilities":          capabilities,
		"credential_protection": map[string]string{"state": h.vault.Status(), "protection": "software"},
	})
}

func (h *Handler) keyOperation(w http.ResponseWriter, path string, raw []byte) {
	if h.identity == nil {
		cryptoFailure(w, ErrKeystore)
		return
	}
	if path == "/v3/keys/signing/sign" {
		fields, valid := strictjson.Object(raw, "algorithm", "input")
		value := strictjson.StringField(fields, "input")
		if !valid || strictjson.StringField(fields, "algorithm") != "ES256" ||
			len(value) > base64.RawURLEncoding.EncodedLen(deviceidentity.MaxSignInput) {
			cryptoFailure(w, ErrRequest)
			return
		}
		input, err := decode(value)
		if err != nil {
			cryptoFailure(w, err)
			return
		}
		defer clear(input)
		sig, err := h.identity.Sign(input)
		if err != nil {
			cryptoFailure(w, err)
			return
		}
		reply(w, 200, map[string]string{"signature": base64.RawURLEncoding.EncodeToString(sig)})
		return
	}
	fields, valid := strictjson.Object(raw, "algorithm", "peer_public_key")
	point, validPoint := strictjson.Object(fields["peer_public_key"], "kty", "crv", "x", "y")
	if !valid || !validPoint || strictjson.StringField(fields, "algorithm") != "ECDH" {
		cryptoFailure(w, ErrRequest)
		return
	}
	peer := deviceidentity.JWK{
		Type: strictjson.StringField(point, "kty"), Curve: strictjson.StringField(point, "crv"),
		X: strictjson.StringField(point, "x"), Y: strictjson.StringField(point, "y"),
	}
	secret, err := h.identity.Derive(peer)
	if err != nil {
		cryptoFailure(w, err)
		return
	}
	defer clear(secret)
	reply(w, 200, map[string]string{"shared_secret": base64.RawURLEncoding.EncodeToString(secret)})
}
