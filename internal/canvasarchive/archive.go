// Package canvasarchive stores retired structured canvases as offline documents.
// It is used once by migration; the application never renders or edits legacy data.
package canvasarchive

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Revision struct {
	Hash            string          `json:"hash"`
	ParentRevision  string          `json:"parentRevision"`
	ClientRequestID string          `json:"clientRequestID"`
	CreatedAt       int64           `json:"createdAt"`
	Content         json.RawMessage `json:"content"`
}
type Snapshot struct {
	ID                 string           `json:"id"`
	Name               string           `json:"name"`
	SourceSessionID    string           `json:"sourceSessionID"`
	SourceSessionTitle string           `json:"sourceSessionTitle"`
	ActiveRevision     string           `json:"activeRevision"`
	HeadRevision       string           `json:"headRevision"`
	Revision           int64            `json:"revision"`
	Deleted            bool             `json:"deleted"`
	CreatedAt          int64            `json:"createdAt"`
	UpdatedAt          int64            `json:"updatedAt"`
	Revisions          []Revision       `json:"revisions"`
	Mounts             []map[string]any `json:"mounts"`
	Favorites          []map[string]any `json:"favorites"`
	Recent             []map[string]any `json:"recent"`
}
type Entry struct {
	ID           string   `json:"id"`
	CanvasID     string   `json:"canvasID"`
	Name         string   `json:"name"`
	ArchivedAt   string   `json:"archivedAt"`
	VersionCount int      `json:"versionCount"`
	Path         string   `json:"path"`
	Warnings     []string `json:"warnings"`
}
type manifest struct {
	Entry
	SnapshotHash string            `json:"snapshotHash"`
	Files        map[string]string `json:"files"`
}

