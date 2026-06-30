package skill

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	DraftDirName        = "skills-draft"
	DraftDeleteFileName = ".delete"

	DraftChangeAdded    = "added"
	DraftChangeModified = "modified"
)

var ErrInvalidDraft = errors.New("skill: invalid draft")

func (s *Service) ListDrafts(ctx context.Context) ([]Draft, error) {
	if s.homeDir == "" {
		return nil, nil
	}
	root := filepath.Join(s.homeDir, DraftDirName)
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Draft, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		draft, err := s.draftSummary(ctx, entry.Name())
		if err != nil {
			continue
		}
		out = append(out, draft)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Service) DraftDetail(ctx context.Context, id string) (*DraftDetail, error) {
	draft, err := s.draftSummary(ctx, id)
	if err != nil {
		return nil, err
	}
	files, err := s.draftFiles(id)
	if err != nil {
		return nil, err
	}
	return &DraftDetail{Draft: draft, Files: files}, nil
}

func (s *Service) ValidateDraft(_ context.Context, id string) (*Validation, error) {
	id = strings.TrimSpace(id)
	if !skillIDPattern.MatchString(id) {
		return &Validation{Errors: []string{"draft name must use lowercase letters, numbers, and hyphens"}}, nil
	}
	validation := validateDraftDir(s.homeDir, filepath.Join(s.homeDir, DraftDirName, id), id)
	return &validation, nil
}

