package voice

import (
	"reflect"
	"testing"
)

func TestSentenceSplitterEmitsCompleteSentences(t *testing.T) {
	s := NewSentenceSplitter(0)
	if got := s.Push("Hello"); len(got) != 0 {
		t.Fatalf("Push emitted early: %+v", got)
	}
	if got, want := s.Push(" world. Next"), []string{"Hello world."}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Push = %+v, want %+v", got, want)
	}
	if got, want := s.Flush(), []string{"Next"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Flush = %+v, want %+v", got, want)
	}
}

func TestSentenceSplitterHandlesChineseTerminators(t *testing.T) {
	s := NewSentenceSplitter(0)
	if got := s.Push("你好"); len(got) != 0 {
		t.Fatalf("Push emitted early: %+v", got)
	}
	if got, want := s.Push("呀！还有吗"), []string{"你好呀！"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Push = %+v, want %+v", got, want)
	}
	if got, want := s.Flush(), []string{"还有吗"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Flush = %+v, want %+v", got, want)
	}
}

func TestSentenceSplitterSplitsLongSegments(t *testing.T) {
	s := NewSentenceSplitter(12)
	got := s.Push("one two three four")
	if len(got) != 1 || got[0] != "one two" {
		t.Fatalf("Push = %+v, want [one two]", got)
	}
	if tail := s.Flush(); len(tail) != 1 || tail[0] != "three four" {
		t.Fatalf("Flush = %+v, want [three four]", tail)
	}
}
