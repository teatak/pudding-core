package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/oauthbroker"
)

type gmailRoundTrip func(*http.Request) (*http.Response, error)

func (f gmailRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type gmailConnectionStore struct {
	mu sync.Mutex
	fakeConnectionStore
}

func (s *gmailConnectionStore) GetPluginConnection(ctx context.Context, id string) (*Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeConnectionStore.GetPluginConnection(ctx, id)
}
func (s *gmailConnectionStore) PutPluginConnection(ctx context.Context, c *Connection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeConnectionStore.PutPluginConnection(ctx, c)
}
func (s *gmailConnectionStore) ListPluginConnections(ctx context.Context) ([]*Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeConnectionStore.ListPluginConnections(ctx)
}
func gmailFixture(t *testing.T, transport http.RoundTripper) (*Service, *gmailConnectionStore) {
	t.Helper()
	connections := &gmailConnectionStore{fakeConnectionStore: fakeConnectionStore{items: map[string]*Connection{"gmail-main": {
		ID: "gmail-main", PluginID: "gmail", Name: "Gmail", Account: &ConnectionAccount{ID: "account", Login: "test@example.com"},
		Auth: Auth{Type: AuthTypeOAuth2, MethodID: "google-oauth", AccessToken: "old", RefreshToken: "refresh-old", TokenType: "Bearer", Scopes: []string{"read", "send"}, ExpiresAt: time.Now().Add(-time.Hour), RefreshExpiresAt: time.Now().Add(6 * 24 * time.Hour)},
	}}}}
	return NewService(t.TempDir(), connections).WithOAuthBroker(oauthbroker.New("", &http.Client{Transport: transport})), connections
}
func TestGmailRefreshClassifiesFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		reauth  bool
		network bool
	}{
		{"revoked", 400, `{"error":"invalid_grant"}`, true, false},
		{"invalid client", 401, `{"error":"invalid_client"}`, false, false},
		{"rate limit", 429, `{"error":"rate_limit_exceeded"}`, false, false},
		{"server error", 503, `{"error":"invalid_grant"}`, false, false},
		{"bad response", 200, `not json`, false, false},
		{"missing access token", 200, `{"expires_in":3600}`, false, false},
		{"missing expiry", 200, `{"access_token":"new"}`, false, false},
		{"network", 0, "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, store := gmailFixture(t, gmailRoundTrip(func(r *http.Request) (*http.Response, error) {
				if tc.network {
					return nil, context.DeadlineExceeded
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			}))
			before, _ := store.GetPluginConnection(context.Background(), "gmail-main")
			_, err := svc.ReadyConnection(context.Background(), "gmail-main")
			if err == nil || errors.Is(err, ErrConnectionReauthorizationRequired) != tc.reauth {
				t.Fatalf("err=%v reauth=%v", err, tc.reauth)
			}
			after, _ := store.GetPluginConnection(context.Background(), "gmail-main")
			if !reflect.DeepEqual(before, after) {
				t.Fatal("failed refresh changed stored credentials")
			}
		})
	}
}
func TestGmailRefreshPersistsAndDeduplicates(t *testing.T) {
	for _, rotate := range []bool{false, true} {
		t.Run(map[bool]string{false: "retain refresh token", true: "rotated refresh token"}[rotate], func(t *testing.T) {
			var requests atomic.Int32
			svc, store := gmailFixture(t, gmailRoundTrip(func(r *http.Request) (*http.Response, error) {
				requests.Add(1)
				if r.URL.String() != oauthbroker.GmailRefreshURL || r.Method != "POST" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["refresh_token"] != "refresh-old" {
					t.Error("wrong refresh token")
				}
				payload := `{"access_token":"new","expires_in":3600}`
				if rotate {
					payload = `{"access_token":"new","expires_in":3600,"refresh_token":"refresh-new","refresh_token_expires_in":864000,"scope":"read send extra"}`
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(payload)), Header: make(http.Header)}, nil
			}))
			before, _ := store.GetPluginConnection(context.Background(), "gmail-main")
			var wg sync.WaitGroup
			for i := 0; i < 12; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					c, err := svc.ReadyConnection(context.Background(), "gmail-main")
					if err != nil {
						t.Error(err)
						return
					}
					if c.Auth.AccessToken != "new" {
						t.Error("stale access token")
					}
				}()
			}
			wg.Wait()
			if requests.Load() != 1 {
				t.Fatalf("refreshes=%d", requests.Load())
			}
			c, _ := store.GetPluginConnection(context.Background(), "gmail-main")
			if !c.Auth.ExpiresAt.After(time.Now().Add(58*time.Minute)) || c.Auth.TokenType != "Bearer" || !reflect.DeepEqual(c.Account, before.Account) {
				t.Fatal("metadata or expiry lost")
			}
			if rotate {
				if c.Auth.RefreshToken != "refresh-new" || len(c.Auth.Scopes) != 3 || !c.Auth.RefreshExpiresAt.After(before.Auth.RefreshExpiresAt) {
					t.Fatal("rotated token not persisted")
				}
			} else if c.Auth.RefreshToken != "refresh-old" || !c.Auth.RefreshExpiresAt.Equal(before.Auth.RefreshExpiresAt) || !reflect.DeepEqual(c.Auth.Scopes, before.Auth.Scopes) {
				t.Fatal("omitted fields lost")
			}
			// A new service instance uses persisted credentials without another refresh.
			restarted := NewService(t.TempDir(), store).WithOAuthBroker(svc.oauthBroker)
			if _, err := restarted.ReadyConnection(context.Background(), "gmail-main"); err != nil {
				t.Fatal(err)
			}
			if requests.Load() != 1 {
				t.Fatal("restart unnecessarily refreshed")
			}
		})
	}
}
func TestGmailFreshTokenAndUnusableRefreshToken(t *testing.T) {
	for _, scenario := range []string{"fresh", "missing refresh", "expired refresh"} {
		t.Run(scenario, func(t *testing.T) {
			svc, store := gmailFixture(t, gmailRoundTrip(func(*http.Request) (*http.Response, error) {
				t.Error("unexpected refresh request")
				return nil, errors.New("unexpected request")
			}))
			c, _ := store.GetPluginConnection(context.Background(), "gmail-main")
			switch scenario {
			case "fresh":
				c.Auth.ExpiresAt = time.Now().Add(time.Hour)
			case "missing refresh":
				c.Auth.RefreshToken = ""
			case "expired refresh":
				c.Auth.RefreshExpiresAt = time.Now().Add(-time.Minute)
			}
			_ = store.PutPluginConnection(context.Background(), c)
			_, err := svc.ReadyConnection(context.Background(), "gmail-main")
			if scenario == "fresh" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrConnectionReauthorizationRequired) {
				t.Fatal(err)
			}
		})
	}
}

