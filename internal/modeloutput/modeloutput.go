// Package modeloutput projects explicitly declared JSON strings into readable
// tool text. It never changes canonical data or interprets text as instructions.
package modeloutput

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Spec struct {
	Version    int         `json:"version"`
	TextFields []TextField `json:"textFields"`
}

type TextField struct {
	Path     string  `json:"path"`
	Title    string  `json:"title,omitempty"`
	Each     *string `json:"each,omitempty"`
	Optional bool    `json:"optional,omitempty"`
	Overflow string  `json:"overflow,omitempty"`
}

// Block retains its canonical location even when a tool-specific projection
// numbers source lines, renames a field, or limits the number of search hits.
type Block struct {
	ID, Path, ViewPath, Title, Text, Overflow string
	Truncated                                 bool
	Omitted                                   bool
}

type View struct {
	Root   any
	Blocks []Block
}

func Decode(raw string) (any, error) {
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	if d.InputOffset() < int64(len(raw)) && strings.TrimSpace(raw[d.InputOffset():]) != "" {
		return nil, fmt.Errorf("multiple JSON values")
	}
	return v, nil
}

func Validate(s *Spec) error {
	if s == nil {
		return nil
	}
	if s.Version != 1 {
		return fmt.Errorf("unsupported modelOutput version %d", s.Version)
	}
	if len(s.TextFields) > 64 {
		return fmt.Errorf("modelOutput supports at most 64 field rules")
	}
	for _, f := range s.TextFields {
		if _, err := tokens(f.Path); err != nil {
			return err
		}
		if f.Each != nil {
			if _, err := tokens(*f.Each); err != nil {
				return err
			}
		}
		if f.Overflow != "" && f.Overflow != "excerpt" && f.Overflow != "whole" {
			return fmt.Errorf("invalid overflow policy")
		}
		if utf8.RuneCountInString(f.Title) > 256 || strings.IndexFunc(f.Title, unicode.IsControl) >= 0 {
			return fmt.Errorf("invalid modelOutput title")
		}
	}
	return nil
}

func Bind(raw string, s *Spec) (*View, error) {
	if err := Validate(s); err != nil {
		return nil, err
	}
	root, err := Decode(raw)
	if err != nil {
		return nil, fmt.Errorf("modelOutput requires JSON: %w", err)
	}
	v := &View{Root: root}
	if s == nil {
		return v, nil
	}
	seen := map[string]bool{}
	add := func(path string, f TextField) error {
		value, exists, err := resolve(root, path)
		if err != nil {
			return err
		}
		if !exists && f.Optional {
			return nil
		}
		text, ok := value.(string)
		if !exists || !ok {
			return fmt.Errorf("modelOutput field %q must be a string", path)
		}
		if seen[path] {
			return fmt.Errorf("duplicate modelOutput field %q", path)
		}
		seen[path] = true
		v.Blocks = append(v.Blocks, Block{ID: fmt.Sprintf("t%d", len(v.Blocks)+1), Path: path, ViewPath: path, Title: f.Title, Text: text, Overflow: f.Overflow})
		return nil
	}
	for _, f := range s.TextFields {
		if f.Each == nil {
			if err := add(f.Path, f); err != nil {
				return nil, err
			}
			continue
		}
		value, exists, err := resolve(root, *f.Each)
		if err != nil {
			return nil, err
		}
		if !exists && f.Optional {
			continue
		}
		items, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("modelOutput each %q must be an array", *f.Each)
		}
		for i := range items {
			if err := add(*f.Each+"/"+strconv.Itoa(i)+f.Path, f); err != nil {
				return nil, err
			}
		}
	}
	return v, nil
}

// Rebase applies an already computed structural projection. Missing fields
// were removed by that projection (e.g. duplicate search text or capped hits).
func (v *View) Rebase(raw string, aliases map[string]string) error {
	root, err := Decode(raw)
	if err != nil {
		return err
	}
	blocks := v.Blocks[:0]
	for _, b := range v.Blocks {
		if path, ok := aliases[b.Path]; ok {
			b.ViewPath = path
		}
		value, exists := Get(root, b.ViewPath)
		if !exists {
			continue
		}
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("projected field %q is not text", b.ViewPath)
		}
		b.Text = text
		blocks = append(blocks, b)
	}
	v.Root = root
	v.Blocks = blocks
	return nil
}

