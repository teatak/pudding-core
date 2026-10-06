package sqlitestore

import (
	"context"
	"errors"
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

func requestedRemote(t *testing.T, s *Store, scope store.RemoteScope) (*store.RemotePairingCode, *store.RemotePairingRequested) {
	t.Helper()
	ctx := context.Background()
	code, err := s.CreateRemotePairing(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	request, err := s.RequestRemotePairing(ctx, store.RemotePairingRequest{RemoteScope: scope, Code: code.Code, DeviceName: "My phone"})
	if err != nil {
		t.Fatal(err)
	}
	return code, request
}
func approvedRemote(t *testing.T, s *Store, scope store.RemoteScope) (*store.RemotePairingRequested, *store.RemotePollResult) {
	t.Helper()
	ctx := context.Background()
	_, request := requestedRemote(t, s, scope)
	if err := s.ApproveRemotePairing(ctx, request.ID); err != nil {
		t.Fatal(err)
	}
	result, err := s.PollRemotePairing(ctx, request.ID, store.RemotePollInput{RemoteScope: scope, PollToken: request.PollToken})
	if err != nil {
		t.Fatal(err)
	}
	return request, result
}
func TestRemotePairingCredentialsScopeAndRestart(t *testing.T) {
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
	code, request := requestedRemote(t, s, remoteLAN)
	var codeHash, pollHash string
	if err := s.db.QueryRow(`SELECT coalesce(code_hash,''),poll_hash FROM remote_pairings WHERE id=?`, request.ID).Scan(&codeHash, &pollHash); err != nil {
		t.Fatal(err)
	}
	if codeHash != "" || pollHash == request.PollToken || pollHash != remoteHash(request.PollToken) {
		t.Fatal("pairing stores plaintext or retains consumed code")
	}
	if _, err := s.RequestRemotePairing(ctx, store.RemotePairingRequest{RemoteScope: remoteLAN, Code: code.Code, DeviceName: "again"}); !errors.Is(err, store.ErrRemoteUnauthorized) {
		t.Fatal("code reused", err)
	}
	for _, scope := range []store.RemoteScope{remoteRelay, {Mode: "lan", Origin: "http://192.168.1.11:18443"}, {Mode: "lan", Origin: "http://192.168.1.10:18444"}} {
		if _, err := s.PollRemotePairing(ctx, request.ID, store.RemotePollInput{RemoteScope: scope, PollToken: request.PollToken}); !errors.Is(err, store.ErrRemoteUnauthorized) {
			t.Fatal("poll scope not bound", scope, err)
		}
	}
	pending, err := s.PollRemotePairing(ctx, request.ID, store.RemotePollInput{RemoteScope: remoteLAN, PollToken: request.PollToken})
	if err != nil || pending.Status != "pending" || pending.Token != "" || pending.Device != nil {
		t.Fatal(pending, err)
	}
	if err := s.ApproveRemotePairing(ctx, request.ID); err != nil {
		t.Fatal(err)
	}
	result, err := s.PollRemotePairing(ctx, request.ID, store.RemotePollInput{RemoteScope: remoteLAN, PollToken: request.PollToken})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "approved" || result.Token == "" || result.Device == nil {
		t.Fatal("missing claim", result)
	}
	if _, err := s.PollRemotePairing(ctx, request.ID, store.RemotePollInput{RemoteScope: remoteLAN, PollToken: request.PollToken}); !errors.Is(err, store.ErrRemoteUnauthorized) {
		t.Fatal("poll reused", err)
	}
	var credentialHash string
	if err := s.db.QueryRow(`SELECT credential_hash FROM remote_devices WHERE id=?`, result.Device.ID).Scan(&credentialHash); err != nil {
		t.Fatal(err)
	}
	if credentialHash == result.Token || credentialHash != remoteHash(result.Token) {
		t.Fatal("device plaintext stored")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	auth, err := s.AuthorizeRemote(ctx, store.RemoteAuthorizeInput{RemoteScope: remoteLAN, Token: result.Token})
	if err != nil || auth.DesktopID != before.DesktopID || auth.Device.ID != result.Device.ID {
		t.Fatal("restart lost identity or credential", auth, err)
	}
	for _, scope := range []store.RemoteScope{remoteRelay, {Mode: "lan", Origin: "http://192.168.1.11:18443"}, {Mode: "lan", Origin: "http://192.168.1.10:18444"}} {
		if _, err := s.AuthorizeRemote(ctx, store.RemoteAuthorizeInput{RemoteScope: scope, Token: result.Token}); !errors.Is(err, store.ErrRemoteUnauthorized) {
			t.Fatal("credential crossed origin/mode", scope, err)
		}
	}
	_, relay := approvedRemote(t, s, remoteRelay)
	if relay.Device.ID == result.Device.ID {
		t.Fatal("registrations merged across modes")
	}
	if err := s.DeleteRemoteDevice(ctx, result.Device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthorizeRemote(ctx, store.RemoteAuthorizeInput{RemoteScope: remoteLAN, Token: result.Token}); !errors.Is(err, store.ErrRemoteUnauthorized) {
		t.Fatal("revoked token accepted", err)
	}
	if _, err := s.AuthorizeRemote(ctx, store.RemoteAuthorizeInput{RemoteScope: remoteRelay, Token: relay.Token}); err != nil {
		t.Fatal("revoke changed other registration", err)
	}
	if _, err := s.db.Exec(`UPDATE remote_devices SET expires_at=? WHERE id=?`, time.Now().UnixMilli()-1, relay.Device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthorizeRemote(ctx, store.RemoteAuthorizeInput{RemoteScope: remoteRelay, Token: relay.Token}); !errors.Is(err, store.ErrRemoteUnauthorized) {
		t.Fatal("expired token accepted", err)
	}
	after, err := s.RemoteAccess(ctx)
	if err != nil || after.DesktopID != before.DesktopID || len(after.Devices) != 0 || after.Devices == nil || after.Pairings == nil {
		t.Fatal(after, err)
	}
}
func TestRemoteConcurrentRequestAndClaim(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	code, err := s.CreateRemotePairing(ctx, remoteLAN)
	if err != nil {
		t.Fatal(err)
	}
	var requested atomic.Int32
	var wg sync.WaitGroup
	var request *store.RemotePairingRequested
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := s.RequestRemotePairing(ctx, store.RemotePairingRequest{RemoteScope: remoteLAN, Code: code.Code, DeviceName: "Race phone"})
			if err == nil {
				requested.Add(1)
				request = out
			} else if !errors.Is(err, store.ErrRemoteUnauthorized) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if requested.Load() != 1 {
		t.Fatalf("code consumed %d times", requested.Load())
	}
	if err := s.ApproveRemotePairing(ctx, request.ID); err != nil {
		t.Fatal(err)
	}
	var claimed atomic.Int32
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := s.PollRemotePairing(ctx, request.ID, store.RemotePollInput{RemoteScope: remoteLAN, PollToken: request.PollToken})
			if err == nil {
				if out.Token == "" {
					t.Error("approved claim missing token")
				}
				claimed.Add(1)
			} else if !errors.Is(err, store.ErrRemoteUnauthorized) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if claimed.Load() != 1 {
		t.Fatalf("credential claimed %d times", claimed.Load())
	}
	snapshot, err := s.RemoteAccess(ctx)
	if err != nil || len(snapshot.Devices) != 1 || len(snapshot.Pairings) != 0 {
		t.Fatal(snapshot, err)
	}
}
func TestRemotePairingExpiryCancellationAndInvalidInput(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	for _, scope := range []store.RemoteScope{{Mode: "other", Origin: remoteLAN.Origin}, {Mode: "lan", Origin: "http://localhost"}, {Mode: "lan", Origin: "https://phone.example/"}, {Mode: "lan", Origin: "https://user@phone.example"}, {Mode: "lan", Origin: "https://phone.example?secret=yes"}} {
		if _, err := s.CreateRemotePairing(ctx, scope); !errors.Is(err, store.ErrInvalidRemote) {
			t.Fatal(scope, err)
		}
	}
	code, err := s.CreateRemotePairing(ctx, remoteLAN)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApproveRemotePairing(ctx, code.ID); !errors.Is(err, store.ErrRemoteConflict) {
		t.Fatal("approved unrequested pairing", err)
	}
	if _, err := s.RequestRemotePairing(ctx, store.RemotePairingRequest{RemoteScope: remoteRelay, Code: code.Code, DeviceName: "phone"}); !errors.Is(err, store.ErrRemoteUnauthorized) {
		t.Fatal("request scope not bound", err)
	}
	if _, err := s.RequestRemotePairing(ctx, store.RemotePairingRequest{RemoteScope: remoteLAN, Code: code.Code, DeviceName: strings.Repeat("x", 81)}); !errors.Is(err, store.ErrInvalidRemote) {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE remote_pairings SET expires_at=1 WHERE id=?`, code.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestRemotePairing(ctx, store.RemotePairingRequest{RemoteScope: remoteLAN, Code: code.Code, DeviceName: "phone"}); !errors.Is(err, store.ErrRemoteUnauthorized) {
		t.Fatal("expired code accepted", err)
	}
	_, request := requestedRemote(t, s, remoteLAN)
	if _, err := s.db.Exec(`UPDATE remote_pairings SET expires_at=1 WHERE id=?`, request.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.ApproveRemotePairing(ctx, request.ID); !errors.Is(err, store.ErrRemoteConflict) {
		t.Fatal("approved expired pairing", err)
	}
	if _, err := s.PollRemotePairing(ctx, request.ID, store.RemotePollInput{RemoteScope: remoteLAN, PollToken: request.PollToken}); !errors.Is(err, store.ErrRemoteUnauthorized) {
		t.Fatal("expired poll accepted", err)
	}
	_, request = requestedRemote(t, s, remoteLAN)
	if err := s.DeleteRemotePairing(ctx, request.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PollRemotePairing(ctx, request.ID, store.RemotePollInput{RemoteScope: remoteLAN, PollToken: request.PollToken}); !errors.Is(err, store.ErrRemoteUnauthorized) {
		t.Fatal("cancelled poll accepted", err)
	}
}
func remoteV30Fixture(t *testing.T, path string) {
	t.Helper()
	db := openMigrationTestDB(t, path)
	defer db.Close()
	schema := store.SchemaSQL[:strings.Index(store.SchemaSQL, "\nCREATE TABLE remote_identity")]
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions(id,provider,model,created_at,updated_at,last_activity_at) VALUES('old','mock','model',1,1,1); INSERT INTO messages(id,session_id,role,text,created_at) VALUES('old-message','old','user','preserve this',1); PRAGMA user_version=30;`); err != nil {
		t.Fatal(err)
	}
}
func TestRemoteMigrationPreservesOldDataAndRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	remoteV30Fixture(t, path)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	assertWorkspaceMigrationValue(t, s.db, "PRAGMA user_version", "31")
	assertWorkspaceMigrationValue(t, s.db, "SELECT text FROM messages WHERE id='old-message'", "preserve this")
	before, err := s.RemoteAccess(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	after, err := s.RemoteAccess(context.Background())
	if err != nil || before.DesktopID != after.DesktopID {
		t.Fatal("identity changed", after, err)
	}
	assertWorkspaceMigrationValue(t, s.db, "SELECT COUNT(*) FROM sessions WHERE id='old'", "1")
}
func TestRemoteMigrationFailureRollsBackAndCanRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	remoteV30Fixture(t, path)
	db := openMigrationTestDB(t, path)
	if _, err := db.Exec(`CREATE TABLE remote_devices(conflict TEXT)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("conflicting migration succeeded")
	}
	db = openMigrationTestDB(t, path)
	assertWorkspaceMigrationValue(t, db, "PRAGMA user_version", "30")
	assertWorkspaceMigrationValue(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE name IN ('remote_identity','remote_pairings')", "0")
	assertWorkspaceMigrationValue(t, db, "SELECT text FROM messages WHERE id='old-message'", "preserve this")
	if _, err := db.Exec(`DROP TABLE remote_devices`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	assertWorkspaceMigrationValue(t, s.db, "PRAGMA user_version", "31")
}

func TestRemoteHTTPSLANCredentialCannotMoveToHTTP(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	_, claimed := approvedRemote(t, s, remoteLAN)
	if _, err := s.db.Exec(`UPDATE remote_devices SET origin=? WHERE id=?`, "https://192.168.1.10:18443", claimed.Device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthorizeRemote(ctx, store.RemoteAuthorizeInput{RemoteScope: remoteLAN, Token: claimed.Token}); !errors.Is(err, store.ErrRemoteUnauthorized) {
		t.Fatal("credential moved from HTTPS to HTTP", err)
	}
	oldScope := store.RemoteScope{Mode: "lan", Origin: "https://192.168.1.10:18443"}
	if _, err := s.AuthorizeRemote(ctx, store.RemoteAuthorizeInput{RemoteScope: oldScope, Token: claimed.Token}); !errors.Is(err, store.ErrInvalidRemote) {
		t.Fatal("HTTPS LAN compatibility remains", err)
	}
}
