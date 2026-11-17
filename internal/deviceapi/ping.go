package deviceapi

import (
	"sync"
	"time"
)

const h2ClientPrefaceLength = len("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")

// pingMonitor measures the most recent completed server PING. It skips all
// non-PING payloads and never exposes the opaque PING bytes.
type pingMonitor struct {
	mu      sync.Mutex
	in      pingFrames
	out     pingFrames
	sentAt  time.Time
	token   [8]byte
	lastRTT time.Duration
}

func (m *pingMonitor) observe(data []byte, outbound bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if outbound {
		m.out.feed(data, false, func(token [8]byte) {
			m.token, m.sentAt = token, time.Now()
		})
		return
	}
	m.in.feed(data, true, func(token [8]byte) {
		if !m.sentAt.IsZero() && token == m.token {
			m.lastRTT = time.Since(m.sentAt)
			m.sentAt = time.Time{}
		}
	})
}

func (m *pingMonitor) seconds() *float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastRTT <= 0 {
		return nil
	}
	seconds := m.lastRTT.Seconds()
	return &seconds
}

type pingFrames struct {
	preface     int
	header      [9]byte
	headerBytes int
	remaining   int
	isPing      bool
	token       [8]byte
}

func (f *pingFrames) feed(data []byte, ack bool, record func([8]byte)) {
	skip := min(f.preface, len(data))
	f.preface -= skip
	data = data[skip:]
	var flags byte
	if ack {
		flags = 1
	}
	for len(data) != 0 {
		if f.headerBytes < len(f.header) {
			n := copy(f.header[f.headerBytes:], data)
			f.headerBytes += n
			data = data[n:]
			if f.headerBytes < len(f.header) {
				return
			}
			h := f.header
			f.remaining = int(h[0])<<16 | int(h[1])<<8 | int(h[2])
			f.isPing = f.remaining == 8 && h[3] == 6 && h[4] == flags &&
				h[5]&0x7f == 0 && h[6] == 0 && h[7] == 0 && h[8] == 0
		}
		n := min(f.remaining, len(data))
		if f.isPing {
			copy(f.token[8-f.remaining:], data[:n])
		}
		data = data[n:]
		f.remaining -= n
		if f.remaining == 0 {
			if f.isPing {
				record(f.token)
			}
			f.headerBytes = 0
		}
	}
}