var mu sync.Mutex
var validID = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[a-f0-9]{32}$`)
var assetRef = regexp.MustCompile(`(?:file://[^\s"<>\)]+|/sessions/[A-Za-z0-9_-]+/attachments/[^\s"<>\)]+)`)

func Root(home string) string { return filepath.Join(home, "archives", "legacy-canvas") }
func digest(b []byte) string  { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func checkRoot(home string) error {
	p := Root(home)
	for _, v := range []string{filepath.Join(home, "archives"), p} {
		st, err := os.Lstat(v)
		if err != nil {
			return err
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return errors.New("archive directory must not be a symlink")
		}
	}
	return nil
}
func load(home, id string) (manifest, error) {
	var m manifest
	if !validID.MatchString(id) {
		return m, fs.ErrInvalid
	}
	if err := checkRoot(home); err != nil {
		return m, err
	}
	root, err := os.OpenRoot(Root(home))
	if err != nil {
		return m, err
	}
	defer root.Close()
	st, err := root.Lstat(id)
	if err != nil {
		return m, err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return m, fs.ErrInvalid
	}
	b, err := root.ReadFile(filepath.Join(id, "manifest.json"))
	if err != nil {
		return m, err
	}
	if err = json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	for name := range m.Files {
		if !fs.ValidPath(name) || name == "manifest.json" {
			return m, fs.ErrInvalid
		}
	}
	if m.ID != id {
		return m, fs.ErrInvalid
	}
	m.Path = filepath.Join(Root(home), id)
	return m, nil
}
func List(home string) ([]Entry, error) { mu.Lock(); defer mu.Unlock(); return list(home) }
func list(home string) ([]Entry, error) {
	out := []Entry{}
	if err := checkRoot(home); errors.Is(err, fs.ErrNotExist) {
		return out, nil
	} else if err != nil {
		return nil, err
	}
	dirs, err := os.ReadDir(Root(home))
	if err != nil {
		return nil, err
	}
	for _, d := range dirs {
		if !validID.MatchString(d.Name()) {
			continue
		}
		m, err := load(home, d.Name())
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, m.Entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}
func Preview(home, id string) ([]byte, error) {
	mu.Lock()
	defer mu.Unlock()
	if _, err := load(home, id); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(Root(home))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.ReadFile(filepath.Join(id, "index.html"))
}
func Export(home, id string, w io.Writer) error {
	mu.Lock()
	defer mu.Unlock()
	m, err := load(home, id)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Join(Root(home), id))
	if err != nil {
		return err
	}
	defer root.Close()
	zw := zip.NewWriter(w)
	defer zw.Close()
	names := []string{"manifest.json"}
	for name := range m.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		b, err := root.ReadFile(name)
		if err != nil {
			return err
		}
		if name != "manifest.json" && digest(b) != m.Files[name] {
			return fmt.Errorf("archive checksum mismatch: %s", name)
		}
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err = f.Write(b); err != nil {
			return err
		}
	}
	return zw.Close()
}

// The manifest is removed last. A failed cleanup keeps the entry discoverable and retryable.
func Remove(home string, ids []string) error {
	mu.Lock()
	defer mu.Unlock()
	for _, id := range ids {
		if !validID.MatchString(id) {
			return fs.ErrInvalid
		}
	}
	for _, id := range ids {
		_, err := load(home, id)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		root, err := os.OpenRoot(filepath.Join(Root(home), id))
		if err != nil {
			return err
		}
		entries, err := fs.ReadDir(root.FS(), ".")
		if err == nil {
			for _, e := range entries {
				if e.Name() == "manifest.json" {
					continue
				}
				if err = root.RemoveAll(e.Name()); err != nil {
					break
				}
			}
		}
		if err == nil {
			err = root.Remove("manifest.json")
		}
		root.Close()
		if err != nil {
			return err
		}
		// An empty directory carries no archived content and is not a UI entry.
		_ = os.Remove(filepath.Join(Root(home), id))
	}
	return nil
}

// Write installs a verified immutable archive before the database transaction retires it.
// A deterministic ID makes a restart after an interrupted migration reuse the same archive.
func Write(home string, s Snapshot) (Entry, error) {
	mu.Lock()
	defer mu.Unlock()
	original, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return Entry{}, err
	}
	hash := digest(original)
	id := time.UnixMilli(s.UpdatedAt).UTC().Format("20060102T150405Z") + "-" + hash[:32]
	if _, err := os.Stat(filepath.Join(Root(home), id)); err == nil {
		m, err := load(home, id)
		if err != nil {
			return Entry{}, err
		}
		if m.SnapshotHash != hash {
			return Entry{}, errors.New("archive identity conflict")
		}
		root, err := os.OpenRoot(m.Path)
		if err != nil {
			return Entry{}, err
		}
		defer root.Close()
		for name, want := range m.Files {
			b, err := root.ReadFile(name)
			if err != nil || digest(b) != want {
				return Entry{}, fmt.Errorf("archive verification failed: %s", name)
			}
		}
		return m.Entry, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Entry{}, err
	}
	for _, dir := range []string{filepath.Join(home, "archives"), Root(home)} {
		if err = os.Mkdir(dir, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
			return Entry{}, err
		}
		st, err := os.Lstat(dir)
		if err != nil {
			return Entry{}, err
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return Entry{}, fs.ErrInvalid
		}
	}
	if err = checkRoot(home); err != nil {
		return Entry{}, err
	}
	tmp, err := os.MkdirTemp(Root(home), ".pending-")
	if err != nil {
		return Entry{}, err
	}
	defer os.RemoveAll(tmp)
	m := manifest{Entry: Entry{ID: id, CanvasID: s.ID, Name: s.Name, ArchivedAt: time.Now().UTC().Format(time.RFC3339), VersionCount: len(s.Revisions), Path: filepath.Join(Root(home), id), Warnings: []string{}}, SnapshotHash: hash, Files: map[string]string{}}
	files := map[string][]byte{"original.json": original}
	assets := map[string]string{}
	for _, match := range localReferences(original) {
		if _, ok := assets[match]; ok {
			continue
		}
		u, err := url.Parse(match)
		if err != nil {
			return Entry{}, err
		}
		local := u.Path
		if u.Scheme != "file" && strings.HasPrefix(u.Path, "/sessions/") {
			parts := strings.Split(strings.TrimPrefix(u.Path, "/sessions/"), "/attachments/")
			if len(parts) != 2 || !fs.ValidPath(parts[0]) || strings.Contains(parts[0], "/") || !fs.ValidPath(parts[1]) {
				return Entry{}, fs.ErrInvalid
			}
			local = filepath.Join(home, "attachments", "sessions", parts[0], filepath.FromSlash(parts[1]))
		}
		if !filepath.IsAbs(local) {
			return Entry{}, fs.ErrInvalid
		}

		st, err := os.Lstat(local)
		if errors.Is(err, fs.ErrNotExist) {
			m.Warnings = append(m.Warnings, "Missing local asset: "+match)
			assets[match] = ""
			continue
		}
		if err != nil {
			return Entry{}, err
		}
		if !st.Mode().IsRegular() {
			return Entry{}, fmt.Errorf("unsupported local asset: %s", local)
		}
		var data []byte
		if u.Scheme != "file" && strings.HasPrefix(u.Path, "/sessions/") {
			root, e := os.OpenRoot(filepath.Join(home, "attachments", "sessions"))
			if e != nil {
				return Entry{}, e
			}
			relative, e := filepath.Rel(filepath.Join(home, "attachments", "sessions"), local)
			if e != nil {
				root.Close()
				return Entry{}, e
			}
			data, err = root.ReadFile(relative)
			root.Close()
		} else {
			data, err = os.ReadFile(local)
		}
		if err != nil {
			return Entry{}, err
		}
		name := "assets/" + digest(data) + filepath.Ext(local)
		files[name] = data
		contentType := mime.TypeByExtension(filepath.Ext(local))
		if strings.HasPrefix(contentType, "image/") {
			assets[match] = "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
		} else {
			assets[match] = name
		}
	}
	assetJSON, _ := json.MarshalIndent(assets, "", "  ")
	files["assets.json"] = assetJSON
	var sections strings.Builder
	for i, r := range s.Revisions {
		var c struct {
			Kind  string          `json:"kind"`
			Title string          `json:"title"`
			Item  json.RawMessage `json:"item"`
		}
		if err = json.Unmarshal(r.Content, &c); err != nil {
			return Entry{}, err
		}
		prefix := fmt.Sprintf("versions/%03d-", i+1)
		body, extra := render(c.Kind, c.Title, c.Item, prefix, assets)
		for name, data := range extra {
			files[name] = data
		}
		fmt.Fprintf(&sections, "<section><h2>%s</h2><p>Version %s</p>%s</section>", html.EscapeString(c.Title), html.EscapeString(r.Hash[:min(8, len(r.Hash))]), body)
	}
	warning := ""
	for _, w := range m.Warnings {
		warning += "<p>" + html.EscapeString(w) + "</p>"
	}
	files["index.html"] = []byte(`<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"><title>` + html.EscapeString(s.Name) + `</title><style>body{font:15px system-ui;max-width:1100px;margin:40px auto;padding:0 24px;color:#222}section{border-top:1px solid #ddd;padding:20px 0}pre{white-space:pre-wrap;overflow-wrap:anywhere}table{border-collapse:collapse;width:100%}td,th{padding:8px;border:1px solid #ddd;text-align:left}img{max-width:100%;max-height:600px}svg{max-width:100%}</style></head><body><h1>` + html.EscapeString(s.Name) + `</h1><p>Archived canvas · ` + html.EscapeString(s.SourceSessionTitle) + `</p>` + warning + sections.String() + `</body></html>`)
	for name, data := range files {
		target := filepath.Join(tmp, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return Entry{}, err
		}
		if err = writeFile(target, data); err != nil {
			return Entry{}, err
		}
		read, err := os.ReadFile(target)
		if err != nil || !bytes.Equal(data, read) {
			return Entry{}, fmt.Errorf("archive verification failed: %s", name)
		}
		m.Files[name] = digest(data)
	}
	encoded, _ := json.MarshalIndent(m, "", "  ")
	if err = writeFile(filepath.Join(tmp, "manifest.json"), encoded); err != nil {
		return Entry{}, err
	}
	if err = filepath.WalkDir(tmp, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			return nil
		}
		dir, e := os.Open(name)
		if e != nil {
			return e
		}
		defer dir.Close()
		return dir.Sync()
	}); err != nil {
		return Entry{}, err
	}
	if err = os.Rename(tmp, m.Path); err != nil {
		return Entry{}, err
	}
	dir, err := os.Open(Root(home))
	if err != nil {
		return Entry{}, err
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		return Entry{}, err
	}
	return m.Entry, nil
}
func writeFile(path string, b []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.Write(b); err != nil {
		return err
	}
	return f.Sync()
}
func render(kind, title string, item json.RawMessage, prefix string, assets map[string]string) (string, map[string][]byte) {
	files := map[string][]byte{}
	var v map[string]any
	_ = json.Unmarshal(item, &v)
	text := func(x any) string {
		if s, ok := x.(string); ok {
			return s
		}
		b, _ := json.Marshal(x)
		return string(b)
	}
	var out strings.Builder
	switch kind {
	case "markdown":
		s, _ := v["markdown"].(string)
		if s == "" {
			s, _ = v["content"].(string)
		}
		files[prefix+"content.md"] = []byte(s)
		out.WriteString("<pre>" + html.EscapeString(s) + "</pre>")
	case "table", "chart":
		if kind == "chart" {
			if nested, ok := v["chart"].(map[string]any); ok {
				v = nested
			}
		}
		rows, _ := v["rows"].([]any)
		if kind == "chart" {
			rows, _ = v["data"].([]any)
		}
		cols, _ := v["columns"].([]any)
		keys := []string{}
		labels := []string{}
		for _, col := range cols {
			if c, ok := col.(string); ok {
				keys = append(keys, c)
				labels = append(labels, c)
			} else if c, ok := col.(map[string]any); ok {
				k, _ := c["key"].(string)
				l, _ := c["label"].(string)
				if l == "" {
					l = k
				}
				keys = append(keys, k)
				labels = append(labels, l)
			}
		}
		if len(keys) == 0 && len(rows) > 0 {
			if row, ok := rows[0].(map[string]any); ok {
				for k := range row {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				labels = append(labels, keys...)
			}
		}
		var b bytes.Buffer
		w := csv.NewWriter(&b)
		_ = w.Write(labels)
		out.WriteString("<table><thead><tr>")
		for _, l := range labels {
			out.WriteString("<th>" + html.EscapeString(l) + "</th>")
		}
		out.WriteString("</tr></thead><tbody>")
		for _, row := range rows {
			r, _ := row.(map[string]any)
			line := []string{}
			out.WriteString("<tr>")
			for _, k := range keys {
				s := text(r[k])
				line = append(line, s)
				out.WriteString("<td>" + html.EscapeString(s) + "</td>")
			}
			_ = w.Write(line)
			out.WriteString("</tr>")
		}
		w.Flush()
		out.WriteString("</tbody></table>")
		files[prefix+"table.csv"] = b.Bytes()
	case "gallery", "timeline", "grid":
		out.WriteString(renderFields(v))
	default:
		pretty := &bytes.Buffer{}
		_ = json.Indent(pretty, item, "", "  ")
		out.WriteString("<pre>" + html.EscapeString(pretty.String()) + "</pre>")
	}
	for src, dest := range assets {
		if strings.HasPrefix(dest, "data:image/") && strings.Contains(string(item), src) {
			out.WriteString(`<img alt="Archived image" src="` + html.EscapeString(dest) + `">`)
		}
	}
	return out.String(), files
}

// Discover references from decoded strings so JSON escaping cannot alter a filename.
func localReferences(raw []byte) []string {
	var value any
	_ = json.Unmarshal(raw, &value)
	refs := map[string]bool{}
	var visit func(any, string)
	visit = func(v any, key string) {
		switch x := v.(type) {
		case map[string]any:
			for k, v := range x {
				visit(v, k)
			}
		case []any:
			for _, v := range x {
				visit(v, key)
			}
		case string:
			for _, ref := range assetRef.FindAllString(x, -1) {
				refs[ref] = true
			}
			if (key == "src" || key == "url" || key == "path") && strings.HasPrefix(x, "/") && !strings.HasPrefix(x, "//") {
				refs[x] = true
			}
		}
	}
	visit(value, "")
	out := []string{}
	for ref := range refs {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}
func renderFields(value any) string {
	switch v := value.(type) {
	case map[string]any:
		keys := []string{}
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var out strings.Builder
		out.WriteString("<dl>")
		for _, k := range keys {
			out.WriteString("<dt><strong>" + html.EscapeString(k) + "</strong></dt><dd>" + renderFields(v[k]) + "</dd>")
		}
		out.WriteString("</dl>")
		return out.String()
	case []any:
		var out strings.Builder
		out.WriteString("<ol>")
		for _, item := range v {
			out.WriteString("<li>" + renderFields(item) + "</li>")
		}
		out.WriteString("</ol>")
		return out.String()
	case string:
		return "<p>" + html.EscapeString(v) + "</p>"
	default:
		b, _ := json.Marshal(v)
		return html.EscapeString(string(b))
	}
}
