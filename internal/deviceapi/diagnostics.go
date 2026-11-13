package deviceapi

import (
	"net/http"
	"sync"
	"time"
)

// Event contains only firmware-owned enums and aggregate values. The bounded
// queue is independent of request handling; slow/dropped diagnostics never block it.
type Event struct {
	Event           string  `json:"event"`
	Operation       string  `json:"operation"`
	Phase           string  `json:"phase"`
	Outcome         string  `json:"outcome"`
	Count           uint64  `json:"count"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
}
type aggregate struct {
	count    uint64
	duration time.Duration
	last     time.Time
}
type diagnostics struct {
	mu     sync.Mutex
	values map[string]aggregate
	events chan Event
}

func newDiagnostics() *diagnostics {
	return &diagnostics{values: make(map[string]aggregate), events: make(chan Event, 32)}
}
func (d *diagnostics) record(name, op, phase, outcome string, duration time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := name + ":" + op + ":" + phase + ":" + outcome
	a := d.values[key]
	a.count++
	a.duration += duration
	if time.Since(a.last) >= 5*time.Second {
		event := Event{name, op, phase, outcome, a.count, a.duration.Seconds()}
		select {
		case d.events <- event:
			a.last = time.Now()
		default:
		}
	}
	d.values[key] = a
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rc := http.NewResponseController(w)
	rc.SetReadDeadline(start.Add(2 * time.Second))
	rc.SetWriteDeadline(start.Add(2 * time.Second))
	out := &statusWriter{ResponseWriter: w}
	op := "unknown"
	phase := "unary"
	switch r.URL.Path {
	case "/v1/capabilities":
		op = "capabilities"
	case "/v1/crypto/seal":
		op = "seal"
	case "/v1/crypto/unseal":
		op = "unseal"
	case "/v1/enrollment/code":
		op = "enrollment_code"
	case "/v1/config/agent/changes":
		op = "changes"
		phase = "watch"
	}
	defer func() {
		outcome := "success"
		switch {
		case out.status == 401:
			outcome = "unauthorized"
		case out.status == 403:
			outcome = "forbidden"
		case out.status == 429:
			outcome = "exhausted"
		case out.status >= 500:
			outcome = "unavailable"
		case out.status >= 400:
			outcome = "rejected"
		}
		h.diagnostics.record("device_api_request", op, phase, outcome, time.Since(start))
		if out.status == 401 || out.status == 403 {
			h.diagnostics.record("device_api_auth", "authenticate", "check", outcome, 0)
		}
	}()
	h.serve(out, r)
}
