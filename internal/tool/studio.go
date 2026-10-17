package tool

import (
	"encoding/json"
	"errors"
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
	return NormalizeLegacyContentTool(name)
}

func IsStudioTool(name string) bool {
	switch NormalizeLegacyContentTool(name) {
	case ArtifactList, ArtifactOpen, NoteCreate, NoteRead, NoteEdit, SheetCreate, SheetRead, SheetEdit:
		return true
	}
	return false
}

func StudioDefinitions() []provider.ToolDef {
	return append([]provider.ToolDef{
		{Name: ArtifactList, Capability: store.ModeChat, Description: "List global artifacts: Notes (note), tables and widgets, with their names, IDs, kinds and update times. Content is independent of projects.", InputSchema: json.RawMessage(`{"type":"object","properties":{"kind":{"type":"string","enum":["note","sheet","widget"]}},"additionalProperties":false}`)},
		{Name: ArtifactOpen, Capability: store.ModeChat, Description: "Open existing artifact content in this conversation. Returns a session mount referring to the same global item; it does not copy the content.", InputSchema: json.RawMessage(`{"type":"object","properties":{"item_id":{"type":"string"}},"required":["item_id"],"additionalProperties":false}`)},
	}, append(NoteDefinitions(), SheetDefinitions()...)...)
}

type StudioArgs struct {
	Action       string                 `json:"action"`
	OperationID  string                 `json:"operation_id"`
	LeaseToken   string                 `json:"lease_token"`
	Target       string                 `json:"target"`
	From         int                    `json:"from"`
	To           int                    `json:"to"`
	Replacement  *string                `json:"replacement"`
	Sheet        *store.SheetBody       `json:"sheet"`
	LegacyTable  *store.SheetBody       `json:"table"`
	Operations   []store.SheetOperation `json:"operations"`
	RowIDs       []string               `json:"row_ids"`
	ColumnIDs    []string               `json:"column_ids"`
	Format       string                 `json:"format"`
	RevisionID   string                 `json:"revision_id"`
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
	if err != nil {
		return args, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return args, err
	}
	_, current := fields["sheet"]
	_, legacy := fields["table"]
	if current && legacy {
		return args, errors.New("provide sheet only; table is a legacy alias")
	}
	if legacy {
		args.Sheet = args.LegacyTable
		args.LegacyTable = nil
	}
	return args, nil
}
