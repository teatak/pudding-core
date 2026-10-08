package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

var remoteLAN = store.RemoteScope{Mode: "lan", Origin: "http://192.168.1.10:18443"}
var remoteRelay = store.RemoteScope{Mode: "relay", Origin: "https://phone.example.com"}

func claimedRemote(t *testing.T, s *Store, scope store.RemoteScope) (*store.RemotePairingCode, *store.RemotePairingClaim) {
	t.Helper()
	ctx := context.Background()
	code, err := s.CreateRemotePairing(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := s.RequestRemotePairing(ctx, store.RemotePairingRequest{RemoteScope: scope, Code: code.Code, DeviceName: "Browser device"})
	if err != nil {
		t.Fatal(err)
	}
	return code, claim
}
func TestRemoteCodeClaimScopeHashAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pudding.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.RemoteAccess(ctx)
	if err != nil {
		t.Fatal(err)
	}
	code, err := s.CreateRemotePairing(ctx, remoteLAN)
	if err != nil {
		t.Fatal(err)
	}
	var digest string
	if err := s.db.QueryRow(`SELECT code_hash FROM remote_pairings WHERE id=?`, code.ID).Scan(&digest); err != nil {
		t.Fatal(err)
	}
	if digest != remoteHash(code.Code) || digest == code.Code {
		t.Fatal("plaintext code stored")
	}
	for _, scope := range []store.RemoteScope{remoteRelay, {Mode: "lan", Origin: "http://192.168.1.11:18443"}, {Mode: "lan", Origin: "http://192.168.1.10:18444"}} {
		if _, err := s.RequestRemotePairing(ctx, store.RemotePairingRequest{RemoteScope: scope, Code: code.Code, DeviceName: "Browser device"}); !errors.Is(err, store.ErrRemoteUnauthorized) {
			t.Fatal("code scope not bound", scope, err)
		}
	}
	claim, err := s.RequestRemotePairing(ctx, store.RemotePairingRequest{RemoteScope: remoteLAN, Code: code.Code, DeviceName: "Browser device"})
	if err != nil {
		t.Fatal(err)
	}
	if claim.Token == "" || claim.Device.Name != "Browser device" {
		t.Fatal("missing direct grant", claim)
	}
	if _, err := s.RequestRemotePairing(ctx, store.RemotePairingRequest{RemoteScope: remoteLAN, Code: code.Code, DeviceName: "again"}); !errors.Is(err, store.ErrRemoteUnauthorized) {
		t.Fatal("code reused", err)
	}
	if err := s.db.QueryRow(`SELECT credential_hash FROM remote_devices WHERE id=?`, claim.Device.ID).Scan(&digest); err != nil {
		t.Fatal(err)
	}
	if digest != remoteHash(claim.Token) || digest == claim.Token {
		t.Fatal("plaintext credential stored")
	}
	assertWorkspaceMigrationValue(t, s.db, `SELECT COUNT(*) FROM remote_pairings`, "0")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	auth, err := s.AuthorizeRemote(ctx, store.RemoteAuthorizeInput{RemoteScope: remoteLAN, Token: claim.Token})
	if err != nil || auth.DesktopID != before.DesktopID || auth.Device.ID != claim.Device.ID {
		t.Fatal("restart lost authority", auth, err)
	}
	for _, scope := range []store.RemoteScope{remoteRelay, {Mode: "lan", Origin: "http://192.168.1.11:18443"}, {Mode: "lan", Origin: "http://192.168.1.10:18444"}} {
		if _, err := s.AuthorizeRemote(ctx, store.RemoteAuthorizeInput{RemoteScope: scope, Token: claim.Token}); !errors.Is(err, store.ErrRemoteUnauthorized) {
			t.Fatal("credential scope not bound", scope, err)
		}
	}
	_, relay := claimedRemote(t, s, remoteRelay)
	if err := s.DeleteRemoteDevice(ctx, claim.Device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthorizeRemote(ctx, store.RemoteAuthorizeInput{RemoteScope: remoteLAN, Token: claim.Token}); !errors.Is(err, store.ErrRemoteUnauthorized) {
		t.Fatal("revoked token accepted", err)
	}
	if _, err := s.AuthorizeRemote(ctx, store.RemoteAuthorizeInput{RemoteScope: remoteRelay, Token: relay.Token}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE remote_devices SET expires_at=1 WHERE id=?`, relay.Device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthorizeRemote(ctx, store.RemoteAuthorizeInput{RemoteScope: remoteRelay, Token: relay.Token}); !errors.Is(err, store.ErrRemoteUnauthorized) {
		t.Fatal("expired token accepted", err)
	}
}
func TestRemoteConcurrentCodeClaimCreatesExactlyOneGrant(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	code, err := s.CreateRemotePairing(ctx, remoteLAN)
	if err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claim, err := s.RequestRemotePairing(ctx, store.RemotePairingRequest{RemoteScope: remoteLAN, Code: code.Code, DeviceName: "Browser device"})
			if err == nil {
				if claim.Token == "" {
					t.Error("missing token")
				}
				successes.Add(1)
			} else if !errors.Is(err, store.ErrRemoteUnauthorized) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("claimed %d times", successes.Load())
	}
	access, err := s.RemoteAccess(ctx)
	if err != nil || len(access.Devices) != 1 || len(access.Pairings) != 0 {
		t.Fatal(access, err)
	}
}
func TestRemoteCodeCancellationExpiryAndClaimRollback(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	code, err := s.CreateRemotePairing(ctx, remoteLAN)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRemotePairing(ctx, code.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRemotePairing(ctx, code.ID); err != nil {
		t.Fatal("cancel not idempotent", err)
	}
	if _, err := s.RequestRemotePairing(ctx, store.RemotePairingRequest{RemoteScope: remoteLAN, Code: code.Code, DeviceName: "Browser device"}); !errors.Is(err, store.ErrRemoteUnauthorized) {
		t.Fatal("cancelled code accepted", err)
	}
	code, err = s.CreateRemotePairing(ctx, remoteLAN)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE remote_pairings SET expires_at=1 WHERE id=?`, code.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestRemotePairing(ctx, store.RemotePairingRequest{RemoteScope: remoteLAN, Code: code.Code, DeviceName: "Browser device"}); !errors.Is(err, store.ErrRemoteUnauthorized) {
		t.Fatal("expired code accepted", err)
	}
	code, err = s.CreateRemotePairing(ctx, remoteLAN)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestRemotePairing(ctx, store.RemotePairingRequest{RemoteScope: remoteLAN, Code: code.Code, DeviceName: strings.Repeat("x", 81)}); !errors.Is(err, store.ErrInvalidRemote) {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER reject_remote_grant BEFORE INSERT ON remote_devices BEGIN SELECT RAISE(ABORT,'fixture failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestRemotePairing(ctx, store.RemotePairingRequest{RemoteScope: remoteLAN, Code: code.Code, DeviceName: "Browser device"}); err == nil {
		t.Fatal("failed insert claimed code")
	}
	assertWorkspaceMigrationValue(t, s.db, `SELECT COUNT(*) FROM remote_pairings WHERE id='`+code.ID+`'`, "1")
	assertWorkspaceMigrationValue(t, s.db, `SELECT COUNT(*) FROM remote_devices`, "0")
	if _, err := s.db.Exec(`DROP TRIGGER reject_remote_grant`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestRemotePairing(ctx, store.RemotePairingRequest{RemoteScope: remoteLAN, Code: code.Code, DeviceName: "Browser device"}); err != nil {
		t.Fatal("rollback lost code", err)
	}
}
func remoteV31Fixture(t *testing.T, path string) string {
	t.Helper()
	db := openMigrationTestDB(t, path)
	defer db.Close()
	schema, err := os.ReadFile("testdata/schema-v31.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	token, err := remoteSecret()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO sessions(id,provider,model,created_at,updated_at,last_activity_at) VALUES('old','mock','model',1,1,1);
 INSERT INTO messages(id,session_id,role,text,created_at) VALUES('old-message','old','user','preserve this',1);
 INSERT INTO remote_identity(singleton,desktop_id) VALUES(1,'desktop_preserved');
 INSERT INTO remote_devices(id,name,mode,origin,credential_hash,created_at,expires_at) VALUES('device_preserved','Browser device','lan',?,?,1,?);
 INSERT INTO remote_pairings(id,mode,origin,status,code_hash,poll_hash,expires_at) VALUES('p_pending','lan',?,'pending','code_hash',NULL,?),('p_requested','lan',?,'requested',NULL,'poll_requested',?),('p_approved','lan',?,'approved',NULL,'poll_approved',?);
 PRAGMA user_version=31;`, remoteLAN.Origin, remoteHash(token), time.Now().Add(time.Hour).UnixMilli(), remoteLAN.Origin, time.Now().Add(time.Minute).UnixMilli(), remoteLAN.Origin, time.Now().Add(time.Minute).UnixMilli(), remoteLAN.Origin, time.Now().Add(time.Minute).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	return token
}
func TestRemoteMigration32InvalidatesOldCodesPreservesGrantsAndRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	token := remoteV31Fixture(t, path)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	assertWorkspaceMigrationValue(t, s.db, "PRAGMA user_version", fmt.Sprint(currentSchemaVersion))
	assertWorkspaceMigrationValue(t, s.db, "SELECT text FROM messages WHERE id='old-message'", "preserve this")
	assertWorkspaceMigrationValue(t, s.db, "SELECT COUNT(*) FROM remote_pairings", "0")
	assertWorkspaceMigrationValue(t, s.db, "SELECT COUNT(*) FROM pragma_table_info('remote_pairings') WHERE name IN ('status','poll_hash','device_name')", "0")
	auth, err := s.AuthorizeRemote(context.Background(), store.RemoteAuthorizeInput{RemoteScope: remoteLAN, Token: token})
	if err != nil || auth.DesktopID != "desktop_preserved" || auth.Device.ID != "device_preserved" {
		t.Fatal(auth, err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.AuthorizeRemote(context.Background(), store.RemoteAuthorizeInput{RemoteScope: remoteLAN, Token: token}); err != nil {
		t.Fatal("restart lost grant", err)
	}
	if _, err := s.RequestRemotePairing(context.Background(), store.RemotePairingRequest{RemoteScope: remoteLAN, Code: "ABCDEFGH", DeviceName: "Browser device"}); !errors.Is(err, store.ErrRemoteUnauthorized) {
		t.Fatal("old code acquired new semantics", err)
	}
}
func TestRemoteMigration32RollbackPreservesOldCodesAndDevices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	token := remoteV31Fixture(t, path)
	db := openMigrationTestDB(t, path)
	sentinel := errors.New("fixture failure")
	err := runSchemaMigration(db, 32, func(tx *sql.Tx) error {
		if err := migratePreauthorizedRemotePairings(tx); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	assertWorkspaceMigrationValue(t, db, "PRAGMA user_version", "31")
	assertWorkspaceMigrationValue(t, db, "SELECT COUNT(*) FROM remote_pairings", "3")
	assertWorkspaceMigrationValue(t, db, "SELECT COUNT(*) FROM remote_devices", "1")
	assertWorkspaceMigrationValue(t, db, "SELECT text FROM messages WHERE id='old-message'", "preserve this")
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.AuthorizeRemote(context.Background(), store.RemoteAuthorizeInput{RemoteScope: remoteLAN, Token: token}); err != nil {
		t.Fatal(err)
	}
}
