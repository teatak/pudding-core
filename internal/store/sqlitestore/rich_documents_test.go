package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestRichDocumentPersistenceAssetsConflicts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pudding.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	body := store.EmptyRichDocument()
	data, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	hash := store.DocumentHash(string(data))
	body.Assets = []store.RichAsset{{ID: "image", Hash: hash, Type: "image/png"}}
	body.Body.Content[0].Content = []store.RichNode{{Type: "text", Text: "结构化正文🙂"}, {Type: "image", Attrs: map[string]any{"assetId": "image"}}}
	blobs := []store.RichBlob{{Hash: hash, Type: "image/png", Data: data}}
	failed := &store.StudioItem{ID: "failed", Kind: store.StudioItemKindRichDocument, Name: "Failed"}
	if _, err = s.CreateRichDocument(ctx, failed, body, nil); err == nil {
		t.Fatal("missing asset accepted")
	}
	if _, err = s.GetStudioItem(ctx, "failed"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("failed import left item", err)
	}
	d, err := s.CreateRichDocument(ctx, &store.StudioItem{ID: "rich", Kind: store.StudioItemKindRichDocument, Name: "Rich"}, body, blobs)
	if err != nil {
		t.Fatal(err)
	}
	initial := *d
	body.Body.Content[0].Content[0].Text = "已编辑"
	write := store.RichDocumentWrite{ClientRequestID: "save", ExpectedHash: d.ContentHash, Body: &body}
	d, err = s.WriteRichDocument(ctx, "rich", write)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.WriteRichDocument(ctx, "rich", write)
	if err != nil || retry.RevisionID != d.RevisionID {
		t.Fatal("idempotency", err)
	}
	write.ClientRequestID = "stale"
	if _, err = s.WriteRichDocument(ctx, "rich", write); err == nil {
		t.Fatal("stale write accepted")
	}
	write.ClientRequestID = "save"
	write.ExpectedHash = d.ContentHash
	if _, err = s.WriteRichDocument(ctx, "rich", write); err == nil {
		t.Fatal("request id reused")
	}
	if _, err = s.GetDocument(ctx, "rich"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("rich document entered markdown API", err)
	}
	other := store.EmptyRichDocument()
	other.Assets = body.Assets
	if _, err = s.CreateRichDocument(ctx, &store.StudioItem{ID: "other", Kind: store.StudioItemKindRichDocument, Name: "Other"}, other, nil); err == nil {
		t.Fatal("cross-item asset reference accepted")
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.GetRichDocument(ctx, "rich")
	if err != nil || saved.Body.Body.Content[0].Content[0].Text != "已编辑" {
		t.Fatal("restart lost content", err)
	}
	asset, err := s.GetRichBlob(ctx, "rich", hash)
	if err != nil || string(asset.Data) != string(data) {
		t.Fatal("restart lost image", err)
	}
	restored, err := s.WriteRichDocument(ctx, "rich", store.RichDocumentWrite{ClientRequestID: "restore", ExpectedHash: saved.ContentHash, RestoreRevision: initial.RevisionID})
	if err != nil || restored.ContentHash != initial.ContentHash {
		t.Fatal("restore", err)
	}
	body.Body.Content[0].Content[0].Marks = []store.RichMark{{Type: "link", Attrs: map[string]any{"href": "javascript:alert(1)"}}}
	if _, err = s.WriteRichDocument(ctx, "rich", store.RichDocumentWrite{ClientRequestID: "unsafe", ExpectedHash: restored.ContentHash, Body: &body}); err == nil {
		t.Fatal("unsafe link saved")
	}
}
func TestRichMigrationPreservesMarkdownAndForeignKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pudding.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	schema := strings.Replace(store.SchemaSQL, "'widget','rich-document'", "'widget'", 1)
	schema = schema[:strings.LastIndex(schema, "CREATE TABLE studio_rich_blobs")]
	if _, err = db.Exec(schema + `PRAGMA user_version=40;`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	// Insert a real v40 item and its dependent content/revision before migration.
	db, err = sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO studio_items(id,kind,name,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at)VALUES('md','doc','MD',2,'revision','','{}',1,0,1,1);
 INSERT INTO studio_item_content(item_id,body,content_hash)VALUES('md',?,'hash');
 INSERT INTO studio_item_revisions(item_id,hash,parent_revision,client_request_id,created_at,build_receipt,content)VALUES('md','revision','','create',1,'null',?);`, "# 原文\r\n\n_keep_  \n", "# 原文\r\n\n_keep_  \n")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	d, err := s.GetDocument(context.Background(), "md")
	if err != nil || d.Body != "# 原文\r\n\n_keep_  \n" || d.RevisionID != "revision" {
		t.Fatal("Markdown changed", d, err)
	}
	revision, err := s.GetStudioItemRevision(context.Background(), "md", "revision")
	if err != nil || *revision.Body != d.Body {
		t.Fatal("history lost", err)
	}
	var foreignKeys int
	s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys)
	if foreignKeys != 1 {
		t.Fatal("foreign keys not restored")
	}
	if _, err = s.db.Exec(`DELETE FROM studio_items WHERE id='md'`); err != nil {
		t.Fatal(err)
	}
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM studio_item_content WHERE item_id='md'`).Scan(&count)
	if count != 0 {
		t.Fatal("foreign key cascade broken")
	}
}

