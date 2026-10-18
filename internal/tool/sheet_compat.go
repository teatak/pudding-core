package tool

import "encoding/json"

// NormalizeLegacyContentTool accepts exact retired content tools without adding
// duplicate catalog definitions. All aliases use canonical permission checks.
// Sheet aliases retire six calendar months after the first stable release of
// builtin_sheet_*; record the release and removal dates in docs/sheets.md.
// Never use a runtime date gate or rewrite stored messages.
func NormalizeLegacyContentTool(name string) string {
	switch name {
	case "builtin_table_create":
		return SheetCreate
	case "builtin_table_read":
		return SheetRead
	case "builtin_table_update":
		return SheetEdit
	default:
		return NormalizeLegacyPageTool(name)
	}
}

// CurrentSheetToolArgs projects the legacy create payload for model context.
// It never mutates raw, guesses conflicting values, or touches document tables.
func CurrentSheetToolArgs(name string, raw json.RawMessage) json.RawMessage {
	if NormalizeLegacyContentTool(name) != SheetCreate {
		return raw
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return raw
	}
	value, legacy := fields["table"]
	if _, current := fields["sheet"]; !legacy || current {
		return raw
	}
	delete(fields, "table")
	fields["sheet"] = value
	projected, err := json.Marshal(fields)
	if err != nil {
		return raw
	}
	return projected
}
