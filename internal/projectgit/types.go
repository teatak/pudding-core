package projectgit

type Repository struct {
	ProjectRoot string
	Root        string
}

type StatusFile struct {
	Path           string `json:"path"`
	OriginalPath   string `json:"originalPath,omitempty"`
	Kind           string `json:"kind"`
	IndexStatus    string `json:"indexStatus"`
	WorktreeStatus string `json:"worktreeStatus"`
}

type Status struct {
	Head       string
	Branch     string
	Upstream   string
	Detached   bool
	Ahead      int
	Behind     int
	Files      []StatusFile
	Staged     int
	Unstaged   int
	Untracked  int
	Conflicted int
}

type DiffFile struct {
	Path         string `json:"path"`
	OriginalPath string `json:"originalPath,omitempty"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	Binary       bool   `json:"binary,omitempty"`
}

type PatchDiff struct {
	Patch     string
	Truncated bool
	Files     []DiffFile
	Additions int
	Deletions int
}

type FileDiff struct {
	Path         string
	OriginalPath string
	Staged       bool
	OldContent   string
	NewContent   string
	Binary       bool
	TooLarge     bool
}
