package config

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestUpdateProviderProfileAtomicAndExistingOnly(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	m := NewManager(dir)
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
	p, err := NewManager(dir).GetProviderProfile(ctx, "profile")
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
	if _, err := NewManager(dir).GetProviderProfile(ctx, "profile"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted profile restored: %v", err)
	}
}
