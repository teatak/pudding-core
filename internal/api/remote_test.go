package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

func TestRemoteAPIStartupAuthDirectClaimAndRevoke(t *testing.T) {
	st := storetest.New(t)
	server := httptest.NewServer(New(nil, st, st, nil).Handler(testToken, nil))
	defer server.Close()
	for _, path := range []string{"/remote/access", "/remote/events", "/remote/models"} {
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unguarded %s: %d", path, resp.StatusCode)
		}
	}
	scope := store.RemoteScope{Mode: "lan", Origin: "http://192.168.1.10:18443"}
	initial := decodeJSON[store.RemoteAccess](t, req(t, http.MethodGet, server.URL+"/remote/access", nil))
	if initial.DesktopID == "" || initial.Devices == nil || initial.Pairings == nil {
		t.Fatal(initial)
	}
	create := req(t, http.MethodPost, server.URL+"/remote/pairings", scope)
	if create.StatusCode != http.StatusCreated || create.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("pairing response status/cache", create.StatusCode, create.Header)
	}
	code := decodeJSON[store.RemotePairingCode](t, create)
	claimResponse := req(t, http.MethodPost, server.URL+"/remote/pairings/request", store.RemotePairingRequest{RemoteScope: scope, Code: code.Code, DeviceName: "Browser device"})
	if claimResponse.StatusCode != http.StatusOK || claimResponse.Header.Get("Cache-Control") != "no-store" {
		t.Fatal(claimResponse.StatusCode, claimResponse.Header)
	}
	claimed := decodeJSON[store.RemotePairingClaim](t, claimResponse)
	if claimed.Token == "" || claimed.Device.Name != "Browser device" {
		t.Fatal("missing immediate grant", claimed)
	}
	resp := req(t, http.MethodPost, server.URL+"/remote/pairings/request", store.RemotePairingRequest{RemoteScope: scope, Code: code.Code, DeviceName: "Browser device"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatal("duplicate code accepted", resp.StatusCode)
	}
	for _, suffix := range []string{"poll", "approve", "deny"} {
		response := req(t, http.MethodPost, server.URL+"/remote/pairings/"+code.ID+"/"+suffix, map[string]any{})
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatal("old route remains", suffix, response.StatusCode)
		}
	}
	for i := 0; i < 2; i++ {
		response := req(t, http.MethodDelete, server.URL+"/remote/pairings/"+code.ID, nil)
		response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			t.Fatal("cancel consumed code not idempotent", response.StatusCode)
		}
	}
	auth := decodeJSON[store.RemoteAuthorization](t, req(t, http.MethodPost, server.URL+"/remote/authorize", store.RemoteAuthorizeInput{RemoteScope: scope, Token: claimed.Token}))
	if auth.DesktopID != initial.DesktopID || auth.Device.ID != claimed.Device.ID {
		t.Fatal(auth)
	}
	wrong := store.RemoteScope{Mode: "relay", Origin: "https://phone.example.com"}
	resp = req(t, http.MethodPost, server.URL+"/remote/authorize", store.RemoteAuthorizeInput{RemoteScope: wrong, Token: claimed.Token})
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatal("cross-mode credential accepted", resp.StatusCode)
	}
	resp = req(t, http.MethodDelete, server.URL+"/remote/devices/"+claimed.Device.ID, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatal(resp.StatusCode)
	}
	resp = req(t, http.MethodPost, server.URL+"/remote/authorize", store.RemoteAuthorizeInput{RemoteScope: scope, Token: claimed.Token})
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatal("revoked credential accepted", resp.StatusCode)
	}
}
func TestRemoteModelsReturnsOnlyCanonicalConfiguredIDsAndLabels(t *testing.T) {
	st := storetest.New(t)
	if err := st.PutProviderProfile(context.Background(), &store.ProviderProfile{ID: "profile", DisplayName: "Name", Protocol: "openai-compatible", BaseURL: "https://secret-endpoint.example", APIKey: "secret-key-123", Models: []store.ProviderModel{{ID: "model-a", DisplayName: "Model A"}, {ID: "unavailable", Unavailable: true}}}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(nil, st, st, nil).Handler(testToken, nil))
	defer server.Close()
	resp := req(t, http.MethodGet, server.URL+"/remote/models", nil)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "secret") || strings.Contains(string(body), "baseURL") || strings.Contains(string(body), "apiKey") || strings.Contains(string(body), "unavailable") {
		t.Fatalf("model metadata exposed configuration: %s", body)
	}
	var out struct {
		Models []remoteModel `json:"models"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Models) != 1 || out.Models[0].Provider != "profile" || out.Models[0].Model != "model-a" || out.Models[0].Label != "Model A" {
		t.Fatal(out)
	}
}
func TestRemoteEventsAreCanonicalMutationInvalidations(t *testing.T) {
	st := storetest.New(t)
	api := New(nil, st, st, nil)
	server := httptest.NewServer(api.Handler(testToken, nil))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/remote/events", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	lines := make(chan string, 16)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	expect := func(want string) {
		t.Helper()
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					t.Fatal("stream ended")
				}
				if line == want {
					return
				}
			case <-time.After(3 * time.Second):
				t.Fatalf("did not receive %s", want)
			}
		}
	}
	expect("event: remote.ready")
	code := decodeJSON[store.RemotePairingCode](t, req(t, http.MethodPost, server.URL+"/remote/pairings", store.RemoteScope{Mode: "relay", Origin: "https://phone.example.com"}))
	expect("event: remote.changed")
	expect(`data: {"pairingID":"` + code.ID + `"}`)
	respDelete := req(t, http.MethodDelete, server.URL+"/remote/pairings/"+code.ID, nil)
	respDelete.Body.Close()
	expect("event: remote.changed")
	expect(`data: {"pairingID":"` + code.ID + `"}`)
	// Overflow terminates a slow consumer: it cannot silently miss revoke events.
	api.remoteMu.Lock()
	slow := make(chan remoteChange, 1)
	api.remoteSubscribers[slow] = struct{}{}
	api.remoteMu.Unlock()
	api.notifyRemote(remoteChange{DeviceID: "one"})
	api.notifyRemote(remoteChange{DeviceID: "two"})
	<-slow
	if _, open := <-slow; open {
		t.Fatal("overflow did not close subscription")
	}
}

func TestRemoteAPILANHTTPIPv4OriginBoundary(t *testing.T) {
	st := storetest.New(t)
	server := httptest.NewServer(New(nil, st, st, nil).Handler(testToken, nil))
	defer server.Close()
	for _, scope := range []store.RemoteScope{
		{Mode: "lan", Origin: "https://127.0.0.1:18443"},
		{Mode: "lan", Origin: "http://localhost:18443"},
		{Mode: "lan", Origin: "http://[::1]:18443"},
		{Mode: "lan", Origin: "http://127.0.0.1:18443?"},
		{Mode: "relay", Origin: "http://phone.example.com"},
	} {
		response := req(t, http.MethodPost, server.URL+"/remote/pairings", scope)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("%+v status=%d", scope, response.StatusCode)
		}
	}
	response := req(t, http.MethodPost, server.URL+"/remote/pairings", store.RemoteScope{Mode: "lan", Origin: "http://127.0.0.1:18443"})
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("loopback HTTP pairing status=%d", response.StatusCode)
	}
}

func TestRemoteModelConfigurationStripsArbitraryProviderOptions(t *testing.T) {
	model := store.ProviderModel{ID: "model", ContextWindow: 120000,
		ProviderOptions: &store.ProviderOptions{
			OpenAI:    map[string]any{"reasoning_effort": "high", "api_key": "fixture-model-secret"},
			Google:    map[string]any{"thinking": map[string]any{"level": "medium", "credential": "fixture-private"}, "headers": "fixture-secret"},
			Anthropic: map[string]any{"output_config": map[string]any{"effort": "max", "token": "fixture-secret"}},
		}}
	safe := remoteModelConfiguration(model)
	raw, err := json.Marshal(safe)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "fixture-") {
		t.Fatalf("secret escaped into catalog: %s", raw)
	}
	if safe.ContextWindow != model.ContextWindow || safe.ProviderOptions.OpenAI["reasoning_effort"] != "high" {
		t.Fatal(safe)
	}
	if model.ProviderOptions.OpenAI["api_key"] != "fixture-model-secret" {
		t.Fatal("canonical configuration mutated")
	}
}
