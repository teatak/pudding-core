package tool

// NormalizeLegacyPageTool projects the exact doc/note compatibility names to
// Page tools. Catalogs advertise builtin_page_* only, including rename; aliases
// pass through the same loading, ownership and capability checks. Unreleased
// rich_document and structured document names are not Markdown Page aliases.
//
// Retirement: six calendar months after the first stable Desktop release that
// contains the Page rename. Record its version, release date and remove-after
// date in docs/plugin-ids.md and docs/studio.md at release time. Retire executable
// aliases and alias-only callers/tests together; do not add a runtime date gate
// or rewrite original tool history, content hashes or retry fingerprints.
// Release version/date: pending first stable release of this change.
func NormalizeLegacyPageTool(name string) string {
	switch name {
	case "builtin_doc_create", "builtin_note_create":
		return PageCreate
	case "builtin_doc_read", "builtin_note_read":
		return PageRead
	case "builtin_doc_edit", "builtin_note_edit":
		return PageEdit
	case "builtin_doc_rename", "builtin_note_rename":
		return PageRename
	}
	return name
}
