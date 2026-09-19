package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"golang.org/x/sync/singleflight"
)

func TestProviderSyncSharesOverlappingRequests(t *testing.T) {
	for _, brand := range []string{"openrouter", "buzzhive"} {
		for _, scenario := range []string{"success", "failure", "cancel first caller"} {
			t.Run(brand+"/"+scenario, func(t *testing.T) {
				started, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				var requests atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if requests.Add(1) == 1 {
						close(started)
						select {
						case <-release:
						case <-r.Context().Done():
							return
						}
						if scenario == "failure" {
							http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
							return
						}
						_, _ = w.Write([]byte(`{"data":[{"id":"m1","context_length":128000}]}`))
						return
					}
					_, _ = w.Write([]byte(`{"data":[{"id":"m1","context_length":256000}]}`))
				}))
				defer upstream.Close()
				defer unblock()
				cfg := &countingProviderSyncConfig{Manager: config.NewManager(t.TempDir())}
				if err := cfg.Prepare(); err != nil {
					t.Fatal(err)
				}
				p := &store.ProviderProfile{
					ID: "shared-sync", Brand: brand, Protocol: "openai-compatible", BaseURL: upstream.URL,
					Models: []store.ProviderModel{{ID: "m1", ContextWindow: 64000, Unavailable: true}},
				}
				if err := cfg.PutProviderProfile(context.Background(), p); err != nil {
					t.Fatal(err)
				}
				s := &Server{}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				first := s.syncProviderProfile(ctx, cfg, p)
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					t.Fatal("first sync did not start")
				}
				// Registration is synchronous; no sleep is needed to ensure overlap.
				second := s.syncProviderProfile(context.Background(), cfg, p)
				if scenario == "cancel first caller" {
					cancel()
				}
				unblock()
				wantStatus, wantUpdates := http.StatusOK, int32(1)
				if scenario == "failure" {
					wantStatus, wantUpdates = http.StatusBadGateway, 0
				}
				for _, result := range []<-chan singleflight.Result{first, second} {
					got := awaitProviderSync(t, result)
					if !got.Shared || got.Val.(providerSyncResponse).status != wantStatus {
						t.Fatalf("unshared or wrong response: %+v", got)
					}
				}
				if requests.Load() != 1 || cfg.updates.Load() != wantUpdates {
					t.Fatalf("overlap fetched %d times and merged %d times", requests.Load(), cfg.updates.Load())
				}
				current, err := cfg.GetProviderProfile(context.Background(), p.ID)
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "failure" {
					if !reflect.DeepEqual(current.Models, p.Models) {
						t.Fatalf("failed sync changed models: %+v", current.Models)
					}
				} else if current.Models[0].Unavailable || current.Models[0].ContextWindow != 128000 {
					t.Fatalf("wrong merged models: %+v", current.Models)
				}
				// Completed successes and errors are not cached by the flight group.
				got := awaitProviderSync(t, s.syncProviderProfile(context.Background(), cfg, current))
				response := got.Val.(providerSyncResponse)
				if got.Shared || response.status != http.StatusOK || response.body.(providerProfileView).Models[0].ContextWindow != 256000 || requests.Load() != 2 || cfg.updates.Load() != wantUpdates+1 {
					t.Fatalf("fresh sync did not fetch/merge a new catalog: %+v", got)
				}
			})
		}
	}
}

