package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/audio/runtimeassets"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestDaemonCatalogFollowsAssembledCapabilities(t *testing.T) {
	for _, name := range []string{"PUDDING_ELECTRON_COMPUTER_BRIDGE_URL", "PUDDING_ELECTRON_COMPUTER_BRIDGE_TOKEN", "PUDDING_ELECTRON_BROWSER_BRIDGE_URL", "PUDDING_ELECTRON_BROWSER_BRIDGE_TOKEN"} {
		t.Setenv(name, "")
	}
	d, err := Start(Options{Home: t.TempDir(), Addr: "127.0.0.1:0", Mock: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := d.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	get := func(route string, out any) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, "http://"+d.Addr()+route, nil)
		req.Header.Set("Authorization", "Bearer "+d.Token())
		client := &http.Client{Timeout: 5 * time.Second}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: HTTP %d", route, res.StatusCode)
		}
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
	var catalog struct {
		Plugins []*plugin.Definition `json:"plugins"`
	}
	get("/plugins", &catalog)
	var camera, screen bool
	for _, def := range catalog.Plugins {
		if def.ID == plugin.BuiltinComputerUseID {
			t.Fatal("Computer Use exposed without an Electron bridge")
		}
		for _, item := range def.Tools {
			camera = camera || item.Name == tool.CameraCapture
			screen = screen || item.Name == tool.DesktopScreenshot
		}
	}
	if camera != (runtime.GOOS == "darwin") || !screen {
		t.Fatalf("capture tools do not match platform implementations: camera=%v screen=%v", camera, screen)
	}
	var status runtimeassets.Status
	get("/settings/audio/runtime", &status)
	if (status.State == "unsupported") != (runtime.GOOS == "windows") {
		t.Fatalf("unexpected voice status on %s: %+v", runtime.GOOS, status)
	}
}
