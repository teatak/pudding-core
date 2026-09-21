package store

import "strings"

// PrepareChildSession validates the creation boundary without granting any
// capability. The child's project starts at the parent's current project;
// provider/model and available capabilities remain explicit session settings.
func PrepareChildSession(parent, child *Session) error {
	if parent == nil || child == nil || parent.ArchivedAt != nil ||
		strings.TrimSpace(child.ID) == "" || child.ID != strings.TrimSpace(child.ID) ||
		child.ID == parent.ID || child.Pinned || child.ArchivedAt != nil ||
		(child.ProjectID != "" && child.ProjectID != parent.ProjectID) {
		return ErrInvalidSessionRelation
	}
	child.ProjectID = parent.ProjectID
	if err := NormalizeSessionProviderModel(child); err != nil {
		return err
	}
	child.PinnedOrder = 0
	return nil
}
