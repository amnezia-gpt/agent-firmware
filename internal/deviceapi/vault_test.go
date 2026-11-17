package deviceapi

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func testVault(t *testing.T) (*Vault, []byte) {
	t.Helper()
	key := bytes.Repeat([]byte{1}, 32)
	vault, err := NewVault(strings.Repeat("1", 32), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(vault.Close)
	t.Cleanup(func() { clear(key) })
	return vault, key
}

func TestSealContextTamperAndRestart(t *testing.T) {
	vault, key := testVault(t)
	const purpose = "example-purpose"
	plain := []byte("sensitive-example")
	sealed, err := vault.Seal(t.Context(), purpose, "default", plain)
	if err != nil || !strings.HasPrefix(sealed, "afseal2.") {
		t.Fatalf("seal format or error: %v", err)
	}
	next, err := vault.Seal(t.Context(), purpose, "default", plain)
	if err != nil || next == sealed {
		t.Fatal("seal used the same nonce")
	}
	tampered := []byte(sealed)
	if tampered[40] == 'A' {
		tampered[40] = 'B'
	} else {
		tampered[40] = 'A'
	}
	for _, c := range []struct{ purpose, id, blob string }{
		{"another-purpose", "default", sealed},
		{purpose, "other", sealed},
		{purpose, "default", string(tampered)},
		{purpose, "default", "afseal1." + strings.TrimPrefix(sealed, "afseal2.")},
		{purpose, "default", "invalid"},
	} {
		opened, err := vault.Unseal(c.purpose, c.id, c.blob)
		if !errors.Is(err, ErrCiphertext) || opened != nil {
			t.Fatal("context or tamper bypass")
		}
	}
	vault.Close()

	for _, tc := range []struct {
		name, installation string
		key                []byte
		success            bool
	}{
		{"same installation", strings.Repeat("1", 32), key, true},
		{"different installation", strings.Repeat("2", 32), key, false},
		{"different root secret", strings.Repeat("1", 32), bytes.Repeat([]byte{9}, 32), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reopened, err := NewVault(tc.installation, tc.key)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			opened, err := reopened.Unseal(purpose, "default", sealed)
			if tc.success {
				if err != nil || !bytes.Equal(opened, plain) || reopened.token() != vault.token() {
					t.Fatal("restart lost encrypted data or changed the socket capability")
				}
			} else if !errors.Is(err, ErrCiphertext) || opened != nil || reopened.token() == vault.token() {
				t.Fatal("wrong installation or root secret accepted")
			}
		})
	}
}

func TestSealBoundsAndCancellation(t *testing.T) {
	vault, _ := testVault(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := vault.Seal(ctx, "example", "row", []byte("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled seal: %v", err)
	}
	if _, err := vault.Seal(t.Context(), "Invalid", "row", []byte("x")); !errors.Is(err, ErrRequest) {
		t.Fatalf("malformed purpose: %v", err)
	}
	max := make([]byte, MaxPlaintext)
	sealed, err := vault.Seal(t.Context(), "example", "row", max)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := vault.Unseal("example", "row", sealed); err != nil || !bytes.Equal(got, max) {
		t.Fatalf("maximum-length round trip: %v", err)
	}
	if _, err := vault.Seal(t.Context(), "example", "row", make([]byte, MaxPlaintext+1)); !errors.Is(err, ErrRequest) {
		t.Fatalf("oversize plaintext: %v", err)
	}
	vault.Close()
	if _, err := vault.Unseal("example", "row", sealed); !errors.Is(err, ErrKeystore) {
		t.Fatalf("closed vault: %v", err)
	}
}
