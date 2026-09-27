package tool

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileCopyCommitFailureRestoresDestination(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[directory], func(t *testing.T) {
			root := t.TempDir()
			target, backup := filepath.Join(root, "target"), filepath.Join(root, "backup")
			original := target
			if directory {
				original = filepath.Join(target, "nested", "file")
			}
			writeCopyFixture(t, original, "original")
			// The staging path disappearing forces the final rename to fail
			// after the existing target has been moved to the backup.
			if err := installFileCopy(nil, filepath.Join(root, "missing-stage"), target, backup, true); err == nil {
				t.Fatal("missing staged copy committed")
			}
			data, err := os.ReadFile(original)
			if err != nil || string(data) != "original" {
				t.Fatalf("rollback lost original: %v %q", err, data)
			}
			if _, err := os.Lstat(backup); !os.IsNotExist(err) {
				t.Fatal("successful rollback left a backup")
			}
		})
	}
}

func TestFileCopyRootedCommitFailureRestoresDirectory(t *testing.T) {
	dir := t.TempDir()
	writeCopyFixture(t, filepath.Join(dir, "target", "nested", "file"), "original")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := installFileCopy(root, "missing-stage", "target", "backup", true); err == nil {
		t.Fatal("missing staged export committed")
	}
	data, err := os.ReadFile(filepath.Join(dir, "target", "nested", "file"))
	if err != nil || string(data) != "original" {
		t.Fatalf("rollback lost original directory: %q %v", data, err)
	}
	if _, err := root.Lstat("backup"); !os.IsNotExist(err) {
		t.Fatalf("successful rollback retained backup: %v", err)
	}
}

func TestFileCopyStagedOverwriteRechecksDestination(t *testing.T) {
	root, external := t.TempDir(), t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(external, "target")
	writeCopyFixture(t, source, "new")
	writeCopyFixture(t, filepath.Join(target, "nested", "file"), "original")
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
	call := overwriteCopyCall(copyPathCall(source, target, root), false)
	details, err := runner.ApprovalDetails(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	call.FileCopyGrant = FileCopyPlanFromDetails(details)
	writeCopyFixture(t, filepath.Join(target, "nested", "file"), "edited before commit")
	args, err := decodeFileCopyArgs(call.Args)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the commit-time recheck independently of the pre-copy check.
	if _, err := runner.applyFileCopy(call, args, call.FileCopyGrant); err == nil {
		t.Fatal("staged copy replaced edited destination")
	}
	data, err := os.ReadFile(filepath.Join(target, "nested", "file"))
	if err != nil || string(data) != "edited before commit" {
		t.Fatalf("edited file lost: %v %q", err, data)
	}
	entries, err := os.ReadDir(external)
	if err != nil || len(entries) != 1 {
		t.Fatalf("staging remains: %v %+v", err, entries)
	}
}

func TestFileCopyOverwriteReplacesDifferentTypes(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{false: "file-over-directory", true: "directory-over-file"}[directory], func(t *testing.T) {
			root := t.TempDir()
			source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
			fromFile, toFile := source, target
			if directory {
				fromFile = filepath.Join(source, "nested", "file")
			} else {
				toFile = filepath.Join(target, "nested", "old")
			}
			writeCopyFixture(t, fromFile, "new")
			writeCopyFixture(t, toFile, "old")
			runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
			call := overwriteCopyCall(copyPathCall(source, target, root), directory)
			if result := runner.Call(context.Background(), call); !result.Ok {
				t.Fatal(result.Content)
			}
			newFile := target
			if directory {
				newFile = filepath.Join(target, "nested", "file")
			}
			data, err := os.ReadFile(newFile)
			if err != nil || string(data) != "new" {
				t.Fatalf("wrong replacement: %v %q", err, data)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 2 {
				t.Fatalf("successful replacement retained staging/backup: %v %+v", err, entries)
			}
		})
	}
}
