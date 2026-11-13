package deviceapi

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testVault(t *testing.T) (*Vault, string, []byte) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	var key []byte
	if err := Initialize(context.Background(), dir, strings.Repeat("1", 32), func(_ context.Context, k []byte) ([]byte, error) {
		key = bytes.Clone(k)
		return append([]byte("test-wrapper:"), k...), nil
	}); err != nil {
		t.Fatal("initialize:", err)
	}
	v, err := OpenVault(dir, strings.Repeat("1", 32), key)
	if err != nil {
		t.Fatal("open:", err)
	}
	t.Cleanup(func() { v.Close() })
	return v, dir, key
}

func TestSealContextTamperAndRestart(t *testing.T) {
	v, dir, key := testVault(t)
	sealed, err := v.Seal(context.Background(), "gateway-session", "default", []byte("sensitive-example"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "sensitive-example") {
		t.Fatal("plaintext returned")
	}
	tampered := []byte(sealed)
	if tampered[40] == 'A' {
		tampered[40] = 'B'
	} else {
		tampered[40] = 'A'
	}
	for _, c := range []struct {
		purpose, id, blob string
		good              bool
	}{
		{"gateway-session", "default", sealed, true},
		{"mcp-oauth", "default", sealed, false},
		{"gateway-session", "other", sealed, false},
		{"gateway-session", "default", string(tampered), false},
		{"gateway-session", "default", "invalid", false},
	} {
		plain, err := v.Unseal(c.purpose, c.id, c.blob)
		if c.good {
			if err != nil || string(plain) != "sensitive-example" {
				t.Fatal("roundtrip failed")
			}
		} else if !errors.Is(err, ErrCiphertext) || plain != nil {
			t.Fatal("context/tamper bypass")
		}
	}
	first, err := Inspect(dir, strings.Repeat("1", 32))
	if err != nil {
		t.Fatal(err)
	}
	v.Close()
	reopened, err := OpenVault(dir, strings.Repeat("1", 32), key)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	plain, err := reopened.Unseal("gateway-session", "default", sealed)
	if err != nil || string(plain) != "sensitive-example" {
		t.Fatal("restart lost ciphertext")
	}
	if _, err := reopened.Seal(context.Background(), "gateway-session", "default", []byte("new")); err != nil {
		t.Fatal(err)
	}
	second, _ := Inspect(dir, strings.Repeat("1", 32))
	if second.Reserved <= first.Reserved || second.KeyID != first.KeyID || second.Token != first.Token {
		t.Fatal("budget or identity reset")
	}
}

func TestMissingCorruptAndUnsafeKeyAreNotRegenerated(t *testing.T) {
	v, dir, key := testVault(t)
	v.Close()
	called := false
	wrap := func(context.Context, []byte) ([]byte, error) { called = true; return nil, errors.New("must not wrap") }
	if err := Initialize(context.Background(), dir, strings.Repeat("1", 32), wrap); err != nil || called {
		t.Fatal("existing key regenerated")
	}
	if _, err := OpenVault(dir, strings.Repeat("1", 32), bytes.Repeat([]byte{9}, 32)); !errors.Is(err, ErrKeystore) {
		t.Fatal("wrong loaded key accepted")
	}
	if _, err := OpenVault(dir, strings.Repeat("2", 32), key); !errors.Is(err, ErrKeystore) {
		t.Fatal("foreign installation accepted")
	}
	if err := os.Remove(filepath.Join(dir, "device-api", "key.cred")); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(context.Background(), dir, strings.Repeat("1", 32), wrap); !errors.Is(err, ErrKeystore) || called {
		t.Fatal("missing key regenerated")
	}
}

func TestBudgetReservationCancellationHasOneOwnedWorker(t *testing.T) {
	v, _, _ := testVault(t)
	block := make(chan struct{})
	var calls atomic.Int32
	real := v.persist
	v.persist = func(m metadata) error { calls.Add(1); <-block; return real(m) }
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := v.Seal(ctx, "mcp-oauth", "id", []byte("x")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel2()
	if _, err := v.Seal(ctx2, "mcp-oauth", "id", []byte("x")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("reservation workers multiplied")
	}
	close(block)
	if _, err := v.Seal(context.Background(), "mcp-oauth", "id", []byte("x")); err != nil {
		t.Fatal(err)
	}
}

func TestGCMExhaustionStopsSealingButPreservesUnseal(t *testing.T) {
	v, dir, key := testVault(t)
	v.meta.Reserved = maxBudget - 1
	blob, err := v.Seal(t.Context(), "mcp-oauth", "id", []byte("last"))
	if err != nil {
		t.Fatal(err)
	}
	if v.Status() != "read_only" {
		t.Fatal("exhaustion not advertised")
	}
	if _, err := v.Seal(t.Context(), "mcp-oauth", "id", []byte("extra")); !errors.Is(err, ErrRotation) {
		t.Fatal("GCM budget exceeded")
	}
	if b, err := v.Unseal("mcp-oauth", "id", blob); err != nil || string(b) != "last" {
		t.Fatal("exhaustion broke reads")
	}
	v.Close()
	reopened, err := OpenVault(dir, strings.Repeat("1", 32), key)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.Seal(t.Context(), "mcp-oauth", "id", []byte("extra")); !errors.Is(err, ErrRotation) {
		t.Fatal("restart reset exhaustion")
	}
}
func TestUnsafeMetadataAndWrapFailureNeverPublishNewState(t *testing.T) {
	for _, mode := range []string{"symlink", "mode", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			v, dir, _ := testVault(t)
			v.Close()
			path := filepath.Join(dir, "device-api", "state.json")
			switch mode {
			case "symlink":
				b, _ := os.ReadFile(path)
				os.WriteFile(filepath.Join(dir, "foreign"), b, 0600)
				os.Remove(path)
				os.Symlink("../foreign", path)
			case "mode":
				os.Chmod(path, 0644)
			case "corrupt":
				os.WriteFile(path, []byte(`{"schema_version":1}`), 0600)
			}
			if _, err := Inspect(dir, strings.Repeat("1", 32)); !errors.Is(err, ErrKeystore) {
				t.Fatal("unsafe metadata accepted")
			}
			called := false
			if err := Initialize(t.Context(), dir, strings.Repeat("1", 32), func(context.Context, []byte) ([]byte, error) { called = true; return nil, nil }); !errors.Is(err, ErrKeystore) || called {
				t.Fatal("unsafe state replaced")
			}
		})
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	if err := Initialize(t.Context(), dir, strings.Repeat("1", 32), func(context.Context, []byte) ([]byte, error) { return nil, errors.New("private-example") }); !errors.Is(err, ErrKeystore) {
		t.Fatal("wrap diagnostic escaped")
	}
	if _, err := os.Lstat(filepath.Join(dir, "device-api")); !os.IsNotExist(err) {
		t.Fatal("incomplete key published")
	}
}
