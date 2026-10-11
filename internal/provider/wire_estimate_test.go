package provider

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"
)

func TestWireEstimateDecodesEscapesAndMedia(t *testing.T) {
	text := strings.Repeat("\n", 1000)
	got, err := (WireInput{Messages: []any{map[string]any{"text": text}}}).Estimate()
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageTokens != EstimateTextTokens(text)+EstimateTextTokens("text")+4 {
		t.Fatal("JSON escapes inflated input", got)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1600, 900))); err != nil {
		t.Fatal(err)
	}
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
	got, err = (WireInput{Messages: []any{map[string]any{"type": "input_image", "image_url": url}}}).Estimate()
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageTokens != 1411 {
		t.Fatalf("image should use dimensions instead of base64: %+v", got)
	}
}
