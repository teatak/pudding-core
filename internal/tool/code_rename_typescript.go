package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/teatak/pudding-core/internal/lsp"
)

// TLS returns rename coordinates without validating newName. Check actual edit
// contexts, then ask TypeScript to validate the speculative source before any
// disk write. The enclosing operation lease isolates these temporary overlays.
func (r *BuiltinRunner) validateTypeScriptRename(ctx context.Context, target resolvedCodeTarget, name, encoding string, edits map[string]codeRenameFileEdits, snapshots map[string]codeRenameSnapshot, files []patchFileArg) (resultErr error) {
	service := r.codeLanguageService(ctx)
	for uri, edit := range edits {
		for _, item := range edit.Edits {
			text := snapshots[uri].Text
			if safeQuotedTypeScriptRename(text, item, name, encoding) {
				continue
			}
			old, err := codeTextForLSPRange(text, item.Range, encoding)
			if err != nil { return err }
			if strings.HasPrefix(old, "#") {
				if strings.HasPrefix(name, "#") && typeScriptIdentifierName(name[1:]) && item.NewText == name {
					continue
				}
			} else if typeScriptIdentifierName(name) {
				continue
			}
			return newCodeRenameError("rename_invalid_name", "new_name is not a valid identifier at every rename location")
		}
	}
	paths := make(map[string]string, len(snapshots))
	for uri, snapshot := range snapshots {
		paths[snapshot.Path] = uri
	}
	// Do not compare diagnostic counts: a new error can replace an old error
	// with the same code. Existing syntax errors require an explicit repair.
	for _, file := range files {
		if err := r.checkTypeScriptRenameSyntax(ctx, target, paths[file.Path], true); err != nil {
			return err
		}
	}
	changed := make([]string, 0, len(files))
	defer func() {
		if len(changed) == 0 {
			return
		}
		// Restore even when the user canceled the original call. If restoration
		// fails, discard this process so no later query sees speculative code.
		restoreCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		sort.Strings(changed)
		for _, uri := range changed {
			snapshot := snapshots[uri]
			_, languageID := codeLanguageForPath(snapshot.Path)
			if _, err := service.SyncDocument(restoreCtx, target.spec, lsp.Document{URI: uri, LanguageID: languageID, Text: snapshot.Text}); err != nil {
				if aborter, ok := service.(interface{ Abort() }); ok {
					aborter.Abort()
				}
				resultErr = newCodeRenameError("rename_validation_failed", fmt.Sprintf("No files were written; the temporary language-server state could not be restored: %v", err))
				return
			}
		}
	}()
	for _, file := range files {
		uri := paths[file.Path]
		changed = append(changed, uri)
		_, languageID := codeLanguageForPath(file.Path)
		if _, err := service.SyncDocument(ctx, target.spec, lsp.Document{URI: uri, LanguageID: languageID, Text: *file.Content}); err != nil {
			return newCodeRenameError("rename_validation_failed", err.Error())
		}
	}
	for _, file := range files {
		if err := r.checkTypeScriptRenameSyntax(ctx, target, paths[file.Path], false); err != nil {
			return err
		}
	}
	return nil
}

func (r *BuiltinRunner) checkTypeScriptRenameSyntax(ctx context.Context, target resolvedCodeTarget, uri string, baseline bool) error {
	for _, command := range []string{"syntacticDiagnosticsSync", "semanticDiagnosticsSync"} {
		diagnostics, err := r.typeScriptDiagnosticKinds(ctx, target, uri, []string{command})
		if err != nil {
			return newCodeRenameError("rename_validation_failed", err.Error())
		}
		for _, diagnostic := range diagnostics {
			if diagnostic.Severity != 1 || command == "semanticDiagnosticsSync" && !typeScriptNameGrammarDiagnostic(diagnostic.Code) {
				continue
			}
			if baseline {
				return newCodeRenameError("rename_validation_failed", "Fix existing TypeScript/JavaScript syntax or name grammar errors before renaming: "+diagnostic.Message)
			}
			return newCodeRenameError("rename_invalid_name", "The rename would produce invalid TypeScript/JavaScript syntax or name grammar: "+diagnostic.Message)
		}
	}
	return nil
}

// TypeScript reports some grammar restrictions from the semantic checker,
// notably strict-module bindings such as `let`, `eval` and `arguments`. Ordinary
// type errors are allowed, as they can predate the rename. These codes are the
// pinned TypeScript compiler's identifier/binding grammar diagnostics.
func typeScriptNameGrammarDiagnostic(raw json.RawMessage) bool {
	var code int
	if json.Unmarshal(raw, &code) != nil {
		return false
	}
	switch code {
	case 1100, 1212, 1213, 1214, 1215, 1216, 1262, 1359, 1389, 1390,
		2368, 2398, 2414, 2427, 2431, 2438, 2452, 2457, 2725, 2819, 18006, 18012, 18024, 18039:
		return true
	}
	return false
}

// ECMAScript IdentifierName admits Unicode and contextual keywords. The
// compiler checks whether a keyword is legal in each binding/property context.
func typeScriptIdentifierName(name string) bool {
	if name == "" {
		return false
	}
	for index, char := range name {
		start := char == '$' || char == '_' || unicode.IsLetter(char) || unicode.Is(unicode.Nl, char) || unicode.Is(unicode.Other_ID_Start, char)
		if start {
			continue
		}
		if index == 0 || !(unicode.Is(unicode.Mn, char) || unicode.Is(unicode.Mc, char) || unicode.Is(unicode.Nd, char) || unicode.Is(unicode.Pc, char) || unicode.Is(unicode.Other_ID_Continue, char) || char == '\u200c' || char == '\u200d') {
			return false
		}
	}
	return true
}

func safeQuotedTypeScriptRename(text string, edit codeRenameTextEdit, name, encoding string) bool {
	// TLS inserts this text verbatim. Reject escapes, interpolation and quote
	// injection instead of accepting a different literal value or expression.
	if edit.NewText != name || strings.ContainsAny(name, "\"'\\`\r\n\u2028\u2029") || strings.Contains(name, "${") {
		return false
	}
	start, err := codeLSPPositionByteOffset(text, edit.Range.Start, encoding)
	if err != nil || start == 0 {
		return false
	}
	end, err := codeLSPPositionByteOffset(text, edit.Range.End, encoding)
	if err != nil || end < start || end >= len(text) {
		return false
	}
	quote := text[start-1]
	return (quote == '\'' || quote == '"') && text[end] == quote && !strings.ContainsAny(text[start:end], "\\\"'")
}
