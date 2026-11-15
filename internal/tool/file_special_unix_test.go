//go:build darwin || linux

package tool

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestFileTextToolsRejectFIFOWithoutBlocking(t *testing.T) {
	for _, tool := range []string{FileRead, FileSlice, FileSearch, FileWrite} {
		t.Run(tool, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "pipe.txt")
			if err := unix.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
			// Keep both ends available so a regressed blocking open can be
			// released after timeout, rather than leaving the test hung forever.
			fd, err := unix.Open(path, unix.O_RDWR|unix.O_NONBLOCK, 0)
			if err != nil {
				t.Fatal(err)
			}
			pipe := os.NewFile(uintptr(fd), path)
			defer pipe.Close()
			runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
			defer runner.Close()
			args := map[string]any{"scope": "project", "path": "pipe.txt"}
			if tool == FileWrite {
				args["content"] = "must not write"
			}
			if tool == FileSearch {
				args["query"] = "needle"
			}
			done := make(chan Result, 1)
			go func() {
				done <- runner.Call(context.Background(), Call{Name: tool, ProjectDirs: []string{root}, Args: resultJSON(args)})
			}()
			select {
			case result := <-done:
				if result.Ok || decodeToolResult(t, result)["reason"] != "regular_file_required" {
					t.Fatalf("special file did not fail explicitly: %s", result.Content)
				}
			case <-time.After(2 * time.Second):
				_, _ = pipe.Write([]byte("unblock\n"))
				_ = pipe.Close()
				t.Fatal("text tool waited for FIFO contents instead of rejecting its type")
			}
			info, err := os.Lstat(path)
			if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
				t.Fatalf("special file was replaced: %v, %v", info, err)
			}
		})
	}
}
