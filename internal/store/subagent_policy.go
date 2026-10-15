package store

import "errors"

// SubagentTotalLimit counts every child owned by one parent, including archived,
// failed and cancelled children. Archiving only changes presentation; it never
// frees an identity slot. Existing over-limit groups are retained unchanged.
const SubagentTotalLimit = 24

var ErrSubagentTotalLimit = errors.New("subagent total limit reached (24 per parent conversation, including archived children); list with include_archived, restore or reuse a suitable existing child, or continue in the main conversation; archiving does not free a slot")

// SubagentArchiveStatusEligible describes terminal execution, not successful
// verification. Callers must separately check running/queued work, approvals,
// unanswered questions, background processes and the expected result version.
func SubagentArchiveStatusEligible(status string) bool {
	switch TurnStatus(status) {
	case TurnCompleted, TurnFailed, TurnCancelled:
		return true
	default:
		return false
	}
}
