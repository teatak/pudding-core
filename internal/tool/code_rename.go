package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/lsp"
	"github.com/teatak/pudding-core/internal/store"
)

const (
	maxCodeRenameRunes = 256
	maxCodeRenameEdits = 4096
)

type codeRenameArgs struct {
	codePositionArgs
	NewName string `json:"new_name"`
}

type codeRenameTextEdit struct {
	Range   lsp.Range `json:"range"`
	NewText string    `json:"newText"`
}

type codeRenameWorkspaceEdit struct {
	Changes         map[string][]codeRenameTextEdit `json:"changes,omitempty"`
	DocumentChanges []json.RawMessage               `json:"documentChanges,omitempty"`
}

type codeRenameDocumentEdit struct {
	TextDocument struct {
		URI     string `json:"uri"`
		Version *int   `json:"version,omitempty"`
	} `json:"textDocument"`
	Edits []codeRenameTextEdit `json:"edits"`
}

type codeRenameByteEdit struct {
	start   int
	end     int
	newText string
}

type codeRenameError struct {
	reason string
	detail string
}

func (e *codeRenameError) Error() string { return e.detail }

func newCodeRenameError(reason, detail string) error {
	return &codeRenameError{reason: reason, detail: detail}
}

func (r *BuiltinRunner) codeRename(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	if strings.TrimSpace(call.SessionID) == "" {
		return toolJSONError(out, "session_required", "session id is required for semantic rename")
	}
	var args codeRenameArgs
	if len(call.Args) == 0 || json.Unmarshal(call.Args, &args) != nil {
		return toolJSONError(out, "invalid_arguments", "code rename arguments must be a JSON object")
	}
	if result := validateCodePositionArgs(out, args.codePositionArgs); result != nil {
		return *result
	}
	args.NewName = strings.TrimSpace(args.NewName)
	if err := validateCodeRenameName(args.NewName); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}

	target, document, lines, state, failed := r.prepareCodePosition(ctx, out, call, args.codePositionArgs)
	if failed != nil {
		return *failed
	}
	position, err := codePosition(lines, args.Line, args.Column, state.PositionEncoding)
	if err != nil {
		return toolJSONError(out, "invalid_position", err.Error())
	}
	params := map[string]any{
		"textDocument": map[string]string{"uri": document.URI},
		"position":     position,
	}

	oldName, available, err := r.prepareCodeRename(ctx, target, document.Text, state.PositionEncoding, params)
	if err != nil {
		var renameErr *codeRenameError
		if errors.As(err, &renameErr) {
			return codeRenameFailure(out, err)
		}
		return codeRenameRequestError(out, "rename_not_available", err)
	}
	if !available {
		return toolJSONError(out, "rename_not_available", "the selected symbol cannot be renamed")
	}
	if oldName != "" && oldName == args.NewName {
		return toolJSONError(out, "rename_no_changes", "the new symbol name is unchanged")
	}

	renameParams := map[string]any{
		"textDocument": map[string]string{"uri": document.URI},
		"position":     position,
		"newName":      args.NewName,
	}
	var rawEdit json.RawMessage
	if err := r.languageService.Request(ctx, target.spec, "textDocument/rename", renameParams, &rawEdit); err != nil {
		return codeRenameRequestError(out, "rename_rejected", err)
	}
	edits, err := parseCodeRenameWorkspaceEdit(rawEdit)
	if err != nil {
		return codeRenameFailure(out, err)
	}
	patchProjectDirs, err := resolvedCodeProjectDirs(call.ProjectDirs)
	if err != nil {
		return codeRenameFailure(out, err)
	}
	files, editCount, err := buildCodeRenamePatchFiles(patchProjectDirs, target, state.PositionEncoding, edits)
	if err != nil {
		return codeRenameFailure(out, err)
	}
	mutatedPaths := make([]string, 0, len(files))
	for _, file := range files {
		mutatedPaths = append(mutatedPaths, file.Path)
	}
	reportMutationTracking(ctx, mutatedPaths, store.FileChangeOriginStructured)

	patchArgs, err := json.Marshal(filePatchArgs{Scope: managedScopeProject, Files: files})
	if err != nil {
		return toolJSONError(out, "rename_failed", err.Error())
	}
	patchCall := call
	patchCall.Args = patchArgs
	patchCall.ProjectDirs = patchProjectDirs
	patch, err := preparePatch(patchCall, filePatchArgs{Scope: managedScopeProject, Files: files})
	if err != nil {
		return patchFailure(out, err)
	}
	applied := applyPreparedPatchResult(out, patchProjectDirs, patch)
	if !applied.Ok {
		return applied
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(applied.Content), &payload); err != nil {
		return toolJSONError(out, "rename_failed", "rename result could not be encoded")
	}
	payload["operation"] = "rename"
	payload["newName"] = args.NewName
	payload["editCount"] = editCount
	payload["language"] = target.language
	payload["languageRoot"] = target.languageRoot
	payload["server"] = target.spec.Key.ServerKind
	if oldName != "" {
		payload["oldName"] = oldName
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return toolJSONError(out, "rename_failed", err.Error())
	}
	applied.Content = string(encoded)
	return applied
}

