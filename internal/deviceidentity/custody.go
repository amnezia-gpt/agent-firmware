// Package deviceidentity holds firmware-owned device and enrollment keys.
package deviceidentity

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

var ErrCustody = errors.New("keystore_unavailable")
var ErrInput = errors.New("invalid_request")

const (
	scalarSize       = 32
	envelopePrefix   = "afkey1."
	envelopeOverhead = 28
	deviceDomain     = "idevid"
)

type Device struct {
	signing *ecdsa.PrivateKey
	public  PublicKey
}

type Operational struct {
	signing    *ecdsa.PrivateKey
	encryption *ecdh.PrivateKey
	signPublic PublicKey
	encPublic  PublicKey
}

func (d *Device) Public() PublicKey { return d.public }

func GenerateDevice(root []byte) (string, error) {
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return "", ErrCustody
	}
	scalar := key.Bytes()
	defer clear(scalar)
	return seal(root, deviceDomain, scalar)
}

func GenerateOperational(root []byte, issuer string) (string, error) {
	sign, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return "", ErrCustody
	}
	enc, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return "", ErrCustody
	}
	scalars := make([]byte, 2*scalarSize)
	signBytes, encBytes := sign.Bytes(), enc.Bytes()
	defer clear(signBytes)
	defer clear(encBytes)
	defer clear(scalars)
	copy(scalars, signBytes)
	copy(scalars[scalarSize:], encBytes)
	return seal(root, operationalDomain(issuer), scalars)
}

func OpenDevice(root []byte, sealed string) (*Device, error) {
	scalar, err := unseal(root, deviceDomain, sealed, scalarSize)
	if err != nil {
		return nil, err
	}
	defer clear(scalar)
	key, pub, err := parseSigning(scalar)
	if err != nil {
		return nil, err
	}
	return &Device{key, pub}, nil
}

func OpenOperational(root []byte, issuer, sealed string) (*Operational, error) {
	scalars, err := unseal(root, operationalDomain(issuer), sealed, 2*scalarSize)
	if err != nil {
		return nil, err
	}
	defer clear(scalars)
	key, pub, err := parseSigning(scalars[:scalarSize])
	if err != nil {
		return nil, err
	}
	enc, err := ecdh.P256().NewPrivateKey(scalars[scalarSize:])
	if err != nil {
		return nil, ErrCustody
	}
	return &Operational{key, enc, pub, public(enc.PublicKey().Bytes())}, nil
}

func parseSigning(scalar []byte) (*ecdsa.PrivateKey, PublicKey, error) {
	key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), scalar)
	if err != nil {
		return nil, PublicKey{}, ErrCustody
	}
	point, err := key.PublicKey.Bytes()
	if err != nil {
		return nil, PublicKey{}, ErrCustody
	}
	return key, public(point), nil
}

func operationalDomain(issuer string) string { return "operational\n" + issuer }

func aead(root []byte, domain string) (cipher.AEAD, error) {
	if len(root) != 32 {
		return nil, ErrCustody
	}
	key, err := hkdf.Key(sha256.New, root, nil, "agent-firmware/key-custody/v1\n"+domain, 32)
	if err != nil {
		return nil, ErrCustody
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrCustody
	}
	mode, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, ErrCustody
	}
	return mode, nil
}

func seal(root []byte, domain string, secret []byte) (string, error) {
	mode, err := aead(root, domain)
	if err != nil {
		return "", err
	}
	return envelopePrefix + base64.RawURLEncoding.EncodeToString(mode.Seal(nil, nil, secret, []byte(domain))), nil
}

func unseal(root []byte, domain, sealed string, size int) ([]byte, error) {
	encoded, ok := strings.CutPrefix(sealed, envelopePrefix)
	if !ok || len(encoded) != base64.RawURLEncoding.EncodedLen(size+envelopeOverhead) {
		return nil, ErrCustody
	}
	ciphertext, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(ciphertext) != size+envelopeOverhead {
		return nil, ErrCustody
	}
	mode, err := aead(root, domain)
	if err != nil {
		return nil, err
	}
	plain, err := mode.Open(nil, nil, ciphertext, []byte(domain))
	if err != nil {
		return nil, ErrCustody
	}
	return plain, nil
}
