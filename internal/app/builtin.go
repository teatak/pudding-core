package app

import "strings"

const (
	BuiltinBrowserID  = "browser"
	BuiltinTerminalID = "terminal"
)

type builtinDefinition struct {
	definition *Definition
	skills     map[string]SkillDetail
}

var builtinDefinitions = []builtinDefinition{
	{
		definition: &Definition{
			ID:             BuiltinBrowserID,
			Name:           "Browser",
			Description:    "Browse and operate webpages in Pudding's built-in browser.",
			Source:         SourceBuiltin,
			Enabled:        true,
			CanUninstall:   false,
			RequiredMode:   "work",
			DefaultSkillID: BuiltinBrowserID,
			Skills: []SkillRef{{
				ID:          BuiltinBrowserID,
				Name:        "Browser",
				Description: "Open, inspect, and interact with webpages.",
				Path:        "skills/browser/SKILL.md",
			}},
		},
		skills: map[string]SkillDetail{
			BuiltinBrowserID: {
				ID:          BuiltinBrowserID,
				Name:        "Browser",
				Description: "Open, inspect, and interact with webpages.",
				Path:        "skills/browser/SKILL.md",
				Content: `# Browser

Use Pudding's built-in browser for webpages that require navigation or interaction.

- Check browser status before assuming a tab exists.
- Observe the page before clicking or typing.
- Re-observe after navigation or a significant page change.
- Use web search or fetch instead when no interactive browser state is needed.
`,
			},
		},
	},
	{
		definition: &Definition{
			ID:             BuiltinTerminalID,
			Name:           "Terminal",
			Description:    "Run and manage interactive terminals and background processes.",
			Source:         SourceBuiltin,
			Enabled:        true,
			CanUninstall:   false,
			RequiredMode:   "code",
			DefaultSkillID: BuiltinTerminalID,
			Skills: []SkillRef{{
				ID:          BuiltinTerminalID,
				Name:        "Terminal",
				Description: "Manage long-running commands, output, and process lifecycle.",
				Path:        "skills/terminal/SKILL.md",
			}},
		},
		skills: map[string]SkillDetail{
			BuiltinTerminalID: {
				ID:          BuiltinTerminalID,
				Name:        "Terminal",
				Description: "Manage long-running commands, output, and process lifecycle.",
				Path:        "skills/terminal/SKILL.md",
				Content: `# Terminal

Use Terminal for interactive or long-running commands and background services.

- Use the ordinary command tool for bounded builds, tests, and one-shot commands.
- Start a background process only when the command must outlive one tool call.
- Poll output with offsets and stop processes explicitly when the task no longer needs them.
`,
			},
		},
	},
}

func BuiltinDefinitions() []*Definition {
	out := make([]*Definition, 0, len(builtinDefinitions))
	for _, item := range builtinDefinitions {
		out = append(out, CloneDefinition(item.definition))
	}
	return out
}

func BuiltinDefinition(id string) (*Definition, bool) {
	id = strings.TrimSpace(id)
	for _, item := range builtinDefinitions {
		if item.definition.ID == id {
			return CloneDefinition(item.definition), true
		}
	}
	return nil, false
}

func IsBuiltinID(id string) bool {
	_, ok := BuiltinDefinition(id)
	return ok
}

func ReadBuiltinSkill(appID, selector string) (*SkillDetail, bool) {
	appID = strings.TrimSpace(appID)
	selector = strings.TrimSpace(selector)
	for _, item := range builtinDefinitions {
		if item.definition.ID != appID {
			continue
		}
		for id, detail := range item.skills {
			if selector == id || selector == detail.Name || selector == detail.Path {
				out := detail
				return &out, true
			}
		}
		return nil, false
	}
	return nil, false
}