func resolvedCodeProjectDirs(projectDirs []string) ([]string, error) {
	resolved := make([]string, 0, len(projectDirs))
	seen := make(map[string]bool, len(projectDirs))
	for _, root := range normalizeProjectDirs(projectDirs) {
		candidate, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		candidate = filepath.Clean(candidate)
		if !seen[candidate] {
			seen[candidate] = true
			resolved = append(resolved, candidate)
		}
	}
	if len(resolved) == 0 {
		return nil, newCodeRenameError("rename_outside_project", "project directories are unavailable")
	}
	return resolved, nil
}

func validateCodeRenameName(name string) error {
	if name == "" {
		return errors.New("new_name is required")
	}
	if utf8.RuneCountInString(name) > maxCodeRenameRunes {
		return fmt.Errorf("new_name must not exceed %d characters", maxCodeRenameRunes)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return errors.New("new_name must not contain control characters")
		}
	}
	return nil
}

func (r *BuiltinRunner) prepareCodeRename(ctx context.Context, target resolvedCodeTarget, text, encoding string, params map[string]any) (string, bool, error) {
	var raw json.RawMessage
	err := r.languageService.Request(ctx, target.spec, "textDocument/prepareRename", params, &raw)
	if err != nil {
		var responseErr *lsp.ResponseError
		if errors.As(err, &responseErr) && responseErr.Code == -32601 {
			return "", true, nil
		}
		return "", false, err
	}
	renameRange, placeholder, available, err := parseCodePrepareRename(raw)
	if err != nil || !available || placeholder != "" || renameRange == nil {
		return truncateDiagnosticText(placeholder, maxCodeRenameRunes), available, err
	}
	oldName, err := codeTextForLSPRange(text, *renameRange, encoding)
	if err != nil {
		return "", false, err
	}
	return truncateDiagnosticText(oldName, maxCodeRenameRunes), true, nil
}

func parseCodePrepareRename(raw json.RawMessage) (*lsp.Range, string, bool, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, "", false, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, "", false, newCodeRenameError("unsafe_workspace_edit", "prepareRename returned an invalid result")
	}
	if _, hasStart := fields["start"]; hasStart {
		var renameRange lsp.Range
		if err := json.Unmarshal(raw, &renameRange); err != nil {
			return nil, "", false, newCodeRenameError("unsafe_workspace_edit", "prepareRename returned an invalid range")
		}
		return &renameRange, "", true, nil
	}
	if rangeRaw, ok := fields["range"]; ok {
		var renameRange lsp.Range
		if err := json.Unmarshal(rangeRaw, &renameRange); err != nil {
			return nil, "", false, newCodeRenameError("unsafe_workspace_edit", "prepareRename returned an invalid range")
		}
		var placeholder string
		if value := fields["placeholder"]; len(value) > 0 {
			_ = json.Unmarshal(value, &placeholder)
		}
		return &renameRange, placeholder, true, nil
	}
	var defaultBehavior bool
	if value := fields["defaultBehavior"]; len(value) > 0 {
		_ = json.Unmarshal(value, &defaultBehavior)
	}
	if defaultBehavior {
		return nil, "", true, nil
	}
	return nil, "", false, newCodeRenameError("unsafe_workspace_edit", "prepareRename returned an unsupported result")
}

