package contextbuilder

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/prompt"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestBuildUsesCoreAndUserPrompt(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: "s1", TurnID: "t1", UserMessageID: "m1",
		ClientMessageID: "c1", UserText: "hi",
	}); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "pudding.md"), []byte("请尽量简短。"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := New(ms, prompt.NewLoader(home))

	req, err := b.Build(ctx, "s1", "m", string(store.ModeChat))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req.System, "You are Pudding") || !strings.Contains(req.System, "请尽量简短。") {
		t.Fatalf("unexpected system prompt: %q", req.System)
	}

	if err := ms.SetSettings(ctx, map[string]string{"system_prompt": "你是布丁"}); err != nil {
		t.Fatal(err)
	}
	req, err = b.Build(ctx, "s1", "m", string(store.ModeChat))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(req.System, "你是布丁") {
		t.Fatalf("settings system_prompt must not affect contextbuilder prompt: %q", req.System)
	}
	if len(req.Messages) != 1 || req.Messages[0].Text != "hi" {
		t.Fatalf("unexpected messages: %+v", req.Messages)
	}
}

func TestBuildIncludesAttachmentSummary(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "s1",
		TurnID:          "t1",
		UserMessageID:   "m1",
		ClientMessageID: "c1",
		UserParts: []store.ContentPart{store.AttachmentPart(store.Attachment{
			ID:            "att_1",
			Name:          "report.pdf",
			AttachmentKey: "sessions/s1/blobs/report.pdf",
			MIME:          "application/pdf",
			Size:          42,
		})},
	}); err != nil {
		t.Fatal(err)
	}
	req, err := New(ms, nil).Build(ctx, "s1", "m", string(store.ModeChat))
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || !strings.Contains(req.Messages[0].Text, "report.pdf") || !strings.Contains(req.Messages[0].Text, "application/pdf") {
		t.Fatalf("attachment summary missing from provider context: %+v", req.Messages)
	}
}

