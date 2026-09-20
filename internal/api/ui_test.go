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
