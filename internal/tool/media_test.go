package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/store"
)

func TestBuiltinMediaReadRoutesExistingImageAndAudioAttachments(t *testing.T) {
	home := t.TempDir()
	service := attachment.NewService(home)
	image, err := service.StoreReader("sess_media", "photo.png", "image/png", bytes.NewReader(testPNGBytes()))
	if err != nil {
		t.Fatal(err)
	}
	audio, err := service.StoreReader("sess_media", "clip.wav", "audio/wav", bytes.NewReader(testWAVBytes()))
	if err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(home))

	for _, tc := range []struct {
		name      string
		args      map[string]string
		stored    string
		mediaType string
	}{
		{name: "image URL", args: map[string]string{"source": "attachment", "url": image.URL}, stored: image.AttachmentKey, mediaType: mediaTypeImage},
		{name: "audio key", args: map[string]string{"source": "attachment", "attachmentKey": audio.AttachmentKey}, stored: audio.AttachmentKey, mediaType: mediaTypeAudio},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, err := json.Marshal(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			res := runner.Call(context.Background(), Call{SessionID: "sess_media", Name: MediaRead, Args: args})
			if !res.Ok {
				t.Fatalf("media read should succeed: %+v", res)
			}
			if len(res.Attachments) != 1 || len(res.ContextAttachments) != 1 {
				t.Fatalf("media should be routed for display and model context: %+v", res)
			}
			if got := res.ContextAttachments[0]; got.AttachmentKey != tc.stored || got.Origin != attachment.OriginTool {
				t.Fatalf("unexpected context attachment: %+v", got)
			}
			payload := decodeToolResult(t, res)
			if payload["kind"] != "media_routed" || payload["mediaType"] != tc.mediaType || payload["source"] != mediaSourceAttachment {
				t.Fatalf("unexpected payload: %+v", payload)
			}
		})
	}
}

func TestBuiltinMediaReadRoutesProjectImageAndAudioFiles(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	files := []struct {
		name      string
		data      []byte
		mediaType string
	}{
		{name: "photo.png", data: testPNGBytes(), mediaType: mediaTypeImage},
		{name: "clip.wav", data: testWAVBytes(), mediaType: mediaTypeAudio},
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(project, file.name), file.data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runner := NewBuiltinRunner(WithHomeDir(home))
	canonicalProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}

	for _, file := range files {
		t.Run(file.name, func(t *testing.T) {
			args, err := json.Marshal(map[string]string{"source": "file", "scope": "project", "path": file.name})
			if err != nil {
				t.Fatal(err)
			}
			res := runner.Call(context.Background(), Call{
				SessionID:   "sess_media",
				Name:        MediaRead,
				Args:        args,
				Mode:        store.ModeCode,
				ProjectDirs: []string{project},
			})
			if !res.Ok || len(res.ContextAttachments) != 1 {
				t.Fatalf("project media read should succeed: %+v", res)
			}
			item := res.ContextAttachments[0]
			if item.SourcePath != filepath.Join(canonicalProject, file.name) || item.Origin != attachment.OriginTool {
				t.Fatalf("unexpected stored media: %+v", item)
			}
			payload := decodeToolResult(t, res)
			if payload["mediaType"] != file.mediaType || payload["source"] != mediaSourceFile || payload["attachmentKey"] == "" {
				t.Fatalf("unexpected payload: %+v", payload)
			}
			storedPath, ok, err := attachment.NewService(home).Path("sess_media", item.AttachmentKey)
			if err != nil || !ok {
				t.Fatalf("stored attachment missing: ok=%v err=%v", ok, err)
			}
			got, err := os.ReadFile(storedPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, file.data) {
				t.Fatalf("stored bytes differ: %x", got)
			}
		})
	}
}

func TestBuiltinMediaReadRejectsCrossSessionAndFilesystemAttachmentKeys(t *testing.T) {
	home := t.TempDir()
	stored, err := attachment.NewService(home).StoreReader("sess_other", "photo.png", "image/png", bytes.NewReader(testPNGBytes()))
	if err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(home))
	for _, tc := range []struct {
		name   string
		args   map[string]string
		reason string
	}{
		{name: "other session", args: map[string]string{"source": "attachment", "url": stored.URL}, reason: "session_mismatch"},
		{name: "other session key", args: map[string]string{"source": "attachment", "attachmentKey": stored.AttachmentKey}, reason: "session_mismatch"},
		{name: "filesystem path", args: map[string]string{"source": "attachment", "attachmentKey": "/Users/me/photo.png"}, reason: "invalid_attachment_key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, err := json.Marshal(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			res := runner.Call(context.Background(), Call{SessionID: "sess_media", Name: MediaRead, Args: args})
			if res.Ok || decodeToolResult(t, res)["reason"] != tc.reason {
				t.Fatalf("unexpected result: %+v", res)
			}
		})
	}
}

