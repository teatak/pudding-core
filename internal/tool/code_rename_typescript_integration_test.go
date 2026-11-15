package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/lsp"
)

func TestCodeRenameTypeScriptChecksNameAndContextBeforeWriting(t *testing.T) {
	if os.Getenv("PUDDING_LSP_TS_INTEGRATION") != "1" {
		t.Skip("set PUDDING_LSP_TS_INTEGRATION=1 to test the installed TypeScript language server")
	}
	root := t.TempDir()
	writeCodeTestFile(t, filepath.Join(root, "tsconfig.json"), `{"compilerOptions":{"strict":true,"target":"ES2022","allowJs":true,"checkJs":false},"include":["*.ts","*.js"]}`)
	manager := lsp.NewManager(lsp.WithIdleTimeout(0), lsp.WithReapInterval(0))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = manager.Close(ctx)
	})
	runner := NewBuiltinRunner(WithLanguageService(manager))
	for _, test := range []struct {
		name, extension, source, old, next string
		valid                              bool
	}{
		{"hyphen", ".ts", "export const count = 1; export const use = count;\n", "count", "user-name", false},
		{"leading digit", ".js", "export const count = 1; export const use = count;\n", "count", "1bad", false},
		{"reserved keyword", ".ts", "export const count = 1; export const use = count;\n", "count", "class", false},
		{"strict binding", ".ts", "export function f(value: number) { return value; }\n", "value", "let", false},
		{"strict eval", ".js", "export function f(/** @type {number} */ value) { return value; }\n", "value", "eval", false},
		{"unchecked JS let", ".js", "export function f(value) { return value; }\n", "value", "let", false},
		{"nocheck JS arguments", ".js", "// @ts-nocheck\nexport function f(value) { return value; }\n", "value", "arguments", false},
		{"uppercase nocheck JS eval", ".js", "// @TS-NOCHECK\nexport function f(value) { return value; }\n", "value", "eval", false},
		{"ignored JS binding", ".js", "// @ts-ignore\nexport function f(value) { return value; }\n", "value", "let", false},
		{"expected error JS binding", ".js", "// @ts-expect-error\nexport function f(value) { return value; }\n", "value", "eval", false},
		{"JSDoc ignored JS binding", ".js", "/** @ts-ignore */\nexport function f(value) { return value; }\n", "value", "eval", false},
		{"nonBMP prefix directive", ".js", "// 😀\n// @ts-nocheck\nexport function f(value) { return value; }\n", "value", "let", false},
		{"literal directives preserved", ".js", "// @ts-nocheck\nexport const marker = '@ts-ignore'; export const template = `@ts-check`; export const regexp = /@ts-expect-error/;\nexport const obj = { 'value': 1 }; export const use = obj['value'];\n", "value", "user-name", true},
		{"ignored TS type name", ".ts", "// @ts-ignore\nexport type Target = string;\n", "Target", "number", false},
		{"shebang nocheck binding", ".js", "#!/usr/bin/env node\n// @ts-nocheck\nexport function f(value) { return value; }\n", "value", "eval", false},
		{"unchecked keyword property", ".js", "// @ts-nocheck\nexport const obj = { value: 1 }; export const use = obj.value;\n", "value", "class", true},
		{"unchecked quoted property", ".js", "// @ts-nocheck\nexport const obj = { 'value': 1 }; export const use = obj['value'];\n", "value", "user-name", true},
		{"type name", ".ts", "export type Target = string;\n", "Target", "number", false},
		{"constructor parameter property", ".ts", "export class Box { constructor(public value: number) {} }\n", "value", "constructor", false},
		{"constructor field", ".ts", "export class Box { value = 1; }\n", "value", "constructor", false},
		{"numeric enum member", ".ts", "export enum Example { 'value' = 0 }\n", "value", "123", false},
		{"parameter injection", ".ts", "export function f(value: number) { return value; }\n", "value", "a = 1", false},
		{"unicode", ".ts", "export const count = 1; export const use = count;\n", "count", "计数", true},
		{"private property", ".ts", "export class Box { #value = 1; get() { return this.#value; } }\n", "#value", "#count", true},
		{"private constructor", ".ts", "export class Box { #value = 1; get() { return this.#value; } }\n", "#value", "#constructor", false},
		{"keyword property", ".ts", "export const obj = { value: 1 }; export const use = obj.value;\n", "value", "class", true},
		{"quoted property", ".ts", "export const obj = { 'value': 1 }; export const use = obj['value'];\n", "value", "user-name", true},
		{"quote injection", ".ts", "export const obj = { 'value': 1 }; export const use = obj['value'];\n", "value", "a' + 'b", false},
		{"existing syntax", ".ts", "export const count = 1; export const broken = ;\n", "count", "renamed", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := "main" + test.extension
			fullPath := filepath.Join(root, path)
			writeCodeTestFile(t, fullPath, test.source)
			prefix := test.source[:strings.Index(test.source, test.old)]
			line := strings.Count(prefix, "\n") + 1
			column := utf8.RuneCountInString(prefix[strings.LastIndex(prefix, "\n")+1:]) + 1
			args, _ := json.Marshal(map[string]any{"scope": "project", "path": path, "line": line, "column": column, "new_name": test.next})
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			readArgs, _ := json.Marshal(map[string]any{"scope": "project", "paths": []string{path}})
			beforeRead := runner.Call(ctx, Call{Name: CodeDiagnostics, Args: readArgs, ProjectDirs: []string{root}})
			if !beforeRead.Ok {
				t.Fatalf("baseline diagnostics failed: %s", beforeRead.Content)
			}
			beforeDiagnostics := decodeToolResult(t, beforeRead)["diagnostics"]
			result := runner.Call(ctx, Call{Name: CodeRename, SessionID: "rename-validation", Args: args, ProjectDirs: []string{root}})
			if result.Ok != test.valid {
				t.Fatalf("unexpected rename result: %s", result.Content)
			}
			if !test.valid {
				expectedReason := "rename_invalid_name"
				if test.name == "existing syntax" {
					expectedReason = "rename_validation_failed"
				}
				if decodeToolResult(t, result)["reason"] != expectedReason {
					t.Fatalf("rename did not reach name validation (want %s): %s", expectedReason, result.Content)
				}
			}
			content, err := os.ReadFile(fullPath)
			if err != nil {
				t.Fatal(err)
			}
			if !test.valid && string(content) != test.source {
				t.Fatalf("rejected rename changed disk: %q", content)
			}
			if test.valid && string(content) != strings.ReplaceAll(test.source, test.old, test.next) {
				t.Fatalf("valid rename incorrect: %q", content)
			}
			// Query again through a new operation to catch leaked speculative
			// overlays on both rejection and successful disk application.
			read := runner.Call(ctx, Call{Name: CodeDiagnostics, Args: readArgs, ProjectDirs: []string{root}})
			if !read.Ok {
				t.Fatalf("next query failed after rename: %s", read.Content)
			}
			if !test.valid && !reflect.DeepEqual(decodeToolResult(t, read)["diagnostics"], beforeDiagnostics) {
				t.Fatalf("rejected rename left changed diagnostics: before=%s after=%s", beforeRead.Content, read.Content)
			}
			if test.valid && decodeToolResult(t, read)["diagnosticCount"] != float64(0) {
				t.Fatalf("valid rename left invalid overlay: %s", read.Content)
			}
		})
	}
}
