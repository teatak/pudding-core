package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBrowserActionsSmoke(t *testing.T) {
	if os.Getenv("PUDDING_BROWSER_SMOKE") != "1" {
		t.Skip("set PUDDING_BROWSER_SMOKE=1 to launch Chrome and run the browser action smoke test")
	}
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html>
<head><title>Pudding Browser Smoke</title></head>
<body>
  <label>Name <input id="name" /></label>
  <button id="save" onclick="document.getElementById('status').textContent='Saved:' + document.getElementById('name').value">Save</button>
  <p id="status">Waiting</p>
  <div style="height: 2000px"></div>
  <p id="bottom">Bottom</p>
</body>
</html>`))
	}))
	defer page.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	mgr := NewManager(Config{HomeDir: t.TempDir(), Headless: true})
	defer mgr.Close()

	tab, err := mgr.CreateTab(ctx, "sess_smoke")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Open(ctx, "sess_smoke", tab.ID, page.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Type(ctx, "sess_smoke", tab.ID, TypeInput{Selector: "#name", Text: "Pudding", Clear: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Click(ctx, "sess_smoke", tab.ID, ClickInput{Selector: "#save"}); err != nil {
		t.Fatal(err)
	}
	scroll, err := mgr.Scroll(ctx, "sess_smoke", tab.ID, ScrollInput{DeltaY: 900})
	if err != nil {
		t.Fatal(err)
	}
	if y, ok := scroll.Result["y"].(float64); !ok || y <= 0 {
		t.Fatalf("scroll did not move page: %+v", scroll.Result)
	}
	obs, err := mgr.Observe(ctx, "sess_smoke", tab.ID, ObserveOptions{MaxTextChars: 2000, MaxElements: 20})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(obs.Text, "Saved:Pudding") {
		t.Fatalf("typed/clicked state missing from observation: %q", obs.Text)
	}
}