func TestProviderSyncIndependentProfilesAndConnections(t *testing.T) {
	for _, change := range []string{"profile", "connection"} {
		t.Run(change, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			var requests atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if requests.Add(1) == 1 {
					close(started)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
					_, _ = w.Write([]byte(`{"data":[]}`))
					return
				}
				_, _ = w.Write([]byte(`{"data":[{"id":"m1","context_length":128000}]}`))
			}))
			defer upstream.Close()
			defer unblock()
			cfg := config.NewManager(t.TempDir())
			if err := cfg.Prepare(); err != nil {
				t.Fatal(err)
			}
			original := &store.ProviderProfile{
				ID: "first", Brand: "openrouter", Protocol: "openai-compatible", BaseURL: upstream.URL,
				Models: []store.ProviderModel{{ID: "m1"}},
			}
			if err := cfg.PutProviderProfile(context.Background(), original); err != nil {
				t.Fatal(err)
			}
			s := &Server{}
			first := s.syncProviderProfile(context.Background(), cfg, original)
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("first sync did not start")
			}
			updated := *original
			if change == "profile" {
				updated.ID = "second"
			} else {
				updated.APIKey = "new-test-key"
			}
			if err := cfg.PutProviderProfile(context.Background(), &updated); err != nil {
				t.Fatal(err)
			}
			second := awaitProviderSync(t, s.syncProviderProfile(context.Background(), cfg, &updated))
			if second.Shared || second.Val.(providerSyncResponse).status != http.StatusOK || requests.Load() != 2 {
				t.Fatalf("independent sync was blocked or shared: %+v", second)
			}
			unblock()
			got := awaitProviderSync(t, first)
			wantStatus := http.StatusOK
			if change == "connection" {
				wantStatus = http.StatusConflict
			}
			if got.Shared || got.Val.(providerSyncResponse).status != wantStatus {
				t.Fatalf("old sync status is wrong: %+v", got)
			}
			current, err := cfg.GetProviderProfile(context.Background(), updated.ID)
			if err != nil || current.Models[0].Unavailable || current.Models[0].ContextWindow != 128000 {
				t.Fatalf("old sync overwrote independent result: %+v, %v", current, err)
			}
		})
	}
}

type countingProviderSyncConfig struct {
	*config.Manager
	updates atomic.Int32
}

func (c *countingProviderSyncConfig) UpdateProviderProfile(ctx context.Context, id string, update func(*store.ProviderProfile) error) (*store.ProviderProfile, error) {
	c.updates.Add(1)
	return c.Manager.UpdateProviderProfile(ctx, id, update)
}

func awaitProviderSync(t *testing.T, ch <-chan singleflight.Result) singleflight.Result {
	t.Helper()
	select {
	case result := <-ch:
		if result.Err != nil {
			t.Fatal(result.Err)
		}
		return result
	case <-time.After(3 * time.Second):
		t.Fatal("sync did not complete")
		return singleflight.Result{}
	}
}

