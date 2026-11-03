package contracts

import (
	"encoding/json"
	"testing"
)

func TestIconStringAndObjectRoundTrips(t *testing.T) {
	for _, raw := range []string{`"star"`, `"data:image/svg+xml;base64,PHN2Zy8+"`, `{"svg":"data:image/svg+xml;base64,PHN2Zy8+"}`, `{"svg":"assets/icon.svg","color":{"dark":"#fff"},"background":{"light":"rgb(1 2 3 / 50%)"}}`} {
		var icon Icon
		if err := json.Unmarshal([]byte(raw), &icon); err != nil {
			t.Fatal(raw, err)
		}
		encoded, err := json.Marshal(icon)
		if err != nil {
			t.Fatal(err)
		}
		var before, after any
		json.Unmarshal([]byte(raw), &before)
		json.Unmarshal(encoded, &after)
		a, _ := json.Marshal(before)
		b, _ := json.Marshal(after)
		if string(a) != string(b) {
			t.Fatalf("round trip: %s -> %s", raw, encoded)
		}
	}
}
func TestIconRejectsInvalidObjectsAndColors(t *testing.T) {
	for _, raw := range []string{`{}`, `{"svg":"a.svg","unknown":true}`, `{"svg":"a.svg","color":{"light":"url(https://bad.test)"}}`, `{"svg":"a.svg","background":{"light":"red; display:none"}}`, `{"svg":4}`, `"{\"svg\":\"a.svg\"}"`} {
		var icon Icon
		if err := json.Unmarshal([]byte(raw), &icon); err == nil {
			t.Fatal("accepted invalid icon", raw)
		}
	}
	for _, color := range []string{"#fff", "#ffff", "#FFFFFF", "#12345678", "transparent", "currentColor", "rgba(1,2,3,.5)", "hsl(0 50% 50%)", "HSL(30deg 50% 50%)", "oklch(.7 .2 30)", "color(display-p3 1 0 .4)"} {
		if err := ValidateIconColors(IconSpec{Color: &IconThemeColor{Light: color}}); err != nil {
			t.Fatal(color, err)
		}
	}
}
