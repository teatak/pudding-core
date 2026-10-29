package tool

const (
	patchRecoveryMaxCandidates = 5
	patchRecoveryContextLines  = 5
	patchRecoveryLineBytes     = 1024
)

// Candidate matching is shared by recovery diagnostics and explicitly opted-in
// relocation. Only a unique exact non-empty block can be relocated.
type patchHunkRecovery struct {
	Path                string                     `json:"path"`
	Hunk                int                        `json:"hunk"` // One-based, within this file.
	StartLine           int                        `json:"startLine"`
	FileLineCount       int                        `json:"fileLineCount"`
	ExactMatchCount     int                        `json:"exactMatchCount"`
	Candidates          []patchRecoveryCandidate   `json:"candidates"`
	CandidatesTruncated bool                       `json:"candidatesTruncated,omitempty"`
	Mismatch            *patchHunkMismatch         `json:"mismatch,omitempty"`
	Context             []patchRecoveryContextLine `json:"context"`
}

type patchRecoveryCandidate struct {
	StartLine int                        `json:"startLine"`
	Context   []patchRecoveryContextLine `json:"context"`
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

func newPatchHunkError(reason, detail, path string, index int, hunk patchHunkArg, lines []patchTextLine, mismatchOffset int) *patchError {
	oldLines := *hunk.OldLines
	candidates, count := patchExactCandidateLines(lines, oldLines)
	recovery := &patchHunkRecovery{
		Path:                path,
		Hunk:                index + 1,
		StartLine:           hunk.StartLine,
		FileLineCount:       len(lines),
		Candidates:          make([]patchRecoveryCandidate, 0, len(candidates)),
		ExactMatchCount:     count,
		CandidatesTruncated: count > len(candidates),
	}
	for _, start := range candidates {
		recovery.Candidates = append(recovery.Candidates, patchRecoveryCandidate{StartLine: start, Context: patchRecoveryContext(lines, start-1)})
	}
	center := hunk.StartLine - 1
	if mismatchOffset >= 0 {
		center += mismatchOffset
		mismatch := &patchHunkMismatch{Line: center + 1}
		mismatch.Expected, mismatch.ExpectedTruncated = truncateUTF8Bytes(oldLines[mismatchOffset], patchRecoveryLineBytes)
		mismatch.Actual, mismatch.ActualTruncated = truncateUTF8Bytes(lines[center].text, patchRecoveryLineBytes)
		recovery.Mismatch = mismatch
	}
	recovery.Context = patchRecoveryContext(lines, center)
	return &patchError{reason: reason, detail: detail, recovery: recovery}
}

func patchRecoveryContext(lines []patchTextLine, center int) []patchRecoveryContextLine {
	context := make([]patchRecoveryContextLine, 0, patchRecoveryContextLines)
	center = max(0, min(center, len(lines)-1))
	end := min(len(lines), max(0, center-2)+patchRecoveryContextLines)
	for i := max(0, end-patchRecoveryContextLines); i < end; i++ {
		text, truncated := truncateUTF8Bytes(lines[i].text, patchRecoveryLineBytes)
		context = append(context, patchRecoveryContextLine{Line: i + 1, Text: text, Truncated: truncated})
	}
	return context
}

func (r *patchHunkRecovery) hint() string {
	if r.ExactMatchCount == 1 && !r.CandidatesTruncated && len(r.Candidates) == 1 {
		complete := true
		for _, line := range r.Candidates[0].Context {
			complete = complete && !line.Truncated
		}
		if complete {
			return "No files were changed. One exact old_lines block was found. If the candidate context identifies the intended target, retry with its startLine or set relocate=true on this hunk; no extra read is needed. Keep the complete old_lines you already supplied. Otherwise read a focused builtin_file_slice with order=natural."
		}
	}
	matchHint := "No exact non-empty old_lines block was found."
	if r.ExactMatchCount == 1 {
		matchHint = "One exact old_lines block was found, but its diagnostic context is truncated."
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
