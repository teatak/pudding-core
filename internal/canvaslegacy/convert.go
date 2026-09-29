// Package canvaslegacy converts stored structured content to ordinary editable
// source packages at upgrade time. It is not a second canvas runtime.
package canvaslegacy

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"github.com/teatak/pudding-core/internal/canvas"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

//go:embed templates/style.css
var stylesheet string

//go:embed templates/Table.tsx
var tableSource string

//go:embed templates/Chart.tsx
var chartSource string

// Convert preserves content as source, with no requests or executable legacy HTML.
// Asset resolution is explicit so migration tests never use the network.
func Convert(content store.CanvasContent, image func(string) (string, error)) (canvas.Package, error) {
	var item map[string]any
	if err := json.Unmarshal(content.Item, &item); err != nil {
		return canvas.Package{}, err
	}
	g := generator{files: map[string]string{"canvas.json": `{"schemaVersion":1,"sdkVersion":"1","entry":"src/App.tsx","sources":{},"operations":{}}`, "src/style.css": stylesheet, "src/styles.d.ts": "declare module '*.css';\n"}, image: image}
	body, err := g.render(content.Kind, item, 0)
	if err != nil {
		return canvas.Package{}, err
	}
	imports := `/// <reference path="./styles.d.ts" />
import "./style.css";` + "\n"
	if g.files["src/Table.tsx"] != "" {
		imports += "import { DataTable } from './Table';\n"
	}
	if g.files["src/Chart.tsx"] != "" {
		imports += "import { Chart } from './Chart';\n"
	}
	g.files["src/App.tsx"] = imports + "\nexport default function App() {\n  return <main className=\"canvas-content\">\n<h1>" + expr(content.Title) + "</h1>\n" + body + "\n</main>;\n}\n"
	p := canvas.Package{Files: g.files}
	_, _, err = p.Validate()
	return p, err
}

type generator struct {
	files map[string]string
	image func(string) (string, error)
}

func jsonValue(v any) string { b, _ := json.Marshal(v); return string(b) }
func expr(v any) string      { return "{" + jsonValue(v) + "}" }
func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return jsonValue(v)
}
func first(v ...any) string {
	for _, x := range v {
		if s := str(x); s != "" {
			return s
		}
	}
	return ""
}
func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }
func arr(v any) []any          { a, _ := v.([]any); return a }
func number(v any, fallback int) int {
	if n, ok := v.(float64); ok {
		return int(n)
	}
	return fallback
}
func paragraph(v any) string {
	if str(v) == "" {
		return ""
	}
	return "<p>" + expr(str(v)) + "</p>\n"
}
func link(label, url string) string {
	if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") {
		return "<a href=" + expr(url) + " target=\"_blank\" rel=\"noreferrer\">" + expr(label) + "</a>"
	}
	return expr(label)
}

