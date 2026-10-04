package tool

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/teatak/pudding-core/internal/home"
)

type FileCopyLocation struct {
	Path     string `json:"path"`
	Scope    string `json:"scope"`
	External bool   `json:"external"`
}

type copyEndpoint struct {
	FileCopyLocation
	resolved resolvedFilePath
}

// FileCopyPlan is prepared without reading source contents. Only the engine can
// attach it to a Call after policy/approval succeeds. No directory grant is saved.
type FileCopyPlan struct {
	From            FileCopyLocation `json:"from"`
	To              FileCopyLocation `json:"to"`
	Recursive       bool             `json:"recursive"`
	Overwrite       bool             `json:"overwrite"`
	ReplacesTarget  bool             `json:"replacesTarget"`
	ExternalAccess  bool             `json:"externalAccess"`
	from, to        copyEndpoint
	sourceInfo      os.FileInfo
	destinationInfo os.FileInfo
	sourceEntries   map[string]os.FileInfo
	targetEntries   map[string]os.FileInfo
	args            []byte
	sessionID       string
	turnID          string
	callID          string
	used            atomic.Bool
}

func FileCopyPlanFromDetails(details map[string]any) *FileCopyPlan {
	plan, _ := details["fileCopy"].(*FileCopyPlan)
	return plan
}

func (p *FileCopyPlan) consume(call Call) bool {
	return call.Name == FileCopy && p.sessionID == call.SessionID && p.turnID == call.TurnID && p.callID == call.CallID &&
		bytes.Equal(p.args, call.Args) && p.used.CompareAndSwap(false, true)
}

func (p *FileCopyPlan) matches(current *FileCopyPlan) bool {
	return p.From == current.From && p.To == current.To &&
		sameCopyFile(p.sourceInfo, current.sourceInfo) && sameCopyFile(p.destinationInfo, current.destinationInfo) &&
		sameCopyEntries(p.sourceEntries, current.sourceEntries) && sameCopyEntries(p.targetEntries, current.targetEntries)
}

// Metadata only: preparing approval must not read external file contents.
// WalkDir does not follow symlinks; replacing a destination removes the link,
// not its referent. Source links are rejected when staging the copy.
func copyDirectoryEntries(path string, info os.FileInfo) (map[string]os.FileInfo, error) {
	if info == nil || !info.IsDir() {
		return nil, nil
	}
	entries := make(map[string]os.FileInfo)
	err := filepath.WalkDir(path, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == path {
			return nil
		}
		rel, err := filepath.Rel(path, name)
		if err != nil {
			return err
		}
		// Windows directory enumeration may cache stale metadata. Stat each
		// path so unchanged trees compare against the same live metadata.
		entries[rel], err = copyFileMetadata(name)
		return err
	})
	return entries, err
}

func sameCopyEntries(before, after map[string]os.FileInfo) bool {
	if len(before) != len(after) {
		return false
	}
	for name, info := range before {
		if !sameCopyFile(info, after[name]) {
			return false
		}
	}
	return true
}

func sameCopyFile(before, after os.FileInfo) bool {
	if before == nil || after == nil {
		return before == nil && after == nil
	}
	return os.SameFile(before, after) && before.Mode() == after.Mode() && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime())
}

func copyFileMetadata(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	// Go loads Windows file IDs lazily in SameFile. Capture the ID now,
	// before approval/staging, so a later replacement cannot inherit it.
	// This reads metadata only and does not follow the final symlink.
	if !os.SameFile(info, info) {
		return nil, &os.PathError{Op: "stat copy identity", Path: path, Err: errors.New("file identity unavailable")}
	}
	return info, nil
}

type fileCopyError struct {
	reason, detail string
	scope          string
	cause          error
}

func (e *fileCopyError) Error() string { return e.detail }

func fileCopyFailure(out Result, err error) Result {
	if errors.Is(err, errFileCopyDestinationExists) {
		return toolJSONError(out, "to_exists", "destination exists; pass overwrite=true to replace it")
	}
	var copyErr *fileCopyError
	if errors.As(err, &copyErr) {
		if copyErr.cause != nil {
			return filePathErrorWithReason(out, copyErr.scope, copyErr.reason, copyErr.cause)
		}
		return toolJSONError(out, copyErr.reason, copyErr.detail)
	}
	return toolJSONError(out, "copy_failed", err.Error())
}

func (r *BuiltinRunner) fileCopyApprovalDetails(call Call) (map[string]any, error) {
	args, err := decodeFileCopyArgs(call.Args)
	if err != nil {
		return nil, err
	}
	plan, err := r.prepareFileCopy(call, args, true)
	if err != nil {
		return nil, err
	}
	return map[string]any{"fileCopy": plan, "paths": []string{plan.From.Path, plan.To.Path}}, nil
}

