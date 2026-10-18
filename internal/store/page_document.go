package store

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const PageDocumentFormat = "page-blocks-v1"

// PageDocument is the authoritative, ordered block snapshot. Markdown belongs
// to individual blocks, including their original separators; body is a derived
// compatibility/export view. Keeping source syntax here avoids lossy rewrites
// of code, reference links, unknown extensions and imported whitespace.
type PageDocument struct {
	SchemaVersion int         `json:"schemaVersion"`
	Generation    int64       `json:"generation"`
	Prefix        string      `json:"prefix"`
	Blocks        []PageBlock `json:"blocks"`
}
type PageBlock struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Version  int64  `json:"version"`
	Markdown string `json:"markdown"`
}

// Identity hints come from the editor's mapped nodes, never from text matching.
// Offsets are UTF-8 bytes in the submitted body. The server owns boundaries,
// types and versions; hints can only assign identities to matching blocks.
type PageBlockHint struct {
	ID   string `json:"id"`
	From int    `json:"from"`
	To   int    `json:"to"`
}
type PageBlockTarget struct {
	BlockID   string `json:"blockID"`
	Version   int64  `json:"version"`
	Placement string `json:"placement,omitempty"` // replace, before, after
}

func (d *PageDocument) Markdown() string {
	var b strings.Builder
	b.WriteString(d.Prefix)
	for _, block := range d.Blocks {
		b.WriteString(block.Markdown)
	}
	return b.String()
}
func (d *PageDocument) Hints() []PageBlockHint {
	out := make([]PageBlockHint, 0, len(d.Blocks))
	at := len(d.Prefix)
	for _, b := range d.Blocks {
		out = append(out, PageBlockHint{b.ID, at, at + len(b.Markdown)})
		at += len(b.Markdown)
	}
	return out
}

// Capture opening markers as well as content lines (including empty fences).
// AST line spans alone omit list markers, fence openers and thematic breaks.
type sourceBlockParser struct{ parser.BlockParser }

func (p sourceBlockParser) Open(parent ast.Node, r text.Reader, c parser.Context) (ast.Node, parser.State) {
	_, line := r.PeekLine()
	n, s := p.BlockParser.Open(parent, r, c)
	if n != nil {
		n.SetAttributeString("pudding-source-start", line.Start)
	}
	return n, s
}

// Reference definitions are removed from the rendering AST. Preserve them as
// separate source blocks so replacing a neighboring paragraph cannot erase them.
type sourceReferences struct{ starts *[]int }

func (p sourceReferences) Transform(node *ast.Paragraph, r text.Reader, c parser.Context) {
	top := node.Parent() != nil && node.Parent().Kind() == ast.KindDocument
	start := nodeSourceStart(node, len(r.Source()))
	parser.LinkReferenceParagraphTransformer.Transform(node, r, c)
	if !top {
		return
	}
	if node.Parent() == nil || node.Lines().Len() == 0 {
		*p.starts = append(*p.starts, start)
	} else if next := node.Lines().At(0).Start; next > start {
		// A heading/paragraph opener may skip indentation, so only a consumed
		// reference definition (not whitespace) creates an extra boundary.
		if strings.TrimSpace(string(r.Source()[start:next])) != "" {
			*p.starts = append(*p.starts, start)
			node.SetAttributeString("pudding-source-start", next)
		}
	}
}

func nodeSourceStart(n ast.Node, end int) int {
	start := end
	if value, ok := n.AttributeString("pudding-source-start"); ok {
		start = value.(int)
	}
	if n.Type() == ast.TypeBlock && n.Lines().Len() > 0 {
		start = min(start, n.Lines().At(0).Start)
	}
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		start = min(start, nodeSourceStart(child, end))
	}
	return start
}

var pageFrontmatter = regexp.MustCompile(`^(?:\x{FEFF})?---\r?\n[\s\S]*?\r?\n(?:---|\.\.\.)(?:\r?\n|$)`)
var pageBlockID = regexp.MustCompile(`^block_[a-zA-Z0-9_-]{8,100}$`)

