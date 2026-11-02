// Package credentialstore persists immutable, encrypted credential records.
// The OS-backed cipher is owned by Desktop; no plaintext fallback is provided.
package credentialstore

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
)

var ErrUnavailable = errors.New("credential_storage_unavailable")
var validRef = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Cipher interface {
	Seal([]byte) ([]byte, error)
	Open([]byte) ([]byte, error)
}
type Store struct {
	scope  string
	dir    string
	cipher Cipher
}

func New(home string, cipher Cipher) *Store {
	return NewScoped(home, "plugins", cipher)
}

// NewScoped isolates independent config snapshots and their orphan cleanup.
// scope is a host-owned constant, never external input.
func NewScoped(home, scope string, cipher Cipher) *Store {
	return &Store{scope: scope, dir: filepath.Join(home, "credentials", scope), cipher: cipher}
}

func (s *Store) owner(owner string) string {
	// Keep existing plugin records readable. Plugin connection IDs cannot contain /.
	if s.scope == "plugins" {
		return owner
	}
	return s.scope + "/" + owner
}

type record struct {
	Owner string          `json:"owner"`
	Data  json.RawMessage `json:"data"`
}

func (s *Store) Read(ref, owner string) ([]byte, error) {
	if !validRef.MatchString(ref) {
		return nil, errors.New("invalid credential reference")
	}
	sealed, err := os.ReadFile(filepath.Join(s.dir, ref))
	if err != nil {
		return nil, fmt.Errorf("credential record unavailable: %w", err)
	}
	data, err := s.cipher.Open(sealed)
	if err != nil {
		return nil, err
	}
	var r record
	if json.Unmarshal(data, &r) != nil || r.Owner != s.owner(owner) || !json.Valid(r.Data) {
		return nil, errors.New("invalid credential record")
	}
	return r.Data, nil
}
func (s *Store) Write(owner string, data []byte) (string, error) {
	plain, err := json.Marshal(record{s.owner(owner), data})
	if err != nil {
		return "", err
	}
	sealed, err := s.cipher.Seal(plain)
	if err != nil {
		return "", err
	}
	// Verify before committing a reference or removing legacy plaintext.
	opened, err := s.cipher.Open(sealed)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(plain, opened) {
		return "", errors.New("credential verification failed")
	}
	if err = os.MkdirAll(s.dir, 0700); err != nil {
		return "", err
	}
	id := make([]byte, 32)
	if _, err = rand.Read(id); err != nil {
		return "", err
	}
	ref := hex.EncodeToString(id)
	f, err := os.OpenFile(filepath.Join(s.dir, ref), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	_, err = f.Write(sealed)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(filepath.Join(s.dir, ref))
		return "", err
	}
	// Persist the new record and newly created directories before a config can
	// reference it. Never delete old records until the config is also durable.
	for _, dir := range []string{s.dir, filepath.Dir(s.dir), filepath.Dir(filepath.Dir(s.dir))} {
		if err = SyncDirectory(dir); err != nil {
			return "", err
		}
	}
	return ref, nil
}

// SyncDirectory orders file creation/renames on Unix. Windows does not support
// fsync on directory handles; file data is flushed before its atomic rename.
func SyncDirectory(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// Prune is called only after the referencing config snapshot commits. Encrypted
// orphans from an interrupted write are harmless and are removed on the next commit.
func (s *Store) Prune(live map[string]bool) error {
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() && validRef.MatchString(e.Name()) && !live[e.Name()] {
			if err = os.Remove(filepath.Join(s.dir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