func TestGmailSessionRefreshReportsReauthorizationOnlyForInvalidGrant(t *testing.T) {
	for _, bound := range []bool{false, true} {
		for _, invalid := range []bool{false, true} {
			t.Run(fmt.Sprintf("bound=%t/invalid=%t", bound, invalid), func(t *testing.T) {
				var recovered atomic.Bool
				svc, connections := gmailFixture(t, gmailRoundTrip(func(*http.Request) (*http.Response, error) {
					status, body := 503, `{"error":"temporarily_unavailable"}`
					if invalid {
						status, body = 400, `{"error":"invalid_grant"}`
					}
					if recovered.Load() {
						status, body = 200, `{"access_token":"new","expires_in":3600}`
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
				}))
				db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				ctx := context.Background()
				if err := db.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "mock"}); err != nil {
					t.Fatal(err)
				}
				svc.WithSessionConnections(db)
				if bound {
					c, _ := connections.GetPluginConnection(ctx, "gmail-main")
					if err := db.SetSessionPluginConnection(ctx, "s", "gmail", "gmail-main", connectionIdentity(c)); err != nil {
						t.Fatal(err)
					}
				}
				def := &Definition{ID: "gmail", Auth: &AuthConfig{Required: true, Methods: []AuthMethod{{ID: "google-oauth", Type: AuthTypeOAuth2}}}}
				_, err = svc.selectedSessionConnection(ctx, "s", def)
				if err == nil {
					t.Fatal("expected refresh failure")
				}
				var selection *EndpointResolveError
				if invalid {
					if !errors.As(err, &selection) || selection.Reason != "connection_reauthorization_required" {
						t.Fatal(err)
					}
				} else if errors.As(err, &selection) {
					t.Fatalf("temporary error requested a new connection: %v", err)
				}
				// Neither failure discards the saved account; a subsequent refresh can recover.
				recovered.Store(true)
				c, err := svc.selectedSessionConnection(ctx, "s", def)
				if err != nil || c.Auth.AccessToken != "new" {
					t.Fatalf("recovery failed: %v", err)
				}
				if bound {
					binding, err := db.GetSessionPluginConnection(ctx, "s", "gmail")
					if err != nil || binding == nil || binding.ConnectionID != "gmail-main" {
						t.Fatal("binding lost")
					}
				}
			})
		}
	}
}
