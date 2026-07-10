package tool

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/lsp"
)

type codeLocation struct {
	Path         string `json:"path"`
	RelativePath string `json:"relativePath"`
	Line         int    `json:"line"`
	Column       int    `json:"column"`
	EndLine      int    `json:"endLine"`
	EndColumn    int    `json:"endColumn"`
	LineStart    int    `json:"lineStart"`
	LineEnd      int    `json:"lineEnd"`
	Excerpt      string `json:"excerpt,omitempty"`
}

type rawLSPLocation struct {
	URI   string    `json:"uri"`
	Range lsp.Range `json:"range"`
}

type rawLSPLocationLink struct {
	TargetURI            string    `json:"targetUri"`
	TargetRange          lsp.Range `json:"targetRange"`
	TargetSelectionRange lsp.Range `json:"targetSelectionRange"`
}

type codeLocationConverter struct {
	projectDirs []string
	encoding    string
	files       map[string][]string
}

func newCodeLocationConverter(projectDirs []string, encoding string) *codeLocationConverter {
	canonicalDirs := make([]string, 0, len(projectDirs))
	for _, root := range normalizeProjectDirs(projectDirs) {
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			canonicalDirs = append(canonicalDirs, resolved)
			continue
		}
		canonicalDirs = append(canonicalDirs, root)
	}
	return &codeLocationConverter{projectDirs: canonicalDirs, encoding: encoding, files: map[string][]string{}}
}

func (c *codeLocationConverter) convert(raw rawLSPLocation) (codeLocation, bool) {
	path, ok := codePathFromURI(raw.URI)
	if !ok {
		return codeLocation{}, false
	}
	_, resolved, relative, err := resolveProjectPath(c.projectDirs, path, true, false)
	if err != nil {
		return codeLocation{}, false
	}
	lines := c.lines(resolved)
	line := raw.Range.Start.Line + 1
	endLine := raw.Range.End.Line + 1
	column := raw.Range.Start.Character + 1
	endColumn := raw.Range.End.Character + 1
	if len(lines) > 0 {
		column = lspCharacterToColumn(lineText(lines, raw.Range.Start.Line), raw.Range.Start.Character, c.encoding)
		endColumn = lspCharacterToColumn(lineText(lines, raw.Range.End.Line), raw.Range.End.Character, c.encoding)
	}
	lineStart, lineEnd, excerpt := codeExcerpt(lines, line)
	return codeLocation{
		Path:         resolved,
		RelativePath: relative,
		Line:         max(1, line),
		Column:       max(1, column),
		EndLine:      max(1, endLine),
		EndColumn:    max(1, endColumn),
		LineStart:    lineStart,
		LineEnd:      lineEnd,
		Excerpt:      excerpt,
	}, true
}

func (c *codeLocationConverter) lines(path string) []string {
	if lines, ok := c.files[path]; ok {
		return lines
	}
	_, lines, err := readCodeDocument(path)
	if err != nil {
		c.files[path] = nil
		return nil
	}
	c.files[path] = lines
	return lines
}

func codePosition(lines []string, line, column int, encoding string) (lsp.Position, error) {
	if line < 1 || line > len(lines) {
		return lsp.Position{}, fmt.Errorf("line must be between 1 and %d", len(lines))
	}
	text := strings.TrimSuffix(lines[line-1], "\r")
	runes := []rune(text)
	if column < 1 || column > len(runes)+1 {
		return lsp.Position{}, fmt.Errorf("column must be between 1 and %d", len(runes)+1)
	}
	prefix := string(runes[:column-1])
	character := len([]rune(prefix))
	switch strings.ToLower(encoding) {
	case "utf-8":
		character = len([]byte(prefix))
	case "utf-16", "":
		character = len(utf16.Encode([]rune(prefix)))
	case "utf-32":
		character = len([]rune(prefix))
	}
	return lsp.Position{Line: line - 1, Character: character}, nil
}

func lspCharacterToColumn(line string, character int, encoding string) int {
	line = strings.TrimSuffix(line, "\r")
	if character <= 0 {
		return 1
	}
	switch strings.ToLower(encoding) {
	case "utf-8":
		if character > len(line) {
			character = len(line)
		}
		for character > 0 && character < len(line) && !utf8.RuneStart(line[character]) {
			character--
		}
		return utf8.RuneCountInString(line[:character]) + 1
	case "utf-32":
		return min(character, utf8.RuneCountInString(line)) + 1
	default:
		units := 0
		columns := 0
		for _, r := range line {
			width := 1
			if r > 0xFFFF {
				width = 2
			}
			if units+width > character {
				break
			}
			units += width
			columns++
		}
		return columns + 1
	}
}

func codeExcerpt(lines []string, line int) (int, int, string) {
	if len(lines) == 0 || line < 1 {
		return max(1, line), max(1, line), ""
	}
	start := max(1, line-2)
	end := min(len(lines), line+2)
	out := make([]string, 0, end-start+1)
	for _, item := range lines[start-1 : end] {
		out = append(out, truncateDiagnosticText(strings.TrimSuffix(item, "\r"), maxFileSearchExcerptLineChars))
	}
	return start, end, strings.Join(out, "\n")
}

func lineText(lines []string, index int) string {
	if index < 0 || index >= len(lines) {
		return ""
	}
	return lines[index]
}

func parseLocationResponse(raw json.RawMessage) ([]rawLSPLocation, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	items := []json.RawMessage{raw}
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
	}
	locations := make([]rawLSPLocation, 0, len(items))
	for _, item := range items {
		var probe struct {
			URI       string `json:"uri"`
			TargetURI string `json:"targetUri"`
		}
		if err := json.Unmarshal(item, &probe); err != nil {
			return nil, err
		}
		if probe.TargetURI != "" {
			var link rawLSPLocationLink
			if err := json.Unmarshal(item, &link); err != nil {
				return nil, err
			}
			rangeValue := link.TargetSelectionRange
			if rangeValue == (lsp.Range{}) {
				rangeValue = link.TargetRange
			}
			locations = append(locations, rawLSPLocation{URI: link.TargetURI, Range: rangeValue})
			continue
		}
		var location rawLSPLocation
		if err := json.Unmarshal(item, &location); err != nil {
			return nil, err
		}
		if location.URI != "" {
			locations = append(locations, location)
		}
	}
	return locations, nil
}

func sortAndDedupeLocations(locations []codeLocation) []codeLocation {
	sort.Slice(locations, func(i, j int) bool {
		left, right := locations[i], locations[j]
		if left.RelativePath != right.RelativePath {
			return left.RelativePath < right.RelativePath
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		return left.Column < right.Column
	})
	out := locations[:0]
	seen := map[string]bool{}
	for _, location := range locations {
		key := location.Path + ":" + strconv.Itoa(location.Line) + ":" + strconv.Itoa(location.Column)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, location)
	}
	return out
}

func diagnosticCode(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return ""
}