func TestRichDocumentLayoutUpgradeAndRestore(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "pudding.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	legacy := store.EmptyRichDocument()
	initial, err := s.CreateRichDocument(ctx, &store.StudioItem{ID: "layout", Kind: store.StudioItemKindRichDocument, Name: "Layout"}, legacy, nil)
	if err != nil {
		t.Fatal(err)
	}
	read, err := s.GetRichDocument(ctx, "layout")
	if err != nil || read.ContentHash != initial.ContentHash || read.Body.SchemaVersion != 1 {
		t.Fatal("read rewrote legacy body", err)
	}
	empty := store.RichNode{Type: "doc", Content: []store.RichNode{{Type: "paragraph"}}}
	next := legacy
	next.SchemaVersion = 2
	next.HeaderFooter = &store.RichHeaderFooter{Header: store.RichRegionVariants{Default: empty}, Footer: store.RichRegionVariants{Default: store.RichNode{Type: "doc", Content: []store.RichNode{{Type: "paragraph", Content: []store.RichNode{{Type: "text", Text: "第"}, {Type: "pageNumber"}, {Type: "totalPages"}}}}}}, HeaderDistance: 720, FooterDistance: 720}
	next.Page.Width, next.Page.Height = next.Page.Height, next.Page.Width
	saved, err := s.WriteRichDocument(ctx, "layout", store.RichDocumentWrite{ClientRequestID: "layout", ExpectedHash: initial.ContentHash, Body: &next})
	if err != nil {
		t.Fatal(err)
	}
	read, err = s.GetRichDocument(ctx, "layout")
	if err != nil || read.Body.HeaderFooter == nil || read.Body.Page.Width != 16838 || read.ContentHash != saved.ContentHash {
		t.Fatal("layout lost", err)
	}
	bad := next
	copied := *next.HeaderFooter
	bad.HeaderFooter = &copied
	bad.HeaderFooter.HeaderDistance = bad.Page.Top + 1
	if store.ValidateRichDocument(bad) == nil {
		t.Fatal("overlapping header distance accepted")
	}
	bad = next
	bad.Body = store.RichNode{Type: "doc", Content: []store.RichNode{{Type: "paragraph", Content: []store.RichNode{{Type: "pageNumber"}}}}}
	if store.ValidateRichDocument(bad) == nil {
		t.Fatal("page field accepted in main body")
	}
	restored, err := s.WriteRichDocument(ctx, "layout", store.RichDocumentWrite{ClientRequestID: "restore-layout", ExpectedHash: saved.ContentHash, RestoreRevision: initial.RevisionID})
	if err != nil || restored.ContentHash != initial.ContentHash || restored.Body.HeaderFooter != nil {
		t.Fatal("v1 restore changed", err)
	}
}
