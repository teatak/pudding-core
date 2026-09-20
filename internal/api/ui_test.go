package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExternalUIBundle(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"index.html": "<h1>Desktop</h1>", "assets/app.js": "console.log('desktop');"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	handler, err := UIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		url    string
		status int
		body   string
	}{
		{"/?token=launch", http.StatusOK, "<h1>Desktop</h1>"},
		{"/assets/app.js", http.StatusOK, "console.log('desktop');"},
		{"/assets/", http.StatusNotFound, ""},
		{"/missing.js", http.StatusNotFound, ""},
		{"/../daemon.token", http.StatusNotFound, ""},
	} {
		t.Run(tc.url, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.url, nil))
			if response.Code != tc.status || (tc.body != "" && !strings.Contains(response.Body.String(), tc.body)) {
				t.Fatalf("response = %d %q", response.Code, response.Body.String())
			}
		})
	}
}

func TestExternalUIIsOptionalAndValidated(t *testing.T) {
	if handler, err := UIHandler(""); handler != nil || err != nil {
		t.Fatalf("headless UI = %v, %v", handler, err)
	}
	for _, dir := range []string{"relative/dist", t.TempDir()} {
		if _, err := UIHandler(dir); err == nil {
			t.Fatalf("accepted invalid UI bundle %q", dir)
		}
	}
}

func TestExternalUIConfinesSymlinksToBundle(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "private.txt")
	for name, data := range map[string]string{filepath.Join(dir, "index.html"): "desktop", outside: "private fixture"} {
		if err := os.WriteFile(name, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range map[string]string{"leak.txt": outside, "safe.html": "index.html"} {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	handler, err := UIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	for url, want := range map[string]int{"/leak.txt": http.StatusNotFound, "/safe.html": http.StatusOK} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, url, nil))
		if response.Code != want {
			t.Errorf("%s status = %d, want %d", url, response.Code, want)
		}
	}
	if err := os.Remove(filepath.Join(dir, "index.html")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "index.html")); err != nil {
		t.Fatal(err)
	}
	if _, err := UIHandler(dir); err == nil {
		t.Fatal("accepted an index outside the UI bundle")
	}
}
