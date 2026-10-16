package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func (e *Engine) readRichDocument(ctx context.Context, args tool.RichDocumentArgs) (any, error) {
	if args.Format == "" {
		args.Format = "text"
	}
	if args.Format != "text" && args.Format != "outline" && args.Format != "blocks" {
		return nil, errors.New("invalid read format")
	}
	if args.Format == "text" && len(args.BlockIDs) > 0 {
		return nil, errors.New("block_ids require a structured format")
	}
	if args.Limit == 0 {
		if args.Format == "text" {
			args.Limit = 16000
		} else {
			args.Limit = 20
		}
	}
	if args.Offset < 0 || args.Limit < 1 || args.Limit > 16384 || args.Format != "text" && args.Limit > 100 || len(args.BlockIDs) > 100 {
		return nil, errors.New("invalid rich document read range")
	}
	// This checks kind and active/deleted state even for historical reads.
	doc, err := e.store.GetRichDocument(ctx, args.ItemID)
	if err != nil {
		return nil, err
	}
	if args.RevisionID != "" && args.RevisionID != doc.RevisionID {
		revision, err := e.store.GetStudioItemRevision(ctx, args.ItemID, args.RevisionID)
		if err != nil {
			return nil, err
		}
		if revision.Body == nil {
			return nil, errors.New("structured revision content missing")
		}
		var body store.RichDocumentBody
		if err := json.Unmarshal([]byte(*revision.Body), &body); err != nil {
			return nil, err
		}
		doc = &store.RichDocumentContent{ItemID: args.ItemID, Body: body, ContentHash: revision.ContentHash, RevisionID: revision.Hash}
	}
	if args.Format != "text" {
		doc.Body = store.AnchorRichBody(doc.Body)
	}
	region, variant := args.Region, args.Variant
	if region == "" {
		region = "body"
	}
	if variant == "" {
		variant = "default"
	}
	if variant != "default" && variant != "first" && variant != "even" {
		return nil, errors.New("invalid header/footer variant")
	}
	node := &doc.Body.Body
	switch region {
	case "body":
		if variant != "default" {
			return nil, errors.New("body has no page variants")
		}
	case "header", "footer":
		node = nil
		if hf := doc.Body.HeaderFooter; hf != nil {
			variants := hf.Header
			if region == "footer" {
				variants = hf.Footer
			}
			switch variant {
			case "default":
				node = &variants.Default
			case "first":
				node = variants.First
			case "even":
				node = variants.Even
			}
		}
	default:
		return nil, errors.New("invalid document region")
	}
	if args.Format != "text" {
		return richStructuredRead(doc, node, args, region, variant)
	}
	text := ""
	if node != nil {
		text = richDocumentText(*node)
	}
	chars := []rune(text)
	start := min(args.Offset, len(chars))
	end := min(start+args.Limit, len(chars))
	result := map[string]any{"itemID": doc.ItemID, "revisionID": doc.RevisionID, "contentHash": doc.ContentHash,
		"region": region, "variant": variant, "format": "plain-text", "body": string(chars[start:end]), "offset": start, "totalCharacters": len(chars),
		"page": doc.Body.Page, "pageUnits": "twips", "readOnly": true}
	if end < len(chars) {
		result["nextOffset"] = end
	}
	return result, nil
}

func richDocumentText(node store.RichNode) string {
	switch node.Type {
	case "text":
		return node.Text
	case "image":
		alt, _ := node.Attrs["alt"].(string)
		return "[image: " + alt + "]"
	case "hardBreak", "pageBreak", "horizontalRule":
		return "\n"
	case "pageNumber":
		return "[PAGE]"
	case "totalPages":
		return "[NUMPAGES]"
	}
	separator := "\n"
	switch node.Type {
	case "paragraph", "heading", "codeBlock":
		separator = ""
	case "tableRow":
		separator = "\t"
	}
	parts := make([]string, len(node.Content))
	for i, child := range node.Content {
		parts[i] = richDocumentText(child)
	}
	return strings.Join(parts, separator)
}

