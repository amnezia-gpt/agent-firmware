package backendprofile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile("testdata/profile-v3.json")
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

func encode(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func trustFields(t *testing.T, profile map[string]any) map[string]any {
	t.Helper()
	var fields map[string]any
	if err := json.Unmarshal([]byte(profile["ingress_trust_bundle"].(string)), &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

func setTrust(profile map[string]any, trust string) {
	profile["ingress_trust_bundle"] = trust
	digest := sha256.Sum256([]byte(trust))
	profile["ingress_trust_sha256"] = hex.EncodeToString(digest[:])
}

func TestParsePreservesTrustBytesAndNormalizesCoordinates(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		t.Run(strings.TrimSpace(hex.EncodeToString([]byte(ending))), func(t *testing.T) {
			fields := fixture(t)
			trust := strings.TrimSpace(fields["ingress_trust_bundle"].(string)) + ending
			setTrust(fields, trust)
			fields["backend_profile"] = "another.profile-2"
			fields["auth_issuer"] = "HTTPS://AUTH.EXAMPLE/"
			fields["router_base_url"] = "https://ROUTER.EXAMPLE/v1/"
			fields["runtime_gateway_base_url"] = "https://RUNTIME.EXAMPLE/"
			fields["ingress_trust_sha256"] = strings.ToUpper(fields["ingress_trust_sha256"].(string))
			got, err := Parse(bytes.NewReader(encode(t, fields)))
			if err != nil {
				t.Fatal(err)
			}
			if got.ID != "another.profile-2" || got.AuthIssuer != "https://auth.example" ||
				got.RouterBaseURL != "https://router.example/v1" || got.RuntimeGatewayBaseURL != "https://runtime.example" {
				t.Fatalf("unexpected normalized coordinates: %#v", got)
			}
			if got.IngressTrustBundle != trust {
				t.Fatal("trust bytes changed")
			}
			if got.IngressTrustSHA256 != strings.ToLower(fields["ingress_trust_sha256"].(string)) {
				t.Fatal("pin was not normalized")
			}
		})
	}
}

func TestParseRejectsInvalidProfile(t *testing.T) {
	tests := []struct {
		name string
		edit func(map[string]any)
		want error
	}{
		{"unsupported version", func(p map[string]any) { p["schema_version"] = 4 }, ErrVersion},
		{"missing version", func(p map[string]any) { delete(p, "schema_version") }, ErrJSON},
		{"string version", func(p map[string]any) { p["schema_version"] = "3" }, ErrVersion},
		{"null version", func(p map[string]any) { p["schema_version"] = nil }, ErrVersion},
		{"unknown field", func(p map[string]any) { p["device_token"] = "sensitive-example" }, ErrJSON},
		{"case alias", func(p map[string]any) { p["Auth_Issuer"] = p["auth_issuer"]; delete(p, "auth_issuer") }, ErrJSON},
		{"invalid id", func(p map[string]any) { p["backend_profile"] = "../other" }, ErrProfileID},
		{"long id", func(p map[string]any) { p["backend_profile"] = strings.Repeat("a", 65) }, ErrProfileID},
		{"null id", func(p map[string]any) { p["backend_profile"] = nil }, ErrProfileID},
		{"http issuer", func(p map[string]any) { p["auth_issuer"] = "http://auth.example" }, ErrAuthIssuer},
		{"relative issuer", func(p map[string]any) { p["auth_issuer"] = "/auth" }, ErrAuthIssuer},
		{"issuer userinfo", func(p map[string]any) { p["auth_issuer"] = "https://user:secret@auth.example" }, ErrAuthIssuer},
		{"issuer query", func(p map[string]any) { p["auth_issuer"] = "https://auth.example?" }, ErrAuthIssuer},
		{"issuer fragment", func(p map[string]any) { p["auth_issuer"] = "https://auth.example#" }, ErrAuthIssuer},
		{"issuer whitespace", func(p map[string]any) { p["auth_issuer"] = " https://auth.example" }, ErrAuthIssuer},
		{"hostless issuer", func(p map[string]any) { p["auth_issuer"] = "https:///auth" }, ErrAuthIssuer},
		{"non ascii host", func(p map[string]any) { p["auth_issuer"] = "https://例.example" }, ErrAuthIssuer},
		{"invalid port", func(p map[string]any) { p["auth_issuer"] = "https://auth.example:99999" }, ErrAuthIssuer},
		{"router path", func(p map[string]any) { p["router_base_url"] = "https://router.example/v2" }, ErrRouterURL},
		{"router escaped path", func(p map[string]any) { p["router_base_url"] = "https://router.example/%76%31" }, ErrRouterURL},
		{"runtime v1", func(p map[string]any) { p["runtime_gateway_base_url"] = "https://runtime.example/v1/" }, ErrRuntimeURL},
		{"runtime nested v1", func(p map[string]any) { p["runtime_gateway_base_url"] = "https://runtime.example/prefix/v1//" }, ErrRuntimeURL},
		{"runtime encoded v1", func(p map[string]any) { p["runtime_gateway_base_url"] = "https://runtime.example/%76%31" }, ErrRuntimeURL},
		{"wrong pin", func(p map[string]any) { p["ingress_trust_sha256"] = strings.Repeat("0", 64) }, ErrTrustPin},
		{"short pin", func(p map[string]any) { p["ingress_trust_sha256"] = "01" }, ErrTrustPin},
		{"trust reformat", func(p map[string]any) { p["ingress_trust_bundle"] = p["ingress_trust_bundle"].(string) + " " }, ErrTrustPin},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fields := fixture(t)
			test.edit(fields)
			got, err := Parse(bytes.NewReader(encode(t, fields)))
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if got != (Profile{}) {
				t.Fatal("invalid input returned a partial profile")
			}
		})
	}
}