func parseCodeRenameWorkspaceEdit(raw json.RawMessage) (map[string][]codeRenameTextEdit, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, newCodeRenameError("rename_no_changes", "the language server returned no rename edits")
	}
	var workspaceEdit codeRenameWorkspaceEdit
	if err := json.Unmarshal(raw, &workspaceEdit); err != nil {
		return nil, newCodeRenameError("unsafe_workspace_edit", "the language server returned an invalid WorkspaceEdit")
	}
	if len(workspaceEdit.Changes) > 0 && len(workspaceEdit.DocumentChanges) > 0 {
		return nil, newCodeRenameError("unsafe_workspace_edit", "WorkspaceEdit must not mix changes and documentChanges")
	}
	edits := make(map[string][]codeRenameTextEdit)
	for uri, items := range workspaceEdit.Changes {
		edits[uri] = append(edits[uri], items...)
	}
	for _, rawChange := range workspaceEdit.DocumentChanges {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(rawChange, &fields); err != nil {
			return nil, newCodeRenameError("unsafe_workspace_edit", "documentChanges contains an invalid entry")
		}
		if kindRaw := fields["kind"]; len(kindRaw) > 0 {
			var kind string
			_ = json.Unmarshal(kindRaw, &kind)
			return nil, newCodeRenameError("unsafe_workspace_edit", "rename does not support WorkspaceEdit resource operation: "+kind)
		}
		if len(fields["textDocument"]) == 0 {
			return nil, newCodeRenameError("unsafe_workspace_edit", "documentChanges contains an unsupported entry")
		}
		var documentEdit codeRenameDocumentEdit
		if err := json.Unmarshal(rawChange, &documentEdit); err != nil || strings.TrimSpace(documentEdit.TextDocument.URI) == "" {
			return nil, newCodeRenameError("unsafe_workspace_edit", "documentChanges contains an invalid TextDocumentEdit")
		}
		edits[documentEdit.TextDocument.URI] = append(edits[documentEdit.TextDocument.URI], documentEdit.Edits...)
	}
	if len(edits) == 0 {
		return nil, newCodeRenameError("rename_no_changes", "the language server returned no rename edits")
	}
	return edits, nil
}

