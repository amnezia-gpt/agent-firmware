// Package deviceapi implements the installation-scoped local device services.
package deviceapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/strictjson"
)

type Error string

func (e Error) Error() string { return string(e) }

const (
	ErrKeystore   Error = "keystore_unavailable"
	ErrCiphertext Error = "ciphertext_rejected"
	ErrPurpose    Error = "unsupported_purpose"
	ErrRequest    Error = "invalid_request"
	ErrRotation   Error = "key_rotation_required"
)

const MaxPlaintext = 65536
const maxBudget uint64 = 1 << 32

var installationPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var idPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
var b64Pattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var lowerHex = regexp.MustCompile(`^[a-f0-9]{64}$`)

type metadata struct {
	Schema       int    `json:"schema_version"`
	Installation string `json:"installation"`
	KeyID        string `json:"key_id"`
	KeyDigest    string `json:"key_digest"`
	Token        string `json:"capability"`
	Reserved     uint64 `json:"reserved"`
	Protection   string `json:"protection"`
}

func privateRoot(path string) (*os.Root, error) {
	path = filepath.Clean(path)
	i, err := os.Lstat(path)
	if err != nil || !i.IsDir() || !privateInfo(i, 0700) {
		return nil, ErrKeystore
	}
	r, err := os.OpenRoot(path)
	if err != nil {
		return nil, ErrKeystore
	}
	f, err := r.Open(".")
	if err != nil {
		r.Close()
		return nil, ErrKeystore
	}
	opened, err := f.Stat()
	f.Close()
	if err != nil || !os.SameFile(i, opened) {
		r.Close()
		return nil, ErrKeystore
	}
	return r, nil
}
func privateInfo(i os.FileInfo, mode os.FileMode) bool {
	s, ok := i.Sys().(*syscall.Stat_t)
	return ok && i.Mode().Perm() == mode && i.Mode()&(os.ModeSymlink|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0 && s.Uid == uint32(os.Geteuid()) && s.Gid == uint32(os.Getegid()) && (i.IsDir() || i.Mode().IsRegular() && s.Nlink == 1)
}
func privateFile(r *os.Root, name string, flags int) (*os.File, error) {
	f, err := r.OpenFile(name, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, ErrKeystore
	}
	i, err := f.Stat()
	if err != nil || !i.Mode().IsRegular() || !privateInfo(i, 0600) {
		f.Close()
		return nil, ErrKeystore
	}
	return f, nil
}
func readPrivate(r *os.Root, name string, limit int) ([]byte, error) {
	f, err := privateFile(r, name, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(limit+1)))
	if err != nil || len(b) > limit {
		return nil, ErrKeystore
	}
	return b, nil
}
func syncRoot(r *os.Root) error {
	f, err := r.Open(".")
	if err != nil {
		return ErrKeystore
	}
	defer f.Close()
	if f.Sync() != nil {
		return ErrKeystore
	}
	return nil
}
func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, ErrKeystore
	}
	return b, nil
}
func writeNew(r *os.Root, name string, data []byte) error {
	f, err := privateFile(r, name, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closed := f.Close()
	if err != nil || closed != nil {
		return ErrKeystore
	}
	return nil
}
func atomicMetadata(r *os.Root, m metadata) error {
	b, _ := json.Marshal(m)
	nonce, err := randomBytes(12)
	if err != nil {
		return err
	}
	name := ".state-" + hex.EncodeToString(nonce)
	defer r.Remove(name)
	if err := writeNew(r, name, b); err != nil {
		return err
	}
	if r.Rename(name, "state.json") != nil {
		return ErrKeystore
	}
	return syncRoot(r)
}
func keyIDBytes(id string) ([]byte, error) {
	if len(id) != 25 || id[:3] != "k1_" {
		return nil, ErrKeystore
	}
	b, err := decode(id[3:])
	if err != nil || len(b) != 16 {
		return nil, ErrKeystore
	}
	return b, nil
}
func decode(s string) ([]byte, error) {
	if !b64Pattern.MatchString(s) {
		return nil, ErrRequest
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(s)
	if err != nil {
		return nil, ErrRequest
	}
	return b, nil
}
func loadMetadata(r *os.Root, installation string) (metadata, error) {
	b, err := readPrivate(r, "state.json", 4096)
	if err != nil {
		return metadata{}, err
	}
	if _, ok := strictjson.Object(b, "schema_version", "installation", "key_id", "key_digest", "capability", "reserved", "protection"); !ok {
		return metadata{}, ErrKeystore
	}
	var m metadata
	if json.Unmarshal(b, &m) != nil || m.Schema != 1 || m.Installation != installation || !installationPattern.MatchString(installation) || m.Protection != "host" || !lowerHex.MatchString(m.KeyDigest) || m.Reserved > maxBudget {
		return metadata{}, ErrKeystore
	}
	if _, err := keyIDBytes(m.KeyID); err != nil {
		return metadata{}, ErrKeystore
	}
	token, err := decode(m.Token)
	if err != nil || len(token) != 32 {
		return metadata{}, ErrKeystore
	}
	wrapped, err := readPrivate(r, "key.cred", 65536)
	if err != nil || len(wrapped) == 0 {
		return metadata{}, ErrKeystore
	}
	return m, nil
}

// Inspect validates existing private state; it never initializes or repairs it.
func Inspect(stateDir, installation string) (metadata, error) {
	parent, err := privateRoot(stateDir)
	if err != nil {
		return metadata{}, err
	}
	defer parent.Close()
	i, err := parent.Lstat("device-api")
	if err != nil || !i.IsDir() || !privateInfo(i, 0700) {
		return metadata{}, ErrKeystore
	}
	r, err := parent.OpenRoot("device-api")
	if err != nil {
		return metadata{}, ErrKeystore
	}
	defer r.Close()
	return loadMetadata(r, installation)
}

// Initialize is an explicit bootstrap primitive. An existing directory, even
// incomplete/corrupt, is never replaced or interpreted as a new installation.
func Initialize(ctx context.Context, stateDir, installation string, wrap func(context.Context, []byte) ([]byte, error)) error {
	if !installationPattern.MatchString(installation) || wrap == nil {
		return ErrKeystore
	}
	r, err := privateRoot(stateDir)
	if err != nil {
		return err
	}
	defer r.Close()
	lock, err := privateFile(r, "device-api-init.lock", os.O_RDWR|os.O_CREATE)
	if err != nil {
		return err
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return ErrKeystore
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if _, err := r.Lstat("device-api"); err == nil {
		_, err := Inspect(stateDir, installation)
		return err
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrKeystore
	}
	key, err := randomBytes(32)
	if err != nil {
		return err
	}
	defer clear(key)
	kid, err := randomBytes(16)
	if err != nil {
		return err
	}
	token, err := randomBytes(32)
	if err != nil {
		return err
	}
	wrapped, err := wrap(ctx, key)
	if err != nil || len(wrapped) == 0 || len(wrapped) > 65536 {
		return ErrKeystore
	}
	nonce, err := randomBytes(12)
	if err != nil {
		return err
	}
	name := ".device-api-" + hex.EncodeToString(nonce)
	if r.Mkdir(name, 0700) != nil {
		return ErrKeystore
	}
	defer os.RemoveAll(filepath.Join(stateDir, name))
	tmp, err := r.OpenRoot(name)
	if err != nil {
		return ErrKeystore
	}
	defer tmp.Close()
	digest := sha256.Sum256(key)
	m := metadata{1, installation, "k1_" + base64.RawURLEncoding.EncodeToString(kid), hex.EncodeToString(digest[:]), base64.RawURLEncoding.EncodeToString(token), 0, "host"}
	b, _ := json.Marshal(m)
	if writeNew(tmp, "key.cred", wrapped) != nil || writeNew(tmp, "state.json", b) != nil || syncRoot(tmp) != nil {
		return ErrKeystore
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if r.Rename(name, "device-api") != nil {
		return ErrKeystore
	}
	return syncRoot(r)
}

type cappedWriter struct {
	b     bytes.Buffer
	limit int
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if len(p) > w.limit-w.b.Len() {
		return 0, ErrKeystore
	}
	return w.b.Write(p)
}

func WrapHost(parent context.Context, key []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/systemd-creds", "encrypt", "--with-key=host", "--name=device-api-key", "-", "-")
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "SYSTEMD_LOG_LEVEL=err"}
	cmd.Dir = "/"
	cmd.Stdin = bytes.NewReader(key)
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	out := &cappedWriter{limit: 65536}
	cmd.Stdout = out
	if cmd.Run() != nil {
		return nil, ErrKeystore
	}
	return out.b.Bytes(), nil
}
