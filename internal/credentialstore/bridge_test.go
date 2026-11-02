package credentialstore

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBridgePrivateProtocolAndReconnect(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "run"), 0700); err != nil {
		t.Fatal(err)
	}
	descriptor := filepath.Join(home, "run", "plugin-credentials.json")
	bridge := NewBridge(home)
	if _, err := bridge.Seal([]byte("secret")); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 64)
	serve := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+token || r.Method != "POST" {
				t.Error("missing private authentication")
			}
			var data map[string]string
			_ = json.NewDecoder(r.Body).Decode(&data)
			plain, err := base64.StdEncoding.DecodeString(data["data"])
			if err != nil {
				t.Error(err)
			}
			if r.URL.Path != "/seal" && r.URL.Path != "/open" {
				t.Error("wrong operation")
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"data": base64.StdEncoding.EncodeToString(plain)})
		}))
	}
	write := func(url string) {
		t.Helper()
		data, _ := json.Marshal(map[string]string{"url": url, "token": token})
		if err := os.WriteFile(descriptor, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		srv := serve()
		write(srv.URL)
		got, err := bridge.Seal([]byte("secret"))
		if err != nil || !bytes.Equal(got, []byte("secret")) {
			t.Fatal("bridge protocol failed", err)
		}
		srv.Close()
		token = strings.Repeat("b", 64)
	}
	for _, url := range []string{"https://example.com", "http://localhost:1234", "http://127.0.0.1:1234/path", "http://user@127.0.0.1:1234"} {
		write(url)
		if _, err := bridge.Open([]byte("cipher")); !errors.Is(err, ErrUnavailable) {
			t.Fatal("invalid descriptor accepted", url, err)
		}
	}
}
func TestBridgeDoesNotFollowRedirectsOrEchoProviderErrors(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, "run"), 0700)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("followed redirect") }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/secret", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	data, _ := json.Marshal(map[string]string{"url": srv.URL, "token": strings.Repeat("a", 64)})
	_ = os.WriteFile(filepath.Join(home, "run", "plugin-credentials.json"), data, 0600)
	if _, err := NewBridge(home).Seal([]byte("secret")); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}
