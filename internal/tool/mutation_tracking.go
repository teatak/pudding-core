package tool

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/teatak/pudding-core/internal/store"
)

// ProjectMutationTracking describes the filesystem scope a completed tool call
// may attribute to the current turn.
type ProjectMutationTracking struct {
	Targets []string
	Origin  store.FileChangeOrigin
}

type mutationTrackingSink func(targets []string, origin store.FileChangeOrigin)

type mutationTrackingSinkContextKey struct{}

// WithMutationTrackingSink lets tools report an exact mutation scope once it
// becomes known during execution, before any filesystem writes occur.
func WithMutationTrackingSink(ctx context.Context, sink func(targets []string, origin store.FileChangeOrigin)) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, mutationTrackingSinkContextKey{}, mutationTrackingSink(sink))
}

func reportMutationTracking(ctx context.Context, targets []string, origin store.FileChangeOrigin) {
	sink, _ := ctx.Value(mutationTrackingSinkContextKey{}).(mutationTrackingSink)
	if sink != nil && len(targets) > 0 {
		sink(targets, origin)
	}
}

type mutationPathArgs struct {
	Scope string `json:"scope"`
	Path  string `json:"path"`
}

type mutationMoveArgs struct {
	Scope    string `json:"scope"`
	FromPath string `json:"from_path"`
	ToPath   string `json:"to_path"`
}

type mutationCopyArgs struct {
	Scope  string `json:"scope"`
	ToPath string `json:"to_path"`
}

// MutationTrackingForCall resolves only paths explicitly owned by a structured
// write. Foreground commands intentionally use the complete authorized project
// scope because their mutations cannot be known before execution.
func MutationTrackingForCall(call Call) (ProjectMutationTracking, bool) {
	switch call.Name {
	case CommandRun:
		args, err := decodeCommandRunArgs(call.Args)
		if err != nil || args.Background {
			return ProjectMutationTracking{}, false
		}
		return ProjectMutationTracking{Origin: store.FileChangeOriginCommandObserved}, true
	case FilePatch:
		args, argumentErr := decodeFilePatchArgs(call.Args)
		if argumentErr != nil || strings.TrimSpace(args.Scope) != managedScopeProject || len(args.Files) == 0 {
			return ProjectMutationTracking{}, false
		}
		targets := make([]string, 0, len(args.Files))
		for _, file := range args.Files {
			target, ok := resolveMutationTarget(call.ProjectDirs, file.Path, true)
			if !ok {
				return ProjectMutationTracking{}, false
			}
			targets = append(targets, target)
		}
		return structuredMutationTracking(targets)
	case FileWrite, FileDelete, AttachmentExport:
		var args mutationPathArgs
		if !decodeProjectMutationArgs(call.Args, &args) {
			return ProjectMutationTracking{}, false
		}
		target, ok := resolveMutationTarget(call.ProjectDirs, args.Path, true)
		if !ok {
			return ProjectMutationTracking{}, false
		}
		return structuredMutationTracking([]string{target})
	case FileMove:
		var args mutationMoveArgs
		if !decodeProjectMutationArgs(call.Args, &args) {
			return ProjectMutationTracking{}, false
		}
		from, fromOK := resolveMutationTarget(call.ProjectDirs, args.FromPath, false)
		to, toOK := resolveMutationTarget(call.ProjectDirs, args.ToPath, true)
		if !fromOK || !toOK {
			return ProjectMutationTracking{}, false
		}
		return structuredMutationTracking([]string{from, to})
	case FileCopy:
		var args mutationCopyArgs
		if !decodeProjectMutationArgs(call.Args, &args) {
			return ProjectMutationTracking{}, false
		}
		target, ok := resolveMutationTarget(call.ProjectDirs, args.ToPath, true)
		if !ok {
			return ProjectMutationTracking{}, false
		}
		return structuredMutationTracking([]string{target})
	default:
		return ProjectMutationTracking{}, false
	}
}

func structuredMutationTracking(targets []string) (ProjectMutationTracking, bool) {
	if len(targets) == 0 {
		return ProjectMutationTracking{}, false
	}
	return ProjectMutationTracking{
		Targets: targets,
		Origin:  store.FileChangeOriginStructured,
	}, true
}

func decodeProjectMutationArgs(raw json.RawMessage, dst any) bool {
	if len(raw) == 0 || json.Unmarshal(raw, dst) != nil {
		return false
	}
	scope, ok := mutationScope(dst)
	return ok && strings.TrimSpace(scope) == managedScopeProject
}

func mutationScope(value any) (string, bool) {
	switch args := value.(type) {
	case *mutationPathArgs:
		return args.Scope, true
	case *mutationMoveArgs:
		return args.Scope, true
	case *mutationCopyArgs:
		return args.Scope, true
	default:
		return "", false
	}
}

func resolveMutationTarget(roots []string, path string, allowMissing bool) (string, bool) {
	_, target, _, err := resolveProjectPath(roots, path, true, allowMissing)
	return target, err == nil
}
