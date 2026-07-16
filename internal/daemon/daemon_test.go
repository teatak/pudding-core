package daemon

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/home"
)

func TestStartClaimsPortBeforeOpeningRuntimeState(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	dir := t.TempDir()
	if _, err := Start(Options{Home: dir, Addr: listener.Addr().String()}); err == nil {
		t.Fatal("expected occupied port error")
	}
	for _, path := range []string{home.DBPath(dir), home.TokenPath(dir)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("runtime state created before port claim: path=%s err=%v", path, err)
		}
	}
}

func TestLoadOrCreateTokenIsAtomicAcrossConcurrentStarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.token")
	const workers = 32
	start := make(chan struct{})
	results := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			token, err := loadOrCreateToken(path)
			if err != nil {
				errs <- err
				return
			}
			results <- token
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var want string
	for token := range results {
		if want == "" {
			want = token
		}
		if token != want {
			t.Fatalf("concurrent token mismatch: got %q, want %q", token, want)
		}
	}
	if len(want) != 48 {
		t.Fatalf("token length = %d, want 48", len(want))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("persisted token = %q, want %q", data, want)
	}
}

func TestLoadOrCreateTokenTrimsPersistedToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.token")
	if err := os.WriteFile(path, []byte("existing-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := loadOrCreateToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if token != "existing-token" {
		t.Fatalf("token = %q, want existing-token", token)
	}
}

func TestLoadOrCreateTokenRecoversStaleEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.token")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-3 * time.Second)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatal(err)
	}
	token, err := loadOrCreateToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 48 {
		t.Fatalf("token length = %d, want 48", len(token))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != token {
		t.Fatalf("persisted token = %q, want %q", data, token)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "daemon.token" {
		t.Fatalf("token candidate leaked: %+v", entries)
	}
}

func TestLoadOrCreateTokenRecoversStaleEmptyFileConcurrently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.token")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-3 * time.Second)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatal(err)
	}

	const workers = 16
	start := make(chan struct{})
	results := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			token, err := loadOrCreateToken(path)
			if err != nil {
				errs <- err
				return
			}
			results <- token
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var want string
	for token := range results {
		if want == "" {
			want = token
		}
		if token != want {
			t.Fatalf("concurrent stale-token recovery mismatch: got %q, want %q", token, want)
		}
	}
}

func TestLoadOrCreateTokenRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside-token")
	if err := os.WriteFile(outside, []byte("outside-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "daemon.token")
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := loadOrCreateToken(path); err == nil {
		t.Fatal("symlinked daemon token should be rejected")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "outside-secret" {
		t.Fatalf("outside token changed: %q %v", data, err)
	}
}

func TestLoadOrCreateTokenTightensPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose POSIX permission bits consistently")
	}
	path := filepath.Join(t.TempDir(), "daemon.token")
	if err := os.WriteFile(path, []byte("existing-token"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateToken(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token permissions = %o, want 600", info.Mode().Perm())
	}
}
