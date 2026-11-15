package tool

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGitStageRetainsBuiltInNormalization(t *testing.T) {
	for _, tc := range []struct {
		name, attributes, autocrlf, want string
	}{
		{"attributes-lf", "*.txt text eol=lf\n", "false", "alpha\nbeta\n"},
		{"attributes-crlf", "*.txt text eol=crlf\n", "false", "alpha\nbeta\n"},
		{"autocrlf", "", "true", "alpha\nbeta\n"},
		{"binary-attribute", "*.txt -text\n", "true", "alpha\r\nbeta\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newGitTestRepository(t, false)
			runGitTest(t, root, "config", "core.autocrlf", tc.autocrlf)
			runGitTest(t, root, "config", "core.safecrlf", "false")
			writePatchTestFile(t, filepath.Join(root, ".gitattributes"), tc.attributes)
			writePatchTestFile(t, filepath.Join(root, "file.txt"), "alpha\r\nbeta\r\n")
			result := gitTestCall(root, GitStage, map[string]any{"scope": "project", "paths": []string{"file.txt"}})
			got := decodeGitPayload[gitWritePayload](t, result)
			if !got.OK || got.StagedCount != 1 || got.UnstagedCount != 0 {
				t.Fatalf("staging left a false worktree modification: %s", result.Content)
			}
			blob := runGit(context.Background(), root, 1024, "cat-file", "blob", ":file.txt")
			if blob.err != nil || blob.stdout.String() != tc.want {
				t.Fatalf("index bytes=%q err=%v, want %q", blob.stdout.String(), blob.err, tc.want)
			}
			if got := readPatchTestFile(t, filepath.Join(root, "file.txt")); got != "alpha\r\nbeta\r\n" {
				t.Fatalf("staging modified worktree bytes: %q", got)
			}
		})
	}
}

func TestGitStageNormalizesWithoutExternalFilters(t *testing.T) {
	root := newGitTestRepository(t, false)
	runGitTest(t, root, "config", "core.autocrlf", "false")
	runGitTest(t, root, "config", "core.safecrlf", "false")
	runGitTest(t, root, "config", "filter.pudding.clean", "touch clean-ran; cat")
	runGitTest(t, root, "config", "filter.pudding.process", "touch process-ran; exit 1")
	runGitTest(t, root, "config", "filter.pudding.required", "true")
	writePatchTestFile(t, filepath.Join(root, ".gitattributes"), "*.txt text eol=lf filter=pudding\n")
	writePatchTestFile(t, filepath.Join(root, "file.txt"), "safe\r\ncontent\r\n")
	result := gitTestCall(root, GitStage, map[string]any{"scope": "project", "paths": []string{"file.txt"}})
	if !result.Ok {
		t.Fatalf("disabled required filter prevented staging: %s", result.Content)
	}
	blob := runGit(context.Background(), root, 1024, "cat-file", "blob", ":file.txt")
	if blob.err != nil || blob.stdout.String() != "safe\ncontent\n" {
		t.Fatalf("built-in conversion was lost: %q %v", blob.stdout.String(), blob.err)
	}
	for _, name := range []string{"clean-ran", "process-ran"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("untrusted filter ran: %s %v", name, err)
		}
	}
}

func TestGitStagePreservesLiteralSymlinkTarget(t *testing.T) {
	root := newGitTestRepository(t, false)
	runGitTest(t, root, "config", "core.autocrlf", "true")
	writePatchTestFile(t, filepath.Join(root, ".gitattributes"), "* text eol=lf\n")
	linkText := "target\r\nname"
	if err := os.Symlink(linkText, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	result := gitTestCall(root, GitStage, map[string]any{"scope": "project", "paths": []string{"link"}})
	if !result.Ok {
		t.Fatalf("symlink stage failed: %s", result.Content)
	}
	blob := runGit(context.Background(), root, 1024, "cat-file", "blob", ":link")
	if blob.err != nil || blob.stdout.String() != linkText {
		t.Fatalf("symlink target was normalized: %q %v", blob.stdout.String(), blob.err)
	}
}
