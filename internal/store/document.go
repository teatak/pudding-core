package store

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"
)

// The persisted kind is a protocol identity; do not rename it with the product.
const StudioItemKindDocument = "rich-document"
const DOCXMIME = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
const MaxDocumentBytes = 2 << 20
const MaxDocumentBlobBytes = 20 << 20
const MaxDocumentAssetsBytes = 50 << 20

type DocumentNode struct {
	Type    string         `json:"type"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Text    string         `json:"text,omitempty"`
	Content []DocumentNode `json:"content,omitempty"`
	Marks   []DocumentMark `json:"marks,omitempty"`
}
type DocumentMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}
type DocumentPage struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	Top    int `json:"top"`
	Right  int `json:"right"`
	Bottom int `json:"bottom"`
	Left   int `json:"left"`
}
type DocumentAsset struct {
	ID     string `json:"id"`
	Hash   string `json:"hash"`
	Type   string `json:"type"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}
type DocumentOriginal struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}
type DocumentRegionVariants struct {
	Default DocumentNode  `json:"default"`
	First   *DocumentNode `json:"first,omitempty"`
	Even    *DocumentNode `json:"even,omitempty"`
}
type DocumentHeaderFooter struct {
	Header             DocumentRegionVariants `json:"header"`
	Footer             DocumentRegionVariants `json:"footer"`
	HeaderDistance     int                    `json:"headerDistance"`
	FooterDistance     int                    `json:"footerDistance"`
	DifferentFirstPage bool                   `json:"differentFirstPage"`
	DifferentOddEven   bool                   `json:"differentOddEven"`
}
type DocumentBody struct {
	HeaderFooter  *DocumentHeaderFooter `json:"headerFooter,omitempty"`
	SchemaVersion int                   `json:"schemaVersion"`
	Body          DocumentNode          `json:"body"`
	Page          DocumentPage          `json:"page"`
	Assets        []DocumentAsset       `json:"assets"`
	Warnings      []string              `json:"warnings"`
	Original      *DocumentOriginal     `json:"original,omitempty"`
}
type DocumentBlob struct {
	Hash string `json:"hash"`
	Type string `json:"type"`
	Data []byte `json:"data"`
}
type DocumentContent struct {
	ItemID      string       `json:"itemID"`
	Body        DocumentBody `json:"body"`
	ContentHash string       `json:"contentHash"`
	RevisionID  string       `json:"revisionID"`
}
type DocumentWrite struct {
	ClientRequestID string              `json:"clientRequestID"`
	ExpectedHash    string              `json:"expectedHash"`
	Body            *DocumentBody       `json:"body,omitempty"`
	Blobs           []DocumentBlob      `json:"blobs,omitempty"`
	RestoreRevision string              `json:"restoreRevision,omitempty"`
	Operations      []DocumentOperation `json:"operations,omitempty"`
	UndoRevision    string              `json:"undoRevision,omitempty"`
	Author          ContentAuthor       `json:"-"`
}
type DocumentStore interface {
	CreateDocument(context.Context, *StudioItem, DocumentBody, []DocumentBlob, ...ContentAuthor) (*DocumentContent, error)
	GetDocument(context.Context, string) (*DocumentContent, error)
	WriteDocument(context.Context, string, DocumentWrite) (*DocumentContent, error)
	GetDocumentBlob(context.Context, string, string) (*DocumentBlob, error)
}

