package deviceapi

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/privatefile"
)

var purposes = []string{"gateway-session", "registration-management", "mcp-oauth"}

func supportsPurpose(p string) bool {
	for _, s := range purposes {
		if p == s {
			return true
		}
	}
	return false
}

type Vault struct {
	mu        sync.Mutex
	meta      metadata
	aead      cipher.AEAD
	root      *os.Root
	lock      *os.File
	remaining uint64
	pending   chan struct{}
	budgetErr error
	closed    bool
	persist   func(metadata) error
}

func OpenVault(stateDir, installation string, key []byte) (*Vault, error) {
	m, err := Inspect(stateDir, installation)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(key)
	if len(key) != 32 || hex.EncodeToString(digest[:]) != m.KeyDigest {
		return nil, ErrKeystore
	}
	r, err := privateRoot(filepath.Join(stateDir, host.DeviceAPIDir))
	if err != nil {
		return nil, err
	}
	lock, err := privateFile(r, "api.lock", os.O_RDWR|os.O_CREATE)
	if err != nil {
		r.Close()
		return nil, err
	}
	if privatefile.TryLock(lock) != nil {
		lock.Close()
		r.Close()
		return nil, ErrKeystore
	}
	m, err = loadMetadata(r, installation)
	if err != nil || m.KeyDigest != hex.EncodeToString(digest[:]) {
		lock.Close()
		r.Close()
		return nil, ErrKeystore
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		lock.Close()
		r.Close()
		return nil, ErrKeystore
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		lock.Close()
		r.Close()
		return nil, ErrKeystore
	}
	v := &Vault{meta: m, aead: aead, root: r, lock: lock}
	v.persist = func(m metadata) error { return atomicMetadata(r, m) }
	return v, nil
}
func (v *Vault) Status() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed || v.budgetErr != nil && v.budgetErr != ErrRotation {
		return "unavailable"
	}
	if v.meta.Reserved == maxBudget && v.remaining == 0 {
		return "read_only"
	}
	return "ready"
}
func (v *Vault) KeyID() string { return v.meta.KeyID }
func (v *Vault) token() string { return v.meta.Token }
func (v *Vault) aad(purpose, id string) []byte {
	b, _ := json.Marshal([]string{"afseal1", v.meta.Installation, "gateway", v.meta.KeyID, purpose, id})
	return b
}

func (v *Vault) permit(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		v.mu.Lock()
		if v.closed {
			v.mu.Unlock()
			return ErrKeystore
		}
		if v.remaining > 0 {
			v.remaining--
			v.mu.Unlock()
			return nil
		}
		if v.budgetErr != nil {
			err := v.budgetErr
			v.mu.Unlock()
			return err
		}
		if v.meta.Reserved == maxBudget {
			v.budgetErr = ErrRotation
			v.mu.Unlock()
			return ErrRotation
		}
		if v.pending == nil {
			v.pending = make(chan struct{})
			next := v.meta
			count := min(uint64(4096), maxBudget-next.Reserved)
			next.Reserved += count
			go func() {
				err := v.persist(next)
				v.mu.Lock()
				defer v.mu.Unlock()
				if err != nil {
					v.budgetErr = ErrKeystore
				} else {
					v.meta.Reserved = next.Reserved
					v.remaining = count
				}
				close(v.pending)
				v.pending = nil
			}()
		}
		pending := v.pending
		v.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-pending:
		}
	}
}
func (v *Vault) Seal(ctx context.Context, purpose, id string, plain []byte) (string, error) {
	if !supportsPurpose(purpose) {
		return "", ErrPurpose
	}
	if !idPattern.MatchString(id) || len(plain) == 0 || len(plain) > MaxPlaintext {
		return "", ErrRequest
	}
	if err := v.permit(ctx); err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	kid, _ := keyIDBytes(v.meta.KeyID)
	data := append(kid, v.aead.Seal(nil, nil, plain, v.aad(purpose, id))...)
	return "afseal1." + base64.RawURLEncoding.EncodeToString(data), nil
}
func (v *Vault) Unseal(purpose, id, sealed string) ([]byte, error) {
	v.mu.Lock()
	closed := v.closed
	v.mu.Unlock()
	if closed {
		return nil, ErrKeystore
	}
	if !supportsPurpose(purpose) {
		return nil, ErrPurpose
	}
	if !idPattern.MatchString(id) {
		return nil, ErrRequest
	}
	if len(sealed) < 68 || len(sealed) > 87448 || !strings.HasPrefix(sealed, "afseal1.") {
		return nil, ErrCiphertext
	}
	data, err := decode(sealed[8:])
	if err != nil || len(data) < 45 || len(data) > MaxPlaintext+44 {
		return nil, ErrCiphertext
	}
	kid, _ := keyIDBytes(v.meta.KeyID)
	if string(data[:16]) != string(kid) {
		return nil, ErrCiphertext
	}
	plain, err := v.aead.Open(nil, nil, data[16:], v.aad(purpose, id))
	if err != nil {
		return nil, ErrCiphertext
	}
	return plain, nil
}
func (v *Vault) Close() error {
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		return nil
	}
	v.closed = true
	pending := v.pending
	v.mu.Unlock()
	cleanup := func() { v.lock.Close(); v.root.Close() }
	if pending != nil {
		select {
		case <-pending:
		case <-time.After(5 * time.Second):
			go func() { <-pending; cleanup() }()
			return ErrKeystore
		}
	}
	cleanup()
	return nil
}
