package tool

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/teatak/pudding-core/internal/lsp"
)

// Refresh only documents already open in this language service, including
// changes made by commands or external editors. Revalidate every URI against
// the current call's roots before reading; shared processes outlive grants.
func (r *BuiltinRunner) refreshCodeDocuments(ctx context.Context, out Result, call Call, target resolvedCodeTarget) (map[string]lsp.DocumentState, *Result) {
	var documentErr error
	states, err := r.languageService.RefreshDocuments(ctx, target.spec, func(uri string) (string, bool, error) {
		path, ok := codePathFromURI(uri)
		if !ok || !pathInsideRoot(path, target.languageRoot) {
			return "", false, nil
		}
		_, resolved, _, err := resolveProjectPath(call.ProjectDirs, path, false, false)
		if err != nil || filepath.Clean(resolved) != filepath.Clean(path) || !pathInsideRoot(resolved, target.languageRoot) {
			return "", false, nil
		}
		language, _ := codeLanguageForPath(resolved)
		if language != target.language {
			return "", false, nil
		}
		text, _, err := readCodeDocument(resolved)
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		if err != nil {
			documentErr = err
			return "", false, err
		}
		return text, true, nil
	})
	if err != nil {
		failure := codeServiceError(out, err)
		if documentErr != nil {
			failure = codeDocumentError(out, documentErr)
		}
		return nil, &failure
	}
	return states, nil
}

// A second SyncDocument can be unchanged even when refresh just updated this
// document or one of its open dependencies. Preserve the refresh watermark
// instead of accepting an older cached push notification as fresh.
func codeStateAfterRefresh(state lsp.DocumentState, refreshed map[string]lsp.DocumentState) lsp.DocumentState {
	if previous, ok := refreshed[state.URI]; ok && previous.Changed && previous.Version == state.Version {
		state.Changed = true
		state.PreviousDiagnosticGeneration = previous.PreviousDiagnosticGeneration
	}
	return state
}
