package config

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/credentialstore"
	"github.com/teatak/pudding-core/internal/credentialstore/credentialtest"
	"github.com/teatak/pudding-core/internal/plugin"
)

type failingCredentialCipher struct {
	credentialtest.Cipher
	failSeal, failOpen bool
}

func (c failingCredentialCipher) Seal(b []byte) ([]byte, error) {
	if c.failSeal {
		return nil, credentialstore.ErrUnavailable
	}
	return c.Cipher.Seal(b)
}
func (c failingCredentialCipher) Open(b []byte) ([]byte, error) {
	if c.failOpen {
		return nil, credentialstore.ErrUnavailable
	}
	return c.Cipher.Open(b)
}

func credentialFixture(t *testing.T) (*Manager, pluginConnectionsYAML, []byte) {
	t.Helper()
	m := newTestManager(t.TempDir())
	if err := m.Prepare(); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	cfg := pluginConnectionsYAML{Version: 1, WebMigrated: true, WebSingleActiveMigrated: true,
		Connections: map[string]*plugin.Connection{
			"mail":  {Name: "Mail", PluginID: "gmail", CreatedAt: stamp, UpdatedAt: stamp, Auth: plugin.Auth{Type: plugin.AuthTypeOAuth2, MethodID: "oauth", AccessToken: "secret-access", RefreshToken: "secret-refresh", ExpiresAt: stamp.Add(time.Hour)}, Account: &plugin.ConnectionAccount{ID: "account-1"}},
			"nas":   {Name: "NAS", PluginID: "synology", CreatedAt: stamp, UpdatedAt: stamp, Auth: plugin.Auth{Type: plugin.AuthTypeSession, SessionEstablished: true}, Fields: map[string]string{"sid": "secret-sid", "synotoken": "secret-synotoken"}},
			"basic": {Name: "Basic", PluginID: "custom", CreatedAt: stamp, UpdatedAt: stamp, Auth: plugin.Auth{Type: plugin.AuthTypeBasic, Username: "alice", Password: "secret-password"}},
			"token": {Name: "Token", PluginID: "unknown-plugin", CreatedAt: stamp, UpdatedAt: stamp, Auth: plugin.Auth{Type: plugin.AuthTypeBearer, Token: "secret-api-key"}, Fields: map[string]string{"privateKey": "secret-private-key", "server": "example.com"}},
		}, Bindings: map[string]map[string]string{"gmail": {"active": "mail"}, "synology": {"active": "nas"}}}
	if err := writeYAML(m.path(pluginConnectionsFile), cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(m.path(pluginConnectionsFile))
	if err != nil {
		t.Fatal(err)
	}
	return m, cfg, raw
}

func TestPluginCredentialsMigrateRestartUpdateAndDelete(t *testing.T) {
	m, want, _ := credentialFixture(t)
	got, err := m.readPluginConnectionsRaw()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Bindings, want.Bindings) {
		t.Fatal("migration changed bindings")
	}
	for id, c := range want.Connections {
		actual := got.Connections[id]
		if actual.CredentialRef == "" {
			t.Fatal("missing encrypted reference")
		}
		copy := plugin.CloneConnection(actual)
		copy.CredentialRef = ""
		if !reflect.DeepEqual(copy, c) {
			t.Fatalf("migration changed connection %s", id)
		}
	}
	err = filepath.WalkDir(m.homeDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte("secret-")) {
			t.Fatalf("plaintext in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	restarted := newTestManager(m.homeDir)
	mail, err := restarted.GetPluginConnection(context.Background(), "mail")
	if err != nil {
		t.Fatal(err)
	}
	oldRef := mail.CredentialRef
	mail.Auth.AccessToken = "new-secret-access"
	if err := restarted.PutPluginConnection(context.Background(), mail); err != nil {
		t.Fatal(err)
	}
	saved, err := newTestManager(m.homeDir).GetPluginConnection(context.Background(), "mail")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Auth.AccessToken != mail.Auth.AccessToken || saved.Auth.RefreshToken != "secret-refresh" || saved.CredentialRef == oldRef || !saved.CreatedAt.Equal(mail.CreatedAt) {
		t.Fatal("refresh lost credentials or identity")
	}
	if _, err := os.Stat(filepath.Join(m.homeDir, "credentials", "plugins", oldRef)); !os.IsNotExist(err) {
		t.Fatal("obsolete ciphertext retained")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(m.homeDir, "credentials", "plugins", saved.CredentialRef))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("credential permissions")
		}
	}
	if err := restarted.DeletePluginConnection(context.Background(), "mail"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.homeDir, "credentials", "plugins", saved.CredentialRef)); !os.IsNotExist(err) {
		t.Fatal("deleted ciphertext retained")
	}
	if _, err := restarted.GetPluginConnection(context.Background(), "nas"); err != nil {
		t.Fatal("unrelated connection lost", err)
	}
}

