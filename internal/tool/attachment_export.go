package tool

import (
	"errors"
	"os"
	"strings"

	"github.com/teatak/pudding-core/internal/attachment"
)

const attachmentExportToolHint = "Use builtin_attachment_export in Code mode with this attachmentKey and an authorized project path; do not guess the attachment's internal filesystem path."

func (r *BuiltinRunner) attachmentExport(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		Scope         string `json:"scope"`
		AttachmentKey string `json:"attachmentKey"`
		Path          string `json:"path"`
		Overwrite     bool   `json:"overwrite"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	if strings.TrimSpace(args.Scope) != managedScopeProject {
		return toolJSONError(out, "invalid_scope", "scope must be project")
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
	info, err := os.Lstat(source)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return toolJSONError(out, "attachment_not_found", "attachment file does not exist")
		}
		return toolJSONError(out, "attachment_stat_failed", err.Error())
	}
	if !info.Mode().IsRegular() {
		return toolJSONError(out, "attachment_not_regular", "attachment is not a regular file")
	}
	destination, err := r.resolveFilePath(call, managedScopeProject, args.Path, true, false, true)
	if err != nil {
		return filePathError(out, managedScopeProject, err)
	}
	if err := prepareFileCopyDestination(destination.target, args.Overwrite); err != nil {
		return fileCopyDestinationError(out, destination.outputPath(), err)
	}
	if err := copyFileBytes(source, destination.target, info); err != nil {
		return toolJSONError(out, "attachment_export_failed", err.Error())
	}
	payload := destination.payload(map[string]any{
		"ok":            true,
		"scope":         managedScopeProject,
		"attachmentKey": attachmentKey,
		"bytes":         info.Size(),
	})
	out.Ok = true
	out.Content = jsonString(payload)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(payload)
	return out
}