func (e *Engine) executeRichDocument(ctx context.Context, sessionID, turnID string, call tool.Call) (any, error) {
	args, err := tool.DecodeRichDocumentArgs(call.Args)
	if err != nil {
		return nil, err
	}
	if call.Name == tool.RichDocumentRead {
		return e.readRichDocument(ctx, args)
	}
	author := store.ContentAuthor{Kind: "session", SessionID: sessionID, TurnID: turnID}
	var doc *store.RichDocumentContent
	if call.Name == tool.RichDocumentCreate {
		name := strings.TrimSpace(args.Name)
		if name == "" || len(name) > 200 {
			return nil, errors.New("name is required (max 200 bytes)")
		}
		id := "rich_" + store.DocumentHash(sessionID + "\x00" + turnID + "\x00" + call.CallID)[:32]
		body, err := store.NewRichBody(args.Content, id)
		if err != nil {
			return nil, err
		}
		now := time.Now().UTC()
		doc, err = e.store.CreateRichDocument(ctx, &store.StudioItem{ID: id, Kind: store.StudioItemKindRichDocument, Name: name, Icon: "file-text", IconColor: "blue", SourceSessionID: sessionID, CreatedAt: now, UpdatedAt: now}, body, nil, author)
		if err != nil {
			return nil, err
		}
		mount, err := e.store.OpenStudioItem(ctx, sessionID, id, store.NewID("mount"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"itemID": id, "mountID": mount.ID, "contentHash": doc.ContentHash, "revisionID": doc.RevisionID}, nil
	}
	if args.ItemID == "" {
		return nil, errors.New("item_id is required")
	}
	doc, err = e.store.WriteRichDocument(ctx, args.ItemID, store.RichDocumentWrite{ClientRequestID: turnID + "/" + call.CallID, ExpectedHash: args.ExpectedHash, Operations: args.Operations, Author: author})
	if err != nil {
		return nil, err
	}
	changed := []string{}
	for _, op := range args.Operations {
		changed = append(changed, op.BlockID)
	}
	return map[string]any{"itemID": doc.ItemID, "contentHash": doc.ContentHash, "revisionID": doc.RevisionID, "changedBlockIDs": changed}, nil
}

func richStructuredRead(doc *store.RichDocumentContent, root *store.RichNode, args tool.RichDocumentArgs, region, variant string) (any, error) {
	type block struct {
		BlockID  string          `json:"blockID,omitempty"`
		ParentID string          `json:"parentID,omitempty"`
		Type     string          `json:"type"`
		Text     string          `json:"text"`
		Node     *store.RichNode `json:"node,omitempty"`
		Editable bool            `json:"editable"`
	}
	all := []block{}
	selected := map[string]bool{}
	for _, id := range args.BlockIDs {
		if selected[id] {
			return nil, errors.New("duplicate block ID")
		}
		selected[id] = true
	}
	var visit func(store.RichNode, string, bool)
	visit = func(n store.RichNode, parent string, top bool) {
		id, _ := n.Attrs["blockId"].(string)
		if id != "" || top {
			include := args.Format == "outline" || top
			if len(selected) > 0 {
				include = selected[id]
			}
			if include {
				text := richDocumentText(n)
				entry := block{BlockID: id, ParentID: parent, Type: n.Type, Text: text, Editable: region == "body" && id != ""}
				if args.Format == "blocks" {
					entry.Node = &n
					entry.Text = ""
				} else if chars := []rune(text); len(chars) > 200 {
					entry.Text = string(chars[:200]) + "…"
				}
				all = append(all, entry)
			}
			if id != "" {
				parent = id
			}
		}
		for _, child := range n.Content {
			visit(child, parent, false)
		}
	}
	if root != nil {
		for _, child := range root.Content {
			visit(child, "", true)
		}
	}
	if len(selected) > 0 && len(all) != len(selected) {
		return nil, errors.New("one or more block IDs are not in this region/version")
	}
	start := min(args.Offset, len(all))
	end := min(start+args.Limit, len(all))
	entries := []block{}
	size := 0
	for i := start; i < end; i++ {
		raw, _ := json.Marshal(all[i])
		if len(raw) > 128<<10 {
			return nil, fmt.Errorf("block %s is too large; read outline and request its child blocks", all[i].BlockID)
		}
		if size+len(raw) > 128<<10 {
			break
		}
		size += len(raw)
		entries = append(entries, all[i])
	}
	result := map[string]any{"itemID": doc.ItemID, "revisionID": doc.RevisionID, "contentHash": doc.ContentHash, "region": region, "variant": variant, "format": args.Format, "offset": start, "offsetUnit": "blocks", "totalBlocks": len(all), "blocks": entries, "page": doc.Body.Page, "pageUnits": "twips", "readOnly": true}
	if start+len(entries) < len(all) {
		result["nextOffset"] = start + len(entries)
	}
	return result, nil
}
