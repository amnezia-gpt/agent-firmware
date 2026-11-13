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
	"net/url"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/strictjson"
)

const Protocol = "urn:amnezia-gpt:device-api:v1"
const SealCapability = "urn:amnezia-gpt:device-service:credential-seal:v1"
const WatchCapability = "urn:amnezia-gpt:device-service:codex-config-watch:v1"
const ConfigResource = "urn:amnezia-gpt:device-resource:codex-agent-user-config:v1"
const GatewayUID uint32 = 65532
const maxBody = 128 * 1024

type peerKey struct{}
type Change struct {
	Cursor string `json:"cursor"`
	Resync bool   `json:"resync"`
}
type ChangeSource interface {
	State() string
	Wait(context.Context, string, time.Duration) (Change, error)
}
type Handler struct {
	diagnostics      *diagnostics
	vault            *Vault
	watch            ChangeSource
	enrollment       *CodeStore
	unary, watchSlot chan struct{}
	mu               sync.Mutex
	tokens           float64
	last             time.Time
}

func NewHandler(v *Vault, w ChangeSource) *Handler {
	return &Handler{vault: v, watch: w, unary: make(chan struct{}, 2), watchSlot: make(chan struct{}, 1), tokens: 40, last: time.Now(), diagnostics: newDiagnostics()}
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
	case "/v1/capabilities", "/v1/config/agent/changes":
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
	watch := r.URL.Path == "/v1/config/agent/changes"
	slot := h.unary
	budget := 2 * time.Second
	if watch {
		slot = h.watchSlot
		budget = 27 * time.Second
	}
	select {
	case slot <- struct{}{}:
		defer func() { <-slot }()
	default:
		failure(w, 429, "resource_exhausted", true)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), budget)
	defer cancel()
	r = r.WithContext(ctx)
	rc := http.NewResponseController(w)
	// Real net/http connections support deadlines; recorders deliberately do not.
	rc.SetReadDeadline(time.Now().Add(budget))
	rc.SetWriteDeadline(time.Now().Add(budget))
	if watch {
		h.changes(w, r)
		return
	}
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
			// Gateway removes the code once Auth answered; repeating is harmless.
			if h.enrollment.Delete() != nil {
				failure(w, 500, "internal_error", false)
				return
			}
			reply(w, 204, nil)
		default:
			code, held := h.enrollment.Get()
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
	if !supportsPurpose(purpose) {
		failure(w, 400, string(ErrPurpose), false)
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

var purposePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func cryptoFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrCiphertext):
		failure(w, 422, string(ErrCiphertext), false)
	case errors.Is(err, ErrRotation):
		failure(w, 409, string(ErrRotation), false)
	case errors.Is(err, ErrRequest):
		failure(w, 400, string(ErrRequest), false)
	default:
		failure(w, 503, string(ErrKeystore), true)
	}
}
func (h *Handler) capabilities(w http.ResponseWriter) {
	c := []string{SealCapability}
	response := map[string]any{"protocol": Protocol, "capabilities": c, "credential_protection": map[string]any{"state": h.vault.Status(), "key_id": h.vault.KeyID(), "protection": "host", "purposes": purposes}}
	if h.enrollment != nil {
		c = append(c, EnrollmentCapability)
		response["capabilities"] = c
	}
	if h.watch != nil {
		response["capabilities"] = append(c, WatchCapability)
		response["config_watch"] = map[string]string{"resource": ConfigResource, "state": h.watch.State()}
	}
	reply(w, 200, response)
}
func (h *Handler) changes(w http.ResponseWriter, r *http.Request) {
	q, err := parseWatchQuery(r)
	if err != nil {
		failure(w, 400, "invalid_request", false)
		return
	}
	if h.watch == nil {
		failure(w, 503, "watch_unavailable", true)
		return
	}
	change, err := h.watch.Wait(r.Context(), q.after, q.wait)
	if err != nil {
		failure(w, 503, "watch_unavailable", true)
		return
	}
	reply(w, 200, change)
}

type watchQuery struct {
	after string
	wait  time.Duration
}

func parseWatchQuery(r *http.Request) (watchQuery, error) {
	q, err := urlQuery(r)
	if err != nil {
		return watchQuery{}, err
	}
	after := q.Get("after")
	if len(after) > 80 || after != "" && !idPattern.MatchString(after) {
		return watchQuery{}, ErrRequest
	}
	wait := 25
	if n := q.Get("wait_seconds"); n != "" {
		wait, err = strconv.Atoi(n)
		if err != nil || strconv.Itoa(wait) != n || wait < 1 || wait > 25 {
			return watchQuery{}, ErrRequest
		}
	}
	return watchQuery{after, time.Duration(wait) * time.Second}, nil
}
func urlQuery(r *http.Request) (url.Values, error) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return nil, ErrRequest
	}
	for k, v := range q {
		if (k != "after" && k != "wait_seconds") || len(v) != 1 || v[0] == "" {
			return nil, ErrRequest
		}
	}
	return q, nil
}
