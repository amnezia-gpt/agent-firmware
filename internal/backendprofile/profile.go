// Package backendprofile validates the retained issuer metadata.
package backendprofile

import (
	"bytes"
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/amnezia-gpt/agent-firmware/internal/strictjson"
	"github.com/amnezia-gpt/agent-firmware/internal/workloadrelease"
)

const (
	MaxProfileBytes = 32 * 1024
	maxTrustBytes   = 16 * 1024
	MaxResources    = 16
)

// Error is a bounded diagnostic; it never contains input or underlying errors.
type Error string

func (e Error) Error() string { return string(e) }

const (
	ErrRead           Error = "profile_read_failed"
	ErrTooLarge       Error = "profile_too_large"
	ErrJSON           Error = "invalid_profile_json"
	ErrVersion        Error = "unsupported_profile_version"
	ErrProfileID      Error = "invalid_profile_id"
	ErrAuthIssuer     Error = "invalid_auth_issuer"
	ErrResources      Error = "invalid_protected_resources"
	ErrScopeMissing   Error = "protected_resource_scope_missing"
	ErrScopeAmbiguous Error = "protected_resource_scope_ambiguous"
	ErrTrustBundle    Error = "invalid_ingress_trust_bundle"
	ErrTrustPin       Error = "invalid_ingress_trust_pin"
)

// Profile contains normalized service coordinates and the original trust string.
type Profile struct {
	ID                 string     `json:"backend_profile"`
	AuthIssuer         string     `json:"auth_issuer"`
	Resources          []Resource `json:"protected_resources"`
	IngressTrustBundle string     `json:"ingress_trust_bundle"`
	IngressTrustSHA256 string     `json:"ingress_trust_sha256"`
}

type Resource struct {
	Identifier string   `json:"resource"`
	Scopes     []string `json:"scopes_supported"`
}

func (p Profile) ResolveScope(scope string) (string, error) {
	var result string
	for _, resource := range p.Resources {
		if !slices.Contains(resource.Scopes, scope) {
			continue
		}
		if result != "" {
			return "", ErrScopeAmbiguous
		}
		result = resource.Identifier
	}
	if result == "" {
		return "", ErrScopeMissing
	}
	return result, nil
}

func Encode(profile Profile) ([]byte, error) {
	return json.Marshal(struct {
		SchemaVersion int `json:"schema_version"`
		Profile
	}{4, profile})
}

var profileID = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)

// Parse consumes at most MaxProfileBytes+1 bytes, validates the complete profile,
// and returns no partial configuration on failure. The caller owns the reader.
func Parse(reader io.Reader) (Profile, error) {
	data, err := io.ReadAll(io.LimitReader(reader, MaxProfileBytes+1))
	if err != nil {
		return Profile{}, ErrRead
	}
	if len(data) > MaxProfileBytes {
		return Profile{}, ErrTooLarge
	}
	return parse(data)
}

func parse(data []byte) (Profile, error) {
	fields, ok := strictjson.Object(data, "schema_version", "backend_profile", "auth_issuer", "protected_resources", "ingress_trust_bundle", "ingress_trust_sha256")
	if !ok {
		return Profile{}, ErrJSON
	}
	if string(bytes.TrimSpace(fields["schema_version"])) != "4" {
		return Profile{}, ErrVersion
	}
	profile := Profile{ID: strictjson.StringField(fields, "backend_profile")}
	if !profileID.MatchString(profile.ID) {
		return Profile{}, ErrProfileID
	}
	profile.AuthIssuer, ok = httpsURL(strictjson.StringField(fields, "auth_issuer"))
	if !ok {
		return Profile{}, ErrAuthIssuer
	}
	profile.Resources, ok = resources(fields["protected_resources"])
	if !ok {
		return Profile{}, ErrResources
	}
	profile.IngressTrustBundle = strictjson.StringField(fields, "ingress_trust_bundle")
	if !validTrust(profile.IngressTrustBundle, profile.AuthIssuer) {
		return Profile{}, ErrTrustBundle
	}
	pin := strictjson.StringField(fields, "ingress_trust_sha256")
	digest := sha256.Sum256([]byte(profile.IngressTrustBundle))
	profile.IngressTrustSHA256 = hex.EncodeToString(digest[:])
	if len(pin) != 64 || strings.ToLower(pin) != profile.IngressTrustSHA256 {
		return Profile{}, ErrTrustPin
	}
	return profile, nil
}

