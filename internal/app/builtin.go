package app

import "strings"

const (
	BuiltinBrowserID        = "browser"
	BuiltinSkillAuthoringID = "skill-authoring"
	BuiltinAppAuthoringID   = "app-authoring"
	BuiltinCaptureID        = "capture"
)

const (
	toolBrowserStatus     = "builtin_browser_status"
	toolBrowserOpen       = "builtin_browser_open"
	toolBrowserObserve    = "builtin_browser_observe"
	toolBrowserScreenshot = "builtin_browser_screenshot"
	toolBrowserBack       = "builtin_browser_back"
	toolBrowserForward    = "builtin_browser_forward"
	toolBrowserReload     = "builtin_browser_reload"
	toolBrowserClose      = "builtin_browser_close"
	toolBrowserClick      = "builtin_browser_click"
	toolBrowserType       = "builtin_browser_type"
	toolBrowserScroll     = "builtin_browser_scroll"
	toolSkillValidate     = "builtin_skill_validate"
	toolAppSave           = "builtin_app_save"
	toolCameraCapture     = "builtin_camera_capture"
	toolDesktopScreenshot = "builtin_desktop_screenshot"
)

type builtinDefinition struct {
	definition *Definition
	skills     map[string]SkillDetail
}

var builtinDefinitions = []builtinDefinition{
	{
		definition: &Definition{
			Kind:           KindApp,
			ID:             BuiltinBrowserID,
			Name:           "Browser",
			Description:    "Browse and operate webpages in Pudding's built-in browser.",
			Source:         SourceBuiltin,
			Enabled:        true,
			CanUninstall:   false,
			RequiredMode:   "work",
			DefaultSkillID: BuiltinBrowserID,
			Tools: []ToolRef{
				{Name: toolBrowserStatus},
				{Name: toolBrowserOpen},
				{Name: toolBrowserObserve},
				{Name: toolBrowserScreenshot},
				{Name: toolBrowserBack},
				{Name: toolBrowserForward},
				{Name: toolBrowserReload},
				{Name: toolBrowserClose},
				{Name: toolBrowserClick},
				{Name: toolBrowserType},
				{Name: toolBrowserScroll},
			},
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
			Kind:           KindApp,
			ID:             BuiltinSkillAuthoringID,
			Name:           "Skill Authoring",
			Description:    "Create, update, and validate reusable global Skills.",
			Source:         SourceBuiltin,
			Enabled:        true,
			CanUninstall:   false,
			RequiredMode:   "code",
			DefaultSkillID: "skill-creator",
			Tools:          []ToolRef{{Name: toolSkillValidate}},
			Skills: []SkillRef{{
				ID:          "skill-creator",
				Name:        "Skill Creator",
				Description: "Create or update reusable global Skills.",
				Path:        "skills/skill-creator/SKILL.md",
			}},
		},
		skills: map[string]SkillDetail{
			"skill-creator": {
				ID:          "skill-creator",
				Name:        "Skill Creator",
				Description: "Create or update reusable global Skills.",
				Path:        "skills/skill-creator/SKILL.md",
				Content:     builtinSkillAuthoringInstructions,
			},
		},
	},
	{
		definition: &Definition{
			Kind:           KindApp,
			ID:             BuiltinAppAuthoringID,
			Name:           "App Authoring",
			Description:    "Create or update validated local Pudding App packages.",
			Source:         SourceBuiltin,
			Enabled:        true,
			CanUninstall:   false,
			RequiredMode:   "code",
			DefaultSkillID: "app-creator",
			Tools:          []ToolRef{{Name: toolAppSave}},
			Skills: []SkillRef{{
				ID:          "app-creator",
				Name:        "App Creator",
				Description: "Create or update a local Pudding App package.",
				Path:        "skills/app-creator/SKILL.md",
			}},
		},
		skills: map[string]SkillDetail{
			"app-creator": {
				ID:          "app-creator",
				Name:        "App Creator",
				Description: "Create or update a local Pudding App package.",
				Path:        "skills/app-creator/SKILL.md",
				Content:     builtinAppAuthoringInstructions,
			},
		},
	},
	{
		definition: &Definition{
			Kind:         KindApp,
			ID:           BuiltinCaptureID,
			Name:         "Image Capture",
			Description:  "Capture images from the local screen or camera when explicitly requested.",
			Source:       SourceBuiltin,
			Enabled:      true,
			CanUninstall: false,
			RequiredMode: "chat",
			Tools: []ToolRef{
				{Name: toolDesktopScreenshot},
				{Name: toolCameraCapture},
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

func IsReservedID(id string) bool {
	id = strings.TrimSpace(id)
	return IsBuiltinID(id) || id == RuntimeCanvasID
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
