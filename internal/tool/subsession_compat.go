package tool

// HistoricalSubsessionToolName translates only names that were actually shipped.
// This is history/display compatibility, NOT registration of executable aliases.
//
// Retirement: six calendar months after the first stable desktop release that
// contains builtin_subsession_*. At that release, record its version, release date
// and remove-after date in docs/session-collaboration-plan.md. Remove this map,
// its caller and the corresponding desktop history-display branches in the
// first maintenance release on/after that date; do not use a runtime date gate.
// First stable release: Desktop 0.8.4 (2026-10-31); remove after 2027-04-30.
func HistoricalSubsessionToolName(name string) string {
	switch name {
	case "builtin_subagent_list", "builtin_subtask_list", "builtin_collaboration_list":
		return "builtin_subsession_list"
	case "builtin_subagent_dispatch", "builtin_subtask_dispatch", "builtin_collaboration_dispatch":
		return "builtin_subsession_dispatch"
	case "builtin_subagent_send", "builtin_subtask_send", "builtin_collaboration_send":
		return "builtin_subsession_send"
	case "builtin_subagent_wait", "builtin_subtask_wait", "builtin_collaboration_wait":
		return "builtin_subsession_wait"
	case "builtin_subagent_stop", "builtin_subtask_stop", "builtin_collaboration_stop":
		return "builtin_subsession_stop"
	case "builtin_subagent_archive", "builtin_subtask_archive":
		return "builtin_subsession_archive"
	case "builtin_subagent_restore", "builtin_subtask_restore":
		return "builtin_subsession_restore"
	case "builtin_subagent_reset":
		return "builtin_subsession_reset"
	default:
		return name
	}
}
