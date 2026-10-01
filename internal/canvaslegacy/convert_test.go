package canvaslegacy

import (
	"encoding/json"
	"errors"
	"github.com/teatak/pudding-core/internal/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertContent(t *testing.T) {
	for _, tc := range []struct{ kind, item, want string }{
		{"markdown", `{"content":"# Heading\n\n**Bold** [safe](https://example.com)\n\n<script>alert(1)</script>"}`, "Heading"},
		{"table", `{"columns":[{"key":"price","currency":"¥","divide":100}],"rows":[{"price":1250}]}`, "1250"},
		{"chart", `{"chart":{"type":"donut","data":[{"name":"A","value":3}]}}`, "donut"},
		{"gallery", `{"items":[{"data":"PHN2Zy8+","mime":"image/svg+xml","caption":"Image"}]}`, "data:image/svg+xml"},
		{"timeline", `{"items":[{"title":"Launch","date":"2026-09-29","status":"done"}]}`, "Launch"},
		{"grid", `{"items":[{"kind":"metric","value":42},{"kind":"grid","items":[{"kind":"markdown","content":"Nested"}]}]}`, "Nested"},
		{"form", `{"fields":[{"label":"Name","default_value":"A"}]}`, "readOnly"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			c := store.CanvasContent{Kind: tc.kind, Title: "Title", Item: json.RawMessage(tc.item)}
			p, err := Convert(c, Images(t.TempDir()))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(p.Files["src/App.tsx"], tc.want) {
				t.Fatal(p.Files["src/App.tsx"])
			}
			if tc.kind == "markdown" && strings.Contains(p.Files["src/App.tsx"], "alert(1)") {
				t.Fatal("raw HTML retained executable content")
			}
			_, hash, _ := p.Validate()
			again, err := Convert(c, Images(t.TempDir()))
			if err != nil {
				t.Fatal(err)
			}
			_, hash2, _ := again.Validate()
			if hash != hash2 {
				t.Fatal("conversion is not deterministic")
			}
		})
	}
}
func TestConversionErrorsAbort(t *testing.T) {
	for _, c := range []store.CanvasContent{{Kind: "unknown", Item: json.RawMessage(`{}`)}, {Kind: "table", Item: json.RawMessage(`invalid`)}} {
		if _, err := Convert(c, func(string) (string, error) { return "", errors.New("missing image") }); err == nil {
			t.Fatal("expected conversion failure", c)
		}
	}
}
func TestUnavailableImagesBecomePlaceholders(t *testing.T) {
	missing := func(string) (string, error) { return "", errors.New("missing image") }
	for _, c := range []store.CanvasContent{
		{Kind: "gallery", Item: json.RawMessage(`{"items":[{"src":"/sessions/deleted/attachments/blobs/photo.jpg","caption":"Kept caption"}]}`)},
		{Kind: "gallery", Item: json.RawMessage(`{"items":[{"caption":"Kept caption"}]}`)},
		{Kind: "markdown", Item: json.RawMessage(`{"content":"![Kept caption](https://example.com/gone.png) and text"}`)},
	} {
		p, err := Convert(c, missing)
		if err != nil {
			t.Fatal(c, err)
		}
		if app := p.Files["src/App.tsx"]; !strings.Contains(app, missingImage) || !strings.Contains(app, "Kept caption") {
			t.Fatal(app)
		}
	}
}
func TestImagesEmbedAttachmentsAndBlockUnsafeTargets(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "attachments", "sessions", "s")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "image.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 0600); err != nil {
		t.Fatal(err)
	}
	resolve := Images(home)
	image, err := resolve("/sessions/s/attachments/image.svg")
	if err != nil || !strings.HasPrefix(image, "data:image/svg+xml;base64,") {
		t.Fatal(image, err)
	}
	for _, ref := range []string{"http://127.0.0.1/image", "http://[::1]/image", "javascript:alert(1)", "/sessions/s/attachments/../../../../secret"} {
		if _, err := resolve(ref); err == nil {
			t.Fatal("unsafe image accepted", ref)
		}
	}
	p, err := Convert(store.CanvasContent{Kind: "markdown", Item: json.RawMessage(`{"content":"![image](/sessions/s/attachments/image.svg)"}`)}, resolve)
	if err != nil || !strings.Contains(p.Files["src/App.tsx"], "data:image/svg+xml") {
		t.Fatal(p, err)
	}
}
