package credentialstore

import (
	"bytes"
	"github.com/teatak/pudding-core/internal/credentialstore/credentialtest"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordOwnerIntegrityAndReferenceValidation(t *testing.T) {
	home := t.TempDir()
	s := New(home, credentialtest.Cipher{})
	payload := []byte(`{"sid":"test-secret"}`)
	ref, err := s.Write("nas", payload)
	if err != nil {
		t.Fatal(err)
	}
	got, err := New(home, credentialtest.Cipher{}).Read(ref, "nas")
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal("roundtrip failed", err)
	}
	if _, err := s.Read(ref, "other"); err == nil {
		t.Fatal("swapped record accepted")
	}
	if _, err := s.Read("../outside", "nas"); err == nil {
		t.Fatal("invalid ref accepted")
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, ref))
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 1
	if err := os.WriteFile(filepath.Join(s.dir, ref), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(ref, "nas"); err == nil {
		t.Fatal("tampering accepted")
	}
}

func TestNamespacesCannotReuseEachOthersRecords(t *testing.T) {
	home := t.TempDir()
	cipher := credentialtest.Cipher{}
	plugins, profiles := New(home, cipher), NewScoped(home, "providers", cipher)
	ref, err := plugins.Write("same-id", []byte(`{"apiKey":"test-key"}`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(plugins.dir, ref))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(profiles.dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profiles.dir, ref), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := profiles.Read(ref, "same-id"); err == nil {
		t.Fatal("cross-namespace record accepted")
	}
	if err := profiles.Prune(map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if _, err := plugins.Read(ref, "same-id"); err != nil {
		t.Fatal("profile cleanup pruned plugin key", err)
	}
}