func TestBuildIncludesLocalFoldersTag(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "s1",
		TurnID:          "t1",
		UserMessageID:   "m1",
		ClientMessageID: "c1",
		UserText:        "scan this",
		UserParts: []store.ContentPart{
			store.LocalFolderPart(store.LocalFolder{
				ID:     "folder_1",
				Name:   "files",
				Path:   "/Users/me/files",
				Origin: "local_path",
			}),
			{Type: store.ContentPartText, Text: "scan this"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	req, err := New(ms, nil).Build(ctx, "s1", "m", string(store.ModeChat))
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || !strings.Contains(req.Messages[0].Text, "<pudding-local-folders version=\"1\">") || !strings.Contains(req.Messages[0].Text, `"/Users/me/files"`) {
		t.Fatalf("local folder tag missing from provider context: %+v", req.Messages)
	}
	if strings.Index(req.Messages[0].Text, "<pudding-local-folders version=\"1\">") > strings.Index(req.Messages[0].Text, "scan this") {
		t.Fatalf("local folder tag should stay before user text: %q", req.Messages[0].Text)
	}
}

func TestBuildIncludesAttachmentKeyWhenAvailable(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	home := t.TempDir()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	stored, err := attachment.NewService(home).StoreReader("s1", "demo.wav", "audio/wav", bytes.NewReader([]byte("wav bytes")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "s1",
		TurnID:          "t1",
		UserMessageID:   "m1",
		ClientMessageID: "c1",
		UserParts:       []store.ContentPart{store.AttachmentPart(stored)},
	}); err != nil {
		t.Fatal(err)
	}
	req, err := New(ms, nil, WithAttachmentHome(home)).Build(ctx, "s1", "m", string(store.ModeChat), provider.ModelConfig{
		Capabilities: &provider.ModelCapabilities{Audio: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || !strings.Contains(req.Messages[0].Text, "attachmentKey: "+stored.AttachmentKey) || !strings.Contains(req.Messages[0].Text, "displayURL (UI only): "+stored.URL) {
		t.Fatalf("attachment key metadata missing from fallback: %+v", req.Messages)
	}
	if strings.Contains(req.Messages[0].Text, "Local path for tools:") {
		t.Fatalf("fallback should not expose managed absolute paths: %q", req.Messages[0].Text)
	}
}

func TestBuildIncludesTempAttachmentToolScope(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	home := t.TempDir()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	stored, err := attachment.NewService(home).StoreReader(attachment.DraftSessionID, "pasted-text.txt", "text/plain", bytes.NewReader([]byte("long text")))
	if err != nil {
		t.Fatal(err)
	}
	stored.Origin = attachment.OriginTemp
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "s1",
		TurnID:          "t1",
		UserMessageID:   "m1",
		ClientMessageID: "c1",
		UserParts:       []store.ContentPart{store.AttachmentPart(stored)},
	}); err != nil {
		t.Fatal(err)
	}
	req, err := New(ms, nil, WithAttachmentHome(home)).Build(ctx, "s1", "m", string(store.ModeChat))
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || !strings.Contains(req.Messages[0].Text, "File tool scope: temp") || !strings.Contains(req.Messages[0].Text, "File tool path: attachments/") {
		t.Fatalf("temp attachment tool scope missing from fallback: %+v", req.Messages)
	}
	if strings.Contains(req.Messages[0].Text, "Local path for tools:") {
		t.Fatalf("temp attachment should not expose managed absolute paths: %q", req.Messages[0].Text)
	}
}

func TestBuildInlinesImageAttachmentWhenSupported(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	home := t.TempDir()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	stored, err := attachment.NewService(home).StoreReader("s1", "image.png", "image/png", bytes.NewReader([]byte("png bytes")))
	if err != nil {
		t.Fatal(err)
	}
	stored = attachment.WithSourcePath(stored, "/Users/me/Desktop/image.png")
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "s1",
		TurnID:          "t1",
		UserMessageID:   "m1",
		ClientMessageID: "c1",
		UserText:        "看图",
		UserParts: []store.ContentPart{
			store.AttachmentPart(stored),
			{Type: store.ContentPartText, Text: "看图"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	req, err := New(ms, nil, WithAttachmentHome(home)).Build(ctx, "s1", "m", string(store.ModeChat), provider.ModelConfig{
		Capabilities: &provider.ModelCapabilities{Image: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || len(req.Messages[0].Parts) != 3 || req.Messages[0].Parts[0].Type != provider.PartText || req.Messages[0].Parts[1].Type != provider.PartImage || req.Messages[0].Parts[2].Type != provider.PartText {
		t.Fatalf("image attachment was not inlined: %+v", req.Messages)
	}
	if !strings.Contains(req.Messages[0].Parts[0].Text, "Source path: /Users/me/Desktop/image.png") || !strings.Contains(req.Messages[0].Parts[0].Text, "Image content: provided as an image part.") {
		t.Fatalf("image manifest missing: %+v", req.Messages[0].Parts[0])
	}
	if string(req.Messages[0].Parts[1].Data) != "png bytes" {
		t.Fatalf("unexpected image bytes: %q", string(req.Messages[0].Parts[1].Data))
	}
}

func TestImageProviderPartUsesBoundedDerivative(t *testing.T) {
	home := t.TempDir()
	img := image.NewNRGBA(image.Rect(0, 0, 2400, 120))
	for x := 0; x < 2400; x++ {
		for y := 0; y < 120; y++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x % 251), G: uint8(y % 241), B: 80, A: 0xff})
		}
	}
	var source bytes.Buffer
	if err := png.Encode(&source, img); err != nil {
		t.Fatal(err)
	}
	stored, err := attachment.NewService(home).StoreReader("s1", "wide.png", "image/png", bytes.NewReader(source.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	part, ok := (&Builder{attachmentHome: home}).imageProviderPart("s1", store.AttachmentPart(stored), provider.ModelConfig{
		Capabilities: &provider.ModelCapabilities{Image: true},
	})
	if !ok {
		t.Fatal("expected provider image part")
	}
	if part.MIME != "image/png" || part.Width != attachment.ModelImageMaxDimension || part.Height != 102 {
		t.Fatalf("unexpected provider derivative: type=%s mime=%s size=%dx%d", part.Type, part.MIME, part.Width, part.Height)
	}
	if bytes.Equal(part.Data, source.Bytes()) {
		t.Fatal("provider received original oversized image")
	}
}

func TestBuildFallsBackWhenImageCapabilityUnknown(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	home := t.TempDir()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	stored, err := attachment.NewService(home).StoreReader("s1", "image.png", "image/png", bytes.NewReader([]byte("png bytes")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "s1",
		TurnID:          "t1",
		UserMessageID:   "m1",
		ClientMessageID: "c1",
		UserParts:       []store.ContentPart{store.AttachmentPart(stored)},
	}); err != nil {
		t.Fatal(err)
	}
	req, err := New(ms, nil, WithAttachmentHome(home)).Build(ctx, "s1", "m", string(store.ModeChat))
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || len(req.Messages[0].Parts) != 1 || req.Messages[0].Parts[0].Type != provider.PartText {
		t.Fatalf("unknown image capability should fallback to text summary: %+v", req.Messages)
	}
	if !strings.Contains(req.Messages[0].Text, "Image content: not provided") {
		t.Fatalf("image fallback should warn that visual contents are unavailable: %+v", req.Messages)
	}
}

func TestBuildFallsBackWhenImageUnsupported(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	home := t.TempDir()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	stored, err := attachment.NewService(home).StoreReader("s1", "image.png", "image/png", bytes.NewReader([]byte("png bytes")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "s1",
		TurnID:          "t1",
		UserMessageID:   "m1",
		ClientMessageID: "c1",
		UserParts:       []store.ContentPart{store.AttachmentPart(stored)},
	}); err != nil {
		t.Fatal(err)
	}
	req, err := New(ms, nil, WithAttachmentHome(home)).Build(ctx, "s1", "m", string(store.ModeChat), provider.ModelConfig{
		Capabilities: &provider.ModelCapabilities{Image: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || len(req.Messages[0].Parts) != 1 || req.Messages[0].Parts[0].Type != provider.PartText {
		t.Fatalf("unsupported image should fallback to text summary: %+v", req.Messages)
	}
}

func TestBuildReplaysToolImageAttachmentAsUserMessage(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	home := t.TempDir()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	stored, err := attachment.NewService(home).StoreReader("s1", "image.png", "image/png", bytes.NewReader([]byte("png bytes")))
	if err != nil {
		t.Fatal(err)
	}
	stored.Origin = attachment.OriginTool
	begin, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "s1",
		TurnID:          "t1",
		UserMessageID:   "m1",
		ClientMessageID: "c1",
		UserText:        "读图",
		Mode:            store.ModeCode,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.AppendTurnOutput(ctx, store.AppendTurnOutputInput{
		TurnID: begin.Turn.ID,
		Parts: []store.ContentPart{
			{Type: store.ContentPartToolResult, CallID: "call_image", Name: tool.FileRead, Ok: true, Content: `{"ok":true}`},
			store.AttachmentPart(stored),
		},
	}); err != nil {
		t.Fatal(err)
	}

	req, err := New(ms, nil, WithAttachmentHome(home)).Build(ctx, "s1", "m", string(store.ModeCode), provider.ModelConfig{
		Capabilities: &provider.ModelCapabilities{Image: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 3 {
		t.Fatalf("tool image should be split into assistant tool result + user image: %+v", req.Messages)
	}
	if req.Messages[2].Role != provider.RoleUser || len(req.Messages[2].Parts) != 2 || req.Messages[2].Parts[0].Type != provider.PartText || req.Messages[2].Parts[1].Type != provider.PartImage {
		t.Fatalf("tool image should replay as user image: %+v", req.Messages)
	}
}

func TestBuildInlinesAudioAttachmentWhenSupported(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	home := t.TempDir()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	stored, err := attachment.NewService(home).StoreReader("s1", "demo.wav", "audio/wav", bytes.NewReader([]byte("wav bytes")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "s1",
		TurnID:          "t1",
		UserMessageID:   "m1",
		ClientMessageID: "c1",
		UserText:        "听一下",
		UserParts: []store.ContentPart{
			store.AttachmentPart(stored),
			{Type: store.ContentPartText, Text: "听一下"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	req, err := New(ms, nil, WithAttachmentHome(home)).Build(ctx, "s1", "m", string(store.ModeChat), provider.ModelConfig{
		Capabilities: &provider.ModelCapabilities{Audio: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || len(req.Messages[0].Parts) != 3 || req.Messages[0].Parts[0].Type != provider.PartText || req.Messages[0].Parts[1].Type != provider.PartAudio || req.Messages[0].Parts[2].Type != provider.PartText {
		t.Fatalf("audio attachment was not inlined: %+v", req.Messages)
	}
	if !strings.Contains(req.Messages[0].Parts[0].Text, "Audio content: provided as an audio part.") {
		t.Fatalf("audio manifest missing: %+v", req.Messages[0].Parts[0])
	}
	if string(req.Messages[0].Parts[1].Data) != "wav bytes" {
		t.Fatalf("unexpected audio bytes: %q", string(req.Messages[0].Parts[1].Data))
	}
}

func TestBuildSkipsASRAudioAttachment(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	home := t.TempDir()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	stored, err := attachment.NewService(home).StoreReader("s1", "asr.wav", "audio/wav", bytes.NewReader([]byte("wav bytes")))
	if err != nil {
		t.Fatal(err)
	}
	stored.Origin = attachment.OriginASRAudio
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "s1",
		TurnID:          "t1",
		UserMessageID:   "m1",
		ClientMessageID: "audmsg_test",
		UserText:        "识别文本",
		UserParts: []store.ContentPart{
			store.AttachmentPart(stored),
			{Type: store.ContentPartText, Text: "识别文本"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	req, err := New(ms, nil, WithAttachmentHome(home)).Build(ctx, "s1", "m", string(store.ModeChat), provider.ModelConfig{
		Capabilities: &provider.ModelCapabilities{Audio: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || len(req.Messages[0].Parts) != 1 || req.Messages[0].Parts[0].Type != provider.PartText || req.Messages[0].Parts[0].Text != "识别文本" {
		t.Fatalf("asr audio should not enter provider context: %+v", req.Messages)
	}
}

func TestBuildUsesVoiceAudioInsteadOfTranscriptWhenSupported(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	home := t.TempDir()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	stored, err := attachment.NewService(home).StoreReader("s1", "voice.wav", "audio/wav", bytes.NewReader([]byte("voice bytes")))
	if err != nil {
		t.Fatal(err)
	}
	stored.Origin = attachment.OriginVoiceAudio
	stored.AudioTranscript = "只用于回显"
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "s1",
		TurnID:          "t1",
		UserMessageID:   "m1",
		ClientMessageID: "voicemsg_test",
		UserText:        "只用于回显",
		UserParts:       []store.ContentPart{store.AttachmentPart(stored)},
	}); err != nil {
		t.Fatal(err)
	}
	req, err := New(ms, nil, WithAttachmentHome(home)).Build(ctx, "s1", "m", string(store.ModeChat), provider.ModelConfig{
		Capabilities: &provider.ModelCapabilities{Audio: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || req.Messages[0].Text != "" || len(req.Messages[0].Parts) != 1 || req.Messages[0].Parts[0].Type != provider.PartAudio {
		t.Fatalf("voice input should contain only original audio: %+v", req.Messages)
	}
	if string(req.Messages[0].Parts[0].Data) != "voice bytes" {
		t.Fatalf("unexpected voice bytes: %q", req.Messages[0].Parts[0].Data)
	}
}

func TestBuildFallsBackToVoiceTranscriptWhenAudioUnsupported(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	home := t.TempDir()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	stored, err := attachment.NewService(home).StoreReader("s1", "voice.wav", "audio/wav", bytes.NewReader([]byte("voice bytes")))
	if err != nil {
		t.Fatal(err)
	}
	stored.Origin = attachment.OriginVoiceAudio
	stored.AudioTranscript = "降级文字"
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "s1",
		TurnID:          "t1",
		UserMessageID:   "m1",
		ClientMessageID: "voicemsg_test",
		UserText:        "降级文字",
		UserParts:       []store.ContentPart{store.AttachmentPart(stored)},
	}); err != nil {
		t.Fatal(err)
	}
	req, err := New(ms, nil, WithAttachmentHome(home)).Build(ctx, "s1", "m", string(store.ModeChat), provider.ModelConfig{
		Capabilities: &provider.ModelCapabilities{Audio: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || len(req.Messages[0].Parts) != 1 || req.Messages[0].Parts[0].Type != provider.PartText || req.Messages[0].Parts[0].Text != "降级文字" {
		t.Fatalf("unsupported voice input should use ASR transcript: %+v", req.Messages)
	}
}

func TestBuildFallsBackWhenAudioUnsupported(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	home := t.TempDir()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	stored, err := attachment.NewService(home).StoreReader("s1", "demo.wav", "audio/wav", bytes.NewReader([]byte("wav bytes")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "s1",
		TurnID:          "t1",
		UserMessageID:   "m1",
		ClientMessageID: "c1",
		UserParts:       []store.ContentPart{store.AttachmentPart(stored)},
	}); err != nil {
		t.Fatal(err)
	}
	req, err := New(ms, nil, WithAttachmentHome(home)).Build(ctx, "s1", "m", string(store.ModeChat), provider.ModelConfig{
		Capabilities: &provider.ModelCapabilities{Audio: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || len(req.Messages[0].Parts) != 1 || req.Messages[0].Parts[0].Type != provider.PartText {
		t.Fatalf("unsupported audio should fallback to text summary: %+v", req.Messages)
	}
}

func TestBuildStripsThoughtParts(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: "s1", TurnID: "t1", UserMessageID: "m1",
		ClientMessageID: "c1", UserText: "hi",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.FinishTurn(ctx, store.FinishTurnInput{
		TurnID: "t1",
		Status: store.TurnCompleted,
		AssistantParts: []store.ContentPart{
			{Type: store.ContentPartThought, Text: "private reasoning"},
			{Type: store.ContentPartText, Text: "answer"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	req, err := New(ms, nil).Build(ctx, "s1", "m", string(store.ModeChat))
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("unexpected messages: %+v", req.Messages)
	}
	assistant := req.Messages[1]
	if assistant.Text != "answer" {
		t.Fatalf("text column should remain final answer: %+v", assistant)
	}
	if len(assistant.Parts) != 1 || assistant.Parts[0].Type != provider.PartText || assistant.Parts[0].Text != "answer" {
		t.Fatalf("thought must be stripped from provider context: %+v", assistant.Parts)
	}
}

func TestBuildKeepsToolParts(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: "s1", TurnID: "t1", UserMessageID: "m1",
		ClientMessageID: "c1", UserText: "time",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.FinishTurn(ctx, store.FinishTurnInput{
		TurnID: "t1",
		Status: store.TurnCompleted,
		AssistantParts: []store.ContentPart{
			{Type: store.ContentPartToolUse, CallID: "call_1", Name: "builtin_time_get_current", Args: []byte(`{"timezone":"Asia/Singapore"}`)},
			{Type: store.ContentPartToolResult, CallID: "call_1", Name: "builtin_time_get_current", Ok: true, Content: `{"iso":"now"}`},
			{Type: store.ContentPartText, Text: "done"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	req, err := New(ms, nil).Build(ctx, "s1", "m", string(store.ModeChat))
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("unexpected messages: %+v", req.Messages)
	}
	parts := req.Messages[1].Parts
	if len(parts) != 3 || parts[0].Type != provider.PartToolUse || parts[1].Type != provider.PartToolResult || parts[2].Type != provider.PartText {
		t.Fatalf("unexpected parts: %+v", parts)
	}
	if parts[1].CallID != "call_1" || parts[1].Name != "builtin_time_get_current" || !parts[1].Ok || parts[1].Content == "" {
		t.Fatalf("tool result not preserved: %+v", parts[1])
	}
}

func TestBuildFiltersToolPartsOutsideMode(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: "s1", TurnID: "t1", UserMessageID: "m1",
		ClientMessageID: "c1", UserText: "weather",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.FinishTurn(ctx, store.FinishTurnInput{
		TurnID: "t1",
		Status: store.TurnCompleted,
		AssistantParts: []store.ContentPart{
			{Type: store.ContentPartToolUse, CallID: "call_1", Name: tool.RESTRequest, Args: []byte(`{"endpoint":"github_rest","path":"/user"}`)},
			{Type: store.ContentPartToolResult, CallID: "call_1", Name: tool.RESTRequest, Ok: true, Content: `{"login":"pudding"}`},
			{Type: store.ContentPartText, Text: "sunny"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	chatReq, err := New(ms, nil).Build(ctx, "s1", "m", string(store.ModeChat))
	if err != nil {
		t.Fatal(err)
	}
	chatParts := chatReq.Messages[1].Parts
	if len(chatParts) != 1 || chatParts[0].Type != provider.PartText {
		t.Fatalf("chat context should hide Work tool history: %+v", chatParts)
	}

	workReq, err := New(ms, nil).Build(ctx, "s1", "m", string(store.ModeWork))
	if err != nil {
		t.Fatal(err)
	}
	workParts := workReq.Messages[1].Parts
	if len(workParts) != 3 || workParts[0].Type != provider.PartToolUse || workParts[1].Type != provider.PartToolResult || workParts[2].Type != provider.PartText {
		t.Fatalf("work context should keep Work tool history: %+v", workParts)
	}

	codeReq, err := New(ms, nil).Build(ctx, "s1", "m", string(store.ModeCode))
	if err != nil {
		t.Fatal(err)
	}
	codeParts := codeReq.Messages[1].Parts
	if len(codeParts) != 3 || codeParts[0].Type != provider.PartToolUse || codeParts[1].Type != provider.PartToolResult || codeParts[2].Type != provider.PartText {
		t.Fatalf("code context should inherit Work tool history: %+v", codeParts)
	}
}

func TestBuildUsesLatestCompactBoundary(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: "s1", TurnID: "t1", UserMessageID: "m_old",
		ClientMessageID: "c_old", UserText: "old user",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.FinishTurn(ctx, store.FinishTurnInput{
		TurnID:         "t1",
		Status:         store.TurnCompleted,
		AssistantParts: store.TextPart("old assistant"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: "s1", TurnID: "t2", UserMessageID: "m_tail",
		ClientMessageID: "c_tail", UserText: "tail user",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.FinishTurn(ctx, store.FinishTurnInput{
		TurnID:         "t2",
		Status:         store.TurnCompleted,
		AssistantParts: store.TextPart("tail assistant"),
	}); err != nil {
		t.Fatal(err)
	}
	beforeCompact, err := ms.ListMessages(ctx, "s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	oldAssistantID := testMessageIDByText(t, beforeCompact, "old assistant")
	tailAssistantID := testMessageIDByText(t, beforeCompact, "tail assistant")
	if _, err := ms.AppendCompactSummary(ctx, store.AppendCompactSummaryInput{
		SessionID:       "s1",
		TurnID:          "t_compact",
		MessageID:       "m_compact",
		ClientMessageID: "compact:t_compact",
		Provider:        "mock",
		Model:           "mock",
		Text:            "summary of old history",
		Metadata:        store.CompactMessageMetadata([]string{"m_old", oldAssistantID}, []string{"m_tail", tailAssistantID}),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: "s1", TurnID: "t3", UserMessageID: "m_after",
		ClientMessageID: "c_after", UserText: "after compact",
	}); err != nil {
		t.Fatal(err)
	}

	req, err := New(ms, nil).Build(ctx, "s1", "m", string(store.ModeChat))
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(req.Messages))
	for _, msg := range req.Messages {
		got = append(got, msg.Text)
	}
	want := []string{"summary of old history", "tail user", "tail assistant", "after compact"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected compact context: got %v want %v", got, want)
	}
}

func TestSplitRecentInputTailTreatsSystemAsInputBoundary(t *testing.T) {
	msg := func(id, turn string, role store.Role) *store.Message {
		return &store.Message{ID: id, TurnID: turn, Role: role}
	}
	msgs := []*store.Message{
		msg("m_summary", "t_summary", store.RoleSummary),
		msg("m_user_old", "t_user_old", store.RoleUser),
		msg("m_assistant_old", "t_user_old", store.RoleAssistant),
		msg("m_system", "t_system", store.RoleSystem),
		msg("m_assistant_system", "t_system", store.RoleAssistant),
		msg("m_user_tail", "t_user_tail", store.RoleUser),
		msg("m_assistant_tail", "t_user_tail", store.RoleAssistant),
	}

	cold, tail := SplitRecentInputTail(msgs, 2)
	gotCold := testMessageIDs(cold)
	gotTail := testMessageIDs(tail)
	wantCold := []string{"m_summary", "m_user_old", "m_assistant_old"}
	wantTail := []string{"m_system", "m_assistant_system", "m_user_tail", "m_assistant_tail"}
	if strings.Join(gotCold, "|") != strings.Join(wantCold, "|") {
		t.Fatalf("unexpected cold messages: got %v want %v", gotCold, wantCold)
	}
	if strings.Join(gotTail, "|") != strings.Join(wantTail, "|") {
		t.Fatalf("unexpected tail messages: got %v want %v", gotTail, wantTail)
	}
}

func testMessageIDs(msgs []*store.Message) []string {
	ids := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		ids = append(ids, msg.ID)
	}
	return ids
}

func testMessageIDByText(t *testing.T, msgs []*store.Message, text string) string {
	t.Helper()
	for _, msg := range msgs {
		if msg.Text == text {
			return msg.ID
		}
	}
	t.Fatalf("message text %q not found in %+v", text, msgs)
	return ""
}
