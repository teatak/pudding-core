package store

// SessionPluginConnection is a user selection, never inferred from model output.
// Identity prevents a deleted/recreated connection or changed account from
// inheriting the previous account's session selection.
type SessionPluginConnection struct {
	ConnectionID string
	Identity     string
}