func (r *BuiltinRunner) prepareFileCopy(call Call, args fileCopyArgs, allowExternal bool) (*FileCopyPlan, error) {
	from, err := r.resolveCopyEndpoint(call, args.From, false, allowExternal)
	if err != nil {
		return nil, &fileCopyError{reason: "from_path_not_allowed", detail: err.Error(), scope: args.From.Scope, cause: err}
	}
	to, err := r.resolveCopyEndpoint(call, args.To, true, allowExternal)
	if err != nil {
		return nil, &fileCopyError{reason: "to_path_not_allowed", detail: err.Error(), scope: args.To.Scope, cause: err}
	}
	if from.Path == to.Path {
		return nil, &fileCopyError{reason: "same_path", detail: "source and destination are the same path"}
	}
	if pathInsideRoot(from.Path, to.Path) {
		return nil, &fileCopyError{reason: "copy_overlap", detail: "destination contains the source path"}
	}
	info, err := copyFileMetadata(from.Path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return nil, &fileCopyError{reason: "unsupported_file_type", detail: "copy supports regular files and directories only"}
	}
	if info.IsDir() {
		if !args.Recursive {
			return nil, &fileCopyError{reason: "recursive_required", detail: "recursive=true is required to copy a directory"}
		}
		if pathInsideRoot(to.Path, from.Path) {
			return nil, &fileCopyError{reason: "copy_into_self", detail: "cannot copy a directory into itself or its descendants"}
		}
	}
	destination, err := copyFileMetadata(to.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if destination != nil {
		if os.SameFile(info, destination) {
			return nil, &fileCopyError{reason: "same_path", detail: "source and destination refer to the same file"}
		}
		if !args.Overwrite {
			return nil, &fileCopyError{reason: "to_exists", detail: "destination exists; pass overwrite=true to replace it"}
		}
	}
	sourceEntries, err := copyDirectoryEntries(from.Path, info)
	if err != nil {
		return nil, err
	}
	targetEntries, err := copyDirectoryEntries(to.Path, destination)
	if err != nil {
		return nil, err
	}
	return &FileCopyPlan{
		From: from.FileCopyLocation, To: to.FileCopyLocation, from: from, to: to,
		Recursive: args.Recursive, Overwrite: args.Overwrite, ReplacesTarget: destination != nil,
		ExternalAccess: from.External || to.External, sourceInfo: info, destinationInfo: destination,
		sourceEntries: sourceEntries, targetEntries: targetEntries,
		args: append([]byte(nil), call.Args...), sessionID: call.SessionID, turnID: call.TurnID, callID: call.CallID,
	}, nil
}

func (r *BuiltinRunner) resolveCopyEndpoint(call Call, endpoint fileCopyEndpoint, write, allowExternal bool) (copyEndpoint, error) {
	if endpoint.Scope == managedScopeProject && filepath.IsAbs(endpoint.Path) {
		// Resolve aliases and missing parents before checking any authority. The
		// volume root here is only a canonicalization base, never a granted root.
		volume := filepath.VolumeName(endpoint.Path) + string(filepath.Separator)
		_, canonical, _, err := resolveProjectPath([]string{volume}, endpoint.Path, false, write)
		if err != nil {
			return copyEndpoint{}, err
		}
		// The current session's scratch is an explicitly authorized workspace,
		// even though it lives below temp/.code. Keep project tracking and do
		// not grant other sessions' scratch or ungranted managed paths.
		if scratch, exists, err := home.ExistingCodeScratch(r.homeDir, call.SessionID); err == nil && exists && pathInsideRoot(canonical, scratch) {
			resolved, err := r.resolveFilePath(call, managedScopeProject, canonical, write, false, write)
			return copyEndpoint{FileCopyLocation: FileCopyLocation{Path: resolved.target, Scope: managedScopeProject}, resolved: resolved}, err
		}
		for _, scope := range []string{managedScopeTemp, managedScopeSkill, managedScopePlugin} {
			root, _, err := r.managedRoot(scope)
			if err != nil {
				continue
			}
			resolvedRoot, err := filepath.EvalSymlinks(root)
			if err != nil {
				continue
			}
			// A managed alias may not bypass its reserved/read-only rules.
			if !pathInsideRoot(canonical, resolvedRoot) && !pathInsideRoot(filepath.Clean(endpoint.Path), root) {
				continue
			}
			if pathInsideRoot(filepath.Clean(endpoint.Path), root) {
				lexical, err := filepath.Rel(root, filepath.Clean(endpoint.Path))
				if err != nil {
					return copyEndpoint{}, err
				}
				if err := validateManagedRelativePath(scope, lexical, write); err != nil {
					return copyEndpoint{}, err
				}
			}
			rel, err := filepath.Rel(resolvedRoot, canonical)
			if err != nil {
				return copyEndpoint{}, err
			}
			resolved, err := r.resolveFilePath(call, scope, rel, write, false, write)
			return copyEndpoint{FileCopyLocation: FileCopyLocation{Path: resolved.target, Scope: scope}, resolved: resolved}, err
		}
		resolved, err := r.resolveFilePath(call, managedScopeProject, canonical, write, false, write)
		if err == nil {
			return copyEndpoint{FileCopyLocation: FileCopyLocation{Path: resolved.target, Scope: managedScopeProject}, resolved: resolved}, nil
		}
		if !allowExternal || (!errors.Is(err, errProjectPathNotAllowed) && !errors.Is(err, errProjectDirsRequired)) {
			return copyEndpoint{}, err
		}
		return copyEndpoint{
			FileCopyLocation: FileCopyLocation{Path: canonical, Scope: "local", External: true},
			resolved:         resolvedFilePath{target: canonical, rel: canonical},
		}, nil
	}
	resolved, err := r.resolveFilePath(call, endpoint.Scope, endpoint.Path, write, false, write)
	return copyEndpoint{FileCopyLocation: FileCopyLocation{Path: resolved.target, Scope: endpoint.Scope}, resolved: resolved}, err
}