func parsePageBlocks(body string) *PageDocument {
	d := &PageDocument{SchemaVersion: 1, Generation: 1, Blocks: []PageBlock{}}
	front := pageFrontmatter.FindString(body)
	blocks := parser.DefaultBlockParsers()
	wrapped := make([]util.PrioritizedValue, 0, len(blocks))
	for _, p := range blocks {
		// Match Studio's literal indentation: only fenced code creates a code block.
		if p.Priority != 500 {
			wrapped = append(wrapped, util.Prioritized(sourceBlockParser{p.Value.(parser.BlockParser)}, p.Priority))
		}
	}
	references := []int{}
	md := goldmark.New(goldmark.WithParser(parser.NewParser(parser.WithBlockParsers(wrapped...), parser.WithInlineParsers(parser.DefaultInlineParsers()...), parser.WithParagraphTransformers(util.Prioritized(sourceReferences{&references}, 100)))), goldmark.WithExtensions(extension.GFM))
	source := []byte(body[len(front):])
	root := md.Parser().Parse(text.NewReader(source))
	type boundary struct {
		start int
		kind  string
	}
	starts := []boundary{}
	if front != "" {
		starts = append(starts, boundary{0, "source"})
	}
	for n := root.FirstChild(); n != nil; n = n.NextSibling() {
		start := nodeSourceStart(n, len(source)) + len(front)
		if start >= len(body) {
			continue
		}
		line := strings.LastIndex(body[:start], "\n") + 1
		if strings.Trim(body[line:start], " \t") == "" {
			start = line
		}
		if len(starts) > 0 && start <= starts[len(starts)-1].start {
			continue
		}
		kind := "source"
		switch n.Kind().String() {
		case "Paragraph":
			kind = "paragraph"
		case "Heading":
			kind = "heading"
		case "FencedCodeBlock", "CodeBlock":
			kind = "code"
		case "List":
			kind = "list"
		case "Blockquote":
			kind = "blockquote"
		case "Table":
			kind = "table"
		case "ThematicBreak":
			kind = "divider"
		}
		starts = append(starts, boundary{start, kind})
	}
	for _, start := range references {
		starts = append(starts, boundary{start + len(front), "source"})
	}
	sort.SliceStable(starts, func(i, j int) bool { return starts[i].start < starts[j].start })
	if len(starts) == 0 {
		d.Prefix = body
		return d
	}
	d.Prefix = body[:starts[0].start]
	for i, b := range starts {
		end := len(body)
		if i+1 < len(starts) {
			end = starts[i+1].start
		}
		d.Blocks = append(d.Blocks, PageBlock{Type: b.kind, Markdown: body[b.start:end]})
	}
	return d
}
func blockValue(b PageBlock) string { return strings.TrimRight(b.Markdown, "\r\n") }