func (s *Service) ApplyDraft(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if !skillIDPattern.MatchString(id) {
		return ErrInvalidID
	}
	validation, err := s.ValidateDraft(ctx, id)
	if err != nil {
		return err
	}
	if validation == nil || !validation.OK {
		return ErrInvalidDraft
	}
	src := filepath.Join(s.homeDir, DraftDirName, id)
	skillsRoot := filepath.Join(s.homeDir, "skills")
	if err := os.MkdirAll(skillsRoot, 0o700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(skillsRoot, ".tmp-"+id+"-")
	if err != nil {
		return err
	}
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.RemoveAll(tmp)
		}
	}()
	dst := filepath.Join(skillsRoot, id)
	if _, err := os.Stat(dst); err == nil {
		if err := copyDir(dst, tmp); err != nil {
			return err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	deletes, err := readDeleteManifest(src)
	if err != nil {
		return err
	}
	if err := copyDraftFiles(src, tmp); err != nil {
		return err
	}
	for rel := range deletes {
		if err := os.RemoveAll(filepath.Join(tmp, filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	cleanupTmp = false
	if err := os.RemoveAll(src); err != nil {
		return err
	}
	return nil
}

func (s *Service) DeleteDraft(_ context.Context, id string) error {
	id = strings.TrimSpace(id)
	if !skillIDPattern.MatchString(id) {
		return ErrInvalidID
	}
	return os.RemoveAll(filepath.Join(s.homeDir, DraftDirName, id))
}

func (s *Service) draftSummary(ctx context.Context, id string) (Draft, error) {
	id = strings.TrimSpace(id)
	if !skillIDPattern.MatchString(id) {
		return Draft{}, ErrInvalidID
	}
	if _, err := os.Stat(filepath.Join(s.homeDir, DraftDirName, id)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Draft{}, ErrNotFound
		}
		return Draft{}, err
	}
	validation, err := s.ValidateDraft(ctx, id)
	if err != nil {
		return Draft{}, err
	}
	name := id
	description := ""
	deletes, _ := readDeleteManifest(filepath.Join(s.homeDir, DraftDirName, id))
	data, err := readEffectiveDraftFile(s.homeDir, id, SkillFileName, deletes)
	if err == nil {
		if meta, ok := parseFrontmatter(data); ok {
			if strings.TrimSpace(meta.Name) != "" {
				name = strings.TrimSpace(meta.Name)
			}
			description = strings.TrimSpace(meta.Description)
		}
	}
	change := DraftChangeAdded
	publishedRoot := filepath.Join(s.homeDir, "skills", id)
	if _, err := os.Stat(publishedRoot); err == nil {
		change = DraftChangeModified
	}
	draftRoot := filepath.Join(s.homeDir, DraftDirName, id)
	iconPath := probeIconDisk(draftRoot, path.Join(DraftDirName, id))
	if iconPath == "" && change == DraftChangeModified {
		if deletes, err := readDeleteManifest(draftRoot); err == nil {
			publishedIconPath := probeIconDisk(publishedRoot, id)
			if publishedIconPath != "" && !deletes[strings.TrimPrefix(strings.TrimPrefix(publishedIconPath, id), "/")] {
				iconPath = publishedIconPath
			}
		}
	}
	return Draft{
		ID:          id,
		Name:        name,
		Description: description,
		Path:        path.Join(DraftDirName, id),
		IconPath:    iconPath,
		Change:      change,
		Validation:  *validation,
	}, nil
}

func (s *Service) draftFiles(id string) ([]FileDiff, error) {
	draftRoot := filepath.Join(s.homeDir, DraftDirName, id)
	publishedRoot := filepath.Join(s.homeDir, "skills", id)
	paths := map[string]bool{}
	if err := collectDraftFiles(draftRoot, paths); err != nil {
		return nil, err
	}
	deletes, err := readDeleteManifest(draftRoot)
	if err != nil {
		return nil, err
	}
	for rel := range deletes {
		paths[rel] = true
	}
	var rels []string
	for rel := range paths {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	out := make([]FileDiff, 0, len(rels))
	for _, rel := range rels {
		oldData, oldOK, err := readOptionalFile(filepath.Join(publishedRoot, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		var newData []byte
		newOK := false
		if !deletes[rel] {
			newData, newOK, err = readOptionalFile(filepath.Join(draftRoot, filepath.FromSlash(rel)))
			if err != nil {
				return nil, err
			}
		}
		if oldOK && newOK && bytes.Equal(oldData, newData) {
			continue
		}
		change := "modified"
		switch {
		case !oldOK && newOK:
			change = "added"
		case oldOK && !newOK:
			change = "deleted"
		}
		file := FileDiff{Path: rel, Change: change}
		if isText(oldData) && isText(newData) {
			file.Old = string(oldData)
			file.New = string(newData)
			file.UnifiedDiff = simpleUnifiedDiff(id, rel, file.Old, file.New)
		}
		out = append(out, file)
	}
	return out, nil
}

func validateDraftDir(homeDir, dir, id string) Validation {
	var errs []string
	var warnings []string
	if !skillIDPattern.MatchString(id) {
		errs = append(errs, "draft name must use lowercase letters, numbers, and hyphens")
	}
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			errs = append(errs, "draft directory does not exist")
			return Validation{OK: false, Errors: errs}
		}
		errs = append(errs, err.Error())
		return Validation{OK: false, Errors: errs}
	}
	if !info.IsDir() {
		errs = append(errs, "draft path is not a directory")
	}
	deletes, err := readDeleteManifest(dir)
	if err != nil {
		errs = append(errs, "invalid .delete: "+err.Error())
	}
	if deletes[SkillFileName] {
		errs = append(errs, "SKILL.md cannot be deleted")
	}
	data, err := readEffectiveDraftFile(homeDir, id, SkillFileName, deletes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			errs = append(errs, "effective SKILL.md missing")
		} else {
			errs = append(errs, "read SKILL.md: "+err.Error())
		}
		return Validation{OK: len(errs) == 0, Errors: errs, Warnings: warnings}
	}
	meta, ok := parseFrontmatter(data)
	if !ok {
		errs = append(errs, "SKILL.md frontmatter is missing or invalid")
	}
	name := strings.TrimSpace(meta.Name)
	if name == "" {
		errs = append(errs, "frontmatter name is required")
	} else if name != id {
		errs = append(errs, "frontmatter name must match draft directory name")
	}
	desc := strings.TrimSpace(meta.Description)
	if desc == "" {
		errs = append(errs, "frontmatter description is required")
	} else if len([]rune(desc)) < 12 {
		warnings = append(warnings, "description is very short")
	}
	if strings.TrimSpace(bodyAfterFrontmatter(data)) == "" {
		errs = append(errs, "SKILL.md body is empty")
	}
	return Validation{OK: len(errs) == 0, Errors: errs, Warnings: warnings}
}

func bodyAfterFrontmatter(data []byte) string {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if !bytes.HasPrefix(trimmed, []byte("---")) {
		return string(bytes.TrimSpace(data))
	}
	after := trimmed[3:]
	nl := bytes.IndexByte(after, '\n')
	if nl < 0 {
		return ""
	}
	lines := bytes.Split(after[nl+1:], []byte("\n"))
	for i, line := range lines {
		if strings.TrimRight(string(line), " \t\r") == "---" {
			return string(bytes.TrimSpace(bytes.Join(lines[i+1:], []byte("\n"))))
		}
	}
	return ""
}

func collectRegularFiles(root string, out map[string]bool) error {
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = true
		return nil
	})
}

func collectDraftFiles(root string, out map[string]bool) error {
	if err := collectRegularFiles(root, out); err != nil {
		return err
	}
	delete(out, DraftDeleteFileName)
	return nil
}

func readEffectiveDraftFile(homeDir, id, rel string, deletes map[string]bool) ([]byte, error) {
	if deletes[rel] {
		return nil, os.ErrNotExist
	}
	data, err := os.ReadFile(filepath.Join(homeDir, DraftDirName, id, filepath.FromSlash(rel)))
	if err == nil {
		return data, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return os.ReadFile(filepath.Join(homeDir, "skills", id, filepath.FromSlash(rel)))
}

func readDeleteManifest(draftRoot string) (map[string]bool, error) {
	out := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(draftRoot, DraftDeleteFileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return nil, err
	}
	for _, raw := range strings.Split(string(data), "\n") {
		item := strings.TrimSpace(raw)
		if item == "" || strings.HasPrefix(item, "#") {
			continue
		}
		clean, err := cleanDraftRelPath(item)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", item, err)
		}
		if clean == DraftDeleteFileName {
			return nil, errors.New(".delete cannot delete itself")
		}
		out[clean] = true
	}
	return out, nil
}

func cleanDraftRelPath(raw string) (string, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if raw == "" {
		return "", errors.New("path is empty")
	}
	if path.IsAbs(raw) {
		return "", errors.New("absolute path is not allowed")
	}
	clean := path.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("path escapes draft")
	}
	if strings.HasPrefix(clean, ".") || strings.Contains(clean, "/.") {
		return "", errors.New("hidden paths are reserved")
	}
	return clean, nil
}

