package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/store"
)

func TestBuiltinAttachmentExportWritesAuthorizedProjectFile(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	imageBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3}
	stored, err := attachment.NewService(home).StoreReader("sess_export", "capture.png", "image/png", bytes.NewReader(imageBytes))
	if err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(home))
	res := runner.Call(context.Background(), Call{
		SessionID:   "sess_export",
		Name:        AttachmentExport,
		ProjectDirs: []string{project},
		Args:        json.RawMessage(`{"scope":"project","attachmentKey":"` + stored.AttachmentKey + `","path":"assets/capture.png"}`),
	})
	if !res.Ok {
		t.Fatalf("attachment export should succeed: %+v", res)
	}
	canonicalProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(canonicalProject, "assets", "capture.png")
	payload := decodeToolResult(t, res)
	if payload["path"] != wantPath || payload["relativePath"] != filepath.Join("assets", "capture.png") || payload["attachmentKey"] != stored.AttachmentKey {
		t.Fatalf("unexpected export payload: %+v", payload)
	}
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, imageBytes) {
		t.Fatal("exported attachment bytes changed")
	}
}

func TestBuiltinAttachmentExportTempRoundTrip(t *testing.T) {
	for _, withProject := range []bool{false, true} {
		t.Run(fmt.Sprint("project=", withProject), func(t *testing.T) {
			// Match macOS's /var -> /private/var alias on every test platform.
			homeDir := filepath.Join(t.TempDir(), "home-alias")
			if err := os.Symlink(t.TempDir(), homeDir); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			var projects []string
			if withProject {
				projects = []string{t.TempDir(), t.TempDir()}
			}
			data := testPNGBytes()
			stored, err := attachment.NewService(homeDir).StoreReader("session-a", "capture.png", "image/png", bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			runner := NewBuiltinRunner(WithHomeDir(homeDir))
			t.Cleanup(func() { _ = runner.Close() })
			invoke := func(turn, name string, args any) Result {
				t.Helper()
				raw, _ := json.Marshal(args)
				result := runner.Call(context.Background(), Call{SessionID: "session-a", TurnID: turn, CallID: name, Name: name, Mode: store.ModeCode, ProjectDirs: projects, Args: raw})
				if !result.Ok {
					t.Fatalf("%s failed: %+v", name, result)
				}
				return result
			}
			args := map[string]any{"scope": "temp", "attachmentKey": stored.AttachmentKey}
			first := decodeToolResult(t, invoke("turn-a", AttachmentExport, args))
			second := decodeToolResult(t, invoke("turn-b", AttachmentExport, args))
			path, absolute := first["path"].(string), first["absolutePath"].(string)
			if filepath.IsAbs(path) || first["scope"] != "temp" || !filepath.IsAbs(absolute) || second["path"] == path {
				t.Fatalf("invalid/nonunique temp export contract: first=%+v second=%+v", first, second)
			}
			canonicalHome, _ := filepath.EvalSymlinks(homeDir)
			if filepath.Join(home.TempPath(canonicalHome), path) != absolute || !strings.HasPrefix(path, filepath.Join("session-artifacts", "session-a")+string(filepath.Separator)) {
				t.Fatalf("temp-relative and absolute paths disagree: %+v", first)
			}
			if got, err := os.ReadFile(absolute); err != nil || !bytes.Equal(got, data) {
				t.Fatalf("export changed content: %x %v", got, err)
			}
			invoke("turn-b", FileStat, map[string]any{"scope": "temp", "path": path})
			media := invoke("turn-b", MediaRead, map[string]any{"source": "file", "scope": "temp", "path": path})
			if len(media.ContextAttachments) != 1 {
				t.Fatalf("media could not reuse exported path: %+v", media)
			}
			mediaAttachment := media.ContextAttachments[0]
			exportedInfo, exportStatErr := os.Stat(absolute)
			sourceInfo, sourceStatErr := os.Stat(mediaAttachment.SourcePath)
			if exportStatErr != nil || sourceStatErr != nil || !os.SameFile(exportedInfo, sourceInfo) {
				t.Fatalf("media source is not the exported file: export=%q source=%q exportErr=%v sourceErr=%v", absolute, mediaAttachment.SourcePath, exportStatErr, sourceStatErr)
			}
			mediaPath, owned, err := attachment.NewService(homeDir).Path("session-a", mediaAttachment.AttachmentKey)
			if err != nil || !owned {
				t.Fatalf("media attachment is not owned by this session: %+v %v", mediaAttachment, err)
			}
			if got, err := os.ReadFile(mediaPath); err != nil || !bytes.Equal(got, data) {
				t.Fatalf("media changed exported content: %x %v", got, err)
			}
			copyPath := filepath.Join(filepath.Dir(path), "analysis-copy.png")
			invoke("turn-b", FileCopy, map[string]any{"from": map[string]any{"scope": "temp", "path": path}, "to": map[string]any{"scope": "temp", "path": copyPath}})
			if got, err := os.ReadFile(filepath.Join(home.TempPath(canonicalHome), copyPath)); err != nil || !bytes.Equal(got, data) {
				t.Fatalf("copy could not reuse exported path: %x %v", got, err)
			}
			commandProjects := projects
			if len(commandProjects) == 0 {
				scratch, err := home.PrepareCodeScratch(homeDir, "session-a")
				if err != nil {
					t.Fatal(err)
				}
				commandProjects = []string{scratch}
			}
			commandArgs, _ := json.Marshal(map[string]any{"scope": "project", "command": "wc -c < '" + absolute + "'", "cwd": commandProjects[0]})
			command := runner.Call(context.Background(), Call{SessionID: "session-a", TurnID: "turn-b", Name: CommandRun, Mode: store.ModeCode, ProjectDirs: commandProjects, Args: commandArgs})
			if !command.Ok || strings.TrimSpace(fmt.Sprint(decodeToolResult(t, command)["stdout"])) != strconv.Itoa(len(data)) {
				t.Fatalf("command could not read absolutePath: %+v", command)
			}
			for _, root := range projects {
				entries, _ := os.ReadDir(root)
				if len(entries) != 0 {
					t.Fatalf("temp export mutated project: %s %+v", root, entries)
				}
			}
			raw, _ := json.Marshal(args)
			if tracking, tracked := MutationTrackingForCall(Call{Name: AttachmentExport, Args: raw, ProjectDirs: projects}); tracked {
				t.Fatalf("temp export was tracked as a project mutation: %+v", tracking)
			}
		})
	}
}

func TestBuiltinAttachmentExportValidatesScopeFieldsAndOwner(t *testing.T) {
	homeDir := t.TempDir()
	stored, err := attachment.NewService(homeDir).StoreReader("session-a", "capture.png", "image/png", bytes.NewReader(testPNGBytes()))
	if err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(homeDir))
	t.Cleanup(func() { _ = runner.Close() })
	for _, extra := range []string{`,"path":"chosen.png"`, `,"path":""`, `,"path":null`, `,"overwrite":false`, `,"overwrite":null`} {
		res := runner.Call(context.Background(), Call{SessionID: "session-a", Name: AttachmentExport, Args: json.RawMessage(`{"scope":"temp","attachmentKey":"` + stored.AttachmentKey + `"` + extra + `}`)})
		if res.Ok || decodeToolResult(t, res)["reason"] != "invalid_arguments" {
			t.Fatalf("temp accepted inapplicable field %s: %+v", extra, res)
		}
	}
	for _, path := range []string{"", `,"path":""`, `,"path":"  "`, `,"path":null`} {
		res := runner.Call(context.Background(), Call{SessionID: "session-a", Name: AttachmentExport, ProjectDirs: []string{t.TempDir()}, Args: json.RawMessage(`{"scope":"project","attachmentKey":"` + stored.AttachmentKey + `"` + path + `}`)})
		if res.Ok || decodeToolResult(t, res)["reason"] != "path_required" {
			t.Fatalf("project accepted missing path %s: %+v", path, res)
		}
	}
	res := runner.Call(context.Background(), Call{SessionID: "session-b", Name: AttachmentExport, Args: json.RawMessage(`{"scope":"temp","attachmentKey":"` + stored.AttachmentKey + `"}`)})
	if res.Ok || decodeToolResult(t, res)["reason"] != "attachment_not_found" {
		t.Fatalf("exported other session attachment: %+v", res)
	}
	if _, exists, err := home.ExistingSessionArtifacts(homeDir, "session-a"); err != nil || exists {
		t.Fatalf("invalid export allocated files: %v %v", exists, err)
	}
}

