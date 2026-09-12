package tool

const (
	patchRecoveryMaxCandidates = 5
	patchRecoveryContextLines  = 5
	patchRecoveryLineBytes     = 1024
)

// Recovery is diagnostic only. applyPatchHunks never uses candidates to relocate
// a hunk; the caller must confirm the intended target and submit a new patch.
type patchHunkRecovery struct {
	Path                string                     `json:"path"`
	Hunk                int                        `json:"hunk"` // One-based, within this file.
	StartLine           int                        `json:"startLine"`
	FileLineCount       int                        `json:"fileLineCount"`
	ExactMatchCount     int                        `json:"exactMatchCount"`
	CandidateStartLines []int                      `json:"candidateStartLines"`
	CandidatesTruncated bool                       `json:"candidatesTruncated,omitempty"`
	Mismatch            *patchHunkMismatch         `json:"mismatch,omitempty"`
	Context             []patchRecoveryContextLine `json:"context"`
}

type patchHunkMismatch struct {
	Line              int    `json:"line"`
	Expected          string `json:"expected"`
	Actual            string `json:"actual"`
	ExpectedTruncated bool   `json:"expectedTruncated,omitempty"`
	ActualTruncated   bool   `json:"actualTruncated,omitempty"`
}

type patchRecoveryContextLine struct {
	Line      int    `json:"line"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}

func newPatchHunkError(reason, detail, path string, index int, hunk patchHunkArg, lines []patchTextLine, mismatchOffset int) error {
	oldLines := *hunk.OldLines
	candidates, count := patchExactCandidateLines(lines, oldLines)
	recovery := &patchHunkRecovery{
		Path:                path,
		Hunk:                index + 1,
		StartLine:           hunk.StartLine,
		FileLineCount:       len(lines),
		CandidateStartLines: candidates,
		ExactMatchCount:     count,
		CandidatesTruncated: count > len(candidates),
		Context:             make([]patchRecoveryContextLine, 0, patchRecoveryContextLines),
	}
	center := hunk.StartLine - 1
	if mismatchOffset >= 0 {
		center += mismatchOffset
		mismatch := &patchHunkMismatch{Line: center + 1}
		mismatch.Expected, mismatch.ExpectedTruncated = truncateUTF8Bytes(oldLines[mismatchOffset], patchRecoveryLineBytes)
		mismatch.Actual, mismatch.ActualTruncated = truncateUTF8Bytes(lines[center].text, patchRecoveryLineBytes)
		recovery.Mismatch = mismatch
	}
	center = max(0, min(center, len(lines)-1))
	end := min(len(lines), max(0, center-2)+patchRecoveryContextLines)
	for i := max(0, end-patchRecoveryContextLines); i < end; i++ {
		text, truncated := truncateUTF8Bytes(lines[i].text, patchRecoveryLineBytes)
		recovery.Context = append(recovery.Context, patchRecoveryContextLine{Line: i + 1, Text: text, Truncated: truncated})
	}
	return &patchError{reason: reason, detail: detail, recovery: recovery}
}

func (r *patchHunkRecovery) hint() string {
	matchHint := "No exact non-empty old_lines block was found."
	if r.ExactMatchCount == 1 {
		matchHint = "One exact old_lines block was found; its position is a suggestion, not an automatic relocation."
	} else if r.ExactMatchCount > 1 {
		matchHint = "Multiple exact old_lines blocks were found; use surrounding context to disambiguate, not proximity alone."
	}
	return "No files were changed. " + matchHint + " Read a fresh builtin_file_slice with order=natural around the intended target, then rebuild hunks against the current pre-patch file. Do not copy truncated diagnostic text into old_lines."
}

func patchExactCandidateLines(lines []patchTextLine, oldLines []string) ([]int, int) {
	candidates := make([]int, 0, patchRecoveryMaxCandidates)
	if len(oldLines) == 0 || len(oldLines) > len(lines) {
		return candidates, 0
	}
	// KMP over whole lines bounds the scan even for large, repetitive files.
	// Equality is identical to the apply path, including whitespace and blank lines.
	prefix := make([]int, len(oldLines))
	for i, matched := 1, 0; i < len(oldLines); i++ {
		for matched > 0 && oldLines[i] != oldLines[matched] {
			matched = prefix[matched-1]
		}
		if oldLines[i] == oldLines[matched] {
			matched++
		}
		prefix[i] = matched
	}
	count, matched := 0, 0
	for i, line := range lines {
		for matched > 0 && line.text != oldLines[matched] {
			matched = prefix[matched-1]
		}
		if line.text == oldLines[matched] {
			matched++
		}
		if matched == len(oldLines) {
			count++
			if len(candidates) < patchRecoveryMaxCandidates {
				candidates = append(candidates, i-len(oldLines)+2)
			}
			matched = prefix[matched-1]
		}
	}
	return candidates, count
}
