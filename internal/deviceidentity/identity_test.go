package deviceidentity

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCustodyAndStatement(t *testing.T) {
	root := bytes.Repeat([]byte{42}, 32)
	const issuer = "https://auth.example"
	deviceRecord, err := GenerateDevice(root)
	if err != nil {
		t.Fatal(err)
	}
	opRecord, err := GenerateOperational(root, issuer)
	if err != nil {
		t.Fatal(err)
	}
	device, err := OpenDevice(root, deviceRecord)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := OpenOperational(root, issuer, opRecord)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenOperational(root, "https://other.example", opRecord); err == nil {
		t.Fatal("operational ciphertext was accepted for a different issuer")
	}
	if _, err := OpenDevice(root, opRecord); err == nil {
		t.Fatal("operational record was accepted as IDevID")
	}
	now := time.Unix(1_800_000_000, 0)
	access := NewAccess(device, keys, issuer)
	info, err := access.Identity(now)
	if err != nil {
		t.Fatal(err)
	}
	if info.IDevID.Thumbprint != device.Public().Thumbprint || info.SigningKey == info.IDevID || info.EncryptionKey == info.SigningKey {
		t.Fatalf("invalid key separation: %+v", info)
	}
	parts := strings.Split(info.Statement, ".")
	if len(parts) != 3 {
		t.Fatal("invalid compact statement")
	}
	var header struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
		JWK       JWK    `json:"jwk"`
	}
	var claims struct {
		Issuer       string            `json:"iss"`
		Subject      string            `json:"sub"`
		Audience     string            `json:"aud"`
		IssuedAt     int64             `json:"iat"`
		Expires      int64             `json:"exp"`
		Nonce        string            `json:"jti"`
		Confirmation map[string]string `json:"cnf"`
	}
	if json.Unmarshal(b64(t, parts[0]), &header) != nil || json.Unmarshal(b64(t, parts[1]), &claims) != nil {
		t.Fatal("statement is not JSON")
	}
	if header.Algorithm != "ES256" || header.Type != "device-identity+jwt" || header.JWK != info.IDevID.JWK ||
		claims.Issuer != info.IDevID.Thumbprint || claims.Subject != claims.Issuer || claims.Audience != issuer ||
		claims.IssuedAt != now.Unix() || claims.Expires != now.Unix()+300 || len(b64(t, claims.Nonce)) != 16 ||
		claims.Confirmation["jkt"] != info.SigningKey.Thumbprint {
		t.Fatal("statement fields disagree with the public keys or enrollment")
	}
	verify(t, info.IDevID.JWK, []byte(parts[0]+"."+parts[1]), b64(t, parts[2]))

	message := []byte("opaque OAuth signing input")
	signature, err := access.Sign(message)
	if err != nil {
		t.Fatal(err)
	}
	verify(t, info.SigningKey.JWK, message, signature)
	peer, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := access.Derive(public(peer.PublicKey().Bytes()).JWK)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := info.EncryptionKey.JWK.point()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := peer.ECDH(pub)
	if err != nil || !bytes.Equal(expected, secret) {
		t.Fatal("ECDH secret differs")
	}
	invalid := info.EncryptionKey.JWK
	invalid.X, invalid.Y = strings.Repeat("A", 43), strings.Repeat("A", 43)
	if _, err := access.Derive(invalid); err == nil {
		t.Fatal("off-curve point was accepted")
	}
}

func b64(t *testing.T, value string) []byte {
	t.Helper()
	raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func verify(t *testing.T, jwk JWK, input, signature []byte) {
	t.Helper()
	if len(signature) != 64 {
		t.Fatal("unexpected signature size")
	}
	pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(b64(t, jwk.X)), Y: new(big.Int).SetBytes(b64(t, jwk.Y))}
	digest := sha256.Sum256(input)
	if !ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])) {
		t.Fatal("ES256 signature did not verify")
	}
}

func TestPublicKeyThumbprintsMatchSharedStatementVectors(t *testing.T) {
	raw, err := os.ReadFile("../../.agents/plans/SELF-14/device-api-v3.vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Statement struct {
			Device            JWK    `json:"idevid_public_jwk"`
			DeviceThumbprint  string `json:"idevid_thumbprint"`
			Signing           JWK    `json:"signing_public_jwk"`
			SigningThumbprint string `json:"signing_thumbprint"`
		} `json:"statement"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, vector := range []struct {
		key        JWK
		thumbprint string
	}{
		{vectors.Statement.Device, vectors.Statement.DeviceThumbprint},
		{vectors.Statement.Signing, vectors.Statement.SigningThumbprint},
	} {
		point, err := vector.key.point()
		if err != nil {
			t.Fatal(err)
		}
		if got := public(point.Bytes()); got.JWK != vector.key || got.Thumbprint != vector.thumbprint {
			t.Fatal("public key does not match the published thumbprint")
		}
	}
}
