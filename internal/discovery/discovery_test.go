package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amnezia-gpt/agent-firmware/internal/backendprofile"
)

const (
	routerScope  = "example:primary"
	runtimeScope = "example:secondary"
	keyX         = "axfR8uEsQkf4vOblY6RA8ncDfYEt6zOg9KE5RdiYwpY"
	keyY         = "T-NC4v4af5uO5-tKfA-eFivOM1drMV7Oy7ZAaDe_UfU"
)

// backend serves the documents of one issuer; a test replaces single documents.
type backend struct {
	server *httptest.Server
	pages  map[string]string
	hits   map[string]int
}

func newBackend(t *testing.T) *backend {
	t.Helper()
	b := &backend{pages: map[string]string{}, hits: map[string]int{}}
	b.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.hits[r.URL.Path]++
		body, ok := b.pages[r.URL.Path]
		if strings.HasPrefix(body, "redirect:") {
			http.Redirect(w, r, strings.TrimPrefix(body, "redirect:"), http.StatusFound)
			return
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(b.server.Close)
	issuer := b.server.URL
	b.pages[serverWellKnown] = `{"issuer":"` + issuer + `","token_endpoint":"` + issuer + `/oauth/token","jwks_uri":"` + issuer + `/oauth/jwks","protected_resources":["` + issuer + `/v1","` + issuer + `","` + issuer + `/mcp"]}`
	b.pages[resourceWellKnown+"/v1"] = resource(issuer+"/v1", issuer, "models:read", routerScope)
	b.pages[resourceWellKnown] = resource(issuer, issuer, runtimeScope, "device.connectivity:delete")
	b.pages[resourceWellKnown+"/mcp"] = resource(issuer+"/mcp", issuer, "runtime.tools:execute")
	b.pages["/oauth/jwks"] = `{"keys":[` +
		`{"kty":"RSA","use":"sig","alg":"RS256","kid":"id-token","n":"AQAB","e":"AQAB"},` +
		`{"kty":"EC","crv":"P-256","use":"sig","alg":"ES256","kid":"ingress","x":"` + keyX + `","y":"` + keyY + `","x5t":"ignored"}]}`
	return b
}

func resource(identifier, issuer string, scopes ...string) string {
	data, _ := json.Marshal(map[string]any{
		"resource": identifier, "authorization_servers": []string{issuer},
		"scopes_supported": scopes, "bearer_methods_supported": []string{"header"},
	})
	return string(data)
}

func (b *backend) profile() ([]byte, error) {
	return Profile(context.Background(), b.server.Client(), b.server.URL)
}

func TestProfileIsAssembledFromStandardMetadata(t *testing.T) {
	b := newBackend(t)
	first, err := b.profile()
	if err != nil {
		t.Fatal(err)
	}
	profile, err := backendprofile.Parse(bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	issuer := b.server.URL
	if profile.AuthIssuer != issuer || len(profile.Resources) != 3 {
		t.Fatalf("unexpected profile: %+v", profile)
	}
	for scope, want := range map[string]string{routerScope: issuer + "/v1", runtimeScope: issuer, "runtime.tools:execute": issuer + "/mcp"} {
		if got, err := profile.ResolveScope(scope); err != nil || got != want {
			t.Fatalf("unexpected resource: %s %v", got, err)
		}
	}
	if !strings.HasPrefix(profile.ID, "issuer-") {
		t.Fatalf("profile ID %q", profile.ID)
	}
	bundle := profile.IngressTrustBundle
	if !strings.Contains(bundle, `"kid":"ingress"`) || strings.Contains(bundle, "id-token") || strings.Contains(bundle, "x5t") {
		t.Fatalf("bundle %s", bundle)
	}
	second, err := b.profile()
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("not deterministic: %v", err)
	}
}

func TestProfileIDFromIssuerHost(t *testing.T) {
	if got := ProfileID("Auth.Example"); got != "auth.example" {
		t.Fatal(got)
	}
	for _, host := range []string{"127.0.0.1", "1auth.example", strings.Repeat("a", 65)} {
		if got := ProfileID(host); len(got) != len("issuer-")+16 || !profileID.MatchString(got) {
			t.Fatalf("%s -> %s", host, got)
		}
	}
}

func TestIssuerIsRefusedBeforeAnyRequest(t *testing.T) {
	b := newBackend(t)
	host := strings.TrimPrefix(b.server.URL, "https://")
	for _, issuer := range []string{
		"", "http://" + host, "https://" + host + "/", "https://" + host + "/auth", "https://user@" + host,
		"https://" + host + "?x=1", "https://" + host + "#x", "https://" + strings.ToUpper("localhost") + ":1", "https://" + host + " ",
	} {
		if _, err := Profile(context.Background(), b.server.Client(), issuer); err != ErrIssuer {
			t.Fatalf("%q: %v", issuer, err)
		}
	}
	if len(b.hits) != 0 {
		t.Fatalf("requests were made: %v", b.hits)
	}
}

func TestUntrustedDocumentsAreRejected(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(b *backend, issuer string)
		want   error
	}{
		{"metadata missing", func(b *backend, _ string) { delete(b.pages, serverWellKnown) }, ErrUnavailable},
		{"metadata redirect is not followed", func(b *backend, _ string) { b.pages[serverWellKnown] = "redirect:/elsewhere" }, ErrUnavailable},
		{"metadata too large", func(b *backend, _ string) {
			b.pages[serverWellKnown] = `{"pad":"` + strings.Repeat("a", maxBody) + `"}`
		}, ErrUnavailable},
		{"issuer differs", func(b *backend, issuer string) {
			b.pages[serverWellKnown] = strings.Replace(b.pages[serverWellKnown], `"issuer":"`+issuer+`"`, `"issuer":"`+issuer+`/"`, 1)
		}, ErrMetadata},
		{"duplicate member", func(b *backend, issuer string) {
			b.pages[serverWellKnown] = `{"issuer":"https://other.example",` + b.pages[serverWellKnown][1:]
		}, ErrMetadata},
		{"plain http resource", func(b *backend, issuer string) {
			b.pages[serverWellKnown] = strings.Replace(b.pages[serverWellKnown], `"`+issuer+`/mcp"`, `"http://plain.example/mcp"`, 1)
		}, ErrMetadata},
		{"plain http key set", func(b *backend, issuer string) {
			b.pages[serverWellKnown] = strings.Replace(b.pages[serverWellKnown], issuer+"/oauth/jwks", "http://plain.example/jwks", 1)
		}, ErrMetadata},
		{"resource names another identifier", func(b *backend, issuer string) {
			b.pages[resourceWellKnown+"/v1"] = resource(issuer+"/v2", issuer, routerScope)
		}, ErrResources},
		{"resource does not trust the issuer", func(b *backend, issuer string) {
			b.pages[resourceWellKnown] = resource(issuer, "https://other.example", runtimeScope)
		}, ErrResources},
		{"needed resource unreachable", func(b *backend, _ string) { delete(b.pages, resourceWellKnown) }, ErrUnavailable},
		{"key set missing", func(b *backend, _ string) { delete(b.pages, "/oauth/jwks") }, ErrUnavailable},
		{"no ingress-shaped key", func(b *backend, _ string) {
			b.pages["/oauth/jwks"] = `{"keys":[{"kty":"RSA","use":"sig","alg":"RS256","kid":"id-token","n":"AQAB","e":"AQAB"}]}`
		}, ErrKeys},
		{"private key published", func(b *backend, _ string) {
			b.pages["/oauth/jwks"] = strings.Replace(b.pages["/oauth/jwks"], `"x5t":"ignored"`, `"d":"AQAB"`, 1)
		}, ErrKeys},
		{"invalid point", func(b *backend, _ string) {
			b.pages["/oauth/jwks"] = strings.Replace(b.pages["/oauth/jwks"], keyY, keyX, 1)
		}, ErrProfile},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := newBackend(t)
			test.change(b, b.server.URL)
			profile, err := b.profile()
			if err != test.want || profile != nil {
				t.Fatalf("got %v, want %v", err, test.want)
			}
			if strings.Contains(err.Error(), b.server.URL) {
				t.Fatal("error carries input")
			}
		})
	}
}

