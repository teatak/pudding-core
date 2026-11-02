package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/teatak/pudding-core/internal/store"
)

type profileSecret struct {
	APIKey string `json:"apiKey"`
}

func (m *Manager) readProfiles() (profilesYAML, error) {
	var cfg profilesYAML
	if err := readYAML(m.path(profilesFile), &cfg); err != nil {
		return cfg, err
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Version > 2 {
		return cfg, errors.New("unsupported provider profile storage version")
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]*store.ProviderProfile{}
	}
	legacy := false
	for id, p := range cfg.Profiles {
		if p == nil {
			return cfg, errors.New("invalid provider profile")
		}
		if p.CredentialRef != "" {
			if p.APIKey != "" {
				return cfg, errors.New("mixed plaintext and encrypted provider credentials")
			}
			raw, err := m.profileCredentials.Read(p.CredentialRef, id)
			if err != nil {
				return cfg, err
			}
			var secret profileSecret
			if json.Unmarshal(raw, &secret) != nil || secret.APIKey == "" {
				return cfg, errors.New("invalid provider credentials")
			}
			p.APIKey = secret.APIKey
		} else if p.APIKey != "" {
			legacy = true
		}
	}
	if legacy {
		if err := m.writeProfiles(cfg); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

func (m *Manager) writeProfiles(cfg profilesYAML) error {
	disk := cfg
	disk.Version = 2
	disk.Profiles = make(map[string]*store.ProviderProfile, len(cfg.Profiles))
	live := map[string]bool{}
	for id, p := range cfg.Profiles {
		copy := cloneProfile(id, p)
		copy.CredentialRef = ""
		if p.APIKey != "" {
			data, err := json.Marshal(profileSecret{p.APIKey})
			if err != nil {
				return err
			}
			ref, same := p.CredentialRef, false
			if ref != "" {
				old, err := m.profileCredentials.Read(ref, id)
				if err != nil {
					return err
				}
				same = bytes.Equal(old, data)
			}
			if !same {
				ref, err = m.profileCredentials.Write(id, data)
				if err != nil {
					return err
				}
			}
			copy.CredentialRef = ref
			live[ref] = true
		}
		copy.APIKey = ""
		disk.Profiles[id] = copy
	}
	if err := writeYAML(m.path(profilesFile), disk, true); err != nil {
		return err
	}
	for id, p := range cfg.Profiles {
		p.CredentialRef = disk.Profiles[id].CredentialRef
	}
	if err := m.profileCredentials.Prune(live); err != nil {
		slog.Warn("provider encrypted credential cleanup incomplete")
	}
	return nil
}
