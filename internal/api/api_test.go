package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsvc "github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/audio/voice"
	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/desktopcamera"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/mobileauth"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	skillsvc "github.com/teatak/pudding-core/internal/skill"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
)

const testToken = "test-token"

func newTestServer(t *testing.T) (*httptest.Server, store.Store) {
	t.Helper()
	ms := memstore.New()
	homeDir := t.TempDir()
	hub := event.NewHub()
	eng := engine.New(ms, hub, registry.Static(mock.New(mock.WithScript([]string{"你好", "世界"}), mock.WithDelay(5*time.Millisecond))), ms, engine.WithAttachmentHome(homeDir))
	srv := httptest.NewServer(New(eng, ms, ms, hub).WithHome(homeDir).Handler(testToken, nil))
	t.Cleanup(srv.Close)
	return srv, ms
}

func newConfigTestServer(t *testing.T) (*httptest.Server, store.Store, *config.Manager) {
	t.Helper()
	ms := memstore.New()
	homeDir := t.TempDir()
	cfg := config.NewManager(homeDir)
	if err := cfg.Prepare(); err != nil {
		t.Fatal(err)
	}
	writeOAuthTestApps(t, homeDir)
	hub := event.NewHub()
	eng := engine.New(ms, hub, registry.Static(mock.New(mock.WithScript([]string{"你好", "世界"}), mock.WithDelay(5*time.Millisecond))), cfg, engine.WithAttachmentHome(homeDir))
	srv := httptest.NewServer(New(eng, ms, cfg, hub).WithHome(homeDir).WithApps(appsvc.NewService(homeDir, nil)).Handler(testToken, nil))
	t.Cleanup(srv.Close)
	return srv, ms, cfg
}

func newAudioTestServer(t *testing.T) (*httptest.Server, store.Store, *voice.Manager) {
	t.Helper()
	ms := memstore.New()
	homeDir := t.TempDir()
	hub := event.NewHub()
	eng := engine.New(ms, hub, registry.Static(mock.New(mock.WithScript([]string{"你好"}))), ms, engine.WithAttachmentHome(homeDir))
	manager := voice.NewManager()
	srv := httptest.NewServer(New(eng, ms, ms, hub).WithHome(homeDir).WithVoice(manager).Handler(testToken, nil))
	t.Cleanup(srv.Close)
	return srv, ms, manager
}