func EmptyDocument() DocumentBody {
	return DocumentBody{SchemaVersion: 1, Body: DocumentNode{Type: "doc", Content: []DocumentNode{{Type: "paragraph", Attrs: map[string]any{"blockId": NewID("block")}}}}, Page: DocumentPage{11906, 16838, 1440, 1440, 1440, 1440}, Assets: []DocumentAsset{}, Warnings: []string{}}
}
func invalidDocument(message string) error { return &InvalidContent{Message: message} }
func ValidateDocumentBlob(blob DocumentBlob) error {
	sum := sha256.Sum256(blob.Data)
	if !ValidContentHash(blob.Hash) || hex.EncodeToString(sum[:]) != blob.Hash || len(blob.Data) == 0 || len(blob.Data) > MaxDocumentBlobBytes {
		return invalidDocument("invalid asset hash or size")
	}
	if blob.Type == DOCXMIME {
		z, e := zip.NewReader(bytes.NewReader(blob.Data), int64(len(blob.Data)))
		if e != nil {
			return invalidDocument("invalid DOCX archive")
		}
		if len(z.File) > 1500 {
			return invalidDocument("too many DOCX entries")
		}
		var expanded uint64
		hasDoc, hasTypes := false, false
		for _, f := range z.File {
			expanded += f.UncompressedSize64
			if expanded > 80<<20 {
				return invalidDocument("DOCX archive expands too large")
			}
			hasDoc = hasDoc || f.Name == "word/document.xml"
			hasTypes = hasTypes || f.Name == "[Content_Types].xml"
		}
		if !hasDoc || !hasTypes {
			return invalidDocument("DOCX document parts missing")
		}
		return nil
	}
	if len(blob.Data) > 10<<20 {
		return invalidDocument("image too large")
	}
	config, format, e := image.DecodeConfig(bytes.NewReader(blob.Data))
	if e != nil || "image/"+format != blob.Type || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 40000000 {
		return invalidDocument("invalid image type or dimensions")
	}
	return nil
}
func ValidateDocument(body DocumentBody) error {
	raw, e := json.Marshal(body)
	if e != nil || len(raw) > MaxDocumentBytes || (body.SchemaVersion != 1 && body.SchemaVersion != 2) || body.Body.Type != "doc" {
		return invalidDocument("unsupported or oversized structured document")
	}
	p := body.Page
	if p.Width < 3000 || p.Width > 31680 || p.Height < 3000 || p.Height > 31680 || p.Top < 0 || p.Right < 0 || p.Bottom < 0 || p.Left < 0 || p.Top+p.Bottom >= p.Height || p.Left+p.Right >= p.Width {
		return invalidDocument("invalid page layout")
	}
	if body.Assets == nil || body.Warnings == nil || len(body.Assets) > 100 || len(body.Warnings) > 200 {
		return invalidDocument("too many assets or conversion warnings")
	}
	assets := map[string]bool{}
	for _, a := range body.Assets {
		if a.ID == "" || len(a.ID) > 200 || assets[a.ID] || !ValidContentHash(a.Hash) || (a.Type != "image/png" && a.Type != "image/jpeg" && a.Type != "image/gif") || a.Width < 0 || a.Height < 0 || a.Width > 40000 || a.Height > 40000 {
			return invalidDocument("invalid image reference")
		}
		assets[a.ID] = true
	}
	if o := body.Original; o != nil && (!ValidContentHash(o.Hash) || len(o.Name) > 500 || strings.TrimSpace(o.Name) == "") {
		return invalidDocument("invalid original file reference")
	}
	if body.SchemaVersion == 1 && body.HeaderFooter != nil || body.SchemaVersion == 2 && body.HeaderFooter == nil {
		return invalidDocument("header/footer version mismatch")
	}
	if h := body.HeaderFooter; h != nil {
		if h.HeaderDistance < 0 || h.FooterDistance < 0 || h.HeaderDistance > p.Top || h.FooterDistance > p.Bottom || p.Width-p.Left-p.Right < 720 || p.Height-p.Top-p.Bottom < 720 {
			return invalidDocument("invalid header/footer margins")
		}
	}
	region := false
	ids := map[string]bool{}
	count := 0
	var visit func(DocumentNode, int, string) error
	visit = func(n DocumentNode, depth int, parent string) error {
		count++
		if depth > 64 || count > 100000 {
			return invalidDocument("document structure too complex")
		}
		inline := n.Type == "text" || n.Type == "image" || n.Type == "hardBreak" || n.Type == "pageBreak" || n.Type == "pageNumber" || n.Type == "totalPages"
		if region && n.Type != "doc" && n.Type != "paragraph" && n.Type != "text" && n.Type != "hardBreak" && n.Type != "pageNumber" && n.Type != "totalPages" {
			return invalidDocument("unsupported header/footer node")
		}
		if !region && (n.Type == "pageNumber" || n.Type == "totalPages") {
			return invalidDocument("page fields require header/footer")
		}
		switch parent {
		case "doc", "blockquote", "tableCell", "tableHeader":
			if inline || n.Type == "doc" || n.Type == "tableRow" || n.Type == "tableCell" || n.Type == "tableHeader" || n.Type == "listItem" {
				return invalidDocument("invalid block child")
			}
		case "paragraph", "heading":
			if !inline {
				return invalidDocument("invalid inline child")
			}
		case "codeBlock":
			if n.Type != "text" {
				return invalidDocument("invalid code child")
			}
		case "bulletList", "orderedList":
			if n.Type != "listItem" {
				return invalidDocument("invalid list child")
			}
		case "listItem":
			if inline || n.Type == "listItem" || n.Type == "tableRow" || n.Type == "tableCell" || n.Type == "tableHeader" || n.Type == "doc" {
				return invalidDocument("invalid list block")
			}
		case "table":
			if n.Type != "tableRow" {
				return invalidDocument("invalid table child")
			}
		case "tableRow":
			if n.Type != "tableCell" && n.Type != "tableHeader" {
				return invalidDocument("invalid row child")
			}
		}
		switch n.Type {
		case "doc", "paragraph", "heading", "blockquote", "codeBlock", "bulletList", "orderedList", "listItem", "table", "tableRow", "tableCell", "tableHeader":
			if n.Text != "" {
				return invalidDocument("text must be a text node")
			}
		case "text":
			if n.Text == "" || len(n.Content) > 0 {
				return invalidDocument("invalid text")
			}
		case "image":
			if id, ok := n.Attrs["assetId"].(string); !ok || !assets[id] {
				return invalidDocument("missing image reference")
			}
			fallthrough
		case "hardBreak", "pageBreak", "horizontalRule", "pageNumber", "totalPages":
			if len(n.Content) > 0 || n.Text != "" {
				return invalidDocument("invalid leaf node")
			}
		default:
			return invalidDocument("unsupported node: " + n.Type)
		}
		for key, value := range n.Attrs {
			if value == nil {
				continue
			}
			switch key {
			case "blockId":
				id, ok := value.(string)
				if !ok || id == "" || len(id) > 200 || ids[id] {
					return invalidDocument("invalid or duplicate block identity")
				}
				ids[id] = true
			case "textAlign":
				if value != "left" && value != "right" && value != "center" && value != "justify" {
					return invalidDocument("invalid alignment")
				}
			case "level", "start", "colspan", "rowspan", "width":
				num, ok := value.(float64)
				if !ok {
					if v, yes := value.(int); yes {
						num = float64(v)
						ok = true
					}
				}
				if !ok || num < 1 || num > 40000 || num != float64(int(num)) {
					return invalidDocument("invalid numeric attribute")
				}
				if key == "level" && num > 6 {
					return invalidDocument("invalid heading")
				}
			case "colwidth":
				if widths, ok := value.([]any); ok {
					for _, v := range widths {
						f, ok := v.(float64)
						if !ok || f < 0 || f > 40000 {
							return invalidDocument("invalid column width")
						}
					}
				} else {
					return invalidDocument("invalid column widths")
				}
			case "assetId", "alt", "language", "type":
				if s, ok := value.(string); !ok || len(s) > 1000 {
					return invalidDocument("invalid text attribute")
				}
			default:
				return invalidDocument("unsupported attribute: " + key)
			}
		}
		for _, m := range n.Marks {
			switch m.Type {
			case "bold", "italic", "underline", "strike", "code":
				if len(m.Attrs) > 0 {
					return invalidDocument("unsupported mark attributes")
				}
			case "link":
				href, _ := m.Attrs["href"].(string)
				h := strings.ToLower(href)
				if !(strings.HasPrefix(h, "https://") || strings.HasPrefix(h, "http://") || strings.HasPrefix(h, "mailto:")) {
					return invalidDocument("invalid link")
				}
				for k, v := range m.Attrs {
					if k != "href" && k != "target" && k != "rel" && k != "class" {
						return invalidDocument("unsupported link attribute")
					}
					if v != nil {
						if s, ok := v.(string); !ok || len(s) > 10000 {
							return invalidDocument("invalid link attribute")
						}
					}
				}
			default:
				return invalidDocument("unsupported mark: " + m.Type)
			}
		}
		for _, child := range n.Content {
			if err := visit(child, depth+1, n.Type); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(body.Body, 0, ""); err != nil {
		return err
	}
	region = true
	if h := body.HeaderFooter; h != nil {
		for _, variants := range []DocumentRegionVariants{h.Header, h.Footer} {
			for _, node := range []*DocumentNode{&variants.Default, variants.First, variants.Even} {
				if node == nil {
					continue
				}
				if node.Type != "doc" {
					return invalidDocument("invalid header/footer document")
				}
				if err := visit(*node, 0, ""); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