func resources(raw []byte) ([]Resource, bool) {
	var documents []json.RawMessage
	if json.Unmarshal(raw, &documents) != nil || documents == nil || len(documents) > MaxResources {
		return nil, false
	}
	seen := make(map[string]bool, len(documents))
	result := make([]Resource, 0, len(documents))
	for _, document := range documents {
		fields, ok := strictjson.Object(document, "resource", "scopes_supported")
		if !ok {
			return nil, false
		}
		name := strictjson.StringField(fields, "resource")
		if _, valid := httpsURL(name); !valid || seen[name] {
			return nil, false
		}
		seen[name] = true
		var scopes []string
		if json.Unmarshal(fields["scopes_supported"], &scopes) != nil || scopes == nil {
			return nil, false
		}
		slices.Sort(scopes)
		for i, scope := range scopes {
			if !workloadrelease.ValidScope(scope) || i > 0 && scopes[i-1] == scope {
				return nil, false
			}
		}
		result = append(result, Resource{Identifier: name, Scopes: scopes})
	}
	slices.SortFunc(result, func(a, b Resource) int { return strings.Compare(a.Identifier, b.Identifier) })
	return result, true
}

func ValidIssuer(raw string) bool {
	normalized, ok := httpsURL(raw)
	return ok && normalized == raw
}

func httpsURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || raw == "" || u.Scheme != "https" || u.Hostname() == "" || u.Opaque != "" ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") {
		return "", false
	}
	for _, c := range raw {
		if c <= ' ' || c == 127 || c == '\\' {
			return "", false
		}
	}
	for _, c := range u.Host {
		if c > 127 || c == '%' {
			return "", false
		}
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", false
		}
	}
	u.Host = strings.ToLower(u.Host)
	// Trim a literal trailing slash only; escaped path bytes remain significant.
	if strings.HasSuffix(u.EscapedPath(), "/") {
		u.Path = strings.TrimSuffix(u.Path, "/")
		u.RawPath = strings.TrimSuffix(u.RawPath, "/")
	}
	return u.String(), true
}

func validTrust(raw, issuer string) bool {
	if len(raw) == 0 || len(raw) > maxTrustBytes || strings.HasPrefix(raw, "\ufeff") || strings.ContainsRune(raw, 0) {
		return false
	}
	fields, ok := strictjson.Object([]byte(raw), "schema_version", "issuer", "keys")
	if !ok || string(bytes.TrimSpace(fields["schema_version"])) != "1" || strictjson.StringField(fields, "issuer") != issuer {
		return false
	}
	var keys []json.RawMessage
	if json.Unmarshal(fields["keys"], &keys) != nil || len(keys) < 1 || len(keys) > 8 {
		return false
	}
	kids := make(map[string]bool, len(keys))
	for _, rawKey := range keys {
		key, ok := strictjson.Object(rawKey, "kty", "crv", "use", "alg", "kid", "x", "y")
		if !ok || strictjson.StringField(key, "kty") != "EC" || strictjson.StringField(key, "crv") != "P-256" ||
			strictjson.StringField(key, "use") != "sig" || strictjson.StringField(key, "alg") != "ES256" {
			return false
		}
		kid := strictjson.StringField(key, "kid")
		if kid == "" || len(kid) > 128 || kids[kid] {
			return false
		}
		kids[kid] = true
		point := []byte{4} // SEC 1 uncompressed P-256 point: 0x04 || x || y.
		for _, name := range []string{"x", "y"} {
			coordinate := strictjson.StringField(key, name)
			decoded, err := base64.RawURLEncoding.Strict().DecodeString(coordinate)
			if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != coordinate {
				return false
			}
			point = append(point, decoded...)
		}
		if _, err := ecdh.P256().NewPublicKey(point); err != nil {
			return false
		}
	}
	return true
}
