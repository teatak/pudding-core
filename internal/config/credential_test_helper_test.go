package config

import "github.com/teatak/pudding-core/internal/credentialstore/credentialtest"

func newTestManager(home string) *Manager {
	return NewManager(home).WithCredentialCipher(credentialtest.Cipher{})
}
