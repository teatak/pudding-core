package gitrename

import (
	"reflect"
	"testing"
)

func TestChangedPaths(t *testing.T) {
	raw := " M README.md\nR  old.txt -> docs/new.txt\nA  README.md\n?? scratch.txt\ninvalid\n"
	want := []string{"README.md", "docs/new.txt", "scratch.txt"}
	if got := ChangedPaths(raw); !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedPaths = %v, want %v", got, want)
	}
}
