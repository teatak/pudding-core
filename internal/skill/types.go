package skill

const (
	ScopeGlobal = "global"

	SourceBuiltin = "builtin"
	SourceUser    = "user"

	SystemSubdir = ".system"
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

func cloneSkill(in Skill) Skill {
	return in
}
