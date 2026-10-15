package tool

// HistoricalSubagentToolName translates only names that were actually shipped.
// This is history/display compatibility, NOT registration of executable aliases.
//
// Retirement: six calendar months after the first stable desktop release that
// contains builtin_subagent_*. At that release, record its version, release date
// and remove-after date in docs/session-collaboration-plan.md. Remove this map,
// its caller and the corresponding desktop history-display branches in the
// first maintenance release on/after that date; do not use a runtime date gate.
// Release version/date: pending first stable release of this change.
func HistoricalSubagentToolName(name string) string {
	switch name {
	case "builtin_subtask_list", "builtin_collaboration_list":
		return "builtin_subagent_list"
	case "builtin_subtask_dispatch", "builtin_collaboration_dispatch":
		return "builtin_subagent_dispatch"
	case "builtin_subtask_send", "builtin_collaboration_send":
		return "builtin_subagent_send"
	case "builtin_subtask_wait", "builtin_collaboration_wait":
		return "builtin_subagent_wait"
	case "builtin_subtask_stop", "builtin_collaboration_stop":
		return "builtin_subagent_stop"
	case "builtin_subtask_archive":
		return "builtin_subagent_archive"
	case "builtin_subtask_restore":
		return "builtin_subagent_restore"
	default:
		return name
	}
}
