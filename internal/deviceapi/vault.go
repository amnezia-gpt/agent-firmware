package deviceapi

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
)

type Error string

func (e Error) Error() string { return string(e) }

const (
	ErrKeystore   Error = "keystore_unavailable"
	ErrCiphertext Error = "ciphertext_rejected"
	ErrRequest    Error = "invalid_request"
)

const MaxPlaintext = 65536
const sealFormat = "afseal2"
const sealPrefix = sealFormat + "."
const sealOverhead = 12 + 16 // crypto/cipher prepends the random nonce to ciphertext and tag.

var installationPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var purposePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var idPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
var b64Pattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type Vault struct {
	mu           sync.RWMutex
	closed       bool
	root         [32]byte
	installation string
	capability   string
}

func NewVault(installation string, key []byte) (*Vault, error) {
	if !installationPattern.MatchString(installation) || len(key) != 32 {
		return nil, ErrKeystore
	}
	token, err := hkdf.Key(sha256.New, key, nil, "agent-firmware/socket-capability/v1\n"+installation, 32)
	if err != nil {
		return nil, ErrKeystore
	}
	defer clear(token)
	v := &Vault{installation: installation, capability: base64.RawURLEncoding.EncodeToString(token)}
	copy(v.root[:], key)
	return v, nil
}

func (v *Vault) Status() string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.closed {
		return "unavailable"
	}
	return "ready"
}

func (v *Vault) token() string { return v.capability }

func (v *Vault) aad(purpose, id string) []byte {
	data, _ := json.Marshal([]string{sealFormat, v.installation, purpose, id})
	return data
}

// Each enrollment and purpose has a distinct key. AES-GCM with random nonces
// permits at most 2^32 encryptions with each key.
func (v *Vault) aead(purpose string) (cipher.AEAD, error) {
	key, err := hkdf.Key(sha256.New, v.root[:], nil, "agent-firmware/seal/v2\n"+v.installation+"\n"+purpose, 32)
	if err != nil {
		return nil, ErrKeystore
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrKeystore
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, ErrKeystore
	}
	return aead, nil
}

func (v *Vault) Seal(ctx context.Context, purpose, id string, plain []byte) (string, error) {
	if !purposePattern.MatchString(purpose) || !idPattern.MatchString(id) || len(plain) == 0 || len(plain) > MaxPlaintext {
		return "", ErrRequest
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.closed {
		return "", ErrKeystore
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	aead, err := v.aead(purpose)
	if err != nil {
		return "", err
	}
	data := aead.Seal(nil, nil, plain, v.aad(purpose, id))
	return sealPrefix + base64.RawURLEncoding.EncodeToString(data), nil
}

func (v *Vault) Unseal(purpose, id, sealed string) ([]byte, error) {
	if !purposePattern.MatchString(purpose) || !idPattern.MatchString(id) {
		return nil, ErrRequest
	}
	encoded, ok := strings.CutPrefix(sealed, sealPrefix)
	if !ok || len(encoded) < base64.RawURLEncoding.EncodedLen(sealOverhead+1) ||
		len(encoded) > base64.RawURLEncoding.EncodedLen(sealOverhead+MaxPlaintext) {
		return nil, ErrCiphertext
	}
	data, err := decode(encoded)
	if err != nil {
		return nil, ErrCiphertext
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.closed {
		return nil, ErrKeystore
	}
	aead, err := v.aead(purpose)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, nil, data, v.aad(purpose, id))
	if err != nil {
		return nil, ErrCiphertext
	}
	return plain, nil
}

func decode(s string) ([]byte, error) {
	if !b64Pattern.MatchString(s) {
		return nil, ErrRequest
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(s)
	if err != nil {
		return nil, ErrRequest
	}
	return data, nil
}

func (v *Vault) Close() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.closed = true
	clear(v.root[:])
}