func buildCodeRenamePatchFiles(projectDirs []string, target resolvedCodeTarget, encoding string, edits map[string][]codeRenameTextEdit) ([]patchFileArg, int, error) {
	targetProjectRoot, _, _, err := resolveProjectPath(projectDirs, target.path, false, false)
	if err != nil {
		return nil, 0, newCodeRenameError("rename_outside_project", err.Error())
	}
	targetProjectRoot, err = filepath.EvalSymlinks(targetProjectRoot)
	if err != nil {
		return nil, 0, newCodeRenameError("rename_outside_project", err.Error())
	}
	if len(edits) > patchMaxFiles {
		return nil, 0, newCodeRenameError("rename_too_large", fmt.Sprintf("rename affects more than %d files", patchMaxFiles))
	}

	URIs := make([]string, 0, len(edits))
	for uri := range edits {
		URIs = append(URIs, uri)
	}
	sort.Strings(URIs)
	files := make([]patchFileArg, 0, len(URIs))
	seenTargets := make(map[string]bool, len(URIs))
	totalBytes := 0
	editCount := 0
	for _, uri := range URIs {
		path, err := strictCodePathFromURI(uri)
		if err != nil {
			return nil, 0, err
		}
		root, resolvedPath, _, err := resolveProjectPath(projectDirs, path, false, false)
		if err != nil {
			return nil, 0, newCodeRenameError("rename_outside_project", "rename edit is outside authorized Project roots: "+path)
		}
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil || filepath.Clean(resolvedRoot) != filepath.Clean(targetProjectRoot) || !pathInsideRoot(resolvedPath, target.languageRoot) {
			return nil, 0, newCodeRenameError("rename_outside_project", "rename edit is outside the selected language root: "+path)
		}
		if seenTargets[resolvedPath] {
			return nil, 0, newCodeRenameError("unsafe_workspace_edit", "rename returned duplicate file URIs for: "+path)
		}
		seenTargets[resolvedPath] = true
		language, _ := codeLanguageForPath(resolvedPath)
		if language != target.language {
			return nil, 0, newCodeRenameError("unsafe_workspace_edit", "rename edit targets a different language file: "+path)
		}
		info, err := os.Lstat(resolvedPath)
		if err != nil || !info.Mode().IsRegular() {
			return nil, 0, newCodeRenameError("unsafe_workspace_edit", "rename edit target must be a regular file: "+path)
		}
		if info.Size() > patchMaxFileBytes {
			return nil, 0, newCodeRenameError("rename_too_large", "rename edit target exceeds 512 KiB: "+path)
		}
		text, _, err := readCodeDocument(resolvedPath)
		if err != nil {
			return nil, 0, newCodeRenameError("unsafe_workspace_edit", err.Error())
		}
		items := edits[uri]
		editCount += len(items)
		if editCount > maxCodeRenameEdits {
			return nil, 0, newCodeRenameError("rename_too_large", fmt.Sprintf("rename contains more than %d edits", maxCodeRenameEdits))
		}
		next, err := applyCodeRenameTextEdits(text, items, encoding)
		if err != nil {
			return nil, 0, err
		}
		if next == text {
			continue
		}
		if len(next) > patchMaxFileBytes {
			return nil, 0, newCodeRenameError("rename_too_large", "renamed file exceeds 512 KiB: "+path)
		}
		totalBytes += len(text) + len(next)
		if totalBytes > patchMaxTotalBytes {
			return nil, 0, newCodeRenameError("rename_too_large", "rename source and destination text exceeds 2 MiB")
		}
		newText := next
		files = append(files, patchFileArg{Path: resolvedPath, NewText: &newText})
	}
	if len(files) == 0 {
		return nil, 0, newCodeRenameError("rename_no_changes", "the language server returned no effective rename edits")
	}
	return files, editCount, nil
}

func strictCodePathFromURI(rawURI string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURI))
	if err != nil || u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") || u.RawQuery != "" || u.Fragment != "" {
		return "", newCodeRenameError("rename_outside_project", "rename edits must use local file URIs")
	}
	path, ok := codePathFromURI(rawURI)
	if !ok {
		return "", newCodeRenameError("rename_outside_project", "rename edit contains an invalid file URI")
	}
	return path, nil
}

func applyCodeRenameTextEdits(text string, edits []codeRenameTextEdit, encoding string) (string, error) {
	if len(edits) == 0 {
		return text, nil
	}
	converted := make([]codeRenameByteEdit, 0, len(edits))
	for _, edit := range edits {
		if !utf8.ValidString(edit.NewText) || strings.ContainsRune(edit.NewText, 0) {
			return "", newCodeRenameError("unsafe_workspace_edit", "rename edit contains invalid replacement text")
		}
		start, err := codeLSPPositionByteOffset(text, edit.Range.Start, encoding)
		if err != nil {
			return "", newCodeRenameError("unsafe_workspace_edit", "rename edit start is invalid: "+err.Error())
		}
		end, err := codeLSPPositionByteOffset(text, edit.Range.End, encoding)
		if err != nil {
			return "", newCodeRenameError("unsafe_workspace_edit", "rename edit end is invalid: "+err.Error())
		}
		if end < start {
			return "", newCodeRenameError("unsafe_workspace_edit", "rename edit range is reversed")
		}
		converted = append(converted, codeRenameByteEdit{start: start, end: end, newText: edit.NewText})
	}
	sort.Slice(converted, func(i, j int) bool {
		if converted[i].start != converted[j].start {
			return converted[i].start < converted[j].start
		}
		return converted[i].end < converted[j].end
	})
	for index := 1; index < len(converted); index++ {
		previous, current := converted[index-1], converted[index]
		if current.start < previous.end || current.start == previous.start {
			return "", newCodeRenameError("unsafe_workspace_edit", "rename contains overlapping or duplicate edits")
		}
	}
	for index := len(converted) - 1; index >= 0; index-- {
		edit := converted[index]
		text = text[:edit.start] + edit.newText + text[edit.end:]
	}
	return text, nil
}

