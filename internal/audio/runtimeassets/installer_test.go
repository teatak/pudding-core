package runtimeassets

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/config"
)

func TestRequiredFilesRequireModelsOnly(t *testing.T) {
	home := t.TempDir()
	cfg := config.DefaultAudioConfig()
	required, missing, disabled := RequiredFiles(home, cfg)
	if disabled {
		t.Fatal("runtime should not be disabled")
	}
	if len(required) != 3 {
		t.Fatalf("expected only model files, got %d required files: %+v", len(required), required)
	}

	if len(missing) != 3 {
		t.Fatalf("expected missing model files, got %d missing files: %+v", len(missing), missing)
	}

	for _, file := range required {
		writeTestFile(t, file.Path)
	}
	_, missing, _ = RequiredFiles(home, cfg)
	if len(missing) != 0 {
		t.Fatalf("expected complete runtime, got %d missing files", len(missing))
	}
}

func TestStatusEncodesEmptyFileListsAsArrays(t *testing.T) {
	home := t.TempDir()
	cfg := config.DefaultAudioConfig()
	required, _, _ := RequiredFiles(home, cfg)
	for _, file := range required {
		writeTestFile(t, file.Path)
	}

	status := NewInstaller(home, nil).Status(t.Context(), cfg)
	if status.Missing == nil {
		t.Fatal("missing should be an empty slice, got nil")
	}
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["missing"]) != "[]" {
		t.Fatalf("missing JSON = %s, want []", raw["missing"])
	}
}

func TestRequiredFilesDisabledReturnsEmptyArrays(t *testing.T) {
	off := false
	cfg := config.DefaultAudioConfig()
	cfg.ASR.Enabled = &off

	required, missing, disabled := RequiredFiles(t.TempDir(), cfg)
	if !disabled {
		t.Fatal("runtime should be disabled")
	}
	if required == nil || len(required) != 0 {
		t.Fatalf("required = %+v, want empty slice", required)
	}
	if missing == nil || len(missing) != 0 {
		t.Fatalf("missing = %+v, want empty slice", missing)
	}
}

func TestVoiceInstallPlanSkipsNativeAssets(t *testing.T) {
	manifest := Manifest{
		Native: map[string]Asset{
			"darwin_arm64": {Asset: "runtime-native-v1_darwin_arm64.tar.gz", InstallDir: "ignored-native"},
		},
		Models: map[string]Asset{
			"vad_silero":     {Asset: "vad.tar.gz", InstallDir: "runtime/models/vad"},
			"asr_sensevoice": {Asset: "asr.tar.gz", InstallDir: "runtime/models/asr"},
		},
	}
	plans, err := voiceInstallPlan(manifest, "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 {
		t.Fatalf("expected model-only install plan, got %+v", plans)
	}
	for _, plan := range plans {
		if plan.Kind == "native" {
			t.Fatalf("native asset should not be downloaded: %+v", plan)
		}
	}
}

func TestCancelWaitsForInstallToStop(t *testing.T) {
	installer := NewInstaller(t.TempDir(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	installer.mu.Lock()
	installer.cancel = cancel
	installer.done = done
	installer.state = Status{
		OK:       true,
		Running:  true,
		State:    "downloading",
		Required: []RequiredFile{},
		Missing:  []RequiredFile{},
	}
	installer.mu.Unlock()

	go func() {
		<-ctx.Done()
		installer.mu.Lock()
		installer.cancel = nil
		installer.done = nil
		installer.state.Running = false
		installer.state.State = "canceled"
		installer.state.Message = "download canceled"
		installer.mu.Unlock()
		close(done)
	}()

	status := installer.Cancel()
	if status.Running {
		t.Fatalf("cancel returned while install was still running: %+v", status)
	}
	if status.State != "canceled" {
		t.Fatalf("state = %q, want canceled", status.State)
	}
}

func TestStartCanRetryAfterCanceledDownload(t *testing.T) {
	installer := NewInstaller(t.TempDir(), nil)
	installer.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	installer.mu.Lock()
	installer.state = Status{
		OK:       true,
		State:    "canceled",
		Message:  "download canceled",
		Required: []RequiredFile{},
		Missing:  []RequiredFile{},
	}
	installer.mu.Unlock()

	status := installer.Start(t.Context(), config.DefaultAudioConfig())
	if !status.Running || status.State != "manifest" {
		t.Fatalf("Start after cancel status = %+v, want running manifest", status)
	}
	status = installer.Cancel()
	if status.Running || status.State != "canceled" {
		t.Fatalf("Cancel after retry status = %+v, want canceled and not running", status)
	}
}

func TestRuntimeErrorMessageIsGeneric(t *testing.T) {
	got := runtimeErrorMessage(errors.New("/secret/runtime/path: download failed"))
	if got != "download failed" {
		t.Fatalf("runtimeErrorMessage = %q, want generic message", got)
	}
}

func TestProgressKeepsCompletedAssetBytesDuringInstallStages(t *testing.T) {
	installer := NewInstaller(t.TempDir(), nil)
	installer.setProgress(progressEvent{
		Stage:           "downloaded",
		Asset:           "asr.tar.gz",
		Index:           1,
		Total:           2,
		BytesDownloaded: 100,
		BytesTotal:      100,
	})
	installer.setProgress(progressEvent{Stage: "verifying", Asset: "asr.tar.gz", Index: 1, Total: 2})
	installer.setProgress(progressEvent{Stage: "extracting", Asset: "asr.tar.gz", Index: 1, Total: 2})

	installer.mu.Lock()
	status := installer.state
	installer.mu.Unlock()
	if status.BytesDownloaded != 100 || status.BytesTotal != 100 {
		t.Fatalf("completed asset bytes were reset: %+v", status)
	}

	installer.setProgress(progressEvent{Stage: "asset_start", Asset: "vad.tar.gz", Index: 2, Total: 2})
	installer.mu.Lock()
	status = installer.state
	installer.mu.Unlock()
	if status.BytesDownloaded != 0 || status.BytesTotal != 0 {
		t.Fatalf("next asset did not reset byte counters: %+v", status)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
}
