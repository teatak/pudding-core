package skill

const (
	ScopeGlobal = "global"

	SourceBuiltin = "builtin"
	SourceUser    = "user"

	BuiltinSubdir = "builtin"
)

type Skill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Scope       string `json:"scope"`
	Source      string `json:"source"`
	System      bool   `json:"system"`
	Path        string `json:"path,omitempty"`
	IconPath    string `json:"iconPath,omitempty"`
}

type Document struct {
	Skill
	Content string `json:"content"`
}

type Validation struct {
	OK       bool     `json:"ok"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

func cloneSkill(in Skill) Skill {
	return in
}