func codeTextForLSPRange(text string, targetRange lsp.Range, encoding string) (string, error) {
	start, err := codeLSPPositionByteOffset(text, targetRange.Start, encoding)
	if err != nil {
		return "", err
	}
	end, err := codeLSPPositionByteOffset(text, targetRange.End, encoding)
	if err != nil {
		return "", err
	}
	if end < start {
		return "", errors.New("range is reversed")
	}
	return text[start:end], nil
}

func codeLSPPositionByteOffset(text string, position lsp.Position, encoding string) (int, error) {
	if position.Line < 0 || position.Character < 0 {
		return 0, errors.New("position must not be negative")
	}
	lineStarts := []int{0}
	for index := 0; index < len(text); index++ {
		if text[index] == '\n' {
			lineStarts = append(lineStarts, index+1)
		}
	}
	if position.Line >= len(lineStarts) {
		return 0, fmt.Errorf("line %d is outside the document", position.Line)
	}
	start := lineStarts[position.Line]
	end := len(text)
	if position.Line+1 < len(lineStarts) {
		end = lineStarts[position.Line+1] - 1
	}
	line := text[start:end]
	if strings.HasSuffix(line, "\r") {
		line = strings.TrimSuffix(line, "\r")
	}
	relative, err := codeLSPCharacterByteOffset(line, position.Character, encoding)
	if err != nil {
		return 0, err
	}
	return start + relative, nil
}

func codeLSPCharacterByteOffset(line string, character int, encoding string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "utf-8":
		if character > len(line) || (character < len(line) && !utf8.RuneStart(line[character])) {
			return 0, errors.New("UTF-8 character is outside a Unicode boundary")
		}
		return character, nil
	case "utf-32":
		units := 0
		for offset := range line {
			if units == character {
				return offset, nil
			}
			units++
		}
		if units == character {
			return len(line), nil
		}
		return 0, errors.New("UTF-32 character is outside the line")
	case "", "utf-16":
		units := 0
		for offset, r := range line {
			if units == character {
				return offset, nil
			}
			width := 1
			if r > 0xFFFF {
				width = 2
			}
			if units+width > character {
				return 0, errors.New("UTF-16 character splits a surrogate pair")
			}
			units += width
		}
		if units == character {
			return len(line), nil
		}
		return 0, errors.New("UTF-16 character is outside the line")
	default:
		return 0, errors.New("unsupported position encoding: " + encoding)
	}
}

func codeRenameRequestError(out Result, reason string, err error) Result {
	var responseErr *lsp.ResponseError
	if errors.As(err, &responseErr) {
		detail := strings.TrimSpace(responseErr.Message)
		if detail == "" {
			detail = "the language server rejected the rename"
		}
		return toolJSONError(out, reason, detail)
	}
	return codeServiceError(out, err)
}

func codeRenameFailure(out Result, err error) Result {
	var renameErr *codeRenameError
	if errors.As(err, &renameErr) {
		return toolJSONError(out, renameErr.reason, renameErr.detail)
	}
	return toolJSONError(out, "rename_failed", err.Error())
}
