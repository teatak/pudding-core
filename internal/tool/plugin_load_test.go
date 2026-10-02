package tool

import (
	"encoding/json"
	"testing"
)

func TestDecodeAppLoadRequest(t *testing.T) {
	request, err := DecodeAppLoadRequest(json.RawMessage(`{"app_id":" canvas ","skill_id":" canvas "}`))
	if err != nil {
		t.Fatal(err)
	}
	if request.AppID != "canvas" || request.SkillID != "canvas" {
		t.Fatalf("request = %+v", request)
	}
	if _, err := DecodeAppLoadRequest(json.RawMessage(`{"skill_id":"canvas"}`)); err == nil {
		t.Fatal("missing app_id should fail")
	}
}
