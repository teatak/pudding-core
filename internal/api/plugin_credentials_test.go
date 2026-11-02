package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/credentialstore"
	"github.com/teatak/pudding-core/internal/credentialstore/credentialtest"
	"github.com/teatak/pudding-core/internal/mailclient"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

func TestPluginConnectionSecretsAreWriteOnlyAndBlankPreserves(t *testing.T) {
	srv, _, cfg := newConfigTestServer(t)
	for _, input := range []struct {
		id     string
		body   map[string]any
		secret string
	}{
		{"pat", map[string]any{"pluginID": "gitlab", "authMethodID": "gitlab-token", "token": "private-pat"}, "private-pat"},
		{"local", map[string]any{"pluginID": "local-config", "fields": map[string]string{"apiKey": "private-field"}}, "private-field"},
	} {
		resp := req(t, http.MethodPut, srv.URL+"/plugin-connections/"+input.id, input.body)
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || strings.Contains(string(data), input.secret) {
			t.Fatalf("put status %d", resp.StatusCode)
		}
		resp = req(t, http.MethodGet, srv.URL+"/plugin-connections/"+input.id, nil)
		data, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || strings.Contains(string(data), input.secret) {
			t.Fatal("detail returned secret")
		}
		if input.id == "local" && !strings.Contains(string(data), `"secretFieldsSet":{"apiKey":true}`) {
			t.Fatal("missing configured state")
		}
		input.body["token"] = ""
		input.body["fields"] = map[string]string{}
		input.body["name"] = "Renamed"
		resp = req(t, http.MethodPut, srv.URL+"/plugin-connections/"+input.id, input.body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("blank edit status %d", resp.StatusCode)
		}
		stored, err := cfg.GetPluginConnection(context.Background(), input.id)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Auth.Token != input.secret && stored.Fields["apiKey"] != input.secret {
			t.Fatal("blank edit discarded stored credential")
		}
	}
	// The compatibility settings endpoint must not bypass the write-only contract.
	resp := req(t, http.MethodPatch, srv.URL+"/tools/web", map[string]any{"providers": map[string]any{"tavily": map[string]string{"apiKey": "private-web-key"}}})
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || strings.Contains(string(data), "private-web-key") {
		t.Fatal("legacy response leaked key")
	}
	resp = req(t, http.MethodGet, srv.URL+"/tools/web", nil)
	data, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || strings.Contains(string(data), "private-web-key") {
		t.Fatal("legacy GET leaked key")
	}
}
func TestOptionalSecretClearIsExplicit(t *testing.T) {
	def := &plugin.ConnectionConfig{Fields: []plugin.ConnectionField{{ID: "required", Secret: true, Required: true}, {ID: "optional", Secret: true}, {ID: "public"}}}
	existing := &plugin.Connection{Fields: map[string]string{"required": "old-required", "optional": "old-optional", "public": "old-public"}}
	got, err := normalizePluginConnectionFields(def, map[string]string{"required": "", "optional": "", "public": "public"}, existing)
	if err != nil || !reflect.DeepEqual(got, map[string]string{"required": "old-required", "optional": "old-optional", "public": "public"}) {
		t.Fatal("blank must preserve secret", err)
	}
	got, err = normalizePluginConnectionFields(def, nil, existing, []string{"optional"})
	if err != nil || got["optional"] != "" || got["required"] != "old-required" {
		t.Fatal("explicit clear failed", err)
	}
	got, err = normalizePluginConnectionFields(def, map[string]string{"optional": "new"}, existing)
	if err != nil || got["optional"] != "new" {
		t.Fatal("replacement failed", err)
	}
	for _, id := range []string{"required", "public", "missing"} {
		if _, err := normalizePluginConnectionFields(def, nil, existing, []string{id}); err == nil {
			t.Fatal("invalid clear accepted", id)
		}
	}
	if _, err := normalizePluginConnectionFields(def, map[string]string{"optional": "new"}, existing, []string{"optional"}); err == nil {
		t.Fatal("conflicting edit accepted")
	}
}

type unavailableCredentialCipher struct{}

func (unavailableCredentialCipher) Seal([]byte) ([]byte, error) {
	return nil, credentialstore.ErrUnavailable
}
func (unavailableCredentialCipher) Open([]byte) ([]byte, error) {
	return nil, credentialstore.ErrUnavailable
}
func TestPluginCredentialUnavailableResponse(t *testing.T) {
	srv, _, cfg := newConfigTestServer(t)
	cfg.WithCredentialCipher(unavailableCredentialCipher{})
	resp := req(t, http.MethodPut, srv.URL+"/plugin-connections/pat", map[string]any{"pluginID": "gitlab", "authMethodID": "gitlab-token", "token": "private-token"})
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 503 || !strings.Contains(string(data), "credential_storage_unavailable") || strings.Contains(string(data), "private-token") {
		t.Fatalf("unexpected unavailable response %d", resp.StatusCode)
	}
}

func TestEmailConnectionEditKeepsIdentityAndRejectsMailboxSwitch(t *testing.T) {
	home := t.TempDir()
	cfg := config.NewManager(home).WithCredentialCipher(credentialtest.Cipher{})
	if err := cfg.Prepare(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "plugins", "email")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := `id: email
name: Email
auth:
  required: true
  methods:
    - id: gmail
      type: basic
      default: true
endpoints:
  email:
    kind: rest
    adapter: email
`
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := mailclient.Parse("gmail", "user@gmail.com", "private-password", nil)
	if err != nil {
		t.Fatal(err)
	}
	conn := &plugin.Connection{ID: "email-work", PluginID: "email", Auth: plugin.Auth{Type: "basic", MethodID: "gmail", Username: settings.Address, Password: "private-password"}, Account: &plugin.ConnectionAccount{ID: settings.MailboxID, Login: settings.Address, Type: "email"}}
	if err := cfg.PutPluginConnection(context.Background(), conn); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(nil, storetest.New(t), cfg, nil).WithHome(home).WithPlugins(plugin.NewService(home, cfg)).Handler(testToken, nil))
	defer srv.Close()
	body := map[string]any{"pluginID": "email", "authMethodID": "gmail", "authType": "basic", "name": "Renamed", "username": "user@gmail.com", "password": ""}
	resp := req(t, http.MethodPut, srv.URL+"/plugin-connections/email-work", body)
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || strings.Contains(string(data), "private-password") {
		t.Fatalf("rename: %d %s", resp.StatusCode, data)
	}
	stored, _ := cfg.GetPluginConnection(context.Background(), conn.ID)
	if stored.Name != "Renamed" || stored.Auth.Password != "private-password" || stored.Account.ID != settings.MailboxID {
		t.Fatal("rename changed credentials/identity")
	}
	body["username"] = "other@gmail.com"
	resp = req(t, http.MethodPut, srv.URL+"/plugin-connections/email-work", body)
	data, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 400 || !strings.Contains(string(data), "email_mailbox_change_requires_new_connection") {
		t.Fatalf("switch: %d %s", resp.StatusCode, data)
	}
	stored, _ = cfg.GetPluginConnection(context.Background(), conn.ID)
	if stored.Auth.Username != "user@gmail.com" {
		t.Fatal("rejected switch persisted")
	}
}
