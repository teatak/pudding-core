package resample

import "testing"

func TestLinearCopiesWhenRatesMatch(t *testing.T) {
	r := NewLinear(16000, 16000)
	src := []byte{1, 0, 2, 0}
	got := r.Process(src)
	if string(got) != string(src) {
		t.Fatalf("got %v", got)
	}
	got[0] = 9
	if src[0] != 1 {
		t.Fatal("output aliases input")
	}
}

func TestLinearResamplesAcrossChunks(t *testing.T) {
	r := NewLinear(24000, 16000)
	first := r.Process([]byte{0, 0, 10, 0, 20, 0})
	second := r.Process([]byte{30, 0, 40, 0, 50, 0})
	if len(first) == 0 || len(second) == 0 {
		t.Fatalf("expected output from both chunks: first=%v second=%v", first, second)
	}
}