func TestIncompleteResourcesAreNotRetained(t *testing.T) {
	b := newBackend(t)
	delete(b.pages, resourceWellKnown+"/mcp")
	if _, err := b.profile(); err != ErrUnavailable {
		t.Fatalf("missing advertised resource: %v", err)
	}
}

func TestRefreshPreservesPinnedKeysAndRetainsAmbiguousScopes(t *testing.T) {
	b := newBackend(t)
	initial, err := b.profile()
	if err != nil {
		t.Fatal(err)
	}
	profile, err := backendprofile.Parse(bytes.NewReader(initial))
	if err != nil {
		t.Fatal(err)
	}
	b.pages[resourceWellKnown+"/mcp"] = resource(b.server.URL+"/mcp", b.server.URL, routerScope)
	delete(b.pages, "/oauth/jwks")
	refreshed, err := RefreshResources(t.Context(), b.server.Client(), profile)
	if err != nil {
		t.Fatal(err)
	}
	next, err := backendprofile.Parse(bytes.NewReader(refreshed))
	if err != nil {
		t.Fatal(err)
	}
	if next.IngressTrustBundle != profile.IngressTrustBundle || next.IngressTrustSHA256 != profile.IngressTrustSHA256 || b.hits["/oauth/jwks"] != 1 {
		t.Fatal("refresh changed the pinned trust")
	}
	if _, err := next.ResolveScope(routerScope); err != backendprofile.ErrScopeAmbiguous {
		t.Fatalf("ambiguous scope: %v", err)
	}
}
