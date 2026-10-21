package store

import "strings"

// CurrentPluginID projects saved identities and legacy inputs to one catalog ID.
// Keep these aliases for six calendar months after the first stable release of
// this rename; the release/retirement checklist is in docs/plugin-ids.md.
// This is an identity projection, not authorization to load a plugin.
func CurrentPluginID(id string) string {
	id = strings.TrimSpace(id)
	switch id {
	case "studio", "artifacts":
		return "artifact"
	case "markdown", "note-authoring":
		return "page-authoring"
	case "tables":
		return "sheet-authoring"
	case "collaboration":
		return "subagent"
	case "web":
		return "web-search-fetch"
	default:
		return id
	}
}

// CurrentPluginSkillID projects only the matching built-in's default skill ID
// or registered path. A global skill or a different plugin's skill is not renamed.
func CurrentPluginSkillID(pluginID, selector string) string {
	pluginID, selector = CurrentPluginID(pluginID), strings.TrimSpace(selector)
	switch pluginID {
	case "artifact", "page-authoring", "sheet-authoring", "subagent", "web-search-fetch":
	default:
		return selector
	}
	id := selector
	path := strings.HasPrefix(id, "skills/") && strings.HasSuffix(id, "/SKILL.md")
	if path {
		id = strings.TrimSuffix(strings.TrimPrefix(id, "skills/"), "/SKILL.md")
	}
	if CurrentPluginID(id) != pluginID {
		return selector
	}
	if path {
		return "skills/" + pluginID + "/SKILL.md"
	}
	return pluginID
}
