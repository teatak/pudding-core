package tool

import (
	"encoding/json"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

const (
	ArtifactList = "builtin_artifact_list"
	ArtifactOpen = "builtin_artifact_open"
	StudioList   = "builtin_studio_list"
	StudioOpen   = "builtin_studio_open"
)

func NormalizeStudioTool(name string) string {
	switch name {
	case StudioList:
		return ArtifactList
	case StudioOpen:
		return ArtifactOpen
	}
	return NormalizeLegacyNoteTool(name)
}

func IsStudioTool(name string) bool {
	switch NormalizeLegacyNoteTool(name) {
	case ArtifactList, ArtifactOpen, NoteCreate, NoteRead, DocumentRead, DocumentCreate, DocumentEdit, NoteEdit, TableCreate, TableRead, TableUpdate:
		return true
	}
	return false
}

func StudioDefinitions() []provider.ToolDef {
	return append([]provider.ToolDef{
		{Name: ArtifactList, Capability: store.ModeChat, Description: "List global artifacts: Notes (note), Documents (document), tables and widgets, with their names, IDs, kinds and update times. Content is independent of projects.", InputSchema: json.RawMessage(`{"type":"object","properties":{"kind":{"type":"string","enum":["note","document","table","widget"]}},"additionalProperties":false}`)},
		{Name: ArtifactOpen, Capability: store.ModeChat, Description: "Open existing artifact content in this conversation. Returns a session mount referring to the same global item; it does not copy the content.", InputSchema: json.RawMessage(`{"type":"object","properties":{"item_id":{"type":"string"}},"required":["item_id"],"additionalProperties":false}`)},
	}, append(NoteDefinitions(), append(TableDefinitions(), DocumentDefinitions()...)...)...)
}

type StudioArgs struct {
	Action       string                 `json:"action"`
	OperationID  string                 `json:"operation_id"`
	LeaseToken   string                 `json:"lease_token"`
	Target       string                 `json:"target"`
	From         int                    `json:"from"`
	To           int                    `json:"to"`
	Replacement  *string                `json:"replacement"`
	Table        *store.TableBody       `json:"table"`
	Operations   []store.TableOperation `json:"operations"`
	RowIDs       []string               `json:"row_ids"`
	ColumnIDs    []string               `json:"column_ids"`
	Format       string                 `json:"format"`
	RevisionID   string                 `json:"revision_id"`
	Region       string                 `json:"region"`
	Variant      string                 `json:"variant"`
	ItemID       string                 `json:"item_id"`
	Kind         string                 `json:"kind"`
	Name         string                 `json:"name"`
	Body         *string                `json:"body"`
	Edits        []store.NoteEdit       `json:"edits"`
	ExpectedHash *string                `json:"expected_hash"`
	Offset       int                    `json:"offset"`
	Limit        int                    `json:"limit"`
}

func DecodeStudioArgs(raw json.RawMessage) (StudioArgs, error) {
	var args StudioArgs
	err := decodeStructToolArgs(raw, &args)
	return args, err
}