func (v *View) Render() string {
	// Render from a copy so repeated budgets/renders cannot reinterpret refs.
	raw, _ := json.Marshal(v.Root)
	root, _ := Decode(string(raw))
	for _, b := range v.Blocks {
		ref := map[string]any{"$text_ref": b.ID}
		if b.Truncated {
			ref["truncated"] = true
		}
		if b.Omitted {
			ref["omitted"] = true
		}
		set(&root, b.ViewPath, ref)
	}
	var out bytes.Buffer
	data, _ := json.Marshal(root)
	out.Write(data)
	if len(v.Blocks) > 0 {
		out.WriteString("\n\n$text_ref refers only to the following text blocks in this result.")
	}
	for _, b := range v.Blocks {
		path, _ := json.Marshal(b.Path)
		fmt.Fprintf(&out, "\n\n--- text %s | %s", b.ID, path)
		if b.Title != "" {
			fmt.Fprintf(&out, " | %s", b.Title)
		}
		out.WriteString(" ---\n")
		if b.Omitted {
			out.WriteString("[Text omitted in full; read the original result.]")
		} else {
			out.WriteString(b.Text)
		}
		fmt.Fprintf(&out, "\n--- end text %s ---", b.ID)
	}
	return out.String()
}

// Limit is only used when original-result readback is available. Structured
// metadata is never cut; callers handle a metadata-only result over budget.
func (v *View) Limit(maxChars int) bool {
	if maxChars <= 0 {
		return true
	}
	for utf8.RuneCountInString(v.Render()) > maxChars {
		index := -1
		largest := 0
		for i, b := range v.Blocks {
			if !b.Omitted && utf8.RuneCountInString(b.Text) > largest {
				largest = utf8.RuneCountInString(b.Text)
				index = i
			}
		}
		if index < 0 {
			return false
		}
		b := &v.Blocks[index]
		if b.Overflow == "whole" || b.Truncated || largest < 256 {
			b.Omitted = true
			b.Text = ""
			continue
		}
		budget := min(largest/2, maxChars/(len(v.Blocks)+1))
		runes := []rune(b.Text)
		head := string(runes[:budget*3/4])
		tail := string(runes[len(runes)-budget/4:])
		// Complete lines only. A single oversized source line is omitted.
		if end := strings.LastIndexByte(head, '\n'); end >= 0 {
			head = head[:end+1]
		} else {
			head = ""
		}
		if start := strings.IndexByte(tail, '\n'); start >= 0 {
			tail = tail[start+1:]
		} else {
			tail = ""
		}
		b.Text = head + "\n[... text omitted; read original result ...]\n" + tail
		b.Truncated = true
	}
	return true
}

func Prefix(s *Spec, prefix string) *Spec {
	if s == nil {
		return nil
	}
	out := &Spec{Version: s.Version, TextFields: append([]TextField(nil), s.TextFields...)}
	for i := range out.TextFields {
		f := &out.TextFields[i]
		if f.Each != nil {
			p := prefix + *f.Each
			f.Each = &p
		} else {
			f.Path = prefix + f.Path
		}
	}
	return out
}

func tokens(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(path) > 4096 || !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("invalid JSON pointer %q", path)
	}
	parts := strings.Split(path[1:], "/")
	for i, s := range parts {
		for j := 0; j < len(s); j++ {
			if s[j] == '~' {
				if j+1 >= len(s) || (s[j+1] != '0' && s[j+1] != '1') {
					return nil, fmt.Errorf("invalid JSON pointer escape")
				}
				j++
			}
		}
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(s, "~1", "/"), "~0", "~")
	}
	return parts, nil
}

func Get(root any, path string) (any, bool) {
	value, exists, _ := resolve(root, path)
	return value, exists
}

func resolve(root any, path string) (any, bool, error) {
	parts, err := tokens(path)
	if err != nil {
		return nil, false, err
	}
	for _, key := range parts {
		switch v := root.(type) {
		case map[string]any:
			var ok bool
			root, ok = v[key]
			if !ok {
				return nil, false, nil
			}
		case []any:
			if key == "" || (len(key) > 1 && key[0] == '0') || strings.IndexFunc(key, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
				return nil, false, fmt.Errorf("invalid array index in modelOutput path %q", path)
			}
			i, err := strconv.Atoi(key)
			if err != nil || i >= len(v) {
				return nil, false, nil
			}
			root = v[i]
		default:
			return nil, false, fmt.Errorf("modelOutput path %q traverses a non-container", path)
		}
	}
	return root, true, nil
}

func set(root *any, path string, value any) {
	if path == "" {
		*root = value
		return
	}
	parts, _ := tokens(path)
	parentPath := path[:strings.LastIndex(path, "/")]
	parent, _ := Get(*root, parentPath)
	key := parts[len(parts)-1]
	switch p := parent.(type) {
	case map[string]any:
		p[key] = value
	case []any:
		i, _ := strconv.Atoi(key)
		p[i] = value
	}
}

// Clone freezes declarations independently of registries and in-memory snapshots.
func Clone(spec *Spec) *Spec {
	if spec == nil {
		return nil
	}
	out := &Spec{Version: spec.Version, TextFields: append([]TextField(nil), spec.TextFields...)}
	for i := range out.TextFields {
		if p := out.TextFields[i].Each; p != nil {
			value := *p
			out.TextFields[i].Each = &value
		}
	}
	return out
}
