// Package credentialtest supplies an explicit test-only cipher. Never use it in a daemon.
package credentialtest

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
)

type Cipher struct{}

func (Cipher) aead() cipher.AEAD {
	b, _ := aes.NewCipher(make([]byte, 32))
	a, _ := cipher.NewGCM(b)
	return a
}
func (c Cipher) Seal(p []byte) ([]byte, error) {
	a := c.aead()
	n := make([]byte, a.NonceSize())
	if _, e := rand.Read(n); e != nil {
		return nil, e
	}
	return a.Seal(n, n, p, nil), nil
}
func (c Cipher) Open(p []byte) ([]byte, error) {
	a := c.aead()
	n := a.NonceSize()
	if len(p) < n {
		return nil, errors.New("invalid ciphertext")
	}
	return a.Open(nil, p[:n], p[n:], nil)
}
