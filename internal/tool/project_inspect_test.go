package tool

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestProjectInspectDetectsProjectShape(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"AGENTS.md":                 "# Instructions\n",
		"go.mod":                    "module example.com/demo\n\ngo 1.24\n",
		"main.go":                   "package main\n",
		"web/package.json":          `{"scripts":{"test":"vitest","build":"vite build","dev":"vite"}}`,
		"web/pnpm-lock.yaml":        "lockfileVersion: '9.0'\n",
		"web/src/app.ts":            "export const app = true;\n",
		"node_modules/package.json": `{"scripts":{"test":"ignored"}}`,
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := exec.Command("git", "init", "--quiet", root).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
	result := runner.Call(context.Background(), Call{
		Name:        ProjectInspect,
		Args:        json.RawMessage(`{"scope":"project"}`),
		ProjectDirs: []string{root},
	})
	if !result.Ok {
		t.Fatalf("inspect should succeed: %+v", result)
	}
	payload := decodeToolResult(t, result)
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if payload["projectRoot"] != root || payload["gitRoot"] != resolvedRoot {
		t.Fatalf("unexpected roots: %+v", payload)
	}
	if got := len(payload["manifests"].([]any)); got != 2 {
		t.Fatalf("manifest count=%d want 2: %+v", got, payload["manifests"])
	}
	if got := len(payload["instructions"].([]any)); got != 1 {
		t.Fatalf("instruction count=%d want 1", got)
	}
	languages := payload["languages"].([]any)
	if !hasProjectInspectItem(languages, "name", "Go") || !hasProjectInspectItem(languages, "name", "TypeScript") {
		t.Fatalf("missing detected languages: %+v", languages)
	}
	commands := payload["suggestedCommands"].([]any)
	if !hasProjectInspectCommand(commands, []any{"go", "test", "./..."}) || !hasProjectInspectCommand(commands, []any{"pnpm", "run", "test"}) {
		t.Fatalf("missing suggested commands: %+v", commands)
	}
}

func TestProjectInspectRejectsPathOutsideProject(t *testing.T) {
	root := t.TempDir()
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
	result := runner.Call(context.Background(), Call{
		Name:        ProjectInspect,
		Args:        json.RawMessage(`{"scope":"project","path":".."}`),
		ProjectDirs: []string{root},
	})
	if result.Ok {
		t.Fatalf("outside inspection should fail: %+v", result)
	}
}

func hasProjectInspectItem(items []any, key, value string) bool {
	for _, item := range items {
		if item.(map[string]any)[key] == value {
			return true
		}
	}
	return false
}

func hasProjectInspectCommand(items []any, argv []any) bool {
	for _, item := range items {
		actual := item.(map[string]any)["argv"].([]any)
		if len(actual) != len(argv) {
			continue
		}
		matched := true
		for index := range argv {
			matched = matched && actual[index] == argv[index]
		}
		if matched {
			return true
		}
	}
	return false
}
