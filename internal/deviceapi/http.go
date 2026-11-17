package deviceapi

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/strictjson"
)

const Protocol = "urn:amnezia-gpt:device-api:v1"
const SealCapability = "urn:amnezia-gpt:device-service:credential-seal:v1"
const GatewayUID uint32 = 65532
const maxBody = 128 * 1024

type peerKey struct{}
type Handler struct {
	diagnostics *diagnostics
	vault       *Vault
	enrollment  *CodeStore
	unary       chan struct{}
	mu          sync.Mutex
	tokens      float64
	last        time.Time
}

func NewHandler(v *Vault) *Handler {
	return &Handler{vault: v, unary: make(chan struct{}, 2), tokens: 40, last: time.Now(), diagnostics: newDiagnostics()}
}
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
	// Reject without draining an untrusted unfinished body or reusing its framing.
	w.Header().Set("Connection", "close")
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
	// Compare the fixed-length capability; missing/duplicate headers fail closed.
	auth := r.Header.Values("Authorization")
	if len(auth) != 1 || len(auth[0]) != 50 || subtle.ConstantTimeCompare([]byte(auth[0]), []byte("Bearer "+h.vault.token())) != 1 {
		failure(w, 401, "unauthorized", false)
		return
	}
	uid, ok := r.Context().Value(peerKey{}).(uint32)
	if !ok || uid != GatewayUID {
		failure(w, 403, "forbidden", false)
		return
	}
	if r.URL.Scheme != "" || r.URL.Host != "" {
		failure(w, 400, "invalid_request", false)
		return
	}
	method, allow := "", ""
	switch r.URL.Path {
	case "/v1/capabilities":
		method = "GET"
	case "/v1/crypto/seal", "/v1/crypto/unseal":
		method = "POST"
	case "/v1/enrollment/code":
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
		case r.URL.Path == "/v1/capabilities":
			h.capabilities(w)
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
	field := "plaintext"
	if r.URL.Path == "/v1/crypto/unseal" {
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
	case errors.Is(err, ErrRequest):
		failure(w, 400, string(ErrRequest), false)
	default:
		failure(w, 503, string(ErrKeystore), true)
	}
}
func (h *Handler) capabilities(w http.ResponseWriter) {
	capabilities := []string{SealCapability}
	if h.enrollment != nil {
		capabilities = append(capabilities, EnrollmentCapability)
	}
	reply(w, 200, map[string]any{
		"protocol":              Protocol,
		"capabilities":          capabilities,
		"credential_protection": map[string]string{"state": h.vault.Status(), "protection": "software"},
	})
}
