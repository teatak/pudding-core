package tool

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/home"
)

const attachmentExportToolHint = "Use builtin_attachment_export in Code mode with this attachmentKey: scope=temp allocates an analysis file, or scope=project requires an authorized destination path. Reuse the returned scope/path for file and media tools, or absolutePath for commands; do not guess the attachment's internal filesystem path."

var errAttachmentExportNotRegular = errors.New("attachment is not a regular file")

func (r *BuiltinRunner) attachmentExport(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		Scope         string          `json:"scope"`
		AttachmentKey string          `json:"attachmentKey"`
		Path          json.RawMessage `json:"path"`
		Overwrite     json.RawMessage `json:"overwrite"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	args.Scope = strings.TrimSpace(args.Scope)
	var destinationPath string
	var overwrite bool
	switch args.Scope {
	case managedScopeTemp:
		if len(args.Path) != 0 || len(args.Overwrite) != 0 {
			return toolJSONError(out, "invalid_arguments", "scope=temp allocates a unique destination and does not accept path or overwrite")
		}
	case managedScopeProject:
		if len(args.Path) != 0 {
			if err := json.Unmarshal(args.Path, &destinationPath); err != nil {
				return toolJSONError(out, "invalid_arguments", "path must be a string")
			}
		}
		if strings.TrimSpace(destinationPath) == "" {
			return toolJSONError(out, "path_required", "scope=project requires a non-empty destination path")
		}
		if len(args.Overwrite) != 0 {
			if err := json.Unmarshal(args.Overwrite, &overwrite); err != nil {
				return toolJSONError(out, "invalid_arguments", "overwrite must be a boolean")
			}
		}
	default:
		return toolJSONError(out, "invalid_scope", "scope must be project or temp")
	}
	if strings.TrimSpace(call.SessionID) == "" {
		return toolJSONError(out, "session_required", "session id is required to export an attachment")
	}
	attachmentKey := strings.TrimSpace(args.AttachmentKey)
	if attachmentKey == "" {
		return toolJSONError(out, "attachment_required", "attachmentKey is required")
	}
	source, ok, err := attachment.NewService(r.homeDir).Path(call.SessionID, attachmentKey)
	if err != nil {
		return toolJSONError(out, "attachment_resolve_failed", err.Error())
	}
	if !ok {
		return toolJSONError(out, "attachment_not_found", "attachmentKey does not belong to this session")
	}
	input, info, err := openAttachmentExportSource(r.homeDir, source)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return toolJSONError(out, "attachment_not_found", "attachment file does not exist")
		}
		if errors.Is(err, errAttachmentExportNotRegular) {
			return toolJSONError(out, "attachment_not_regular", err.Error())
		}
		return toolJSONError(out, "attachment_stat_failed", err.Error())
	}
	defer input.Close()
	payload := map[string]any{
		"ok":            true,
		"scope":         args.Scope,
		"attachmentKey": attachmentKey,
	}
	var destinationRoot *os.Root
	var relativePath, cleanupPath string
	if args.Scope == managedScopeTemp {
		var artifactPath string
		destinationRoot, artifactPath, err = home.OpenSessionArtifacts(r.homeDir, call.SessionID)
		if err != nil {
			return toolJSONError(out, "attachment_export_failed", err.Error())
		}
		defer destinationRoot.Close()
		cleanupPath = "export-" + rand.Text()
		if err := destinationRoot.Mkdir(cleanupPath, 0o700); err != nil {
			return toolJSONError(out, "attachment_export_failed", err.Error())
		}
		relativePath = filepath.Join(cleanupPath, filepath.Base(source))
		payload["path"] = filepath.Join("session-artifacts", filepath.Base(artifactPath), relativePath)
		payload["absolutePath"] = filepath.Join(artifactPath, relativePath)
	} else {
		destination, err := r.resolveFilePath(call, managedScopeProject, destinationPath, true, false, true)
		if err != nil {
			return filePathError(out, managedScopeProject, err)
		}
		if err := prepareFileCopyDestination(destination.target, overwrite); err != nil {
			return fileCopyDestinationError(out, destination.outputPath(), err)
		}
		destinationRoot, err = os.OpenRoot(destination.root)
		if err != nil {
			return toolJSONError(out, "attachment_export_failed", err.Error())
		}
		defer destinationRoot.Close()
		relativePath = destination.rel
		payload = destination.payload(payload)
		payload["absolutePath"] = destination.target
	}
	outputPath := filepath.Join(filepath.Dir(relativePath), ".export-"+rand.Text())
	complete := false
	defer func() {
		if !complete {
			_ = destinationRoot.Remove(outputPath)
			if cleanupPath != "" {
				_ = destinationRoot.RemoveAll(cleanupPath)
			}
		}
	}()
	output, err := destinationRoot.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return toolJSONError(out, "attachment_export_failed", err.Error())
	}
	written, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return toolJSONError(out, "attachment_export_failed", err.Error())
	}
	if err := destinationRoot.Rename(outputPath, relativePath); err != nil {
		return toolJSONError(out, "attachment_export_failed", err.Error())
	}
	complete = true
	payload["bytes"] = written
	out.Ok = true
	out.Content = jsonString(payload)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(payload)
	return out
}

// Open the owned attachment through a confined root and reject symlinks at any
// component, including a session/blob directory replaced by another directory.
func openAttachmentExportSource(homeDir, source string) (*os.File, os.FileInfo, error) {
	relative, err := filepath.Rel(homeDir, source)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, nil, errors.New("attachment path escapes home")
	}
	root, err := os.OpenRoot(homeDir)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	var info os.FileInfo
	var prefix string
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		prefix = filepath.Join(prefix, component)
		info, err = root.Lstat(prefix)
		if err != nil {
			return nil, nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if prefix == relative {
				return nil, nil, errAttachmentExportNotRegular
			}
			return nil, nil, errors.New("attachment path must not contain symlinks")
		}
	}
	if info == nil || !info.Mode().IsRegular() {
		return nil, nil, errAttachmentExportNotRegular
	}
	input, err := root.Open(relative)
	if err != nil {
		return nil, nil, err
	}
	opened, err := input.Stat()
	if err != nil || !os.SameFile(info, opened) {
		input.Close()
		return nil, nil, errors.New("attachment changed while opening")
	}
	return input, opened, nil
}