// Reconcile is linear in the number of blocks. Explicit editor identities win;
// unique unchanged/moved source is matched next; changed blocks within each
// surviving ordered gap inherit the old identity. Split keeps the first ID,
// merge keeps the first ID. Copies receive new identities.
func ReconcilePageDocument(body string, previous *PageDocument, hints []PageBlockHint) (*PageDocument, error) {
	if err := ValidatePage(body); err != nil {
		return nil, err
	}
	d := parsePageBlocks(body)
	old := map[string]PageBlock{}
	oldIndex := map[string]int{}
	used := map[string]bool{}
	if previous != nil {
		d.Generation = previous.Generation + 1
		for i, b := range previous.Blocks {
			old[b.ID] = b
			oldIndex[b.ID] = i
		}
	}
	positions := d.Hints()
	byStart := map[int]int{}
	for i, h := range positions {
		byStart[h.From] = i
	}
	hintIDs := map[string]bool{}
	for _, h := range hints {
		if !pageBlockID.MatchString(h.ID) || hintIDs[h.ID] || !ValidPageRange(body, h.From, h.To) {
			return nil, &InvalidContent{"invalid or duplicate block identity"}
		}
		hintIDs[h.ID] = true
		i, ok := byStart[h.From]
		// Different parser extensions may group a source region differently. Never
		// split a code/list/table merely to accept client-supplied metadata.
		if !ok || positions[i].To != h.To {
			continue
		}
		d.Blocks[i].ID = h.ID
		used[h.ID] = true
	}
	candidates := map[string][]string{}
	if previous != nil {
		for _, b := range previous.Blocks {
			if !used[b.ID] {
				key := blockValue(b)
				candidates[key] = append(candidates[key], b.ID)
			}
		}
	}
	for i := range d.Blocks {
		b := &d.Blocks[i]
		if b.ID != "" {
			continue
		}
		key := blockValue(*b)
		if list := candidates[key]; len(list) > 0 {
			b.ID = list[0]
			used[b.ID] = true
			candidates[key] = list[1:]
		}
	}
	// Match changed gaps only between ordered surviving anchors. Moving an
	// unchanged block was already resolved above and cannot steal another ID.
	if previous != nil {
		cursor := 0
		for i := 0; i < len(d.Blocks); {
			if index, ok := oldIndex[d.Blocks[i].ID]; ok {
				cursor = max(cursor, index+1)
				i++
				continue
			}
			if d.Blocks[i].ID != "" {
				i++
				continue
			}
			end := i
			for end < len(d.Blocks) && d.Blocks[end].ID == "" {
				end++
			}
			limit := len(previous.Blocks)
			if end < len(d.Blocks) {
				if index, ok := oldIndex[d.Blocks[end].ID]; ok {
					limit = index
				}
			}
			for j := i; j < end; j++ {
				for cursor < limit && used[previous.Blocks[cursor].ID] {
					cursor++
				}
				if cursor < limit {
					id := previous.Blocks[cursor].ID
					d.Blocks[j].ID = id
					used[id] = true
					cursor++
				}
			}
			i = end
		}
	}
	for i := range d.Blocks {
		b := &d.Blocks[i]
		if b.ID == "" {
			b.ID = NewID("block")
		}
		b.Version = d.Generation
		if old, ok := old[b.ID]; ok && blockValue(old) == blockValue(*b) && old.Type == b.Type {
			b.Version = old.Version
		}
	}
	return d, nil
}

// Legacy identities are deterministic until the first structured save. Neither
// old history bytes nor hashes need to change to obtain repeatable block IDs.
func LegacyPageDocument(itemID, body string) *PageDocument {
	d := parsePageBlocks(body)
	for i := range d.Blocks {
		d.Blocks[i].ID = "block_" + ContentHash(fmt.Sprintf("%s\x00%s\x00%d", itemID, body, i))[:32]
		d.Blocks[i].Version = 1
	}
	return d
}
func DecodeStoredContent(itemID, raw, format string) (string, *PageDocument, error) {
	if format == "plain" {
		return raw, nil, nil
	}
	if format != PageDocumentFormat {
		return "", nil, &InvalidContent{"unsupported content format"}
	}
	var d PageDocument
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return "", nil, err
	}
	if d.SchemaVersion != 1 || d.Generation < 1 || d.Blocks == nil {
		return "", nil, &InvalidContent{"invalid block snapshot"}
	}
	seen := map[string]bool{}
	for _, b := range d.Blocks {
		if !pageBlockID.MatchString(b.ID) || seen[b.ID] || b.Version < 1 || b.Version > d.Generation {
			return "", nil, &InvalidContent{"invalid block snapshot identity"}
		}
		seen[b.ID] = true
	}
	body := d.Markdown()
	return body, &d, ValidatePage(body)
}

// Block reservations use per-block versions, so unrelated edits do not make a
// read stale. The resulting byte ranges continue to drive existing live cursors.
func (d *PageDocument) ResolveTargets(targets []PageBlockTarget) ([]PageEditRange, error) {
	hints := d.Hints()
	indices := map[string]int{}
	for i, b := range d.Blocks {
		indices[b.ID] = i
	}
	out := make([]PageEditRange, 0, len(targets))
	for _, t := range targets {
		i, ok := indices[t.BlockID]
		if !ok || d.Blocks[i].Version != t.Version {
			return nil, &ContentConflict{}
		}
		h := hints[i]
		end := i
		switch t.Placement {
		case "", "replace":
			h.To = hints[end].To - len(d.Blocks[end].Markdown) + len(blockValue(d.Blocks[end]))
		case "before":
			h.To = h.From
		case "after":
			h.From = h.To
		default:
			return nil, &InvalidContent{"invalid block placement"}
		}
		out = append(out, PageEditRange{From: h.From, To: h.To})
	}
	return out, nil
}

