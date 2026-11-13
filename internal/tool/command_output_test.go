package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/home"
)

func outputFilesForTest(t *testing.T, result Result) map[string]commandOutputFile {
	t.Helper()
	var payload struct {
		OutputFiles map[string]commandOutputFile `json:"outputFiles"`
	}
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.OutputFiles
}

func TestCommandOutputFilesRecoverMiddleAndSurviveScratchCleanup(t *testing.T) {
	homeDir, project := t.TempDir(), t.TempDir()
	runner := NewBuiltinRunner(WithHomeDir(homeDir))
	defer runner.Close()
	call := Call{Name: CommandRun, SessionID: "output-test", CallID: "command", ProjectDirs: []string{project}, Args: resultJSON(map[string]any{"scope": "project", "command": commandHelperCommand("recoverable-output")})}
	result := runner.Call(context.Background(), call)
	payload := decodeCommandPayload(t, result)
	if !result.Ok || payload.ExitCode != 1 || !payload.StdoutTruncated || !payload.StderrTruncated || strings.Contains(payload.Stdout, "middle failure") {
		t.Fatalf("unexpected command result: %+v", payload)
	}
	files := outputFilesForTest(t, result)
	if len(files) != 2 {
		t.Fatalf("missing artifacts: %s", result.Content)
	}
	preview := ModelResultContent(CommandRun, true, result.Content, "turn", "command", true)
	if len(outputFilesForTest(t, Result{Content: preview})) != 2 {
		t.Fatal("model preview lost readback paths")
	}
	for stream, artifact := range files {
		if !artifact.Complete || artifact.Bytes != artifact.TotalBytes || artifact.Scope != "temp" {
			t.Fatalf("invalid %s artifact: %+v", stream, artifact)
		}
		data, err := os.ReadFile(filepath.Join(homeDir, "temp", artifact.Path))
		if err != nil {
			t.Fatal(err)
		}
		if int64(len(data)) != artifact.Bytes || !strings.Contains(string(data), "middle failure: 文件错误") {
			t.Fatal("middle output missing")
		}
		search := runner.Call(context.Background(), Call{Name: FileSearch, SessionID: call.SessionID, Args: resultJSON(map[string]any{"scope": artifact.Scope, "path": artifact.Path, "query": "middle failure"})})
		found := decodeToolResult(t, search)
		if !search.Ok || found["matchCount"] != float64(1) || found["complete"] != true {
			t.Fatalf("artifact not searchable: %s", search.Content)
		}
		read := runner.Call(context.Background(), Call{Name: FileSlice, SessionID: call.SessionID, Args: resultJSON(map[string]any{"scope": artifact.Scope, "path": artifact.Path, "start": 10001, "lines": 1})})
		if !read.Ok || !strings.Contains(read.Content, "middle failure") {
			t.Fatalf("artifact not readable: %s", read.Content)
		}
	}
	cleared := runner.Call(context.Background(), Call{Name: TempWorkspace, SessionID: call.SessionID, Args: resultJSON(map[string]any{"action": "clear"})})
	if !cleared.Ok {
		t.Fatal(cleared.Content)
	}
	runner.CloseSession(call.SessionID)
	for _, artifact := range files {
		if _, err := os.Stat(filepath.Join(homeDir, "temp", artifact.Path)); err != nil {
			t.Fatal("scratch/session resource cleanup deleted persistent output", err)
		}
	}
	if err := home.RemoveSessionArtifacts(homeDir, call.SessionID); err != nil {
		t.Fatal(err)
	}
	for _, artifact := range files {
		if _, err := os.Stat(filepath.Join(homeDir, "temp", artifact.Path)); !os.IsNotExist(err) {
			t.Fatalf("session deletion left output: %v", err)
		}
	}
}

