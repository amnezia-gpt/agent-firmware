// Package discovery assembles the backend profile from the standard metadata
// an authorization server and its protected resources publish (RFC 8414,
// RFC 9728, RFC 7517). The issuer is the only input; every document is
// untrusted, bounded and compared with the identifier it was fetched for.
package discovery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/backendprofile"
	"github.com/amnezia-gpt/agent-firmware/internal/strictjson"
)

const (
	maxBody      = 256 << 10
	maxResources = 16
	maxKeys      = 8

	routerScope  = "responses:create"
	runtimeScope = "device.connectivity:create"

	serverWellKnown   = "/.well-known/oauth-authorization-server"
	resourceWellKnown = "/.well-known/oauth-protected-resource"
)

// Error is a bounded diagnostic; it never contains input, URLs or underlying errors.
type Error string

func (e Error) Error() string { return string(e) }

const (
	ErrIssuer      Error = "invalid_issuer"
	ErrUnavailable Error = "discovery_unavailable"
	ErrMetadata    Error = "discovery_metadata_rejected"
	ErrResources   Error = "discovery_resources_unresolved"
	ErrKeys        Error = "discovery_keys_rejected"
	ErrProfile     Error = "discovered_profile_rejected"
)

var (
	errFetch  = errors.New("fetch")
	profileID = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
)

// NewClient returns the HTTP client discovery is meant to run with.
func NewClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	return &http.Client{Transport: transport}
}

// Profile returns a backend profile v3 document for the issuer. The result is
// deterministic for unchanged metadata, so a repeated installation is a NoOp.
func Profile(ctx context.Context, client *http.Client, issuer string) ([]byte, error) {
	base, ok := identifier(issuer)
	// RFC 8414 §3.1 places the well-known segment before an issuer path; an
	// issuer with a path is not supported here, and the comparison is exact (§3.3).
	if !ok || base.Path != "" || issuer != "https://"+base.Host || base.Host != strings.ToLower(base.Host) {
		return nil, ErrIssuer
	}
	bounded := *client
	bounded.Timeout = 20 * time.Second
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	metadata, err := get(ctx, &bounded, issuer+serverWellKnown)
	if err != nil {
		return nil, ErrUnavailable
	}
	server, ok := strictjson.Members(metadata)
	if !ok || strictjson.StringField(server, "issuer") != issuer {
		return nil, ErrMetadata
	}
	resources, ok := stringList(server["protected_resources"])
	if !ok || len(resources) < 1 || len(resources) > maxResources {
		return nil, ErrMetadata
	}
	keysURL := strictjson.StringField(server, "jwks_uri")
	if _, ok := identifier(keysURL); !ok {
		return nil, ErrMetadata
	}

	var router, runtime []string
	unreachable := false
	seen := map[string]bool{}
	for _, resource := range resources {
		location, ok := identifier(resource)
		if !ok || seen[resource] {
			return nil, ErrMetadata
		}
		seen[resource] = true
		document, err := get(ctx, &bounded, "https://"+location.Host+resourceWellKnown+strings.TrimSuffix(location.EscapedPath(), "/"))
		if err != nil {
			unreachable = true
			continue
		}
		// A resource that does not name itself and this issuer is not ours to use (RFC 9728 §3.3).
		fields, ok := strictjson.Members(document)
		if !ok || strictjson.StringField(fields, "resource") != resource {
			continue
		}
		servers, ok := stringList(fields["authorization_servers"])
		if !ok || !contains(servers, issuer) {
			continue
		}
		scopes, _ := stringList(fields["scopes_supported"])
		if contains(scopes, routerScope) {
			router = append(router, resource)
		}
		if contains(scopes, runtimeScope) {
			runtime = append(runtime, resource)
		}
	}
	if len(router) != 1 || len(runtime) != 1 || router[0] == runtime[0] {
		if unreachable && len(router) <= 1 && len(runtime) <= 1 {
			return nil, ErrUnavailable
		}
		return nil, ErrResources
	}

	set, err := get(ctx, &bounded, keysURL)
	if err != nil {
		return nil, ErrUnavailable
	}
	bundle, ok := trustBundle(set, issuer)
	if !ok {
		return nil, ErrKeys
	}
	digest := sha256.Sum256(bundle)
	profile, err := json.Marshal(struct {
		SchemaVersion int    `json:"schema_version"`
		ID            string `json:"backend_profile"`
		Issuer        string `json:"auth_issuer"`
		Router        string `json:"router_base_url"`
		Runtime       string `json:"runtime_gateway_base_url"`
		Bundle        string `json:"ingress_trust_bundle"`
		Pin           string `json:"ingress_trust_sha256"`
	}{3, ProfileID(base.Hostname()), issuer, router[0], runtime[0], string(bundle), hex.EncodeToString(digest[:])})
	if err != nil {
		return nil, ErrProfile
	}
	profile = append(profile, '\n')
	if _, err := backendprofile.Parse(bytes.NewReader(profile)); err != nil {
		return nil, ErrProfile
	}
	return profile, nil
}

// ProfileID derives the opaque profile ID from the issuer host: the host itself
// when it already is a valid ID, otherwise a stable digest of it.
func ProfileID(host string) string {
	host = strings.ToLower(host)
	if profileID.MatchString(host) {
		return host
	}
	digest := sha256.Sum256([]byte(host))
	return "issuer-" + hex.EncodeToString(digest[:8])
}

type key struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// trustBundle keeps the public ES256 signature keys of the set and only their
// registered members. The profile parser validates the points and key IDs.
func trustBundle(set []byte, issuer string) ([]byte, bool) {
	fields, ok := strictjson.Members(set)
	if !ok {
		return nil, false
	}
	var raw []json.RawMessage
	if json.Unmarshal(fields["keys"], &raw) != nil || len(raw) > 64 {
		return nil, false
	}
	keys := []key{}
	for _, item := range raw {
		member, ok := strictjson.Members(item)
		if !ok {
			return nil, false
		}
		value := func(name string) string { return strictjson.StringField(member, name) }
		if value("kty") != "EC" || value("crv") != "P-256" || value("alg") != "ES256" || value("use") != "sig" {
			continue
		}
		if member["d"] != nil {
			return nil, false
		}
		keys = append(keys, key{"EC", "P-256", "sig", "ES256", value("kid"), value("x"), value("y")})
	}
	if len(keys) < 1 || len(keys) > maxKeys {
		return nil, false
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Kid < keys[j].Kid })
	bundle, err := json.Marshal(struct {
		SchemaVersion int    `json:"schema_version"`
		Issuer        string `json:"issuer"`
		Keys          []key  `json:"keys"`
	}{1, issuer, keys})
	return bundle, err == nil
}

// identifier accepts an https URL without credentials, query or fragment
// (RFC 8414 §2, RFC 9728 §1.2).
func identifier(raw string) (*url.URL, bool) {
	for _, c := range raw {
		if c <= ' ' || c >= 127 || c == '\\' {
			return nil, false
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.Opaque != "" || u.User != nil ||
		u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") || strings.Contains(u.Host, "%") {
		return nil, false
	}
	return u, true
}

func get(ctx context.Context, client *http.Client, location string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return nil, errFetch
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, errFetch
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errFetch
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil || len(data) > maxBody {
		return nil, errFetch
	}
	return data, nil
}

func stringList(raw json.RawMessage) ([]string, bool) {
	var values []string
	if raw == nil || json.Unmarshal(raw, &values) != nil {
		return nil, false
	}
	return values, true
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