func TestParseRejectsInvalidPublicTrust(t *testing.T) {
	tests := []struct {
		name string
		edit func(map[string]any)
	}{
		{"issuer mismatch", func(b map[string]any) { b["issuer"] = "https://other.example" }},
		{"schema version", func(b map[string]any) { b["schema_version"] = 2 }},
		{"null keys", func(b map[string]any) { b["keys"] = nil }},
		{"no keys", func(b map[string]any) { b["keys"] = []any{} }},
		{"unknown trust member", func(b map[string]any) { b["extra"] = true }},
		{"duplicate kid", func(b map[string]any) { b["keys"] = append(b["keys"].([]any), b["keys"].([]any)[0]) }},
		{"symmetric key", func(b map[string]any) { b["keys"].([]any)[0].(map[string]any)["kty"] = "oct" }},
		{"private member", func(b map[string]any) { b["keys"].([]any)[0].(map[string]any)["d"] = "private-example" }},
		{"wrong curve", func(b map[string]any) { b["keys"].([]any)[0].(map[string]any)["crv"] = "P-384" }},
		{"wrong algorithm", func(b map[string]any) { b["keys"].([]any)[0].(map[string]any)["alg"] = "HS256" }},
		{"wrong use", func(b map[string]any) { b["keys"].([]any)[0].(map[string]any)["use"] = "enc" }},
		{"empty kid", func(b map[string]any) { b["keys"].([]any)[0].(map[string]any)["kid"] = "" }},
		{"long utf8 kid", func(b map[string]any) { b["keys"].([]any)[0].(map[string]any)["kid"] = strings.Repeat("я", 65) }},
		{"padded coordinate", func(b map[string]any) { k := b["keys"].([]any)[0].(map[string]any); k["x"] = k["x"].(string) + "=" }},
		{"short coordinate", func(b map[string]any) { b["keys"].([]any)[0].(map[string]any)["x"] = "AA" }},
		{"off curve", func(b map[string]any) { b["keys"].([]any)[0].(map[string]any)["x"] = strings.Repeat("A", 43) }},
		{"coordinate whitespace", func(b map[string]any) { k := b["keys"].([]any)[0].(map[string]any); k["x"] = k["x"].(string) + "\n" }},
		{"non canonical coordinate", func(b map[string]any) {
			k := b["keys"].([]any)[0].(map[string]any)
			x := k["x"].(string)
			k["x"] = x[:42] + "Z"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := fixture(t)
			trust := trustFields(t, profile)
			test.edit(trust)
			setTrust(profile, string(encode(t, trust)))
			if _, err := Parse(bytes.NewReader(encode(t, profile))); !errors.Is(err, ErrTrustBundle) {
				t.Fatalf("error = %v, want %v", err, ErrTrustBundle)
			}
		})
	}
}

func TestParseRejectsAmbiguousJSON(t *testing.T) {
	data := string(encode(t, fixture(t)))
	for name, input := range map[string]string{
		"duplicate":          strings.Replace(data, `"schema_version":3`, `"schema_version":3,"schema_version":3`, 1),
		"escaped duplicate":  strings.Replace(data, `"schema_version":3`, `"schema_version":3,"schema_vers\u0069on":3`, 1),
		"trailing document":  data + `{}`,
		"array":              `[]`,
		"null":               `null`,
		"invalid utf8":       strings.Replace(data, "auth.example", "auth.\xffexample", 1),
		"unpaired surrogate": strings.Replace(data, `"backend_profile":"example"`, `"backend_profile":"\ud800"`, 1),
		"truncated":          data[:len(data)-1],
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(input)); !errors.Is(err, ErrJSON) {
				t.Fatalf("error = %v, want %v", err, ErrJSON)
			}
		})
	}
}

