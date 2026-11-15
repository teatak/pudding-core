package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/lsp"
)

type unopenedCodeLanguageService struct {
	*lsp.Manager
	opens atomic.Int32
}

func (s *unopenedCodeLanguageService) SyncDocument(ctx context.Context, spec lsp.ServerSpec, document lsp.Document) (lsp.DocumentState, error) {
	s.opens.Add(1)
	return s.Manager.SyncDocument(ctx, spec, document)
}

type unopenedCodeOperation struct {
	lsp.Operation
	opens *atomic.Int32
}

func (s *unopenedCodeLanguageService) BeginOperation(ctx context.Context, spec lsp.ServerSpec) (lsp.Operation, error) {
	op, err := s.Manager.BeginOperation(ctx, spec)
	if err != nil {
		return nil, err
	}
	return &unopenedCodeOperation{Operation: op, opens: &s.opens}, nil
}

func (o *unopenedCodeOperation) SyncDocument(ctx context.Context, spec lsp.ServerSpec, document lsp.Document) (lsp.DocumentState, error) {
	o.opens.Add(1)
	return o.Operation.SyncDocument(ctx, spec, document)
}

func TestCodeSymbolsTracksUnopenedGoFiles(t *testing.T) {
	if os.Getenv("PUDDING_LSP_INTEGRATION") != "1" {
		t.Skip("set PUDDING_LSP_INTEGRATION=1 to test the installed gopls")
	}
	for _, mode := range []string{"patch", "command_declared", "command_observed", "command_opaque", "external"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "command_observed" && runtime.GOOS == "windows" {
				t.Skip("implicit shell mutation analysis is POSIX-only; declared Windows paths are tested separately")
			}
			root, _ := codeTestProject(t)
			source := filepath.Join(root, "main.go")
			writeCodeTestFile(t, source, "package demo\nfunc PuddingBefore() {}\n")
			service := &unopenedCodeLanguageService{Manager: lsp.NewManager(lsp.WithIdleTimeout(0), lsp.WithReapInterval(0))}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				_ = service.Close(ctx)
			})
			runner := NewBuiltinRunner(WithLanguageService(service))
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			check := func(name string, want, eventual bool) {
				t.Helper()
				deadline := time.Now().Add(15 * time.Second)
				var last string
				for {
					args, _ := json.Marshal(map[string]any{"scope": "project", "path": ".", "query": name})
					result := runner.Call(ctx, Call{Name: CodeSymbols, Args: args, ProjectDirs: []string{root}})
					last = result.Content
					var payload struct {
						Symbols []struct{ Name string } `json:"symbols"`
					}
					found := false
					if result.Ok && json.Unmarshal([]byte(result.Content), &payload) == nil {
						for _, symbol := range payload.Symbols {
							found = found || symbol.Name == name || strings.HasSuffix(symbol.Name, "."+name)
						}
						if found == want {
							break
						}
					}
					if !eventual || time.Now().After(deadline) {
						t.Fatalf("symbols %s, want present=%v (eventual=%v): %s", name, want, eventual, last)
					}
					// Only external/opaque changes use bounded polling. Known
					// mutations must pass on their very first semantic query.
					select {
					case <-ctx.Done():
						t.Fatalf("waiting for external file changes: %v; %s", ctx.Err(), last)
					case <-time.After(50 * time.Millisecond):
					}
				}
				if service.opens.Load() != 0 {
					t.Fatal("test opened a document and masked the workspace-index bug")
				}
			}
			// Index via workspace/symbol only. No definition, references,
			// diagnostics or rename may didOpen these source files.
			check("PuddingBefore", true, false)
			eventual := mode == "external" || mode == "command_opaque"
			changeUnopenedCodeFile(t, ctx, runner, root, source, "replace", "package demo\nfunc PuddingAfter() {}\n", mode)
			check("PuddingAfter", true, eventual)
			check("PuddingBefore", false, false)
			// A new file in an indexed package and a new package in an unseen
			// directory exercise different gopls metadata discovery paths.
			sibling := filepath.Join(root, "sibling.go")
			changeUnopenedCodeFile(t, ctx, runner, root, sibling, "create", "package demo\nfunc PuddingSibling() {}\n", mode)
			check("PuddingSibling", true, eventual)
			created := filepath.Join(root, "nested", "created.go")
			changeUnopenedCodeFile(t, ctx, runner, root, created, "create", "package nested\nfunc PuddingCreated() {}\n", mode)
			// Neither creation nor the next edit is queried separately: event
			// coalescing must retain discovery of a previously unopened file.
			changeUnopenedCodeFile(t, ctx, runner, root, created, "replace", "package nested\nfunc PuddingUpdated() {}\n", mode)
			check("PuddingUpdated", true, eventual)
			check("PuddingCreated", false, false)
			changeUnopenedCodeFile(t, ctx, runner, root, created, "delete", "", mode)
			changeUnopenedCodeFile(t, ctx, runner, root, created, "create", "package nested\nfunc PuddingRecreated() {}\n", mode)
			check("PuddingRecreated", true, eventual)
			check("PuddingUpdated", false, false)
			changeUnopenedCodeFile(t, ctx, runner, root, created, "delete", "", mode)
			check("PuddingRecreated", false, eventual)
		})
	}
}