func (g *generator) render(kind string, p map[string]any, depth int) (string, error) {
	if depth > 16 {
		return "", fmt.Errorf("canvas nesting exceeds 16")
	}
	var out strings.Builder
	switch kind {
	case "markdown":
		source := []byte(first(p["content"], p["markdown"], p["text"]))
		md := goldmark.New(goldmark.WithExtensions(extension.GFM))
		doc := md.Parser().Parse(text.NewReader(source))
		images := map[string]string{}
		err := ast.Walk(doc, func(n ast.Node, enter bool) (ast.WalkStatus, error) {
			if img, ok := n.(*ast.Image); ok && enter {
				dest, err := g.image(string(img.Destination))
				if err != nil {
					return ast.WalkStop, err
				}
				placeholder := fmt.Sprintf("pudding-image-%d", len(images))
				images[placeholder] = dest
				img.Destination = []byte(placeholder)
			}
			return ast.WalkContinue, nil
		})
		if err != nil {
			return "", err
		}
		var html bytes.Buffer
		if err = md.Renderer().Render(&html, source, doc); err != nil {
			return "", err
		}
		out.WriteString("<article className=\"prose-content\" dangerouslySetInnerHTML={{__html:" + jsonValue(replaceImages(html.String(), images)) + "}} />\n")
	case "table":
		g.files["src/Table.tsx"] = tableSource
		out.WriteString("<DataTable data=" + expr(p) + " />\n")
	case "chart":
		g.files["src/Chart.tsx"] = chartSource
		chart := p
		if nested := obj(p["chart"]); nested != nil {
			chart = nested
		}
		out.WriteString("<Chart spec=" + expr(chart) + " />\n")
	case "gallery":
		layout := first(p["layout"], "grid")
		out.WriteString("<div className=" + expr("gallery gallery-"+layout) + ">\n")
		for _, v := range arr(p["items"]) {
			im := obj(v)
			src := first(im["src"], im["url"])
			if src == "" && str(im["data"]) != "" {
				src = "data:" + first(im["mime"], "image/jpeg") + ";base64," + str(im["data"])
			}
			if src == "" {
				return "", fmt.Errorf("gallery image has no source")
			}
			resolved, err := g.image(src)
			if err != nil {
				return "", err
			}
			out.WriteString("<figure><img src=" + expr(resolved) + " alt=" + expr(first(im["alt"], im["caption"])) + " />")
			if str(im["caption"]) != "" {
				out.WriteString("<figcaption>" + expr(str(im["caption"])) + "</figcaption>")
			}
			out.WriteString("</figure>\n")
		}
		out.WriteString("</div>\n")
	case "timeline":
		groups := []string{}
		entries := map[string][]map[string]any{}
		for _, v := range arr(p["items"]) {
			e := obj(v)
			key := first(e["group"], e["date"])
			if _, ok := entries[key]; !ok {
				groups = append(groups, key)
			}
			entries[key] = append(entries[key], e)
		}
		for _, group := range groups {
			out.WriteString("<section className=\"timeline\"><h2>" + expr(group) + "</h2><ol>\n")
			for _, e := range entries[group] {
				out.WriteString("<li><small>" + expr(str(e["time"])) + "</small><h3>" + link(str(e["title"]), str(e["link"])) + "</h3>")
				if str(e["status"]) != "" {
					out.WriteString("<span className=\"badge\">" + expr(str(e["status"])) + "</span>")
				}
				out.WriteString(paragraph(e["description"]) + paragraph(e["meta"]) + "</li>\n")
			}
			out.WriteString("</ol></section>\n")
		}
	case "grid":
		gap := max(4, min(32, number(obj(p["layout"])["gap"], 12)))
		out.WriteString(fmt.Sprintf("<div className=\"content-grid\" style={{gap:%d}}>\n", gap))
		for _, v := range arr(p["items"]) {
			child := obj(v)
			k := first(child["kind"], "markdown")
			span := 12
			if depth == 0 && (k == "metric" || k == "chart") {
				span = 6
			}
			switch str(p["columns"]) {
			case "1":
				span = 12
			case "2":
				span = 6
			case "3":
				span = 4
			}
			for _, breakpoint := range []string{"xs", "sm", "md", "lg"} {
				span = number(obj(child["span"])[breakpoint], span)
			}
			body, err := g.render(k, child, depth+1)
			if err != nil {
				return "", err
			}
			out.WriteString(fmt.Sprintf("<section className=\"content-card span-%d\">\n", max(1, min(12, span))))
			if k != "metric" && str(child["title"]) != "" {
				out.WriteString("<h2>" + expr(str(child["title"])) + "</h2>\n")
			}
			out.WriteString(body + "</section>\n")
		}
		out.WriteString("</div>\n")
	case "metric":
		out.WriteString("<div className=\"metric\"><h2>" + expr(str(p["title"])) + "</h2><strong>" + expr(str(p["value"])) + "</strong>" + paragraph(p["description"]) + "</div>\n")
	case "form":
		for _, v := range arr(p["fields"]) {
			f := obj(v)
			out.WriteString("<label className=\"field\">" + expr(first(f["label"], f["name"])) + "<input readOnly value=" + expr(str(f["default_value"])) + " /></label>\n")
		}
	default:
		return "", fmt.Errorf("unsupported legacy canvas kind %q", kind)
	}
	out.WriteString(paragraph(p["caption"]))
	return out.String(), nil
}

func replaceImages(markup string, images map[string]string) string {
	for key, dest := range images {
		markup = strings.ReplaceAll(markup, `src="`+key+`"`, `src="`+html.EscapeString(dest)+`"`)
	}
	return markup
}
