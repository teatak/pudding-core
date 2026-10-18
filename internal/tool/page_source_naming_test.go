package tool

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Check identifiers, not literals: old wire names and frozen SQL are required
// compatibility inputs, while production Go has one canonical Page vocabulary.
func TestPageSourceUsesCanonicalIdentifiers(t *testing.T) {
	root := filepath.Join("..", "..", "internal")
	obsolete := regexp.MustCompile(`(^|[a-z])Note($|[A-Z])|^note[A-Z]`)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.Contains(entry.Name(), "note") {
			t.Errorf("obsolete Page source path: %s", path)
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if id, ok := node.(*ast.Ident); ok && obsolete.MatchString(id.Name) {
				t.Errorf("obsolete Studio identifier %s in %s", id.Name, path)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