func TestBuiltinMediaReadRejectsUnsupportedAndAmbiguousInputs(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "icon.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))

	for _, tc := range []struct {
		name   string
		args   string
		reason string
	}{
		{name: "missing file path", args: `{"source":"file","scope":"project"}`, reason: "file_required"},
		{name: "mixed sources", args: `{"source":"attachment","attachmentKey":"x","path":"photo.png"}`, reason: "invalid_arguments"},
		{name: "SVG visual", args: `{"source":"file","scope":"project","path":"icon.svg"}`, reason: "unsupported_media"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := runner.Call(context.Background(), Call{
				SessionID:   "sess_media",
				Name:        MediaRead,
				Args:        json.RawMessage(tc.args),
				Mode:        store.ModeCode,
				ProjectDirs: []string{root},
			})
			if res.Ok || decodeToolResult(t, res)["reason"] != tc.reason {
				t.Fatalf("unexpected result: %+v", res)
			}
			if tc.name == "SVG visual" && decodeToolResult(t, res)["recommendedTool"] != FileRead {
				t.Fatalf("SVG should recommend text read: %+v", res)
			}
		})
	}
}

func TestBuiltinMediaReadRequiresCodeForFileSource(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "photo.png"), testPNGBytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	res := NewBuiltinRunner(WithHomeDir(t.TempDir())).Call(context.Background(), Call{
		SessionID:   "sess_media",
		Name:        MediaRead,
		Args:        json.RawMessage(`{"source":"file","scope":"project","path":"photo.png"}`),
		Mode:        store.ModeChat,
		ProjectDirs: []string{root},
	})
	if res.Ok || decodeToolResult(t, res)["reason"] != "capability_required" {
		t.Fatalf("Chat mode must not read project media: %+v", res)
	}
}

func TestBuiltinMediaReadRejectsOversizedFileBeforeCopy(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "large.wav")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(attachment.MaxUploadBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	res := NewBuiltinRunner(WithHomeDir(t.TempDir())).Call(context.Background(), Call{
		SessionID:   "sess_media",
		Name:        MediaRead,
		Args:        json.RawMessage(`{"source":"file","scope":"project","path":"large.wav"}`),
		Mode:        store.ModeCode,
		ProjectDirs: []string{root},
	})
	payload := decodeToolResult(t, res)
	if res.Ok || payload["reason"] != "media_too_large" || payload["limit"] != float64(attachment.MaxUploadBytes) {
		t.Fatalf("oversized media should fail with its limit: %+v", res)
	}
}

func TestBuiltinMediaReadRejectsEmptyAndUnsupportedMedia(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "empty.wav"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "photo.bmp"), []byte("BM"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
	for _, tc := range []struct {
		path   string
		reason string
	}{
		{path: "empty.wav", reason: "empty_media"},
		{path: "photo.bmp", reason: "unsupported_media"},
	} {
		res := runner.Call(context.Background(), Call{
			SessionID:   "sess_media",
			Name:        MediaRead,
			Args:        json.RawMessage(`{"source":"file","scope":"project","path":"` + tc.path + `"}`),
			Mode:        store.ModeCode,
			ProjectDirs: []string{root},
		})
		if res.Ok || decodeToolResult(t, res)["reason"] != tc.reason {
			t.Fatalf("%s should fail with %s: %+v", tc.path, tc.reason, res)
		}
	}
}

func TestBuiltinMediaReadDoesNotRecommendFileReadForSVGAttachment(t *testing.T) {
	home := t.TempDir()
	stored, err := attachment.NewService(home).StoreReader(
		"sess_media",
		"icon.svg",
		"image/svg+xml",
		bytes.NewReader([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)),
	)
	if err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(map[string]string{"source": "attachment", "attachmentKey": stored.AttachmentKey})
	if err != nil {
		t.Fatal(err)
	}
	res := NewBuiltinRunner(WithHomeDir(home)).Call(context.Background(), Call{
		SessionID: "sess_media",
		Name:      MediaRead,
		Args:      args,
	})
	payload := decodeToolResult(t, res)
	if res.Ok || payload["reason"] != "unsupported_media" || payload["recommendedTool"] != nil {
		t.Fatalf("SVG attachment should fail without an unusable file_read recommendation: %+v", res)
	}
}

func testPNGBytes() []byte {
	return []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'E', 'N', 'D'}
}

func testWAVBytes() []byte {
	return []byte{'R', 'I', 'F', 'F', 4, 0, 0, 0, 'W', 'A', 'V', 'E'}
}