func TestParseRejectsMalformedTrustEvenWithMatchingPin(t *testing.T) {
	profile := fixture(t)
	trust := profile["ingress_trust_bundle"].(string)
	for name, raw := range map[string]string{
		"duplicate trust field": strings.Replace(trust, `"schema_version": 1`, `"schema_version":1,"schema_version":1`, 1),
		"duplicate key field":   strings.Replace(trust, `"kid": "example-key"`, `"kid":"example-key","kid":"example-key"`, 1),
		"bom":                   "\ufeff" + trust,
		"nul":                   trust + "\x00",
		"oversize":              trust + strings.Repeat(" ", 16*1024),
		"trailing":              trust + `{}`,
		"unpaired surrogate":    strings.Replace(trust, "example-key", `\udfff`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			setTrust(profile, raw)
			if _, err := Parse(bytes.NewReader(encode(t, profile))); !errors.Is(err, ErrTrustBundle) {
				t.Fatalf("error = %v, want %v", err, ErrTrustBundle)
			}
		})
	}
}

func TestParseReadLimit(t *testing.T) {
	data := encode(t, fixture(t))
	padded := append(data, bytes.Repeat([]byte(" "), 32*1024-len(data))...)
	if _, err := Parse(bytes.NewReader(padded)); err != nil {
		t.Fatalf("exact limit rejected: %v", err)
	}
	reader := &io.LimitedReader{R: bytes.NewReader(append(padded, ' ', ' ')), N: 32*1024 + 2}
	if _, err := Parse(reader); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("error = %v, want %v", err, ErrTooLarge)
	}
	if reader.N != 1 {
		t.Fatal("parser read beyond the size-detection byte")
	}
}

type failedReader struct{}

func (failedReader) Read([]byte) (int, error) { return 0, errors.New("sensitive read failure") }

func TestParseReadFailureIsRedacted(t *testing.T) {
	_, err := Parse(failedReader{})
	if !errors.Is(err, ErrRead) || err.Error() != "profile_read_failed" {
		t.Fatalf("unexpected diagnostic: %v", err)
	}
}

func TestParseTrustKeyLimits(t *testing.T) {
	for _, count := range []int{1, 8, 9} {
		t.Run(strings.Repeat("k", count), func(t *testing.T) {
			profile := fixture(t)
			trust := trustFields(t, profile)
			original := trust["keys"].([]any)[0].(map[string]any)
			keys := make([]any, count)
			for i := range keys {
				key := make(map[string]any)
				for name, value := range original {
					key[name] = value
				}
				key["kid"] = strings.Repeat("k", i+1)
				keys[i] = key
			}
			trust["keys"] = keys
			setTrust(profile, string(encode(t, trust)))
			_, err := Parse(bytes.NewReader(encode(t, profile)))
			if count <= 8 && err != nil || count > 8 && !errors.Is(err, ErrTrustBundle) {
				t.Fatalf("key count %d: %v", count, err)
			}
		})
	}
}

func TestParseRejectsInvalidIPLiteral(t *testing.T) {
	profile := fixture(t)
	profile["runtime_gateway_base_url"] = "https://[invalid]/"
	if _, err := Parse(bytes.NewReader(encode(t, profile))); !errors.Is(err, ErrRuntimeURL) {
		t.Fatalf("invalid bracketed IP literal: error = %v, want %v", err, ErrRuntimeURL)
	}
}

func TestParseAcceptsTrustBoundaryAndUnicodeKid(t *testing.T) {
	profile := fixture(t)
	trust := profile["ingress_trust_bundle"].(string)
	trust = strings.Replace(trust, "example-key", `\ud83d\ude00`, 1)
	trust += strings.Repeat(" ", 16*1024-len(trust))
	setTrust(profile, trust)
	got, err := Parse(bytes.NewReader(encode(t, profile)))
	if err != nil || got.IngressTrustBundle != trust {
		t.Fatalf("valid 16 KiB trust with surrogate-pair kid: %v", err)
	}
	setTrust(profile, trust+" ")
	if _, err := Parse(bytes.NewReader(encode(t, profile))); !errors.Is(err, ErrTrustBundle) {
		t.Fatalf("oversized trust: %v", err)
	}
}

func FuzzParse(f *testing.F) {
	data, err := os.ReadFile("testdata/profile-v3.json")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	f.Add([]byte(`{"schema_version":3}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, data []byte) {
		profile, err := Parse(bytes.NewReader(data))
		if err != nil {
			if profile != (Profile{}) {
				t.Fatal("rejected input returned a partial profile")
			}
			var code Error
			if !errors.As(err, &code) {
				t.Fatal("unbounded diagnostic type")
			}
		}
	})
}
