package store

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestProjectDirectoryNormalizationPreservesPathWhitespace(t *testing.T) {
	root := filepath.Join(t.TempDir(), " project ")
	neighbor := root[:len(root)-1]
	want := []string{root, neighbor}
	got := NormalizeProjectDirs([]string{"", " \t", "relative", " " + root, root, neighbor, root + string(filepath.Separator)})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("directory identity changed: got %q want %q", got, want)
	}
	if SameProjectDirs([]string{root}, []string{neighbor}) {
		t.Fatal("different directory names were treated as the same authority")
	}
	project := &Project{ID: "project", RootDirs: []string{root}}
	if err := NormalizeProject(project); err != nil || !reflect.DeepEqual(project.RootDirs, []string{root}) {
		t.Fatalf("project normalization changed the root: %+v %v", project, err)
	}
	update := ProjectUpdate{RootDirs: &want}
	if err := NormalizeProjectUpdate(&update); err != nil || !reflect.DeepEqual(*update.RootDirs, want) {
		t.Fatalf("project update changed the roots: %+v %v", update, err)
	}
}