func TestAudioBindingsAreSessionScoped(t *testing.T) {
	srv, st, _ := newAudioTestServer(t)
	ctx := context.Background()
	for _, id := range []string{"sess_a", "sess_b"} {
		if err := st.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock"}); err != nil {
			t.Fatal(err)
		}
	}

	type bindingsPayload struct {
		Bindings voice.Bindings `json:"bindings"`
	}
	type bindPayload struct {
		OK       bool           `json:"ok"`
		Bindings voice.Bindings `json:"bindings"`
	}

	resp := req(t, http.MethodGet, srv.URL+"/sessions/sess_a/audio/bindings", nil)
	got := decodeJSON[bindingsPayload](t, resp)
	if got.Bindings.InputOwner != "" || got.Bindings.OutputOwner != "" {
		t.Fatalf("initial bindings = %+v", got.Bindings)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_a/audio/input", map[string]bool{"enabled": true})
	bound := decodeJSON[bindPayload](t, resp)
	if resp.StatusCode != http.StatusOK || !bound.OK || bound.Bindings.InputOwner != "sess_a" {
		t.Fatalf("bind input response status=%d body=%+v", resp.StatusCode, bound)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_a/audio/output", map[string]bool{"enabled": true})
	bound = decodeJSON[bindPayload](t, resp)
	if resp.StatusCode != http.StatusOK || bound.Bindings.OutputOwner != "sess_a" {
		t.Fatalf("bind output response status=%d body=%+v", resp.StatusCode, bound)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_b/audio/input", map[string]bool{"enabled": true})
	bound = decodeJSON[bindPayload](t, resp)
	if resp.StatusCode != http.StatusOK || bound.Bindings.InputOwner != "sess_b" || bound.Bindings.OutputOwner != "sess_a" {
		t.Fatalf("replace input response status=%d body=%+v", resp.StatusCode, bound)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_a/audio/input", map[string]bool{"enabled": false})
	bound = decodeJSON[bindPayload](t, resp)
	if resp.StatusCode != http.StatusOK || bound.Bindings.InputOwner != "sess_b" {
		t.Fatalf("non-owner disable response status=%d body=%+v", resp.StatusCode, bound)
	}

	resp = req(t, http.MethodDelete, srv.URL+"/sessions/sess_a", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}

	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_b/audio/bindings", nil)
	got = decodeJSON[bindingsPayload](t, resp)
	if got.Bindings.InputOwner != "sess_b" || got.Bindings.OutputOwner != "" {
		t.Fatalf("bindings after delete = %+v", got.Bindings)
	}
}

func TestAudioBindingRequiresExistingSession(t *testing.T) {
	srv, _, _ := newAudioTestServer(t)
	resp := req(t, http.MethodPost, srv.URL+"/sessions/missing/audio/input", map[string]bool{"enabled": true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestDesktopAboutIncludesAudioConfig(t *testing.T) {
	srv, _, _ := newConfigTestServer(t)
	resp := req(t, http.MethodGet, srv.URL+"/desktop/about", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	payload := decodeJSON[struct {
		Sections []desktopAboutSection `json:"sections"`
	}](t, resp)
	rows := map[string]string{}
	for _, section := range payload.Sections {
		for _, row := range section.Rows {
			rows[section.ID+"."+row.Key] = row.Value
		}
	}
	if rows["driver.type"] != "portaudio" ||
		rows["asr.engine"] != "sherpa-sensevoice" ||
		rows["asr.language"] != "zh" ||
		rows["asr.use_itn"] != "off" ||
		rows["asr_vad.threshold"] != "0.6" ||
		rows["asr_vad.preroll_millis"] != "500" ||
		rows["aec.enabled"] != "on" ||
		rows["aec.model"] != "webrtc" ||
		rows["ns.enabled"] != "on" ||
		rows["ns.level"] != "moderate" ||
		rows["tts.voice"] != "zh-CN-YunxiaNeural" {
		t.Fatalf("unexpected about rows: %+v", rows)
	}
	if !strings.HasSuffix(rows["audio_config.path"], filepath.Join("config", "audio.yaml")) {
		t.Fatalf("audio config path = %q", rows["audio_config.path"])
	}
}

func TestAudioConfigAPI(t *testing.T) {
	srv, _, _ := newConfigTestServer(t)
	type audioPayload struct {
		Path   string             `json:"path"`
		Config config.AudioConfig `json:"config"`
	}

	resp := req(t, http.MethodGet, srv.URL+"/settings/audio", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	payload := decodeJSON[audioPayload](t, resp)
	if !strings.HasSuffix(payload.Path, filepath.Join("config", "audio.yaml")) {
		t.Fatalf("path = %q", payload.Path)
	}
	if payload.Config.ASR.Language != "zh" || !payload.Config.AECEnabled() || !payload.Config.NSEnabled() || payload.Config.NS.Level != "moderate" || payload.Config.TTS.Profiles["edge"].Voice != "zh-CN-YunxiaNeural" {
		t.Fatalf("unexpected initial audio config: %+v", payload.Config)
	}

	useITN := true
	next := payload.Config
	next.ASR.Language = "en"
	next.ASR.UseInverseTextNormalization = &useITN
	next.ASR.VAD.Threshold = 0.5
	next.ASR.VAD.PrerollMillis = 650
	next.NS.Level = "high"
	edge := next.TTS.Profiles["edge"]
	edge.Voice = "zh-CN-XiaoxiaoNeural"
	edge.Speed = 1.4
	next.TTS.Profiles["edge"] = edge

	resp = req(t, http.MethodPut, srv.URL+"/settings/audio", next)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	updated := decodeJSON[audioPayload](t, resp)
	if updated.Config.ASR.Language != "en" || !updated.Config.ASRUseITN() || updated.Config.ASR.VAD.Threshold != 0.5 || updated.Config.ASR.VAD.PrerollMillis != 650 || updated.Config.NS.Level != "high" || updated.Config.TTS.Profiles["edge"].Speed != 1.4 {
		t.Fatalf("unexpected updated audio config: %+v", updated.Config)
	}

	resp = req(t, http.MethodGet, srv.URL+"/settings/audio", nil)
	defer resp.Body.Close()
	reloaded := decodeJSON[audioPayload](t, resp)
	if reloaded.Config.ASR.Language != "en" || reloaded.Config.NS.Level != "high" || reloaded.Config.TTS.Profiles["edge"].Voice != "zh-CN-XiaoxiaoNeural" {
		t.Fatalf("audio config was not persisted: %+v", reloaded.Config)
	}
}

func writeOAuthTestApps(t *testing.T, homeDir string) {
	t.Helper()
	apps := map[string]string{
		"github": `
id: github
name: GitHub
auth:
  required: true
  methods:
    - id: github-oauth
      type: oauth2
      provider: github
      label: GitHub OAuth
      default: true
    - id: github-pat
      type: bearer
      label: Personal access token
endpoints:
  github_rest:
    kind: rest
    url: https://api.github.com
  github_graphql:
    kind: graphql
    url: https://api.github.com/graphql
`,
		"gmail": `
id: gmail
name: Gmail
auth:
  required: true
  methods:
    - id: google-oauth
      type: oauth2
      provider: gmail
      label: Google OAuth
      default: true
endpoints:
  gmail_rest:
    kind: rest
    url: https://gmail.googleapis.com/gmail/v1
`,
		"unicorn": `
id: unicorn
name: Unicorn
auth:
  required: true
  methods:
    - id: unicorn-header
      type: header
      header: X-Token
      label: Custom header
connection:
  fields:
    - id: hotelCode
      label: 酒店代码
      required: true
      inject:
        - target: query
          methods: [GET, DELETE]
        - target: body
          methods: [POST, PUT, PATCH]
endpoints:
  unicorn_rest:
    kind: rest
    url: https://test-unicorn-uiserver-server.lumous.cn
`,
	}
	for id, body := range apps {
		dir := filepath.Join(homeDir, "apps", id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, appsvc.AppFileName), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStartAppOAuth(t *testing.T) {
	srv, _, cfg := newConfigTestServer(t)
	resp := req(t, http.MethodPost, srv.URL+"/app-oauth/start", map[string]string{"appID": "github"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var payload struct {
		AuthorizationURL string `json:"authorizationURL"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(payload.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "github.com" || u.Path != "/login/oauth/authorize" {
		t.Fatalf("unexpected auth url: %s", payload.AuthorizationURL)
	}
	q := u.Query()
	if q.Get("client_id") == "" || q.Get("state") == "" {
		t.Fatalf("missing auth params: %s", payload.AuthorizationURL)
	}
	if got, want := q.Get("client_id"), "Ov23li6YcOqhzvGBD9s4"; got != want {
		t.Fatalf("client_id = %q, want %q", got, want)
	}
	srvURL, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(srvURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := q.Get("redirect_uri"), "http://localhost:"+port+"/oauth/callback/github"; got != want {
		t.Fatalf("redirect_uri = %q, want %q", got, want)
	}
	if got, want := q.Get("scope"), "read:user repo read:org"; got != want {
		t.Fatalf("scope = %q, want %q", got, want)
	}
	if got, want := q.Get("response_type"), "code"; got != want {
		t.Fatalf("response_type = %q, want %q", got, want)
	}

	if err := cfg.PutAppConnection(context.Background(), &appsvc.Connection{
		ID:    "github-work",
		Name:  "Work",
		AppID: "github",
		Auth:  appsvc.Auth{MethodID: "github-oauth", Type: "oauth2", AccessToken: "old-token"},
	}); err != nil {
		t.Fatal(err)
	}
	resp = req(t, http.MethodPost, srv.URL+"/app-oauth/start", map[string]string{
		"appID":          "github",
		"connectionID":   "github-work",
		"connectionName": "Work GitHub",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reauth status = %d", resp.StatusCode)
	}
	payload = decodeJSON[struct {
		AuthorizationURL string `json:"authorizationURL"`
	}](t, resp)
	u, err = url.Parse(payload.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("state") == q.Get("state") || u.Query().Get("state") == "" {
		t.Fatalf("reauth should create a fresh state: %s", payload.AuthorizationURL)
	}
}

func TestPutAppConnectionSupportsGitHubPAT(t *testing.T) {
	srv, _, _ := newConfigTestServer(t)
	resp := req(t, http.MethodPut, srv.URL+"/app-connections/github-pat", map[string]string{
		"appID":        "github",
		"name":         "GitHub PAT",
		"authMethodID": "github-pat",
		"authType":     "bearer",
		"token":        "ghp_test",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	payload := decodeJSON[appsvc.ConnectionView](t, resp)
	if payload.AuthMethodID != "github-pat" || payload.AuthType != "bearer" || !payload.TokenSet {
		t.Fatalf("unexpected connection view: %+v", payload)
	}
}

func TestPutAppConnectionRejectsEmptyGitHubPAT(t *testing.T) {
	srv, _, _ := newConfigTestServer(t)
	resp := req(t, http.MethodPut, srv.URL+"/app-connections/github-pat", map[string]string{
		"appID":        "github",
		"name":         "GitHub PAT",
		"authMethodID": "github-pat",
		"authType":     "bearer",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestPutAppConnectionStoresConnectionFields(t *testing.T) {
	srv, _, _ := newConfigTestServer(t)
	resp := req(t, http.MethodPut, srv.URL+"/app-connections/unicorn-main", map[string]any{
		"appID":        "unicorn",
		"name":         "麒麟",
		"authMethodID": "unicorn-header",
		"authType":     "header",
		"token":        "secret",
		"fields": map[string]string{
			"hotelCode": "H001",
		},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	payload := decodeJSON[appsvc.ConnectionView](t, resp)
	if payload.ID != "unicorn-main" || !payload.TokenSet {
		t.Fatalf("unexpected connection view: %+v", payload)
	}

	resp = req(t, http.MethodGet, srv.URL+"/app-connections/unicorn-main", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail status = %d", resp.StatusCode)
	}
	detail := decodeJSON[appsvc.ConnectionDetailView](t, resp)
	if detail.Fields["hotelCode"] != "H001" {
		t.Fatalf("connection fields not stored: %+v", detail.Fields)
	}
}

func TestPutAppConnectionRequiresConnectionFields(t *testing.T) {
	srv, _, _ := newConfigTestServer(t)
	resp := req(t, http.MethodPut, srv.URL+"/app-connections/unicorn-main", map[string]string{
		"appID":        "unicorn",
		"name":         "麒麟",
		"authMethodID": "unicorn-header",
		"authType":     "header",
		"token":        "secret",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestPutAppConnectionPreservesLegacyBearerByType(t *testing.T) {
	srv, _, cfg := newConfigTestServer(t)
	if err := cfg.PutAppConnection(context.Background(), &appsvc.Connection{
		ID:    "github-legacy",
		Name:  "Legacy GitHub",
		AppID: "github",
		Auth:  appsvc.Auth{Type: "bearer", Token: "ghp_old"},
	}); err != nil {
		t.Fatal(err)
	}
	resp := req(t, http.MethodPut, srv.URL+"/app-connections/github-legacy", map[string]string{
		"appID":        "github",
		"name":         "GitHub PAT",
		"authMethodID": "github-pat",
		"authType":     "bearer",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	payload := decodeJSON[appsvc.ConnectionView](t, resp)
	if payload.AuthMethodID != "github-pat" || payload.AuthType != "bearer" || !payload.TokenSet {
		t.Fatalf("unexpected connection view: %+v", payload)
	}
}

func TestCanvasItemsAPIUsesSessionActorForGlobalCanvas(t *testing.T) {
	srv, ms := newTestServer(t)
	if err := ms.CreateSession(context.Background(), &store.Session{ID: "sess_left", Provider: "mock", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.CreateSession(context.Background(), &store.Session{ID: "sess_right", Provider: "mock", Model: "m"}); err != nil {
		t.Fatal(err)
	}

	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_left/canvas/items", map[string]any{
		"id":     "canvas_api",
		"kind":   "markdown",
		"title":  "Shared",
		"item":   map[string]any{"kind": "markdown", "content": "hello"},
		"window": map[string]any{"x": 10, "y": 12, "w": 320, "h": 220, "z": 1},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	item := decodeJSON[store.CanvasItem](t, resp)
	if item.SourceSessionID != "sess_left" || item.UpdatedBySessionID != "sess_left" {
		t.Fatalf("unexpected created item: %+v", item)
	}

	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_right/canvas/items", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", resp.StatusCode)
	}
	list := decodeJSON[struct {
		Items []store.CanvasItem `json:"items"`
	}](t, resp)
	if len(list.Items) != 1 || list.Items[0].ID != "canvas_api" {
		t.Fatalf("right session should see shared item: %+v", list.Items)
	}

	resp = req(t, http.MethodPatch, srv.URL+"/sessions/sess_right/canvas/items/canvas_api", map[string]any{
		"window": map[string]any{"x": 42, "y": 12, "w": 320, "h": 220, "z": 2},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d", resp.StatusCode)
	}
	item = decodeJSON[store.CanvasItem](t, resp)
	if item.SourceSessionID != "sess_left" || item.UpdatedBySessionID != "sess_right" {
		t.Fatalf("patch should keep source and update actor: %+v", item)
	}
}

func TestClosedCanvasItemsAPI(t *testing.T) {
	srv, ms := newTestServer(t)
	if err := ms.CreateSession(context.Background(), &store.Session{ID: "sess_canvas", Provider: "mock", Model: "m"}); err != nil {
		t.Fatal(err)
	}

	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_canvas/canvas/closed", map[string]any{
		"id":           "closed_1",
		"sourceItemID": "canvas_1",
		"kind":         "table",
		"title":        "Orders",
		"item":         map[string]any{"kind": "table", "rows": []any{}},
		"window":       map[string]any{"x": 1, "y": 2, "w": 300, "h": 200, "z": 3},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	item := decodeJSON[store.ClosedCanvasItem](t, resp)
	if item.ID != "closed_1" || item.SourceItemID != "canvas_1" || item.ActorSessionID != "sess_canvas" {
		t.Fatalf("unexpected closed item: %+v", item)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_canvas/canvas/closed", map[string]any{
		"id":           "closed_2",
		"sourceItemID": "canvas_1",
		"kind":         "table",
		"title":        "Orders updated",
		"item":         map[string]any{"kind": "table", "rows": []any{}},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("replace status = %d", resp.StatusCode)
	}

	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_canvas/canvas/closed", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", resp.StatusCode)
	}
	list := decodeJSON[struct {
		Items []store.ClosedCanvasItem `json:"items"`
	}](t, resp)
	if len(list.Items) != 1 || list.Items[0].ID != "closed_2" || list.Items[0].Title != "Orders updated" {
		t.Fatalf("closed items should dedupe by source item: %+v", list.Items)
	}

	resp = req(t, http.MethodDelete, srv.URL+"/sessions/sess_canvas/canvas/closed/closed_2", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}

	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_canvas/canvas/closed", nil)
	defer resp.Body.Close()
	list = decodeJSON[struct {
		Items []store.ClosedCanvasItem `json:"items"`
	}](t, resp)
	if len(list.Items) != 0 {
		t.Fatalf("closed item should be deleted: %+v", list.Items)
	}
}

func TestDesktopSaveFileWritesDownload(t *testing.T) {
	downloads := t.TempDir()
	t.Setenv("PUDDING_DESKTOP_DOWNLOADS_DIR", downloads)
	srv, _ := newTestServer(t)

	resp := req(t, http.MethodPost, srv.URL+"/desktop/save-file", map[string]string{
		"filename": `测试/bad:name.csv`,
		"mime":     "text/csv;charset=utf-8",
		"data":     base64.StdEncoding.EncodeToString([]byte("a,b\n1,2\n")),
	})
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("status = %d", resp.StatusCode)
	}
	payload := decodeJSON[struct {
		OK       bool   `json:"ok"`
		Path     string `json:"path"`
		Filename string `json:"filename"`
	}](t, resp)
	if !payload.OK || payload.Path == "" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if got := filepath.Dir(payload.Path); got != downloads {
		t.Fatalf("dir = %q, want %q", got, downloads)
	}
	if strings.ContainsAny(payload.Filename, `\/:*?"<>|`) {
		t.Fatalf("filename was not sanitized: %q", payload.Filename)
	}
	data, err := os.ReadFile(payload.Path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "a,b\n1,2\n"; got != want {
		t.Fatalf("file content = %q, want %q", got, want)
	}
}

func TestStartGmailAppOAuth(t *testing.T) {
	srv, _, _ := newConfigTestServer(t)
	resp := req(t, http.MethodPost, srv.URL+"/app-oauth/start", map[string]string{"appID": "gmail"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	payload := decodeJSON[struct {
		AuthorizationURL string `json:"authorizationURL"`
	}](t, resp)
	u, err := url.Parse(payload.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "accounts.google.com" || u.Path != "/o/oauth2/v2/auth" {
		t.Fatalf("unexpected auth url: %s", payload.AuthorizationURL)
	}
	q := u.Query()
	if got, want := q.Get("client_id"), "226317408426-s2jpl76do0qegl9vesjn1osrkbos1t9o.apps.googleusercontent.com"; got != want {
		t.Fatalf("client_id = %q, want %q", got, want)
	}
	if got, want := q.Get("access_type"), "offline"; got != want {
		t.Fatalf("access_type = %q, want %q", got, want)
	}
	if got, want := q.Get("prompt"), "consent"; got != want {
		t.Fatalf("prompt = %q, want %q", got, want)
	}
	if !strings.Contains(q.Get("scope"), "https://www.googleapis.com/auth/gmail.readonly") {
		t.Fatalf("scope missing gmail readonly: %q", q.Get("scope"))
	}
}

func TestAppOAuthCallbackReturnsHTML(t *testing.T) {
	srv, _, _ := newConfigTestServer(t)
	resp := req(t, http.MethodGet, srv.URL+"/oauth/callback/github", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("content type = %q", got)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); !strings.Contains(got, "<h1>Authorization failed</h1>") {
		t.Fatalf("unexpected body: %q", got)
	}
}

func req(t *testing.T, method, url string, body any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	r, err := http.NewRequest(method, url, &buf)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestSkillsAPI(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	eng := engine.New(ms, hub, registry.Static(mock.New()), ms)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "skills", "test-skill"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "skills", "test-skill", "SKILL.md"), []byte("---\nname: test-skill\ndescription: Test skill.\n---\nBody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "skills", "test-skill", "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "skills", "test-skill", "assets", "icon.svg"), []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, ms, ms, hub).WithSkills(skillsvc.NewService(home)).Handler(testToken, nil))
	t.Cleanup(srv.Close)

	resp := req(t, http.MethodGet, srv.URL+"/skills", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	got := decodeJSON[map[string][]map[string]any](t, resp)
	var foundBuiltin, foundUser bool
	for _, item := range got["skills"] {
		if item["id"] == "skill-creator" {
			if item["scope"] != "global" || item["source"] != "builtin" || item["system"] != true || item["iconPath"] != "builtin/skill-creator/assets/icon.svg" {
				t.Fatalf("unexpected skill-creator view: %+v", item)
			}
			foundBuiltin = true
		}
		if item["id"] == "test-skill" {
			if item["scope"] != "global" || item["source"] != "user" || item["system"] != false || item["iconPath"] != "test-skill/assets/icon.svg" {
				t.Fatalf("unexpected test-skill view: %+v", item)
			}
			foundUser = true
		}
	}
	if !foundBuiltin || !foundUser {
		t.Fatalf("expected builtin and user skills, got: %+v", got)
	}
}

func TestAppAssetAPI(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	eng := engine.New(ms, hub, registry.Static(mock.New()), ms)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "apps", "github", "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "apps", "github", "assets", "icon.svg"), []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, ms, ms, hub).WithApps(appsvc.NewService(home, nil)).Handler(testToken, nil))
	t.Cleanup(srv.Close)

	resp := req(t, http.MethodGet, srv.URL+"/app-assets/github/assets/icon.svg?token="+testToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/svg+xml" {
		t.Fatalf("unexpected content type: %s", got)
	}
}

func TestAppSkillAPI(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	eng := engine.New(ms, hub, registry.Static(mock.New()), ms)
	home := t.TempDir()
	appDir := filepath.Join(home, "apps", "github")
	skillDir := filepath.Join(appDir, "skills", "issues")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "app.yaml"), []byte(`id: github
name: GitHub
skills:
  - skills/issues/SKILL.md
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: github-issues\ndescription: Read issues.\n---\nBody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, ms, ms, hub).WithApps(appsvc.NewService(home, nil)).Handler(testToken, nil))
	t.Cleanup(srv.Close)

	resp := req(t, http.MethodGet, srv.URL+"/app-skills/github/skills/issues/SKILL.md", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	got := decodeJSON[map[string]any](t, resp)
	if got["name"] != "github-issues" || !strings.Contains(got["content"].(string), "Body") {
		t.Fatalf("unexpected app skill detail: %+v", got)
	}

	resp = req(t, http.MethodGet, srv.URL+"/app-skills/github/app.yaml", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

func TestDeleteAppAPI(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	eng := engine.New(ms, hub, registry.Static(mock.New()), ms)
	home := t.TempDir()
	appDir := filepath.Join(home, "apps", "github")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "app.yaml"), []byte("id: github\nname: GitHub\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, ms, ms, hub).WithApps(appsvc.NewService(home, nil)).Handler(testToken, nil))
	t.Cleanup(srv.Close)

	resp := req(t, http.MethodDelete, srv.URL+"/apps/github", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("want 204, got %d", resp.StatusCode)
	}
	if _, err := os.Stat(appDir); !os.IsNotExist(err) {
		t.Fatalf("app dir should be removed, stat err=%v", err)
	}

	resp = req(t, http.MethodDelete, srv.URL+"/apps/github", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

func TestDeleteAppRemovesConnections(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	home := t.TempDir()
	cfg := config.NewManager(home)
	if err := cfg.Prepare(); err != nil {
		t.Fatal(err)
	}
	eng := engine.New(ms, hub, registry.Static(mock.New()), cfg)
	appDir := filepath.Join(home, "apps", "github")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "app.yaml"), []byte("id: github\nname: GitHub\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cfg.PutAppConnection(context.Background(), &appsvc.Connection{
		ID:    "github-main",
		Name:  "GitHub Main",
		AppID: "github",
		Auth:  appsvc.Auth{Type: "bearer", Token: "secret"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.PutAppConnection(context.Background(), &appsvc.Connection{
		ID:    "other-main",
		Name:  "Other Main",
		AppID: "other",
		Auth:  appsvc.Auth{Type: "bearer", Token: "secret"},
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, ms, cfg, hub).WithApps(appsvc.NewService(home, nil)).Handler(testToken, nil))
	t.Cleanup(srv.Close)

	resp := req(t, http.MethodDelete, srv.URL+"/apps/github", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("want 204, got %d", resp.StatusCode)
	}
	if _, err := cfg.GetAppConnection(context.Background(), "github-main"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("github connection should be removed, err=%v", err)
	}
	if _, err := cfg.GetAppConnection(context.Background(), "other-main"); err != nil {
		t.Fatalf("other connection should remain: %v", err)
	}
}

func TestDeleteSkillAPI(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	eng := engine.New(ms, hub, registry.Static(mock.New()), ms)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "skills", "test-skill"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "skills", "test-skill", "SKILL.md"), []byte("---\nname: test-skill\ndescription: Test skill.\n---\nBody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, ms, ms, hub).WithSkills(skillsvc.NewService(home)).Handler(testToken, nil))
	t.Cleanup(srv.Close)

	resp := req(t, http.MethodDelete, srv.URL+"/skills/test-skill", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("want 204, got %d", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(home, "skills", "test-skill")); !os.IsNotExist(err) {
		t.Fatalf("skill dir should be removed, stat err=%v", err)
	}
}

func TestSkillDraftsAPI(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	eng := engine.New(ms, hub, registry.Static(mock.New()), ms)
	home := t.TempDir()
	draftDir := filepath.Join(home, "skills-draft", "test-skill")
	if err := os.MkdirAll(draftDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(draftDir, "SKILL.md"), []byte("---\nname: test-skill\ndescription: Test skill draft.\n---\nBody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, ms, ms, hub).WithSkills(skillsvc.NewService(home)).Handler(testToken, nil))
	t.Cleanup(srv.Close)

	resp := req(t, http.MethodGet, srv.URL+"/skill-drafts", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	list := decodeJSON[map[string][]map[string]any](t, resp)
	if len(list["drafts"]) != 1 || list["drafts"][0]["id"] != "test-skill" {
		t.Fatalf("unexpected drafts: %+v", list)
	}

	resp = req(t, http.MethodGet, srv.URL+"/skill-drafts/test-skill", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	detail := decodeJSON[map[string]any](t, resp)
	files := detail["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("unexpected draft detail: %+v", detail)
	}

	resp = req(t, http.MethodPost, srv.URL+"/skill-drafts/test-skill/apply", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(home, "skills", "test-skill", "SKILL.md")); err != nil {
		t.Fatalf("published skill missing: %v", err)
	}
	if _, err := os.Stat(draftDir); !os.IsNotExist(err) {
		t.Fatalf("draft should be removed, stat err=%v", err)
	}
}

func TestSkillAssetsAPI(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	eng := engine.New(ms, hub, registry.Static(mock.New()), ms)
	home := t.TempDir()
	dir := filepath.Join(home, "skills", "test-skill", "assets")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "skills", "test-skill", "SKILL.md"), []byte("---\nname: test-skill\ndescription: Test skill.\n---\nBody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "icon.svg"), []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, ms, ms, hub).WithSkills(skillsvc.NewService(home)).Handler(testToken, nil))
	t.Cleanup(srv.Close)

	resp := req(t, http.MethodGet, srv.URL+"/skill-assets/test-skill/assets/icon.svg", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("unexpected skill asset response: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "<svg/>" {
		t.Fatalf("unexpected asset body: %q", buf.String())
	}
}

func TestBuiltinToolsAPI(t *testing.T) {
	srv, _ := newTestServer(t)

	resp := req(t, http.MethodGet, srv.URL+"/tools/builtin", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	got := decodeJSON[map[string][]map[string]any](t, resp)
	tools := got["tools"]
	if len(tools) < 15 {
		t.Fatalf("unexpected builtin tools: %+v", got)
	}
	if tools[0]["id"] != tool.RequestCapability {
		t.Fatalf("unexpected builtin tool id: %+v", tools[0])
	}
	if tools[0]["description"] == "" || tools[0]["inputSchema"] == nil {
		t.Fatalf("builtin tool should include description and input schema: %+v", tools[0])
	}
	if tools[0]["capability"] != string(store.ModeChat) {
		t.Fatalf("request capability should be chat-scoped: %+v", tools[0])
	}
	var webSearch map[string]any
	for _, item := range tools {
		if item["id"] == tool.WebSearch {
			webSearch = item
			break
		}
	}
	if webSearch == nil || webSearch["capability"] != string(store.ModeChat) {
		t.Fatalf("web search should declare chat capability: %+v", webSearch)
	}
	var fileWrite map[string]any
	var skillRead map[string]any
	var skillSubmit map[string]any
	var restRequest map[string]any
	var graphqlRequest map[string]any
	for _, item := range tools {
		switch item["id"] {
		case tool.FileWrite:
			fileWrite = item
		case tool.SkillRead:
			skillRead = item
		case tool.SkillSubmit:
			skillSubmit = item
		case tool.RESTRequest:
			restRequest = item
		case tool.GraphQLRequest:
			graphqlRequest = item
		}
	}
	if fileWrite == nil || fileWrite["capability"] != string(store.ModeWorkspace) {
		t.Fatalf("file write should declare workspace capability: %+v", fileWrite)
	}
	if skillRead == nil || skillRead["capability"] != string(store.ModeChat) {
		t.Fatalf("skill read should declare chat capability: %+v", skillRead)
	}
	if skillSubmit == nil || skillSubmit["capability"] != string(store.ModeWorkspace) {
		t.Fatalf("skill submit should declare workspace capability: %+v", skillSubmit)
	}
	if restRequest == nil || restRequest["capability"] != string(store.ModeChat) {
		t.Fatalf("rest request should declare chat capability: %+v", restRequest)
	}
	if graphqlRequest == nil || graphqlRequest["capability"] != string(store.ModeChat) {
		t.Fatalf("graphql request should declare chat capability: %+v", graphqlRequest)
	}
}

func TestMobilePairingIssuesDeviceToken(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	eng := engine.New(ms, hub, registry.Static(mock.New()), ms)
	devices, err := mobileauth.OpenDeviceStore(filepath.Join(t.TempDir(), "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	pairing := mobileauth.NewManager(devices, []string{"http://192.168.1.20:9679/"})
	srv := httptest.NewServer(New(eng, ms, ms, hub).Handler(
		testToken,
		nil,
		WithDeviceTokenValidator(devices),
		WithPairing(pairing),
	))
	t.Cleanup(srv.Close)

	createResp := req(t, http.MethodPost, srv.URL+"/mobile/pairings", nil)
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create pairing: want 201, got %d", createResp.StatusCode)
	}
	created := decodeJSON[mobileauth.Pairing](t, createResp)
	if created.Code == "" || created.URL == "" || created.QRDataURL == "" {
		t.Fatalf("pairing must include code, url and qr: %+v", created)
	}

	claimResp, err := postPairingClaim(t, srv.URL, created.Code)
	if err != nil {
		t.Fatal(err)
	}
	if claimResp.StatusCode != http.StatusOK {
		t.Fatalf("claim pairing: want 200, got %d", claimResp.StatusCode)
	}
	claim := decodeJSON[mobileauth.ClaimResult](t, claimResp)
	if claim.Token == "" || claim.Device.Name != "iPhone" {
		t.Fatalf("unexpected claim: %+v", claim)
	}

	deviceReq, err := http.NewRequest(http.MethodGet, srv.URL+"/sessions", nil)
	if err != nil {
		t.Fatal(err)
	}
	deviceReq.Header.Set("Authorization", "Bearer "+claim.Token)
	deviceResp, err := http.DefaultClient.Do(deviceReq)
	if err != nil {
		t.Fatal(err)
	}
	deviceResp.Body.Close()
	if deviceResp.StatusCode != http.StatusOK {
		t.Fatalf("device token should authorize sessions, got %d", deviceResp.StatusCode)
	}

	replayResp, err := postPairingClaim(t, srv.URL, created.Code)
	if err != nil {
		t.Fatal(err)
	}
	replayResp.Body.Close()
	if replayResp.StatusCode != http.StatusNotFound {
		t.Fatalf("pairing code must be single-use, got %d", replayResp.StatusCode)
	}
}

func postPairingClaim(t *testing.T, baseURL, code string) (*http.Response, error) {
	t.Helper()
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(map[string]string{"deviceName": "iPhone"}); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/mobile/pairings/"+code+"/claim", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

func TestWebToolsAPI(t *testing.T) {
	srv, _, cfg := newConfigTestServer(t)

	resp := req(t, http.MethodGet, srv.URL+"/tools/web", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	got := decodeJSON[config.WebToolsView](t, resp)
	if got.SearchProvider != "" || got.FetchProvider != "" || len(got.Providers) != 1 {
		t.Fatalf("unexpected initial web tools: %+v", got)
	}
	if got.Providers[0].Name != "tavily" || got.Providers[0].APIKeySet {
		t.Fatalf("unexpected initial tavily provider: %+v", got.Providers[0])
	}

	resp = req(t, http.MethodPatch, srv.URL+"/tools/web", map[string]any{
		"providers": map[string]any{
			"tavily": map[string]string{"apiKey": "tvly-http-secret"},
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	got = decodeJSON[config.WebToolsView](t, resp)
	if got.SearchProvider != "tavily" || got.FetchProvider != "tavily" {
		t.Fatalf("patch should enable tavily providers: %+v", got)
	}
	if got.Providers[0].APIKey != "tvly-http-secret" || !got.Providers[0].APIKeySet {
		t.Fatalf("patch should return editable key and status: %+v", got.Providers[0])
	}
	stored, ok, err := cfg.TavilyAPIKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok || stored != "tvly-http-secret" {
		t.Fatalf("unexpected stored tavily api key: %q %v", stored, ok)
	}

	resp = req(t, http.MethodPatch, srv.URL+"/tools/web", map[string]any{"searchProvider": "unsupported"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unsupported web provider must 400, got %d", resp.StatusCode)
	}
}

func decodeJSON[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestAuthRequired(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := http.Get(srv.URL + "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
}

func TestCORSPreflightAllowsWailsLoopback(t *testing.T) {
	srv, _ := newTestServer(t)
	r, err := http.NewRequest(http.MethodOptions, srv.URL+"/sessions/s1", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Origin", "wails://localhost:5174")
	r.Header.Set("Access-Control-Request-Method", "PATCH")
	r.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("want 204, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "wails://localhost:5174" {
		t.Fatalf("unexpected allow origin %q", got)
	}
	if !strings.Contains(resp.Header.Get("Access-Control-Allow-Methods"), "PATCH") {
		t.Fatalf("PATCH must be allowed, got %q", resp.Header.Get("Access-Control-Allow-Methods"))
	}
}

func TestCORSRejectsNonLoopbackOrigin(t *testing.T) {
	srv, _ := newTestServer(t)
	r, err := http.NewRequest(http.MethodOptions, srv.URL+"/sessions", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Origin", "https://example.com")
	r.Header.Set("Access-Control-Request-Method", "GET")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403, got %d", resp.StatusCode)
	}
}

func TestAttachmentUploadSubmitAndRead(t *testing.T) {
	srv, st := newTestServer(t)
	if err := st.CreateSession(context.Background(), &store.Session{ID: "sess_attach", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("hello attachment")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := http.NewRequest(http.MethodPost, srv.URL+"/sessions/sess_attach/attachments", &body)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("upload status = %d body=%s", resp.StatusCode, string(data))
	}
	uploaded := decodeJSON[store.Attachment](t, resp)
	if uploaded.Name != "note.txt" || uploaded.MIME != "text/plain" || uploaded.AttachmentKey == "" || uploaded.URL == "" {
		t.Fatalf("unexpected uploaded attachment: %+v", uploaded)
	}

	resp = req(t, http.MethodGet, srv.URL+uploaded.URL, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read status = %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello attachment" {
		t.Fatalf("unexpected attachment body: %q", string(data))
	}

	r, err = http.NewRequest(http.MethodGet, srv.URL+uploaded.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Range", "bytes=0-4")
	resp, err = http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusPartialContent {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("range read status = %d body=%s", resp.StatusCode, string(data))
	}
	if contentRange := resp.Header.Get("Content-Range"); !strings.HasPrefix(contentRange, "bytes 0-4/") {
		t.Fatalf("unexpected Content-Range: %q", contentRange)
	}
	data, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected range body: %q", string(data))
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_attach/submit", map[string]any{
		"clientMessageID": "client_attach",
		"parts":           []store.ContentPart{store.AttachmentPart(uploaded)},
	})
	if resp.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("submit status = %d body=%s", resp.StatusCode, string(data))
	}
	resp.Body.Close()

	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_attach/messages?limit=10", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("messages status = %d", resp.StatusCode)
	}
	page := decodeJSON[messagePageResponse](t, resp)
	if len(page.Messages) == 0 {
		t.Fatal("missing canonical user message")
	}
	msg := page.Messages[0]
	if msg.ClientMessageID != "client_attach" {
		t.Fatalf("unexpected client message id: %+v", msg)
	}
	attachments := store.AttachmentsFromParts(msg.Parts)
	if len(attachments) != 1 || attachments[0].AttachmentKey != uploaded.AttachmentKey {
		t.Fatalf("attachment part not persisted: %+v", msg.Parts)
	}
}

func TestSubmitSystemTextOnly(t *testing.T) {
	srv, st := newTestServer(t)
	if err := st.CreateSession(context.Background(), &store.Session{ID: "sess_system", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_system/submit", map[string]string{
		"clientMessageID": "client_system",
		"kind":            "system",
		"text":            "summarize this",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("submit status = %d", resp.StatusCode)
	}
}

func TestDesktopScreenshotStoresAttachment(t *testing.T) {
	original := runDesktopScreenshots
	t.Cleanup(func() { runDesktopScreenshots = original })
	runDesktopScreenshots = func(ctx context.Context, dir, filenamePrefix string) ([]string, error) {
		path := filepath.Join(dir, filenamePrefix+".png")
		if err := os.WriteFile(path, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, 0600); err != nil {
			return nil, err
		}
		return []string{path}, nil
	}

	srv, st := newTestServer(t)
	if err := st.CreateSession(context.Background(), &store.Session{ID: "sess_shot", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}

	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_shot/desktop/screenshot", nil)
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("screenshot status = %d body=%s", resp.StatusCode, string(data))
	}
	payload := decodeJSON[struct {
		Attachments []store.Attachment `json:"attachments"`
	}](t, resp)
	if len(payload.Attachments) != 1 {
		t.Fatalf("unexpected screenshot attachment count: %+v", payload)
	}
	uploaded := payload.Attachments[0]
	if uploaded.MIME != "image/png" || uploaded.AttachmentKey == "" || uploaded.URL == "" {
		t.Fatalf("unexpected screenshot attachment: %+v", uploaded)
	}

	resp = req(t, http.MethodGet, srv.URL+uploaded.URL, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read screenshot status = %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}) {
		t.Fatalf("unexpected screenshot body: %v", data)
	}
}

func TestDesktopScreenshotStoresMultipleAttachments(t *testing.T) {
	original := runDesktopScreenshots
	t.Cleanup(func() { runDesktopScreenshots = original })
	runDesktopScreenshots = func(ctx context.Context, dir, filenamePrefix string) ([]string, error) {
		paths := []string{
			filepath.Join(dir, filenamePrefix+" Display 1.png"),
			filepath.Join(dir, filenamePrefix+" Display 2.png"),
		}
		for _, path := range paths {
			if err := os.WriteFile(path, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, 0600); err != nil {
				return nil, err
			}
		}
		return paths, nil
	}

	srv, st := newTestServer(t)
	if err := st.CreateSession(context.Background(), &store.Session{ID: "sess_multi_shot", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}

	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_multi_shot/desktop/screenshot", nil)
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("screenshot status = %d body=%s", resp.StatusCode, string(data))
	}
	payload := decodeJSON[struct {
		Attachments []store.Attachment `json:"attachments"`
	}](t, resp)
	if len(payload.Attachments) != 2 {
		t.Fatalf("unexpected screenshot attachment count: %+v", payload)
	}
}

func TestDesktopPhotoStoresAttachment(t *testing.T) {
	ms := memstore.New()
	homeDir := t.TempDir()
	hub := event.NewHub()
	eng := engine.New(ms, hub, registry.Static(mock.New(mock.WithScript([]string{"你好"}))), ms, engine.WithAttachmentHome(homeDir))
	photoBytes := []byte{0xff, 0xd8, 0xff, 0xd9}
	srv := httptest.NewServer(New(eng, ms, ms, hub).WithHome(homeDir).WithCamera(fakeAPICamera{
		photo: &desktopcamera.Photo{Data: photoBytes, MIME: "image/jpeg", Name: "camera.jpg"},
	}).Handler(testToken, nil))
	t.Cleanup(srv.Close)

	if err := ms.CreateSession(context.Background(), &store.Session{ID: "sess_photo", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}

	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_photo/desktop/photo", nil)
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("photo status = %d body=%s", resp.StatusCode, string(data))
	}
	uploaded := decodeJSON[store.Attachment](t, resp)
	if uploaded.MIME != "image/jpeg" || uploaded.AttachmentKey == "" || uploaded.URL == "" {
		t.Fatalf("unexpected photo attachment: %+v", uploaded)
	}

	resp = req(t, http.MethodGet, srv.URL+uploaded.URL, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read photo status = %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, photoBytes) {
		t.Fatalf("unexpected photo body: %v", data)
	}
}

type fakeAPICamera struct {
	photo *desktopcamera.Photo
	err   error
}

func (f fakeAPICamera) CapturePhoto(context.Context) (*desktopcamera.Photo, error) {
	return f.photo, f.err
}

func TestSubmitLocalFoldersPersistsPart(t *testing.T) {
	srv, st := newTestServer(t)
	if err := st.CreateSession(context.Background(), &store.Session{ID: "sess_folder", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	folder := store.LocalFolder{
		ID:     "folder_1",
		Name:   "files",
		Path:   "/Users/me/files",
		Origin: "local_path",
	}
	resp := req(t, http.MethodPost, srv.URL+"/sessions/sess_folder/submit", map[string]any{
		"clientMessageID": "client_folder",
		"parts":           []store.ContentPart{store.LocalFolderPart(folder)},
	})
	if resp.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("submit status = %d body=%s", resp.StatusCode, string(data))
	}
	resp.Body.Close()

	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_folder/messages?limit=10", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("messages status = %d", resp.StatusCode)
	}
	page := decodeJSON[messagePageResponse](t, resp)
	if len(page.Messages) == 0 {
		t.Fatal("missing canonical user message")
	}
	folders := store.LocalFoldersFromParts(page.Messages[0].Parts)
	if len(folders) != 1 || folders[0].Path != folder.Path {
		t.Fatalf("local folder part not persisted: %+v", page.Messages[0].Parts)
	}
}

func TestDraftAttachmentSubmitCopiesToSession(t *testing.T) {
	srv, st := newTestServer(t)
	if err := st.CreateSession(context.Background(), &store.Session{ID: "sess_from_draft", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "demo.wav")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("RIFFdemo wav data")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := http.NewRequest(http.MethodPost, srv.URL+"/sessions/draft/attachments", &body)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("draft upload status = %d body=%s", resp.StatusCode, string(data))
	}
	uploaded := decodeJSON[store.Attachment](t, resp)
	if !strings.HasPrefix(uploaded.AttachmentKey, "sessions/draft/blobs/") {
		t.Fatalf("unexpected draft key: %q", uploaded.AttachmentKey)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_from_draft/submit", map[string]any{
		"clientMessageID": "client_draft_attach",
		"parts":           []store.ContentPart{store.AttachmentPart(uploaded)},
	})
	if resp.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("submit status = %d body=%s", resp.StatusCode, string(data))
	}
	resp.Body.Close()

	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_from_draft/messages?limit=10", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("messages status = %d", resp.StatusCode)
	}
	page := decodeJSON[messagePageResponse](t, resp)
	if len(page.Messages) == 0 {
		t.Fatal("missing canonical user message")
	}
	attachments := store.AttachmentsFromParts(page.Messages[0].Parts)
	if len(attachments) != 1 {
		t.Fatalf("attachment part not persisted: %+v", page.Messages[0].Parts)
	}
	if !strings.HasPrefix(attachments[0].AttachmentKey, "sessions/sess_from_draft/blobs/") {
		t.Fatalf("attachment was not copied to real session: %+v", attachments[0])
	}
	if attachments[0].URL == uploaded.URL {
		t.Fatalf("attachment URL still points at draft: %+v", attachments[0])
	}

	resp = req(t, http.MethodGet, srv.URL+attachments[0].URL, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("copied read status = %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "RIFFdemo wav data" {
		t.Fatalf("unexpected copied body: %q", string(data))
	}
	resp = req(t, http.MethodGet, srv.URL+uploaded.URL, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("draft read status = %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestTempDraftAttachmentSubmitStaysInTemp(t *testing.T) {
	srv, st := newTestServer(t)
	if err := st.CreateSession(context.Background(), &store.Session{ID: "sess_temp_attach", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("origin", "temp"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", "pasted-text.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("long pasted text")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := http.NewRequest(http.MethodPost, srv.URL+"/sessions/draft/attachments", &body)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("temp upload status = %d body=%s", resp.StatusCode, string(data))
	}
	uploaded := decodeJSON[store.Attachment](t, resp)
	if uploaded.Origin != "temp" {
		t.Fatalf("origin = %q, want temp", uploaded.Origin)
	}

	resp = req(t, http.MethodPost, srv.URL+"/sessions/sess_temp_attach/submit", map[string]any{
		"clientMessageID": "client_temp_attach",
		"parts":           []store.ContentPart{store.AttachmentPart(uploaded)},
	})
	if resp.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("submit status = %d body=%s", resp.StatusCode, string(data))
	}
	resp.Body.Close()

	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_temp_attach/messages?limit=10", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("messages status = %d", resp.StatusCode)
	}
	page := decodeJSON[messagePageResponse](t, resp)
	if len(page.Messages) == 0 {
		t.Fatal("missing canonical user message")
	}
	attachments := store.AttachmentsFromParts(page.Messages[0].Parts)
	if len(attachments) != 1 {
		t.Fatalf("attachment part not persisted: %+v", page.Messages[0].Parts)
	}
	if attachments[0].AttachmentKey != uploaded.AttachmentKey || attachments[0].URL != uploaded.URL || attachments[0].Origin != "temp" {
		t.Fatalf("temp attachment should keep draft temp identity: uploaded=%+v persisted=%+v", uploaded, attachments[0])
	}

	resp = req(t, http.MethodGet, srv.URL+attachments[0].URL, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("temp read status = %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "long pasted text" {
		t.Fatalf("unexpected temp body: %q", string(data))
	}
}

func TestListMessagesPagination(t *testing.T) {
	srv, st := newTestServer(t)
	if err := st.CreateSession(context.Background(), &store.Session{ID: "sess_1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 4; i++ {
		appendAPITestTurn(t, st, "sess_1", i)
	}

	resp := req(t, http.MethodGet, srv.URL+"/sessions/sess_1/messages?limit=4", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	first := decodeJSON[messagePageResponse](t, resp)
	if !first.HasMore {
		t.Fatal("recent page should report older messages")
	}
	if got, want := messageValueLabels(first.Messages), []string{"user:user 3", "assistant:assistant 3", "user:user 4", "assistant:assistant 4"}; !sameStringValues(got, want) {
		t.Fatalf("unexpected recent page: got %v want %v", got, want)
	}

	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_1/messages?limit=4&before="+first.Messages[0].ID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	older := decodeJSON[messagePageResponse](t, resp)
	if older.HasMore {
		t.Fatal("older page should be exhausted")
	}
	if got, want := messageValueLabels(older.Messages), []string{"user:user 1", "assistant:assistant 1", "user:user 2", "assistant:assistant 2"}; !sameStringValues(got, want) {
		t.Fatalf("unexpected older page: got %v want %v", got, want)
	}
}

func TestListTurnsPagination(t *testing.T) {
	srv, st := newTestServer(t)
	if err := st.CreateSession(context.Background(), &store.Session{ID: "sess_1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 4; i++ {
		appendAPITestTurn(t, st, "sess_1", i)
	}

	resp := req(t, http.MethodGet, srv.URL+"/sessions/sess_1/turns?limit=2", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	first := decodeJSON[turnPageResponse](t, resp)
	if !first.HasMore {
		t.Fatal("recent page should report older turns")
	}
	if got, want := turnValueIDs(first.Turns), []string{"turn_3", "turn_4"}; !sameStringValues(got, want) {
		t.Fatalf("unexpected recent page: got %v want %v", got, want)
	}
	if got, want := messagePtrValueLabels(first.Turns[0].Messages), []string{"user:user 3", "assistant:assistant 3"}; !sameStringValues(got, want) {
		t.Fatalf("turn should include complete messages: got %v want %v", got, want)
	}

	resp = req(t, http.MethodGet, srv.URL+"/sessions/sess_1/turns?limit=2&before="+first.Turns[0].ID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	older := decodeJSON[turnPageResponse](t, resp)
	if older.HasMore {
		t.Fatal("older page should be exhausted")
	}
	if got, want := turnValueIDs(older.Turns), []string{"turn_1", "turn_2"}; !sameStringValues(got, want) {
		t.Fatalf("unexpected older page: got %v want %v", got, want)
	}
}

func TestGetTurn(t *testing.T) {
	srv, st := newTestServer(t)
	if err := st.CreateSession(context.Background(), &store.Session{ID: "sess_1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	appendAPITestTurn(t, st, "sess_1", 1)

	resp := req(t, http.MethodGet, srv.URL+"/sessions/sess_1/turns/turn_1", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	turn := decodeJSON[store.ConversationTurn](t, resp)
	if turn.ID != "turn_1" {
		t.Fatalf("unexpected turn id %q", turn.ID)
	}
	if got, want := messagePtrValueLabels(turn.Messages), []string{"user:user 1", "assistant:assistant 1"}; !sameStringValues(got, want) {
		t.Fatalf("turn should include complete messages: got %v want %v", got, want)
	}
}

type messagePageResponse struct {
	Messages []store.Message `json:"messages"`
	HasMore  bool            `json:"hasMore"`
}

type turnPageResponse struct {
	Turns   []store.ConversationTurn `json:"turns"`
	HasMore bool                     `json:"hasMore"`
}

func appendAPITestTurn(t *testing.T, st store.Store, sessionID string, index int) {
	t.Helper()
	turnID := fmt.Sprintf("turn_%d", index)
	_, err := st.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID:       sessionID,
		TurnID:          turnID,
		UserMessageID:   fmt.Sprintf("msg_%d", index),
		ClientMessageID: fmt.Sprintf("client_%d", index),
		UserText:        fmt.Sprintf("user %d", index),
	})
	if err != nil {
		t.Fatal(err)
	}
	text := fmt.Sprintf("assistant %d", index)
	if _, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
		TurnID:         turnID,
		Status:         store.TurnCompleted,
		AssistantParts: store.TextPart(text),
	}); err != nil {
		t.Fatal(err)
	}
}

func messageValueLabels(messages []store.Message) []string {
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		out = append(out, string(msg.Role)+":"+msg.Text)
	}
	return out
}

func messagePtrValueLabels(messages []*store.Message) []string {
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		out = append(out, string(msg.Role)+":"+msg.Text)
	}
	return out
}

func turnValueIDs(turns []store.ConversationTurn) []string {
	out := make([]string, 0, len(turns))
	for _, turn := range turns {
		out = append(out, turn.ID)
	}
	return out
}

func sameStringValues(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type sseFrame struct {
	id    string
	event string
}

// readSSE 读取 SSE 流直到看到 stopKind 事件或超时,返回收到的帧序列。
func readSSE(t *testing.T, url string, stopKind string, timeout time.Duration) []sseFrame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("want text/event-stream, got %q", ct)
	}

	var frames []sseFrame
	var cur sseFrame
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "id: "):
			cur.id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			cur.event = strings.TrimPrefix(line, "event: ")
		case line == "":
			if cur.event != "" {
				frames = append(frames, cur)
				if cur.event == stopKind {
					return frames
				}
			}
			cur = sseFrame{}
		}
	}
	t.Fatalf("stream ended before %s, frames: %+v", stopKind, frames)
	return nil
}

func TestProvidersCRUDReturnsEditableAPIKey(t *testing.T) {
	srv, _ := newTestServer(t)

	resp := req(t, http.MethodPost, srv.URL+"/providers", map[string]string{
		"id": "work", "displayName": "Work", "protocol": "openai-compatible",
		"baseURL": "https://example.com/v1/", "apiKey": "sk-secret",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("want 201, got %d", resp.StatusCode)
	}
	created := decodeJSON[map[string]any](t, resp)
	if created["apiKeySet"] != true {
		t.Fatalf("want apiKeySet true, got %+v", created)
	}
	if created["apiKey"] != "sk-secret" {
		t.Fatalf("want editable apiKey in response, got %+v", created)
	}
	if created["baseURL"] != "https://example.com/v1" {
		t.Fatalf("baseURL must be trimmed: %+v", created)
	}

	// 重名 409
	resp = req(t, http.MethodPost, srv.URL+"/providers", map[string]string{
		"id": "work", "displayName": "Work 2", "protocol": "openai-compatible", "baseURL": "https://x.com",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate name must 409, got %d", resp.StatusCode)
	}

	// 非法 protocol 400
	resp = req(t, http.MethodPost, srv.URL+"/providers", map[string]string{
		"id": "bad", "displayName": "Bad", "protocol": "nope",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unsupported protocol must 400, got %d", resp.StatusCode)
	}

	// PATCH:空 apiKey 不覆盖,非空覆盖
	resp = req(t, http.MethodPatch, srv.URL+"/providers/work", map[string]any{"apiKey": ""})
	patched := decodeJSON[map[string]any](t, resp)
	if patched["apiKeySet"] != true {
		t.Fatalf("empty apiKey must not clear stored key: %+v", patched)
	}
	if patched["apiKey"] != "sk-secret" {
		t.Fatalf("empty apiKey must keep stored key: %+v", patched)
	}
	resp = req(t, http.MethodPatch, srv.URL+"/providers/work", map[string]any{"protocol": "google", "apiKey": "g-key"})
	patched = decodeJSON[map[string]any](t, resp)
	if patched["protocol"] != "google" {
		t.Fatalf("protocol patch failed: %+v", patched)
	}
	if patched["apiKey"] != "g-key" {
		t.Fatalf("apiKey patch failed: %+v", patched)
	}

	// 列表 + 删除
	resp = req(t, http.MethodGet, srv.URL+"/providers", nil)
	list := decodeJSON[map[string][]map[string]any](t, resp)
	if len(list["providers"]) != 1 {
		t.Fatalf("want 1 profile, got %+v", list)
	}
	if list["providers"][0]["apiKey"] != "g-key" {
		t.Fatalf("list must return editable apiKey: %+v", list)
	}
	resp = req(t, http.MethodDelete, srv.URL+"/providers/work", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("want 204, got %d", resp.StatusCode)
	}
	resp = req(t, http.MethodGet, srv.URL+"/providers/work", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 after delete, got %d", resp.StatusCode)
	}
}

func TestProviderModelsProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"m-alpha"},{"id":"m-beta"}]}`))
	}))
	defer upstream.Close()

	srv, _ := newTestServer(t)
	resp := req(t, http.MethodPost, srv.URL+"/providers", map[string]string{
		"id": "up", "displayName": "Upstream", "protocol": "openai-compatible", "baseURL": upstream.URL,
	})
	resp.Body.Close()

	got := decodeJSON[map[string][]string](t, req(t, http.MethodGet, srv.URL+"/providers/up/models", nil))
	if len(got["models"]) != 2 || got["models"][0] != "m-alpha" {
		t.Fatalf("unexpected models: %+v", got)
	}

	got = decodeJSON[map[string][]string](t, req(t, http.MethodPost, srv.URL+"/providers/models", map[string]string{
		"protocol": "openai-compatible", "baseURL": upstream.URL,
	}))
	if len(got["models"]) != 2 || got["models"][1] != "m-beta" {
		t.Fatalf("unexpected probed models: %+v", got)
	}

	resp = req(t, http.MethodGet, srv.URL+"/providers/nope/models", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing profile must 404, got %d", resp.StatusCode)
	}
}

func TestCreateSessionCarriesProviderAndModel(t *testing.T) {
	srv, _ := newTestServer(t)
	sess := decodeJSON[store.Session](t, req(t, http.MethodPost, srv.URL+"/sessions",
		map[string]string{"title": "x", "provider": "gem", "model": "m1"}))
	if sess.Provider != "gem" || sess.Model != "m1" {
		t.Fatalf("create must persist provider/model: %+v", sess)
	}
	got := decodeJSON[store.Session](t, req(t, http.MethodGet, srv.URL+"/sessions/"+sess.ID, nil))
	if got.Provider != "gem" {
		t.Fatalf("provider lost on read: %+v", got)
	}
}

func TestGetSessionUsage(t *testing.T) {
	srv, st := newTestServer(t)
	sess := decodeJSON[store.Session](t, req(t, http.MethodPost, srv.URL+"/sessions",
		map[string]string{"title": "x", "provider": "mock", "model": "m1"}))
	if _, err := st.RecordSessionUsage(context.Background(), sess.ID, store.UsageRecordInput{
		InputUncachedTokens:   10,
		InputCachedTokens:     20,
		CacheCreationTokens:   30,
		OutputContentTokens:   40,
		OutputReasoningTokens: 50,
	}); err != nil {
		t.Fatal(err)
	}

	got := decodeJSON[engine.SessionUsageInfo](t, req(t, http.MethodGet, srv.URL+"/sessions/"+sess.ID+"/usage", nil))
	if got.SessionID != sess.ID || got.RequestCount != 1 || got.LastPromptTokens != 60 || got.LastOutputTokens != 90 || got.CumulativeTotalTokens != 150 {
		t.Fatalf("unexpected session usage: %+v", got)
	}
}

func TestGetDailyUsage(t *testing.T) {
	srv, st := newTestServer(t)
	now := time.Now()
	if _, err := st.RecordUsage(context.Background(), store.UsageRecordInput{
		OccurredAt:            now,
		RequestCount:          2,
		InputUncachedTokens:   10,
		InputCachedTokens:     20,
		CacheCreationTokens:   30,
		OutputContentTokens:   40,
		OutputReasoningTokens: 50,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RecordUsage(context.Background(), store.UsageRecordInput{
		OccurredAt:          now.AddDate(0, 0, -1),
		InputUncachedTokens: 1,
	}); err != nil {
		t.Fatal(err)
	}

	got := decodeJSON[dailyUsageResponse](t, req(t, http.MethodGet, srv.URL+"/usage/daily?days=3", nil))
	if len(got.Days) != 3 {
		t.Fatalf("want 3 days, got %+v", got.Days)
	}
	today := got.Days[2]
	if today.Date != now.Format("2006-01-02") || today.RequestCount != 2 || today.TotalTokens != 150 {
		t.Fatalf("today usage wrong: %+v", today)
	}
	yesterday := got.Days[1]
	if yesterday.RequestCount != 1 || yesterday.TotalTokens != 1 {
		t.Fatalf("yesterday usage wrong: %+v", yesterday)
	}
}

func TestCreateSessionBodyValidation(t *testing.T) {
	srv, _ := newTestServer(t)

	// 空 body 不允许:session 必须显式带 provider/model
	resp, err := http.NewRequest(http.MethodPost, srv.URL+"/sessions", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Header.Set("Authorization", "Bearer "+testToken)
	r, err := http.DefaultClient.Do(resp)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty body must 400, got %d", r.StatusCode)
	}

	// 非空坏 JSON 必须 400,不能静默建空会话
	bad, err := http.NewRequest(http.MethodPost, srv.URL+"/sessions", strings.NewReader(`{bad json`))
	if err != nil {
		t.Fatal(err)
	}
	bad.Header.Set("Authorization", "Bearer "+testToken)
	br, err := http.DefaultClient.Do(bad)
	if err != nil {
		t.Fatal(err)
	}
	br.Body.Close()
	if br.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed json must 400, got %d", br.StatusCode)
	}

	// 空 body 和坏 JSON 都不能留下垃圾 session
	list := decodeJSON[map[string][]store.Session](t, req(t, http.MethodGet, srv.URL+"/sessions", nil))
	if len(list["sessions"]) != 0 {
		t.Fatalf("invalid create must not create a session, got %d", len(list["sessions"]))
	}
}

func TestDeleteSessionCancelsRunningTurn(t *testing.T) {
	// mock 慢流:submit 后 turn 持续 running,delete 必须 cancel 它而非
	// 留 goroutine 跑到自然结束。
	ms := memstore.New()
	if err := ms.PutProviderProfile(context.Background(), &store.ProviderProfile{
		ID:          "mock",
		DisplayName: "mock",
		Protocol:    "openai-compatible",
		BaseURL:     "http://127.0.0.1:11434/v1",
		Models:      []store.ProviderModel{{ID: "m"}},
	}); err != nil {
		t.Fatal(err)
	}
	hub := event.NewHub()
	eng := engine.New(ms, hub,
		registry.Static(mock.New(mock.WithScript([]string{"slow"}), mock.WithDelay(2*time.Second))), ms)
	srv := httptest.NewServer(New(eng, ms, ms, hub).Handler(testToken, nil))
	t.Cleanup(srv.Close)

	sess := decodeJSON[store.Session](t, req(t, http.MethodPost, srv.URL+"/sessions", map[string]string{"title": "d", "provider": "mock", "model": "m"}))
	resp := req(t, http.MethodPost, srv.URL+"/sessions/"+sess.ID+"/submit",
		map[string]string{"clientMessageID": "c1", "text": "hi"})
	resp.Body.Close()
	time.Sleep(100 * time.Millisecond) // 让 turn 进入 running

	resp = req(t, http.MethodDelete, srv.URL+"/sessions/"+sess.ID, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete must 204, got %d", resp.StatusCode)
	}

	// engine.Wait 应很快返回(turn 已被 cancel),不会等满 2s 的 mock 延迟
	waited := make(chan struct{})
	go func() { eng.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(1 * time.Second):
		t.Fatal("delete did not cancel the running turn; goroutine still streaming")
	}
}

func TestSubmitStreamAndResume(t *testing.T) {
	srv, _ := newTestServer(t)

	sess := decodeJSON[store.Session](t, req(t, http.MethodPost, srv.URL+"/sessions", map[string]string{"title": "demo", "provider": "mock", "model": "m"}))
	eventsURL := fmt.Sprintf("%s/sessions/%s/events?token=%s", srv.URL, sess.ID, testToken)

	// live 流:先订阅再 submit
	done := make(chan []sseFrame, 1)
	go func() { done <- readSSE(t, eventsURL, "turn.completed", 5*time.Second) }()
	time.Sleep(100 * time.Millisecond)

	resp := req(t, http.MethodPost, srv.URL+"/sessions/"+sess.ID+"/submit",
		map[string]string{"clientMessageID": "c1", "text": "hi"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("want 202, got %d", resp.StatusCode)
	}

	frames := <-done
	var kinds []string
	seqs := map[string]bool{}
	for _, f := range frames {
		kinds = append(kinds, f.event)
		if f.id != "" {
			if seqs[f.id] {
				t.Fatalf("duplicate seq %s in live stream: %+v", f.id, frames)
			}
			seqs[f.id] = true
		}
		if f.event == "turn.delta" && f.id != "" {
			t.Fatalf("delta must not carry id: %+v", f)
		}
	}
	got := strings.Join(kinds, ",")
	if !strings.HasPrefix(got, "turn.started,turn.delta") || !strings.HasSuffix(got, "turn.completed") {
		t.Fatalf("unexpected live sequence: %s", got)
	}

	// 续传:after=1 只应补发 seq>1 的 lifecycle,不丢不重
	frames = readSSE(t, eventsURL+"&after=1", "turn.completed", 5*time.Second)
	if len(frames) != 1 || frames[0].event != "turn.completed" || frames[0].id != "2" {
		t.Fatalf("resume after=1 must replay exactly seq 2, got %+v", frames)
	}

	// tail:无位点的全新连接不回放历史,只看到连接后的新 turn
	done2 := make(chan []sseFrame, 1)
	go func() { done2 <- readSSE(t, eventsURL, "turn.completed", 5*time.Second) }()
	time.Sleep(100 * time.Millisecond)
	resp = req(t, http.MethodPost, srv.URL+"/sessions/"+sess.ID+"/submit",
		map[string]string{"clientMessageID": "c2", "text": "again"})
	resp.Body.Close()
	for _, f := range <-done2 {
		if f.id == "1" || f.id == "2" {
			t.Fatalf("fresh connection must tail, replayed old seq %s", f.id)
		}
	}
}
