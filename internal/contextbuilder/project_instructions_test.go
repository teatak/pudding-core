package contextbuilder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProjectRootInstructions(t *testing.T) {
	root := t.TempDir()
	writeProjectInstructionTestFile(t, root, "CONTRIBUTING.md", "root contributing\n")
	writeProjectInstructionTestFile(t, root, "AGENTS.md", "root agents\n")
	writeProjectInstructionTestFile(t, root, "web/AGENTS.md", "nested agents\n")

	instructions := loadProjectRootInstructions([]string{root})
	if len(instructions) != 1 {
		t.Fatalf("unexpected root instruction count: %+v", instructions)
	}
	instruction := instructions[0]
	if instruction.path != "AGENTS.md" || instruction.projectRoot != root || instruction.content != "root agents\n" {
		t.Fatalf("unexpected instruction: %+v", instruction)
	}
}

func TestLoadProjectRootInstructionsTruncatesLargeContent(t *testing.T) {
	root := t.TempDir()
	writeProjectInstructionTestFile(t, root, "AGENTS.md", strings.Repeat("instruction line\n", projectInstructionMaxFileBytes/8))
	instructions := loadProjectRootInstructions([]string{root})
	if len(instructions) != 1 || !instructions[0].truncated {
		t.Fatalf("large instruction must be truncated: %+v", instructions)
	}
	if len([]byte(instructions[0].content)) > projectInstructionMaxFileBytes {
		t.Fatalf("instruction exceeded content limit: %d", len([]byte(instructions[0].content)))
	}
}

func TestLoadProjectRootInstructionsSkipsBinaryAndSymlink(t *testing.T) {
	binaryRoot := t.TempDir()
	writeProjectInstructionTestFile(t, binaryRoot, "AGENTS.md", "text\x00binary")
	targetRoot := t.TempDir()
	writeProjectInstructionTestFile(t, targetRoot, "real.md", "follow me\n")
	symlinkRoot := t.TempDir()
	if err := os.Symlink(filepath.Join(targetRoot, "real.md"), filepath.Join(symlinkRoot, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	if instructions := loadProjectRootInstructions([]string{binaryRoot, symlinkRoot}); len(instructions) != 0 {
		t.Fatalf("unsafe instructions should be skipped: %+v", instructions)
	}
}

func writeProjectInstructionTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
