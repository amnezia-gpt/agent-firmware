package deviceidentity

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"time"
)

const MaxSignInput = 8192

// Keep these fields in RFC 7638 member order for the thumbprint.
type JWK struct {
	Curve string `json:"crv"`
	Type  string `json:"kty"`
	X     string `json:"x"`
	Y     string `json:"y"`
}

type PublicKey struct {
	JWK        JWK    `json:"jwk"`
	Thumbprint string `json:"thumbprint"`
}

type Identity struct {
	IDevID        PublicKey `json:"idevid"`
	SigningKey    PublicKey `json:"signing_key"`
	EncryptionKey PublicKey `json:"encryption_key"`
	Statement     string    `json:"statement"`
}

type Access struct {
	device *Device
	keys   *Operational
	issuer string
}

func NewAccess(device *Device, keys *Operational, issuer string) *Access {
	return &Access{device, keys, issuer}
}

func (a *Access) Identity(now time.Time) (Identity, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return Identity{}, ErrCustody
	}
	device, signing := a.device.public, a.keys.signPublic
	header, err := json.Marshal(struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
		JWK       JWK    `json:"jwk"`
	}{"ES256", "device-identity+jwt", device.JWK})
	if err != nil {
		return Identity{}, ErrCustody
	}
	claims, err := json.Marshal(struct {
		Issuer       string            `json:"iss"`
		Subject      string            `json:"sub"`
		Audience     string            `json:"aud"`
		IssuedAt     int64             `json:"iat"`
		Expires      int64             `json:"exp"`
		Nonce        string            `json:"jti"`
		Confirmation map[string]string `json:"cnf"`
	}{device.Thumbprint, device.Thumbprint, a.issuer, now.Unix(), now.Unix() + 300,
		base64.RawURLEncoding.EncodeToString(nonce[:]), map[string]string{"jkt": signing.Thumbprint}})
	if err != nil {
		return Identity{}, ErrCustody
	}
	input := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	if len(input)+87 > 2048 {
		return Identity{}, ErrCustody
	}
	signature, err := sign(a.device.signing, []byte(input))
	if err != nil {
		return Identity{}, err
	}
	return Identity{device, signing, a.keys.encPublic, input + "." + base64.RawURLEncoding.EncodeToString(signature)}, nil
}

func (a *Access) Sign(input []byte) ([]byte, error) {
	if len(input) == 0 || len(input) > MaxSignInput {
		return nil, ErrInput
	}
	return sign(a.keys.signing, input)
}

func sign(key *ecdsa.PrivateKey, input []byte) ([]byte, error) {
	digest := sha256.Sum256(input)
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return nil, ErrCustody
	}
	raw := make([]byte, 64)
	r.FillBytes(raw[:32])
	s.FillBytes(raw[32:])
	return raw, nil
}

func (a *Access) Derive(peer JWK) ([]byte, error) {
	key, err := peer.point()
	if err != nil {
		return nil, err
	}
	secret, err := a.keys.encryption.ECDH(key)
	if err != nil {
		return nil, ErrInput
	}
	return secret, nil
}

func (j JWK) point() (*ecdh.PublicKey, error) {
	if j.Type != "EC" || j.Curve != "P-256" || len(j.X) != 43 || len(j.Y) != 43 {
		return nil, ErrInput
	}
	x, errX := base64.RawURLEncoding.Strict().DecodeString(j.X)
	y, errY := base64.RawURLEncoding.Strict().DecodeString(j.Y)
	if errX != nil || errY != nil || len(x) != 32 || len(y) != 32 {
		return nil, ErrInput
	}
	var point [65]byte
	point[0] = 4
	copy(point[1:33], x)
	copy(point[33:], y)
	key, err := ecdh.P256().NewPublicKey(point[:])
	if err != nil {
		return nil, ErrInput
	}
	return key, nil
}

func public(point []byte) PublicKey {
	jwk := JWK{"P-256", "EC", base64.RawURLEncoding.EncodeToString(point[1:33]), base64.RawURLEncoding.EncodeToString(point[33:])}
	canonical, _ := json.Marshal(jwk)
	digest := sha256.Sum256(canonical)
	return PublicKey{jwk, base64.RawURLEncoding.EncodeToString(digest[:])}
}
