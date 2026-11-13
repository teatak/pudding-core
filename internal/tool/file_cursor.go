package tool

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"path"
	"path/filepath"
	"strings"
)

// A cursor is a traversal position, never an authorization token. Every page
// resolves its scope and applies the same visibility/ignore rules again.
type filePagePosition struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}
type filePageCursor struct {
	Version int    `json:"v"`
	Query   string `json:"query"`
	filePagePosition
}
type filePage struct {
	root    string
	query   string
	after   filePagePosition
	last    filePagePosition
	visited int
	limit   int
}

func newFilePage(root, cursor string, query any) (*filePage, error) {
	data, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	p := &filePage{root: root, query: hex.EncodeToString(hash[:]), limit: maxFileSearchFiles}
	if cursor == "" {
		return p, nil
	}
	invalid := &TextFileSearchError{Code: "invalid_cursor", Detail: "Cursor is invalid or belongs to a different query. Restart without cursor; keep scope, path and search filters unchanged between pages."}
	if len(cursor) > 8192 {
		return nil, invalid
	}
	data, err = base64.RawURLEncoding.DecodeString(cursor)
	var decoded filePageCursor
	if err != nil || json.Unmarshal(data, &decoded) != nil || decoded.Version != 1 || decoded.Query != p.query || decoded.Line < 0 || decoded.Path == "" || strings.ContainsAny(decoded.Path, "\\\x00") || path.IsAbs(decoded.Path) || path.Clean(decoded.Path) != decoded.Path || decoded.Path == ".." || strings.HasPrefix(decoded.Path, "../") {
		return nil, invalid
	}
	p.after, p.last = decoded.filePagePosition, decoded.filePagePosition
	return p, nil
}

func (p *filePage) position(target string, line int) filePagePosition {
	rel, _ := filepath.Rel(p.root, target)
	return filePagePosition{Path: filepath.ToSlash(rel), Line: line}
}

// WalkDir orders siblings before descending; plain string comparison would
// incorrectly put a-file before a/child. Compare path components instead.
func compareTraversalPaths(a, b string) int {
	left, right := strings.Split(a, "/"), strings.Split(b, "/")
	for i := 0; i < min(len(left), len(right)); i++ {
		if order := strings.Compare(left[i], right[i]); order != 0 {
			return order
		}
	}
	return len(left) - len(right)
}

func (p *filePage) skip(target string, directory bool) bool {
	if p.after.Path == "" {
		return false
	}
	pos := p.position(target, 0)
	if directory && (pos.Path == "." || strings.HasPrefix(p.after.Path, pos.Path+"/")) {
		return false
	}
	order := compareTraversalPaths(pos.Path, p.after.Path)
	return order < 0 || (order == 0 && !directory && p.after.Line == 0)
}

func (p *filePage) visit(target string) bool {
	pos := p.position(target, 0)
	if p.after.Path != "" {
		order := compareTraversalPaths(pos.Path, p.after.Path)
		if order < 0 || (order == 0 && p.after.Line == 0) {
			return true
		}
	}
	p.visited++
	if p.visited > p.limit {
		return false
	}
	p.last = pos
	return true
}

func (p *filePage) startLine(target string) int {
	if p.position(target, 0).Path == p.after.Path {
		return p.after.Line
	}
	return 0
}

func (p *filePage) next() string {
	if p.last.Path == "" {
		return ""
	}
	data, _ := json.Marshal(filePageCursor{Version: 1, Query: p.query, filePagePosition: p.last})
	return base64.RawURLEncoding.EncodeToString(data)
}
