package app

import _ "embed"

//go:embed embed/skill-authoring/SKILL.md
var builtinSkillAuthoringInstructions string

//go:embed embed/app-authoring/SKILL.md
var builtinAppAuthoringInstructions string