func TestBuiltinAttachmentExportRejectsSourceSymlinks(t *testing.T) {
	for _, parent := range []bool{false, true} {
		t.Run(fmt.Sprint("parent=", parent), func(t *testing.T) {
			homeDir := t.TempDir()
			service := attachment.NewService(homeDir)
			stored, err := service.StoreReader("session-a", "capture.png", "image/png", bytes.NewReader(testPNGBytes()))
			if err != nil {
				t.Fatal(err)
			}
			source, _, _ := service.Path("session-a", stored.AttachmentKey)
			link := source
			if parent {
				link = filepath.Dir(source)
			}
			outside := filepath.Join(t.TempDir(), filepath.Base(link))
			if err := os.Rename(link, outside); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			runner := NewBuiltinRunner(WithHomeDir(homeDir))
			t.Cleanup(func() { _ = runner.Close() })
			res := runner.Call(context.Background(), Call{SessionID: "session-a", Name: AttachmentExport, Args: json.RawMessage(`{"scope":"temp","attachmentKey":"` + stored.AttachmentKey + `"}`)})
			if res.Ok {
				t.Fatalf("export followed source symlink: %+v", res)
			}
			if _, exists, err := home.ExistingSessionArtifacts(homeDir, "session-a"); err != nil || exists {
				t.Fatalf("rejected source allocated output: %v %v", exists, err)
			}
		})
	}
}

func TestBuiltinAttachmentExportRejectsOtherSessionAndExistingDestination(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	stored, err := attachment.NewService(home).StoreReader("sess_source", "capture.png", "image/png", bytes.NewReader([]byte("image")))
	if err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(home))
	otherSession := runner.Call(context.Background(), Call{
		SessionID:   "sess_other",
		Name:        AttachmentExport,
		ProjectDirs: []string{project},
		Args:        json.RawMessage(`{"scope":"project","attachmentKey":"` + stored.AttachmentKey + `","path":"capture.png"}`),
	})
	if otherSession.Ok || decodeToolResult(t, otherSession)["reason"] != "attachment_not_found" {
		t.Fatalf("other session attachment must be rejected: %+v", otherSession)
	}

	destination := filepath.Join(project, "capture.png")
	if err := os.WriteFile(destination, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	existing := runner.Call(context.Background(), Call{
		SessionID:   "sess_source",
		Name:        AttachmentExport,
		ProjectDirs: []string{project},
		Args:        json.RawMessage(`{"scope":"project","attachmentKey":"` + stored.AttachmentKey + `","path":"capture.png"}`),
	})
	if existing.Ok || decodeToolResult(t, existing)["reason"] != "to_exists" {
		t.Fatalf("existing destination must require overwrite: %+v", existing)
	}
}
