package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/backendprofile"
	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/discovery"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
)

func optionalRelease(t *testing.T) *configuration.Device {
	t.Helper()
	manifest := bytes.Replace(fixture(t, manifestFixture), []byte(`"schema_version": 4`), []byte(`"schema_version": 5`), 1)
	manifest = bytes.Replace(manifest, []byte(`"AUTH_ISSUER": {`), []byte(`"OPTIONAL_URL": {"from":"resource","scope":"example:extra","optional":true},"AUTH_ISSUER": {`), 1)
	device, err := configuration.ParseDevice(bytes.NewReader(manifest))
	if err != nil {
		t.Fatal(err)
	}
	return device
}

func TestEnrollmentAcceptsOptionalResourceWithAndWithoutURL(t *testing.T) {
	original := discover
	t.Cleanup(func() { discover = original })
	raw := fixture(t, profileFixture)
	device := optionalRelease(t)
	for _, test := range []struct{ name, url string }{
		{name: "missing"},
		{name: "present", url: "https://extra.example/api"},
	} {
		t.Run(test.name, func(t *testing.T) {
			discover = func(_ context.Context, _ string) ([]byte, error) {
				profile, err := backendprofile.Parse(bytes.NewReader(raw))
				if err != nil {
					return nil, err
				}
				if test.url != "" {
					profile.Resources = append(profile.Resources, backendprofile.Resource{Identifier: test.url, Scopes: []string{"example:extra"}})
				}
				return backendprofile.Encode(profile)
			}
			input, err := enrollmentInput(t.Context(), configuration.Snapshot{Device: device}, "https://auth.example")
			if err != nil {
				t.Fatal(err)
			}
			stack, err := workload.FromInput(input)
			if err != nil {
				t.Fatal(err)
			}
			assertOptionalURL(t, stack, test.url)
		})
	}
}

func assertOptionalURL(t *testing.T, stack workload.Stack, want string) {
	t.Helper()
	spec := stack.Specs[0]
	got, set := spec.Env["OPTIONAL_URL"]
	if set != (want != "") || got != want || slices.Contains(spec.UnsetEnv, "OPTIONAL_URL") != (want == "") {
		t.Fatalf("optional environment URL = %q, set = %t, unset = %v", got, set, spec.UnsetEnv)
	}
}

func TestServiceRefreshesOptionalResourcesOnlyDuringReleaseChange(t *testing.T) {
	original := refreshResources
	t.Cleanup(func() { refreshResources = original })
	for _, test := range []struct {
		name       string
		urls       []string
		refreshErr error
		wantErr    error
	}{
		{name: "published since enrollment", urls: []string{"https://extra.example/api"}},
		{name: "absent"},
		{name: "ambiguous", urls: []string{"https://extra.example/api", "https://other.example/api"}, wantErr: backendprofile.ErrScopeAmbiguous},
		{name: "discovery error", refreshErr: discovery.ErrUnavailable, wantErr: discovery.ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := fixture(t, profileFixture)
			old, err := configuration.ParseDevice(bytes.NewReader(fixture(t, manifestFixture)))
			if err != nil {
				t.Fatal(err)
			}
			store := configuration.Store{Path: filepath.Join(t.TempDir(), "state")}
			if _, err := store.Manufacture(old, nil); err != nil {
				t.Fatal(err)
			}
			input, err := old.WithProfile(raw)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Exclusive(func(s *configuration.Session) error { return s.BeginEnrollment(input, false) }); err != nil {
				t.Fatal(err)
			}

			refreshes := 0
			refreshResources = func(ctx context.Context, profile backendprofile.Profile) ([]byte, error) {
				refreshes++
				if _, ok := ctx.Deadline(); !ok {
					t.Error("resource refresh had no deadline")
				}
				if test.refreshErr != nil {
					return nil, test.refreshErr
				}
				profile.Resources = []backendprofile.Resource{}
				for _, url := range test.urls {
					profile.Resources = append(profile.Resources, backendprofile.Resource{Identifier: url, Scopes: []string{"example:extra"}})
				}
				return backendprofile.Encode(profile)
			}

			originalSnapshot, err := store.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			device := optionalRelease(t)
			if changed, err := store.Manufacture(device, nil); err != nil || !changed {
				t.Fatalf("manufacture update: changed=%t, %v", changed, err)
			}
			snapshot, err := store.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.Device.Thumbprint() != originalSnapshot.Device.Thumbprint() {
				t.Fatal("device identity changed")
			}
			manager := &apiManager{}
			var refreshed []byte
			for range 2 {
				stack, nextProfile, err := manager.stack(t.Context(), snapshot)
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("candidate stack: %v, want %v", err, test.wantErr)
				}
				if err == nil {
					wantURL := ""
					if len(test.urls) > 0 {
						wantURL = test.urls[0]
					}
					assertOptionalURL(t, stack, wantURL)
					if stack.Specs[0].Env["API_BASE_URL"] != "https://resource.example/api" {
						t.Fatal("refresh discarded an existing required scope")
					}
				}
				refreshed = nextProfile
			}
			if refreshes != 1 {
				t.Fatalf("candidate refreshed %d times", refreshes)
			}
			if test.wantErr != nil {
				return
			}

			if err := store.Exclusive(func(session *configuration.Session) error {
				if refreshed != nil {
					if err := session.RefreshEnrollment(snapshot.Enrollment, refreshed); err != nil {
						return err
					}
				}
				if err := session.CompleteOptionalResourceRefresh(snapshot.Device); err != nil {
					return err
				}
				return session.SetIdentity(snapshot.Enrollment, configuration.IdentityConfirmed, time.Now(), true, false)
			}); err != nil {
				t.Fatal(err)
			}
			snapshot, err = store.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.Device.OptionalResourceRefreshPending() {
				t.Fatal("converged release still requires refresh")
			}
			if changed, err := store.Manufacture(device, nil); err != nil || changed {
				t.Fatalf("same-release install: changed=%t, %v", changed, err)
			}
			snapshot, err = store.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			stack, restartProfile, err := (&apiManager{}).stack(t.Context(), snapshot)
			if err != nil || restartProfile != nil || refreshes != 1 {
				t.Fatalf("routine restart: refreshes=%d, new profile=%t, %v", refreshes, restartProfile != nil, err)
			}
			if len(test.urls) == 0 {
				assertOptionalURL(t, stack, "")
			} else {
				assertOptionalURL(t, stack, test.urls[0])
			}
		})
	}
}