func changeUnopenedCodeFile(t *testing.T, ctx context.Context, runner *BuiltinRunner, root, path, action, content, mode string) {
	t.Helper()
	if mode == "external" {
		if action == "delete" {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		} else {
			writeCodeTestFile(t, path, content)
		}
		return
	}
	call := Call{SessionID: "unopened-go-files", CallID: mode + "-" + action, ProjectDirs: []string{root}}
	if mode == "patch" {
		file := patchFileArg{Path: path, Action: action}
		if action != "delete" {
			file.Content = &content
		}
		if action == "replace" {
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			file.ExpectedHash = patchContentHash(before)
		}
		call.Name = FilePatch
		call.Args, _ = json.Marshal(filePatchArgs{Scope: "project", Files: []patchFileArg{file}})
		if _, err := runner.ApprovalDetails(ctx, call); err != nil {
			t.Fatal(err)
		}
	} else {
		call.Name = CommandRun
		call.CommandSandbox = CommandSandboxBypass
		command := ""
		if mode == "command_opaque" {
			program := fmt.Sprintf("require('node:fs').mkdirSync(%s,{recursive:true});require('node:fs').writeFileSync(%s,%s)", strconv.Quote(filepath.Dir(path)), strconv.Quote(path), strconv.Quote(content))
			if action == "delete" {
				program = fmt.Sprintf("require('node:fs').unlinkSync(%s)", strconv.Quote(path))
			}
			command = "node -e " + quoteCodeMutationShell(program)
		} else if runtime.GOOS == "windows" {
			command = "[IO.Directory]::CreateDirectory(" + quoteCodeMutationShell(filepath.Dir(path)) + ") | Out-Null; [IO.File]::WriteAllText(" + quoteCodeMutationShell(path) + "," + quoteCodeMutationShell(content) + ")"
			if action == "delete" {
				command = "[IO.File]::Delete(" + quoteCodeMutationShell(path) + ")"
			}
		} else {
			command = "mkdir -p " + quoteCodeMutationShell(filepath.Dir(path)) + " && printf '%s' " + quoteCodeMutationShell(content) + " > " + quoteCodeMutationShell(path)
			if action == "delete" {
				command = "rm -- " + quoteCodeMutationShell(path)
			}
		}
		args := map[string]any{"scope": "project", "command": command, "cwd": root}
		if mode == "command_declared" {
			args["mutation_paths"] = []string{path}
		}
		call.Args, _ = json.Marshal(args)
	}
	result := runner.Call(ctx, call)
	if !result.Ok {
		t.Fatalf("%s %s failed: %s", mode, action, result.Content)
	}
	var state struct {
		Yielded bool `json:"yielded"`
	}
	if json.Unmarshal([]byte(result.Content), &state) != nil || state.Yielded {
		t.Fatalf("fixture command did not finish before its semantic query: %s", result.Content)
	}
}

func quoteCodeMutationShell(value string) string {
	if runtime.GOOS == "windows" {
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