func TestProviderSyncConcurrentChanges(t *testing.T) {
	for _, backend := range []string{"memory", "yaml"} {
		for _, change := range []string{"models", "apiKey", "baseURL", "protocol", "brand", "delete"} {
			t.Run(backend+"/"+change, func(t *testing.T) {
				started, release := make(chan struct{}), make(chan struct{})
				var releaseOnce sync.Once
				unblock := func() { releaseOnce.Do(func() { close(release) }) }
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					close(started)
					select {
					case <-release:
						_, _ = w.Write([]byte(`{"data":[{"id":"m1","context_length":128000}]}`))
					case <-r.Context().Done():
					}
				}))
				defer upstream.Close()
				defer unblock()
				ms, hub := memstore.New(), event.NewHub()
				var cfg engine.ConfigSource = ms
				if backend == "yaml" {
					manager := config.NewManager(t.TempDir())
					if err := manager.Prepare(); err != nil {
						t.Fatal(err)
					}
					cfg = manager
				}
				eng := engine.New(ms, hub, registry.Static(mock.New()), cfg)
				srv := httptest.NewServer(New(eng, ms, cfg, hub).Handler(testToken, nil))
				defer srv.Close()
				defer unblock()
				original := decodeJSON[providerProfileView](t, req(t, http.MethodPost, srv.URL+"/providers", map[string]any{
					"id": "sync-test", "displayName": "Original", "brand": "openrouter", "protocol": "openai-compatible",
					"baseURL": upstream.URL, "apiKey": "old-test-key", "models": []map[string]any{{"id": "m1"}},
				}))
				done := make(chan *http.Response, 1)
				go func() { done <- req(t, http.MethodPost, srv.URL+"/providers/sync-test/sync", nil) }()
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					t.Fatal("sync did not reach upstream")
				}
				updated := original
				if change == "delete" {
					resp := req(t, http.MethodDelete, srv.URL+"/providers/sync-test", nil)
					resp.Body.Close()
					if resp.StatusCode != http.StatusNoContent {
						t.Fatalf("delete status = %d", resp.StatusCode)
					}
				} else {
					patch := map[string]any{}
					switch change {
					case "models":
						patch["displayName"] = "User edit"
						patch["models"] = []map[string]any{{"id": "m1", "displayName": "My alias"}, {"id": "new-user-model"}}
					case "apiKey":
						patch[change] = "new-test-key"
					case "baseURL":
						patch[change] = upstream.URL + "/changed"
					case "protocol":
						patch[change] = "openai-responses"
					case "brand":
						patch[change] = "buzzhive"
					}
					updated = decodeJSON[providerProfileView](t, req(t, http.MethodPatch, srv.URL+"/providers/sync-test", patch))
				}
				unblock()
				response := <-done
				response.Body.Close()
				got := req(t, http.MethodGet, srv.URL+"/providers/sync-test", nil)
				if change == "delete" {
					got.Body.Close()
					if response.StatusCode != http.StatusNotFound || got.StatusCode != http.StatusNotFound {
						t.Fatalf("deleted profile resurrected: sync=%d get=%d", response.StatusCode, got.StatusCode)
					}
					return
				}
				profile := decodeJSON[providerProfileView](t, got)
				if change == "models" {
					if response.StatusCode != http.StatusOK || profile.DisplayName != "User edit" || len(profile.Models) != 2 || profile.Models[0].DisplayName != "My alias" || profile.Models[0].ContextWindow != 128000 || profile.Models[1].ID != "new-user-model" {
						t.Fatalf("sync did not merge into latest models: status=%d profile=%+v", response.StatusCode, profile)
					}
				} else if response.StatusCode != http.StatusConflict || !reflect.DeepEqual(profile, updated) {
					t.Fatalf("stale sync changed configuration: status=%d profile=%+v", response.StatusCode, profile)
				}
			})
		}
	}
}

func TestSyncCatalogCapabilitiesPreserveUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, fields   string
		existing, want *store.ModelCaps
	}{
		{"missing", "", nil, nil},
		{"missing preserves disabled", "", &store.ModelCaps{Image: true}, &store.ModelCaps{Image: true}},
		{"partial", `,"capabilities":{"vision":true}`, nil, &store.ModelCaps{Image: true, Tools: true}},
		{"partial preserves disabled", `,"capabilities":{"vision":true}`, &store.ModelCaps{}, &store.ModelCaps{Image: true}},
		{"explicit false", `,"capabilities":{"tools":false}`, nil, &store.ModelCaps{}},
		{"explicit true", `,"supported_parameters":["tools"]`, &store.ModelCaps{}, &store.ModelCaps{Tools: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"data":[{"id":"m1"` + tc.fields + `}]}`))
			}))
			defer upstream.Close()
			candidates, err := fetchProviderModels(context.Background(), "openai-compatible", upstream.URL, "")
			if err != nil {
				t.Fatal(err)
			}
			for _, brand := range []string{"openrouter", "buzzhive"} {
				existing := []store.ProviderModel{{ID: "m1", Capabilities: tc.existing}}
				var before *store.ModelCaps
				if tc.existing != nil {
					cp := *tc.existing
					before = &cp
				}
				var models []store.ProviderModel
				if brand == "openrouter" {
					models = syncOpenRouterModels(existing, candidates)
				} else {
					models = syncBuzzHiveModels(existing, candidates)
				}
				if !reflect.DeepEqual(models[0].Capabilities, tc.want) {
					t.Errorf("%s caps=%+v want=%+v", brand, models[0].Capabilities, tc.want)
				}
				if !reflect.DeepEqual(existing[0].Capabilities, before) {
					t.Errorf("%s mutated input capabilities", brand)
				}
			}
			if tc.existing == nil {
				models := syncBuzzHiveModels(nil, candidates)
				if !reflect.DeepEqual(models[0].Capabilities, tc.want) {
					t.Errorf("new model caps=%+v want=%+v", models[0].Capabilities, tc.want)
				}
			}
		})
	}
}
