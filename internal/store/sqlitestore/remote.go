package sqlitestore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/store"
)

func remoteSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func remoteHash(secret string) string {
	hash := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(hash[:])
}
func remoteDesktopID(ctx context.Context, tx *sql.Tx) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT desktop_id FROM remote_identity WHERE singleton=1`).Scan(&id)
	if !errors.Is(err, sql.ErrNoRows) {
		return id, err
	}
	secret, err := remoteSecret()
	if err != nil {
		return "", err
	}
	id = "desktop_" + secret
	_, err = tx.ExecContext(ctx, `INSERT INTO remote_identity(singleton,desktop_id) VALUES(1,?)`, id)
	return id, err
}
func (s *Store) RemoteAccess(ctx context.Context) (*store.RemoteAccess, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	id, err := remoteDesktopID(ctx, tx)
	if err != nil {
		return nil, err
	}
	out := &store.RemoteAccess{DesktopID: id, Devices: []store.RemoteDevice{}, Pairings: []store.RemotePairing{}}
	now := time.Now().UnixMilli()
	rows, err := tx.QueryContext(ctx, `SELECT id,name,mode,origin,created_at,expires_at FROM remote_devices WHERE expires_at>? ORDER BY created_at,id`, now)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d store.RemoteDevice
		var created, expires int64
		if err := rows.Scan(&d.ID, &d.Name, &d.Mode, &d.Origin, &created, &expires); err != nil {
			rows.Close()
			return nil, err
		}
		d.CreatedAt = time.UnixMilli(created).UTC()
		d.ExpiresAt = time.UnixMilli(expires).UTC()
		out.Devices = append(out.Devices, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT id,mode,origin,status,device_name,expires_at FROM remote_pairings WHERE expires_at>? ORDER BY expires_at,id`, now)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p store.RemotePairing
		var expires int64
		if err := rows.Scan(&p.ID, &p.Mode, &p.Origin, &p.Status, &p.DeviceName, &expires); err != nil {
			rows.Close()
			return nil, err
		}
		p.ExpiresAt = time.UnixMilli(expires).UTC()
		out.Pairings = append(out.Pairings, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return out, tx.Commit()
}
func (s *Store) CreateRemotePairing(ctx context.Context, scope store.RemoteScope) (*store.RemotePairingCode, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	out := &store.RemotePairingCode{ID: store.NewID("pair"), Code: base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), ExpiresAt: time.Now().UTC().Add(store.RemotePairingLifetime)}
	_, err := s.db.ExecContext(ctx, `INSERT INTO remote_pairings(id,mode,origin,status,code_hash,expires_at) VALUES(?,?,?,'pending',?,?)`, out.ID, scope.Mode, scope.Origin, remoteHash(out.Code), out.ExpiresAt.UnixMilli())
	return out, err
}
func (s *Store) RequestRemotePairing(ctx context.Context, in store.RemotePairingRequest) (*store.RemotePairingRequested, error) {
	if err := in.RemoteScope.Validate(); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.DeviceName)
	if name == "" || utf8.RuneCountInString(name) > 80 || strings.ContainsAny(name, "\r\n") {
		return nil, store.ErrInvalidRemote
	}
	code := strings.ToUpper(strings.TrimSpace(in.Code))
	if len(code) != 8 {
		return nil, store.ErrRemoteUnauthorized
	}
	poll, err := remoteSecret()
	if err != nil {
		return nil, err
	}
	var id string
	err = s.db.QueryRowContext(ctx, `UPDATE remote_pairings SET status='requested',code_hash=NULL,poll_hash=?,device_name=? WHERE code_hash=? AND mode=? AND origin=? AND status='pending' AND expires_at>? RETURNING id`, remoteHash(poll), name, remoteHash(code), in.Mode, in.Origin, time.Now().UnixMilli()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrRemoteUnauthorized
	}
	if err != nil {
		return nil, err
	}
	return &store.RemotePairingRequested{ID: id, PollToken: poll, Status: "pending"}, nil
}
func (s *Store) ApproveRemotePairing(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE remote_pairings SET status='approved' WHERE id=? AND status='requested' AND expires_at>?`, id, time.Now().UnixMilli())
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return store.ErrRemoteConflict
	}
	return nil
}
func (s *Store) PollRemotePairing(ctx context.Context, id string, in store.RemotePollInput) (*store.RemotePollResult, error) {
	if err := in.RemoteScope.Validate(); err != nil {
		return nil, err
	}
	if len(in.PollToken) != 43 {
		return nil, store.ErrRemoteUnauthorized
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status, name string
	err = tx.QueryRowContext(ctx, `SELECT status,device_name FROM remote_pairings WHERE id=? AND poll_hash=? AND mode=? AND origin=? AND expires_at>?`, id, remoteHash(in.PollToken), in.Mode, in.Origin, time.Now().UnixMilli()).Scan(&status, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrRemoteUnauthorized
	}
	if err != nil {
		return nil, err
	}
	if status != "approved" {
		return &store.RemotePollResult{Status: "pending"}, nil
	}
	if _, err = remoteDesktopID(ctx, tx); err != nil {
		return nil, err
	}
	token, err := remoteSecret()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	d := &store.RemoteDevice{ID: store.NewID("device"), Name: name, RemoteScope: in.RemoteScope, CreatedAt: now, ExpiresAt: now.Add(store.RemoteDeviceLifetime)}
	if _, err = tx.ExecContext(ctx, `INSERT INTO remote_devices(id,name,mode,origin,credential_hash,created_at,expires_at) VALUES(?,?,?,?,?,?,?)`, d.ID, d.Name, d.Mode, d.Origin, remoteHash(token), d.CreatedAt.UnixMilli(), d.ExpiresAt.UnixMilli()); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM remote_pairings WHERE id=?`, id); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &store.RemotePollResult{Status: "approved", Token: token, Device: d}, nil
}
func (s *Store) DeleteRemotePairing(ctx context.Context, id string) error {
	return s.deleteRemote(ctx, `DELETE FROM remote_pairings WHERE id=?`, id)
}
func (s *Store) DeleteRemoteDevice(ctx context.Context, id string) error {
	return s.deleteRemote(ctx, `DELETE FROM remote_devices WHERE id=?`, id)
}
func (s *Store) deleteRemote(ctx context.Context, query, id string) error {
	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}
func (s *Store) AuthorizeRemote(ctx context.Context, in store.RemoteAuthorizeInput) (*store.RemoteAuthorization, error) {
	if err := in.RemoteScope.Validate(); err != nil {
		return nil, err
	}
	if len(in.Token) != 43 {
		return nil, store.ErrRemoteUnauthorized
	}
	out := &store.RemoteAuthorization{}
	d := &out.Device
	var created, expires int64
	err := s.db.QueryRowContext(ctx, `SELECT i.desktop_id,d.id,d.name,d.mode,d.origin,d.created_at,d.expires_at FROM remote_devices d CROSS JOIN remote_identity i WHERE i.singleton=1 AND d.credential_hash=? AND d.mode=? AND d.origin=? AND d.expires_at>?`, remoteHash(in.Token), in.Mode, in.Origin, time.Now().UnixMilli()).Scan(&out.DesktopID, &d.ID, &d.Name, &d.Mode, &d.Origin, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrRemoteUnauthorized
	}
	if err != nil {
		return nil, err
	}
	d.CreatedAt = time.UnixMilli(created).UTC()
	d.ExpiresAt = time.UnixMilli(expires).UTC()
	return out, nil
}
