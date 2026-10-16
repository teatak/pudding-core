package tool

// NormalizeLegacyNoteTool accepts only the three doc names shipped in Desktop v0.5.0.
// Catalogs advertise note names only; aliases use the same permission checks.
// Documents owns document names. Unreleased rich_document names have no aliases.
//
// Retirement: six calendar months after the first stable desktop release that
// contains builtin_note_*. At that release, record its version, release date
// and remove-after date in docs/studio.md (Tool names and legacy doc retirement).
// Remove this map, its execution/ownership/history callers and the desktop
// noteToolCompat.ts callers/tests in the first maintenance release on/after
// that date. Do not use a runtime date gate or rewrite stored history.
// Release version/date: pending first stable release of this change.
func NormalizeLegacyNoteTool(name string) string {
	switch name {
	case "builtin_doc_create":
		return NoteCreate
	case "builtin_doc_read":
		return NoteRead
	case "builtin_doc_edit":
		return NoteEdit
	}
	return name
}
