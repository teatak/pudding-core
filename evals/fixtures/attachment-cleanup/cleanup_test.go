package attachmentcleanup

import (
	"reflect"
	"testing"
)

func TestReferencedKeysIncludesQueuedAndIsStable(t *testing.T) {
	messages := []Input{{AttachmentKeys: []string{"b.png", "", "a.pdf"}}, {AttachmentKeys: []string{"b.png"}}}
	queued := []Input{{AttachmentKeys: []string{"c.txt", "a.pdf"}}}
	want := []string{"a.pdf", "b.png", "c.txt"}
	if got := ReferencedKeys(messages, queued); !reflect.DeepEqual(got, want) {
		t.Fatalf("ReferencedKeys = %v, want %v", got, want)
	}
}
