package pagination

import (
	"reflect"
	"testing"
)

func TestPageUsesOneBasedNumbers(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	for _, test := range []struct {
		page int
		size int
		want []int
	}{
		{page: 1, size: 2, want: []int{1, 2}},
		{page: 2, size: 2, want: []int{3, 4}},
		{page: 3, size: 2, want: []int{5}},
		{page: 0, size: 2, want: nil},
		{page: 1, size: 0, want: nil},
	} {
		if got := Page(items, test.page, test.size); !reflect.DeepEqual(got, test.want) {
			t.Fatalf("Page(%d, %d) = %v, want %v", test.page, test.size, got, test.want)
		}
	}
}
