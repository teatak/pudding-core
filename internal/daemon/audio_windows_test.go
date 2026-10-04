package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/config"
)

func TestWindowsDoesNotCreateVoiceOrModelInstaller(t *testing.T) {
	dir := t.TempDir()
	voice, installer := newVoiceRuntime(dir, config.NewManager(dir), config.DefaultAudioConfig(), nil, nil)
	if voice != nil || installer != nil {
		t.Fatal("Windows audio must remain unavailable until capture and ASR are implemented")
	}
}

func TestWindowsDaemonAudioEndpointsRemainUnavailable(t *testing.T) {
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
	client := &http.Client{Timeout: 5 * time.Second}
	request := func(method, route, body string, want int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, "http://"+d.Addr()+route, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+d.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != want {
			t.Fatalf("%s %s: status=%d body=%s err=%v", method, route, resp.StatusCode, data, err)
		}
		return data
	}
	var session struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(request(http.MethodPost, "/sessions", `{"provider":"mock","model":"mock"}`, http.StatusCreated), &session); err != nil || session.ID == "" {
		t.Fatalf("create session: %+v %v", session, err)
	}
	for _, route := range []string{"/sessions/" + session.ID + "/audio/bindings", "/sessions/" + session.ID + "/audio/input"} {
		method, body := http.MethodGet, ""
		if strings.HasSuffix(route, "/input") {
			method, body = http.MethodPost, `{"enabled":true}`
		}
		if data := request(method, route, body, http.StatusServiceUnavailable); !strings.Contains(string(data), "audio_unavailable") {
			t.Fatalf("unexpected unavailable response: %s", data)
		}
	}
	request(http.MethodGet, "/desktop/about", "", http.StatusOK)
	request(http.MethodDelete, "/sessions/"+session.ID, "", http.StatusNoContent)
}
