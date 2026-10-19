package store

import (
	"html"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const PageNameAuto = "auto"
const PageNameManual = "manual"
const PageNameFallback = "Untitled page"

// PageAutomaticName is a projection of the body, never a synthetic body block.
// Prefer the first top-level H1; otherwise use the first visible paragraph.
// Frontmatter, code, HTML markup and link destinations are not titles.
func PageAutomaticName(body string) string {
	body = strings.TrimPrefix(body, "\ufeff")
	source := []byte(strings.TrimPrefix(body, pageFrontmatter.FindString(body)))
	root := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(text.NewReader(source))
	for n := root.FirstChild(); n != nil; n = n.NextSibling() {
		if h, ok := n.(*ast.Heading); ok && h.Level == 1 {
			if name := pageNameText(n, source, 80); name != "" {
				return name
			}
		}
	}
	var name string
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && (n.Kind() == ast.KindParagraph || n.Kind() == ast.KindTextBlock) {
			name = pageNameText(n, source, 40)
			if name != "" {
				return ast.WalkStop, nil
			}
		}
		// Studio treats indentation as literal text, not a fenced code block.
		if entering && n.Kind() == ast.KindCodeBlock {
			name = pageNameValue(string(n.Lines().Value(source)), 40)
			if name != "" {
				return ast.WalkStop, nil
			}
		}
		return ast.WalkContinue, nil
	})
	return name
}

func pageNameText(node ast.Node, source []byte, limit int) string {
	var b strings.Builder
	_ = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Text:
			value := n.Segment.Value(source)
			if n.Parent().Kind() != ast.KindCodeSpan {
				value = util.UnescapePunctuations(value)
				b.WriteString(html.UnescapeString(string(value)))
			} else {
				b.Write(value)
			}
			if n.SoftLineBreak() || n.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(n.Value)
		}
		return ast.WalkContinue, nil
	})
	return pageNameValue(b.String(), limit)
}

func pageNameValue(raw string, limit int) string {
	value := strings.Join(strings.FieldsFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	runes := []rune(value)
	end, bytes := 0, 0
	for end < len(runes) && end < limit && bytes+len(string(runes[end])) <= 197 {
		bytes += len(string(runes[end]))
		end++
	}
	if end < len(runes) {
		return strings.TrimSpace(string(runes[:end])) + "…"
	}
	return value
}
