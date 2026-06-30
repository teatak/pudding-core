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

type Draft struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Path        string     `json:"path"`
	IconPath    string     `json:"iconPath,omitempty"`
	Change      string     `json:"change"`
	Validation  Validation `json:"validation"`
}

type DraftDetail struct {
	Draft Draft      `json:"draft"`
	Files []FileDiff `json:"files"`
}

type FileDiff struct {
	Path        string `json:"path"`
	Change      string `json:"change"`
	Old         string `json:"old,omitempty"`
	New         string `json:"new,omitempty"`
	UnifiedDiff string `json:"unifiedDiff,omitempty"`
}

type Validation struct {
	OK       bool     `json:"ok"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

func cloneSkill(in Skill) Skill {
	return in
}
