package toolmodel

import (
	"reflect"
	"testing"
)

func TestSelectForCode(t *testing.T) {
	models := []Model{{ID: "chat", Tools: false}, {ID: "code", Tools: true}, {ID: "later", Tools: true}}
	before := append([]Model(nil), models...)
	got, ok := SelectForCode(models)
	if !ok || got.ID != "code" {
		t.Fatalf("SelectForCode = %+v, %v", got, ok)
	}
	if !reflect.DeepEqual(models, before) {
		t.Fatalf("input reordered: %+v", models)
	}
	if _, ok := SelectForCode([]Model{{ID: "chat"}}); ok {
		t.Fatal("model without tools should not be selected")
	}
}