// Preserve identities using the actual committed operation, even when new text
// equals another block. Content matching alone cannot distinguish those cases.
func PageIdentityHintsAfterEdits(previous *PageDocument, body string, ranges []PageEditRange, replacements []string) []PageBlockHint {
	parsed := parsePageBlocks(body)
	ends := map[int]int{}
	for _, h := range parsed.Hints() {
		ends[h.From] = h.To
	}
	hints := []PageBlockHint{}
	for _, h := range previous.Hints() {
		at, alive := h.From, true
		for i, r := range ranges {
			if r.From == r.To {
				if r.From <= h.From {
					at += len(replacements[i])
				}
			} else if r.To <= h.From {
				at += len(replacements[i]) - (r.To - r.From)
			} else if r.From <= h.From {
				if r.From == h.From && strings.TrimSpace(replacements[i]) != "" {
					break
				}
				alive = false
				break
			}
		}
		if end, ok := ends[at]; alive && ok {
			hints = append(hints, PageBlockHint{h.ID, at, end})
		}
	}
	return hints
}

// Undo by identity restores the AI's blocks without confusing repeated text.
// Disjoint human changes (including within a surviving block) are retained.
func UndoPageDocument(before, after, current *PageDocument) (*PageDocument, error) {
	if before.Markdown() == after.Markdown() {
		return nil, &InvalidContent{"version has no content changes"}
	}
	old, next, now := map[string]PageBlock{}, map[string]PageBlock{}, map[string]PageBlock{}
	for _, b := range before.Blocks {
		old[b.ID] = b
	}
	for _, b := range after.Blocks {
		next[b.ID] = b
	}
	for _, b := range current.Blocks {
		now[b.ID] = b
	}
	result := &PageDocument{SchemaVersion: 1, Generation: current.Generation + 1, Blocks: []PageBlock{}}
	var err error
	oldOrder, newOrder := []string{}, []string{}
	for _, b := range before.Blocks {
		if _, ok := next[b.ID]; ok {
			oldOrder = append(oldOrder, b.ID)
		}
	}
	for _, b := range after.Blocks {
		if _, ok := old[b.ID]; ok {
			newOrder = append(newOrder, b.ID)
		}
	}
	if !slices.Equal(oldOrder, newOrder) {
		return nil, ErrUndoConflict
	}
	result.Prefix = current.Prefix
	if before.Prefix != after.Prefix {
		result.Prefix, err = UndoPage(before.Prefix, after.Prefix, current.Prefix)
	}
	if err != nil {
		return nil, err
	}
	for _, b := range current.Blocks {
		ai, changed := next[b.ID]
		previous, existed := old[b.ID]
		if changed && !existed {
			if b.Markdown != ai.Markdown {
				return nil, ErrUndoConflict
			}
			continue
		}
		if changed && existed && previous.Markdown != ai.Markdown {
			b.Markdown, err = UndoPage(previous.Markdown, ai.Markdown, b.Markdown)
			if err != nil {
				return nil, err
			}
		}
		result.Blocks = append(result.Blocks, b)
	}
	for id, b := range old {
		if ai, ok := next[id]; ok && ai.Markdown != b.Markdown {
			if _, present := now[id]; !present {
				return nil, ErrUndoConflict
			}
		}
	}
	for i, b := range before.Blocks {
		if _, kept := next[b.ID]; kept {
			continue
		}
		if _, present := now[b.ID]; present {
			return nil, ErrUndoConflict
		}
		// Restore before the nearest surviving successor, or after the predecessor.
		at := len(result.Blocks)
		found := false
		for j := i + 1; j < len(before.Blocks) && !found; j++ {
			for k, r := range result.Blocks {
				if r.ID == before.Blocks[j].ID {
					at = k
					found = true
					break
				}
			}
		}
		if !found {
			for j := i - 1; j >= 0 && !found; j-- {
				for k, r := range result.Blocks {
					if r.ID == before.Blocks[j].ID {
						at = k + 1
						found = true
						break
					}
				}
			}
		}
		result.Blocks = append(result.Blocks, PageBlock{})
		copy(result.Blocks[at+1:], result.Blocks[at:])
		result.Blocks[at] = b
	}
	return ReconcilePageDocument(result.Markdown(), current, result.Hints())
}
