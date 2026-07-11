package sherpa

import (
	"reflect"
	"testing"
)

func TestSampleRingSliceAndTail(t *testing.T) {
	ring := newSampleRing(5)
	ring.Append([]float32{1, 2, 3})
	if got := ring.Tail(2); !reflect.DeepEqual(got, []float32{2, 3}) {
		t.Fatalf("tail before wrap = %v", got)
	}

	ring.Append([]float32{4, 5, 6, 7})
	if got := ring.Slice(2, 5); !reflect.DeepEqual(got, []float32{3, 4, 5}) {
		t.Fatalf("slice after wrap = %v", got)
	}
	if got := ring.Slice(0, 3); !reflect.DeepEqual(got, []float32{3}) {
		t.Fatalf("clamped slice = %v", got)
	}
	if got := ring.Tail(3); !reflect.DeepEqual(got, []float32{5, 6, 7}) {
		t.Fatalf("tail after wrap = %v", got)
	}
}

func TestSampleRingResetStartsANewTimeline(t *testing.T) {
	ring := newSampleRing(5)
	ring.Append([]float32{1, 2, 3})
	ring.Reset()
	if got := ring.Tail(5); got != nil {
		t.Fatalf("tail after reset = %v, want nil", got)
	}
	ring.Append([]float32{4, 5})
	if got := ring.Slice(0, 2); !reflect.DeepEqual(got, []float32{4, 5}) {
		t.Fatalf("slice after reset = %v", got)
	}
}