func readOptionalFile(p string) ([]byte, bool, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

func simpleUnifiedDiff(id, rel, oldText, newText string) string {
	if oldText == newText {
		return ""
	}
	oldLines := splitLines(oldText)
	newLines := splitLines(newText)
	var b strings.Builder
	fmt.Fprintf(&b, "--- skills/%s/%s\n", id, rel)
	fmt.Fprintf(&b, "+++ skills-draft/%s/%s\n", id, rel)
	b.WriteString("@@\n")
	for _, op := range diffLines(oldLines, newLines) {
		b.WriteByte(op.kind)
		b.WriteString(op.line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

type lineDiffOp struct {
	kind byte
	line string
}

func diffLines(oldLines, newLines []string) []lineDiffOp {
	m, n := len(oldLines), len(newLines)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var out []lineDiffOp
	for i, j := 0, 0; i < m || j < n; {
		switch {
		case i < m && j < n && oldLines[i] == newLines[j]:
			out = append(out, lineDiffOp{kind: ' ', line: oldLines[i]})
			i++
			j++
		case i < m && (j == n || dp[i+1][j] >= dp[i][j+1]):
			out = append(out, lineDiffOp{kind: '-', line: oldLines[i]})
			i++
		default:
			out = append(out, lineDiffOp{kind: '+', line: newLines[j]})
			j++
		}
	}
	return out
}

func splitLines(text string) []string {
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func isText(data []byte) bool {
	return len(data) == 0 || (utf8.Valid(data) && !bytes.Contains(data, []byte{0}))
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	})
}

func copyDraftFiles(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if relSlash == DraftDeleteFileName {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	})
}
