package store

// SubagentArchiveStatusEligible describes terminal execution, not successful
// verification. Callers must separately check running/queued work, approvals,
// unanswered questions, background processes and the expected result version.
func SubagentArchiveStatusEligible(status string) bool {
	return SubagentSettledStatus(status)
}
