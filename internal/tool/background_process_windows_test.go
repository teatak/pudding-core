//go:build windows

package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsCommandCompletionCleansDescendants(t *testing.T) {
	for _, mode := range []string{"foreground", "background", "no_session"} {
		for _, streams := range []string{"inherited", "closed"} {
			t.Run(mode+"/"+streams, func(t *testing.T) {
				root := t.TempDir()
				runner := NewBuiltinRunner()
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				done := make(chan struct{})
				t.Cleanup(func() {
					cancel()
					_ = runner.Close()
					waitBackgroundProcessSignal(t, done, "command goroutine cleanup")
				})
				sessionID := "windows-tree"
				args := map[string]any{"scope": "project", "command": windowsTreeHelperCommand("parent", root, streams)}
				switch mode {
				case "foreground":
					args["yield_ms"] = 0
				case "background":
					args["background"] = true
				case "no_session":
					sessionID = ""
				}
				type completion struct {
					result  Result
					process BackgroundProcessSnapshot
					err     error
				}
				completed := make(chan completion, 1)
				go func() {
					defer close(done)
					result := runner.Call(ctx, Call{SessionID: sessionID, Name: CommandRun, ProjectDirs: []string{root}, Args: resultJSON(args)})
					if sessionID == "" || !result.Ok {
						completed <- completion{result: result}
						return
					}
					var payload backgroundProcessPayload
					if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
						completed <- completion{result: result, err: err}
						return
					}
					process := runner.processes.Get(sessionID, payload.ProcessID)
					if process == nil {
						completed <- completion{result: result, err: fmt.Errorf("missing process %q", payload.ProcessID)}
						return
					}
					select {
					case <-process.done:
						completed <- completion{result: result, process: process.snapshot()}
					case <-ctx.Done():
						completed <- completion{result: result, err: ctx.Err()}
					}
				}()
				// Ensure Job attachment precedes child creation, independently of
				// scheduler order or PowerShell startup speed.
				ready := waitWindowsTreeFile(t, filepath.Join(root, "parent-ready"))
				var parentPID, shellPID int
				if _, err := fmt.Sscanf(string(ready), "%d %d", &parentPID, &shellPID); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(3 * time.Second)
				for {
					attached := false
					commandJobs.Range(func(key, _ any) bool {
						pid := key.(*os.Process).Pid
						attached = pid == parentPID || pid == shellPID
						return !attached
					})
					if attached {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("command did not attach its Job before spawning descendants")
					}
					time.Sleep(time.Millisecond)
				}
				writePatchTestFile(t, filepath.Join(root, "spawn"), "go")
				childPID, err := strconv.Atoi(string(waitWindowsTreeFile(t, filepath.Join(root, "child-ready"))))
				if err != nil {
					t.Fatal(err)
				}
				child, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(childPID))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					_ = windows.TerminateProcess(child, 1)
					_, _ = windows.WaitForSingleObject(child, 3000)
					_ = windows.CloseHandle(child)
				})
				writePatchTestFile(t, filepath.Join(root, "exit-parent"), "go")
				var result completion
				select {
				case result = <-completed:
				case <-time.After(3 * time.Second):
					t.Fatal("normal root exit waited for descendant-held output handles")
				}
				if result.err != nil || !result.result.Ok {
					t.Fatalf("command failed: result=%+v error=%v", result.result, result.err)
				}
				if sessionID == "" {
					payload := decodeCommandPayload(t, result.result)
					if payload.ExitCode != 0 || payload.Reason != "" {
						t.Fatalf("normal foreground completion changed: %+v", payload)
					}
				} else if result.process.Running || result.process.ExitCode == nil || *result.process.ExitCode != 0 || result.process.Reason != "exited" || result.process.Error != "" {
					t.Fatalf("normal session completion changed: %+v", result.process)
				}
				if count := runner.processes.Count(sessionID); count != 0 {
					t.Fatalf("completed command retained quota: %d", count)
				}
				status, err := windows.WaitForSingleObject(child, 1000)
				if err != nil || status != windows.WAIT_OBJECT_0 {
					t.Fatalf("child survived command completion: status=%d error=%v", status, err)
				}
				writePatchTestFile(t, filepath.Join(root, "survive"), "go")
				if _, err := os.Stat(filepath.Join(root, "survived")); !os.IsNotExist(err) {
					t.Fatalf("descendant wrote after root completion: %v", err)
				}
			})
		}
	}
}

func windowsTreeHelperCommand(args ...string) string {
	executable, _ := os.Executable()
	argv := append([]string{"-test.run=^TestWindowsCommandTreeHelper$", "--"}, args...)
	for i, arg := range argv {
		argv[i] = syscall.EscapeArg(arg)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
	// Inherit the root's handles directly. A native pipeline's own output
	// reader could keep PowerShell alive, which is not root-process exit.
	return "$p = Start-Process -FilePath " + quote(executable) + " -ArgumentList " + quote(strings.Join(argv, " ")) + " -NoNewWindow -PassThru; $p.WaitForExit(); exit $p.ExitCode"
}

func waitWindowsTreeFile(t *testing.T, path string) []byte {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return data
		}
		if time.Now().After(deadline) {
			t.Fatalf("helper did not create %s: %v", filepath.Base(path), err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWindowsCommandTreeHelper(t *testing.T) {
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+2 >= len(os.Args) {
		return
	}
	args := os.Args[separator+1:]
	mode, root := args[0], args[1]
	switch mode {
	case "parent":
		writePatchTestFile(t, filepath.Join(root, "parent-ready"), fmt.Sprintf("%d %d", os.Getpid(), os.Getppid()))
		waitWindowsTreeFile(t, filepath.Join(root, "spawn"))
		child := exec.Command(os.Args[0], "-test.run=^TestWindowsCommandTreeHelper$", "--", "child", root)
		if args[2] == "inherited" {
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
		}
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		waitWindowsTreeFile(t, filepath.Join(root, "exit-parent"))
		fmt.Fprintln(os.Stdout, "parent exited")
	case "child":
		writePatchTestFile(t, filepath.Join(root, "child-ready"), strconv.Itoa(os.Getpid()))
		waitWindowsTreeFile(t, filepath.Join(root, "survive"))
		writePatchTestFile(t, filepath.Join(root, "survived"), "child ran after completion")
	default:
		os.Exit(2)
	}
	os.Exit(0)
}
