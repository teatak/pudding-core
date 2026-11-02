package config

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/teatak/pudding-core/internal/credentialstore"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
)

func TestUpdateProviderProfileAtomicAndExistingOnly(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	m := newTestManager(dir)
	if err := m.Prepare(); err != nil {
		t.Fatal(err)
	}
	if err := m.PutProviderProfile(ctx, &store.ProviderProfile{ID: "profile", DisplayName: "original", Models: []store.ProviderModel{{ID: "model"}}}); err != nil {
		t.Fatal(err)
	}
	var writers sync.WaitGroup
	for range 20 {
		writers.Go(func() {
			_, err := m.UpdateProviderProfile(ctx, "profile", func(p *store.ProviderProfile) error {
				p.Models[0].ContextWindow++
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		})
	}
	writers.Wait()
	p, err := newTestManager(dir).GetProviderProfile(ctx, "profile")
	if err != nil {
		t.Fatal(err)
	}
	if p.Models[0].ContextWindow != 20 {
		t.Fatalf("lost updates: %d", p.Models[0].ContextWindow)
	}
	rejected := errors.New("reject update")
	if _, err := m.UpdateProviderProfile(ctx, "profile", func(p *store.ProviderProfile) error {
		p.DisplayName = "must not persist"
		return rejected
	}); !errors.Is(err, rejected) {
		t.Fatalf("error = %v", err)
	}
	p, err = m.GetProviderProfile(ctx, "profile")
	if err != nil || p.DisplayName != "original" {
		t.Fatalf("rejected update persisted: %+v, %v", p, err)
	}
	if err := m.DeleteProviderProfile(ctx, "profile"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.UpdateProviderProfile(ctx, "profile", func(*store.ProviderProfile) error {
		t.Error("callback ran for deleted profile")
		return nil
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing profile error = %v", err)
	}
	if _, err := newTestManager(dir).GetProviderProfile(ctx, "profile"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted profile restored: %v", err)
	}
}

func legacyProfileFixture(t *testing.T) (*Manager, profilesYAML, []byte) {
	t.Helper()
	m := newTestManager(t.TempDir())
	if err := m.Prepare(); err != nil {
		t.Fatal(err)
	}
	cfg := profilesYAML{Version: 1, Profiles: map[string]*store.ProviderProfile{
		"work":  {DisplayName: "Work", Brand: "custom", Protocol: "openai-compatible", BaseURL: "https://example.com/v1", APIKey: "legacy-model-secret", Models: []store.ProviderModel{{ID: "model", ContextWindow: 100000}}},
		"local": {DisplayName: "Local", Protocol: "openai-compatible", BaseURL: "http://localhost:11434/v1", Models: []store.ProviderModel{}},
	}}
	if err := writeYAML(m.path(profilesFile), cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(m.path(profilesFile))
	if err != nil {
		t.Fatal(err)
	}
	return m, cfg, raw
}

func TestProviderCredentialsMigrateRotateCloneAndDeleteWithoutAffectingPlugins(t *testing.T) {
	m, want, _ := legacyProfileFixture(t)
	ctx := context.Background()
	if err := m.PutPluginConnection(ctx, &plugin.Connection{ID: "work", PluginID: "test", Auth: plugin.Auth{Type: plugin.AuthTypeBearer, Token: "plugin-secret"}}); err != nil {
		t.Fatal(err)
	}
	p, err := m.GetProviderProfile(ctx, "work")
	if err != nil {
		t.Fatal(err)
	}
	copy := cloneProfile("", p)
	copy.CredentialRef = ""
	if !reflect.DeepEqual(copy, want.Profiles["work"]) {
		t.Fatal("migration changed profile metadata or model list")
	}
	raw, err := os.ReadFile(m.path(profilesFile))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("legacy-model-secret")) || !bytes.Contains(raw, []byte("credential_ref:")) {
		t.Fatal("profile key remains plaintext")
	}
	ref := p.CredentialRef
	cipherFile := filepath.Join(m.homeDir, "credentials", "providers", ref)
	encrypted, err := os.ReadFile(cipherFile)
	if err != nil || bytes.Contains(encrypted, []byte("legacy-model-secret")) {
		t.Fatal("missing encrypted profile key", err)
	}
	restarted := newTestManager(m.homeDir)
	p, err = restarted.GetProviderProfile(ctx, "work")
	if err != nil || p.APIKey != "legacy-model-secret" {
		t.Fatal("restart failed", err)
	}
	p.ID = "copy"
	if err := restarted.PutProviderProfile(ctx, p); err != nil {
		t.Fatal("clone must use a new owner", err)
	}
	// Updating only models must reuse the saved secret without changing its record.
	p, err = restarted.UpdateProviderProfile(ctx, "work", func(p *store.ProviderProfile) error { p.Models[0].ContextWindow++; return nil })
	if err != nil || p.CredentialRef != ref || p.APIKey != "legacy-model-secret" {
		t.Fatal("model edit disturbed credential", err)
	}
	p, err = restarted.UpdateProviderProfile(ctx, "work", func(p *store.ProviderProfile) error { p.APIKey = "replacement-secret"; return nil })
	if err != nil || p.CredentialRef == ref {
		t.Fatal("rotation failed", err)
	}
	if _, err := os.Stat(cipherFile); !os.IsNotExist(err) {
		t.Fatal("old ciphertext not pruned")
	}
	if err := restarted.DeletePluginConnection(ctx, "work"); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.GetProviderProfile(ctx, "work"); err != nil {
		t.Fatal("plugin cleanup deleted provider key", err)
	}
	if err := restarted.DeleteProviderProfile(ctx, "work"); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.GetProviderProfile(ctx, "copy"); err != nil {
		t.Fatal("delete affected copied profile", err)
	}
	if _, err := os.Stat(filepath.Join(m.homeDir, "credentials", "providers", p.CredentialRef)); !os.IsNotExist(err) {
		t.Fatal("deleted key retained")
	}
}

func TestProviderCredentialMigrationFailurePreservesLegacy(t *testing.T) {
	for _, stage := range []string{"bridge", "seal", "verify", "disk"} {
		t.Run(stage, func(t *testing.T) {
			m, _, before := legacyProfileFixture(t)
			switch stage {
			case "bridge":
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
			if _, err := m.ListProviderProfiles(context.Background()); err == nil {
				t.Fatal("failed migration accepted")
			}
			after, err := os.ReadFile(m.path(profilesFile))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("migration failure modified prior profile", err)
			}
		})
	}
}

func TestProviderCredentialCommitFailurePreservesOldKeyAndCollectsOrphans(t *testing.T) {
	m, _, _ := legacyProfileFixture(t)
	cfg, err := m.readProfiles()
	if err != nil {
		t.Fatal(err)
	}
	oldRef := cfg.Profiles["work"].CredentialRef
	before, err := os.ReadFile(m.path(profilesFile))
	if err != nil {
		t.Fatal(err)
	}
	originalDir := m.dir
	m.dir = filepath.Join(m.homeDir, "missing")
	cfg.Profiles["work"].APIKey = "next-key"
	if err := m.writeProfiles(cfg); err == nil {
		t.Fatal("expected write failure")
	}
	m.dir = originalDir
	after, _ := os.ReadFile(m.path(profilesFile))
	if !bytes.Equal(before, after) || cfg.Profiles["work"].CredentialRef != oldRef {
		t.Fatal("failed write damaged old snapshot")
	}
	p, err := newTestManager(m.homeDir).GetProviderProfile(context.Background(), "work")
	if err != nil || p.APIKey != "legacy-model-secret" {
		t.Fatal("old key no longer usable", err)
	}
	if err := m.writeProfiles(cfg); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(m.homeDir, "credentials", "providers"))
	if err != nil || len(entries) != 1 {
		t.Fatal("orphan cleanup failed", err)
	}
	if err := os.WriteFile(filepath.Join(m.homeDir, "credentials", "providers", cfg.Profiles["work"].CredentialRef), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetProviderProfile(context.Background(), "work"); err == nil {
		t.Fatal("corrupt key silently accepted")
	}
}

func TestProviderCredentialsUnavailableDoesNotFallbackToPlaintext(t *testing.T) {
	m := NewManager(t.TempDir())
	if err := m.Prepare(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.PutProviderProfile(ctx, &store.ProviderProfile{ID: "local", Protocol: "openai-compatible", BaseURL: "http://localhost"}); err != nil {
		t.Fatal("keyless profile requires no bridge", err)
	}
	if err := m.PutProviderProfile(ctx, &store.ProviderProfile{ID: "remote", APIKey: "new-model-secret"}); !errors.Is(err, credentialstore.ErrUnavailable) {
		t.Fatal("missing bridge must fail closed", err)
	}
	raw, _ := os.ReadFile(m.path(profilesFile))
	if bytes.Contains(raw, []byte("new-model-secret")) {
		t.Fatal("plaintext fallback")
	}
}