func TestPluginCredentialMigrationFailurePreservesLegacy(t *testing.T) {
	for _, stage := range []string{"missing-native-bridge", "seal", "verify", "disk"} {
		t.Run(stage, func(t *testing.T) {
			m, _, before := credentialFixture(t)
			switch stage {
			case "missing-native-bridge":
				m = NewManager(m.homeDir)
			case "seal":
				m.WithCredentialCipher(failingCredentialCipher{failSeal: true})
			case "verify":
				m.WithCredentialCipher(failingCredentialCipher{failOpen: true})
			case "disk":
				if err := os.WriteFile(filepath.Join(m.homeDir, "credentials"), []byte("blocked"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := m.ListPluginConnections(context.Background()); err == nil {
				t.Fatal("migration should fail")
			}
			after, err := os.ReadFile(m.path(pluginConnectionsFile))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("failed migration modified legacy credentials")
			}
		})
	}
}

func TestPluginCredentialCommitFailureAndCorruption(t *testing.T) {
	m, _, _ := credentialFixture(t)
	cfg, err := m.readPluginConnectionsRaw()
	if err != nil {
		t.Fatal(err)
	}
	oldRef := cfg.Connections["mail"].CredentialRef
	before, err := os.ReadFile(m.path(pluginConnectionsFile))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Connections["mail"].Auth.AccessToken = "replacement"
	originalDir := m.dir
	m.dir = filepath.Join(m.homeDir, "missing-config")
	if err := m.writePluginConnections(cfg); err == nil {
		t.Fatal("expected commit failure")
	}
	m.dir = originalDir
	after, _ := os.ReadFile(m.path(pluginConnectionsFile))
	if !bytes.Equal(before, after) || cfg.Connections["mail"].CredentialRef != oldRef {
		t.Fatal("failed commit changed prior snapshot")
	}
	if _, err := newTestManager(m.homeDir).GetPluginConnection(context.Background(), "mail"); err != nil {
		t.Fatal("old record pruned before commit", err)
	}
	// A subsequent successful write collects the interrupted write's orphan.
	if err := m.writePluginConnections(cfg); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(m.homeDir, "credentials", "plugins"))
	if err != nil || len(entries) != 4 {
		t.Fatal("orphan cleanup", err, len(entries))
	}
	currentRef := cfg.Connections["mail"].CredentialRef
	file := filepath.Join(m.homeDir, "credentials", "plugins", currentRef)
	if err := os.WriteFile(file, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ListPluginConnections(context.Background()); err == nil {
		t.Fatal("corruption silently accepted")
	}
}

func TestPluginCredentialsUnavailableDoesNotWriteNewPlaintext(t *testing.T) {
	m := NewManager(t.TempDir())
	if err := m.Prepare(); err != nil {
		t.Fatal(err)
	}
	err := m.PutPluginConnection(context.Background(), &plugin.Connection{ID: "new", PluginID: "test", Auth: plugin.Auth{Type: plugin.AuthTypeBearer, Token: "secret-value"}})
	if !errors.Is(err, credentialstore.ErrUnavailable) {
		t.Fatal("must fail closed", err)
	}
	data, _ := os.ReadFile(m.path(pluginConnectionsFile))
	if strings.Contains(string(data), "secret-value") {
		t.Fatal("plaintext fallback")
	}
}
