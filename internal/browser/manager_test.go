package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeURLDefaultsHTTPS(t *testing.T) {
	got, err := normalizeURL("example.com/path")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.com/path" {
		t.Fatalf("url = %q", got)
	}
}

func TestNormalizeURLDefaultsLocalhostToHTTP(t *testing.T) {
	got, err := normalizeURL("localhost:5173")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://localhost:5173" {
		t.Fatalf("url = %q", got)
	}
}

func TestNormalizeURLRejectsUnsupportedSchemes(t *testing.T) {
	if _, err := normalizeURL("file:///tmp/demo.html"); err == nil {
		t.Fatal("file URL should be rejected")
	}
}

func TestAttachExistingDevToolsPort(t *testing.T) {
	devtools := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Browser":"Chrome/test"}`))
	}))
	defer devtools.Close()
	parsed, err := url.Parse(devtools.URL)
	if err != nil {
		t.Fatal(err)
	}
	port := parsed.Port()
	if port == "" || !strings.HasPrefix(parsed.Host, "127.0.0.1:") {
		t.Fatalf("test server must use loopback port, got %s", devtools.URL)
	}
	home := t.TempDir()
	profile := filepath.Join(home, "browser-profiles", "default")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "DevToolsActivePort"), []byte(port+"\n/devtools/browser/test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	proc, err := attachExisting(context.Background(), Config{HomeDir: home}, "sess_attach", devtools.Client())
	if err != nil {
		t.Fatal(err)
	}
	if proc.cmd != nil || proc.port == 0 || proc.endpoint != "http://127.0.0.1:"+port {
		t.Fatalf("unexpected proc: %+v", proc)
	}
}

func TestProfileDirIsGlobal(t *testing.T) {
	home := t.TempDir()
	got, err := profileDir(Config{HomeDir: home}, "sess/a b")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "browser-profiles", "default")
	if got != want {
		t.Fatalf("profile dir = %q, want %q", got, want)
	}
}

func TestSingletonLockPID(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink("MacBookPro.lan-62668", filepath.Join(dir, "SingletonLock")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	pid, err := singletonLockPID(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pid != 62668 {
		t.Fatalf("pid = %d", pid)
	}
}

func TestScreencastControllerStartIsIdempotent(t *testing.T) {
	var methods []string
	stream := newScreencastController(func(method string, params any) error {
		methods = append(methods, method)
		return nil
	})

	if err := stream.start(800, 600, 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := stream.start(800, 600, 1, 1); err != nil {
		t.Fatal(err)
	}

	got := strings.Join(methods, ",")
	want := "Emulation.setDeviceMetricsOverride,Page.startScreencast"
	if got != want {
		t.Fatalf("methods = %s, want %s", got, want)
	}
}

func TestScreencastControllerRestartsOnConfigChange(t *testing.T) {
	var methods []string
	stream := newScreencastController(func(method string, params any) error {
		methods = append(methods, method)
		return nil
	})

	if err := stream.start(800, 600, 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := stream.start(1024, 768, 1, 1); err != nil {
		t.Fatal(err)
	}

	got := strings.Join(methods, ",")
	want := "Emulation.setDeviceMetricsOverride,Page.startScreencast,Page.stopScreencast,Emulation.setDeviceMetricsOverride,Page.startScreencast"
	if got != want {
		t.Fatalf("methods = %s, want %s", got, want)
	}
}

func TestScreencastControllerStopIsIdempotent(t *testing.T) {
	var methods []string
	stream := newScreencastController(func(method string, params any) error {
		methods = append(methods, method)
		return nil
	})

	if err := stream.stop(); err != nil {
		t.Fatal(err)
	}
	if err := stream.start(800, 600, 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := stream.stop(); err != nil {
		t.Fatal(err)
	}
	if err := stream.stop(); err != nil {
		t.Fatal(err)
	}

	got := strings.Join(methods, ",")
	want := "Emulation.setDeviceMetricsOverride,Page.startScreencast,Page.stopScreencast"
	if got != want {
		t.Fatalf("methods = %s, want %s", got, want)
	}
}

func TestStartScreencastUsesDeviceScaleFactor(t *testing.T) {
	paramsByMethod := map[string]any{}
	if err := startScreencastWithConfig(func(method string, params any) error {
		paramsByMethod[method] = params
		return nil
	}, normalizeScreencastStart(800, 600, 1, 2)); err != nil {
		t.Fatal(err)
	}
	metrics, ok := paramsByMethod["Emulation.setDeviceMetricsOverride"].(map[string]any)
	if !ok {
		t.Fatalf("metrics params missing: %+v", paramsByMethod)
	}
	if metrics["deviceScaleFactor"] != float64(2) {
		t.Fatalf("deviceScaleFactor = %+v", metrics["deviceScaleFactor"])
	}
	start, ok := paramsByMethod["Page.startScreencast"].(map[string]any)
	if !ok {
		t.Fatalf("start params missing: %+v", paramsByMethod)
	}
	if start["format"] != "png" || start["quality"] != nil || start["maxWidth"] != 1600 || start["maxHeight"] != 1200 {
		t.Fatalf("start params = %+v", start)
	}
}