func TestShortCommandOutputDoesNotCreateArtifacts(t *testing.T) {
	homeDir := t.TempDir()
	runner := NewBuiltinRunner(WithHomeDir(homeDir))
	defer runner.Close()
	result := runner.Call(context.Background(), Call{Name: CommandRun, SessionID: "short", ProjectDirs: []string{t.TempDir()}, Args: resultJSON(map[string]any{"scope": "project", "command": commandHelperCommand("report")})})
	if !result.Ok || len(outputFilesForTest(t, result)) != 0 {
		t.Fatal(result.Content)
	}
	if _, exists, err := home.ExistingSessionArtifacts(homeDir, "short"); err != nil || exists {
		t.Fatalf("short output created artifact area: %v %v", exists, err)
	}
}

func TestCommandOutputCaptureLimits(t *testing.T) {
	for _, sessionLimited := range []bool{false, true} {
		t.Run(map[bool]string{false: "stream", true: "session"}[sessionLimited], func(t *testing.T) {
			homeDir := t.TempDir()
			if sessionLimited {
				root, _, err := home.OpenSessionCommandOutputs(homeDir, "quota")
				if err != nil {
					t.Fatal(err)
				}
				file, err := root.OpenFile("previous.txt", os.O_CREATE|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if err := file.Truncate(commandOutputSessionLimit - 100000); err != nil {
					t.Fatal(err)
				}
				file.Close()
				root.Close()
			}
			capture := &commandOutputCapture{homeDir: homeDir, sessionID: "quota"}
			defer capture.close()
			buffer := newTruncatingBuffer(commandOutputLimitBytes)
			buffer.artifact = &commandOutputArtifact{capture: capture, stream: "stdout"}
			// Crossing the threshold over multiple writes must preserve the prefix exactly.
			buffer.Write([]byte(strings.Repeat("x", 40000)))
			buffer.Write([]byte(strings.Repeat("y", commandOutputFileLimit)))
			info := buffer.artifact.finish(buffer.total)
			want := int64(commandOutputFileLimit)
			reason := "stream_limit"
			if sessionLimited {
				want = 100000
				reason = "session_limit"
			}
			if info.Complete || info.Bytes != want || info.Reason != reason {
				t.Fatalf("bad quota metadata: %+v", info)
			}
			data, err := os.ReadFile(filepath.Join(homeDir, "temp", info.Path))
			if err != nil {
				t.Fatal(err)
			}
			if int64(len(data)) != want || !strings.HasPrefix(string(data), strings.Repeat("x", 40000)) {
				t.Fatal("quota/prefix mismatch")
			}
			if sessionLimited {
				second := newTruncatingBuffer(commandOutputLimitBytes)
				second.artifact = &commandOutputArtifact{capture: capture, stream: "stderr"}
				second.Write([]byte(strings.Repeat("z", commandOutputLimitBytes+1)))
				info = second.artifact.finish(second.total)
				if info.Path != "" || info.Bytes != 0 || info.Reason != "session_limit" {
					t.Fatalf("shared quota exceeded: %+v", info)
				}
			}
		})
	}
}

func TestCommandOutputStorageFailureDoesNotChangeExit(t *testing.T) {
	homeDir, outside := t.TempDir(), t.TempDir()
	root, _, err := home.OpenSessionArtifacts(homeDir, "failure")
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Symlink(outside, "command-output"); err != nil {
		t.Fatal(err)
	}
	root.Close()
	runner := NewBuiltinRunner(WithHomeDir(homeDir))
	defer runner.Close()
	result := runner.Call(context.Background(), Call{Name: CommandRun, SessionID: "failure", ProjectDirs: []string{t.TempDir()}, Args: resultJSON(map[string]any{"scope": "project", "command": commandHelperCommand("recoverable-output")})})
	if payload := decodeCommandPayload(t, result); !result.Ok || payload.ExitCode != 1 || payload.Reason != "non_zero_exit" {
		t.Fatalf("storage failure replaced process outcome: %+v", payload)
	}
	for _, info := range outputFilesForTest(t, result) {
		if info.Complete || info.Path != "" || info.Reason != "storage_unavailable" {
			t.Fatalf("storage failure hidden: %+v", info)
		}
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("symlink target written: %v %v", entries, err)
	}
}
