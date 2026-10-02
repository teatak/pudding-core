package plugin

import _ "embed"

//go:embed embed/skill-authoring/SKILL.md
var builtinSkillAuthoringInstructions string

//go:embed embed/plugin-authoring/SKILL.md
var builtinPluginAuthoringInstructions string
