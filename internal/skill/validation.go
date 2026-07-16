package skill

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func (s *Service) ValidateSkill(_ context.Context, id string) (*Validation, error) {
	id = strings.TrimSpace(id)
	if !skillIDPattern.MatchString(id) {
		return nil, ErrInvalidID
	}
	if isBuiltinSkillID(id) {
		validation := Validation{OK: false, Errors: []string{"skill id is reserved by a builtin Skill"}}
		return &validation, nil
	}
	if strings.TrimSpace(s.homeDir) == "" {
		return nil, errors.New("skill: home dir is required")
	}
	root, err := resolveUserSkillsRoot(filepath.Join(s.homeDir, "skills"))
	if errors.Is(err, os.ErrNotExist) {
		validation := Validation{OK: false, Errors: []string{"skill directory does not exist"}}
		return &validation, nil
	}
	if err != nil {
		validation := Validation{OK: false, Errors: []string{err.Error()}}
		return &validation, nil
	}
	validation := validateSkillDir(filepath.Join(root, id), id)
	return &validation, nil
}

func validateSkillDir(dir, id string) Validation {
	var errs []string
	info, err := os.Lstat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			errs = append(errs, "skill directory does not exist")
		} else {
			errs = append(errs, err.Error())
		}
		return Validation{OK: false, Errors: errs}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Validation{OK: false, Errors: []string{"skill directory must not be a symlink"}}
	}
	if !info.IsDir() {
		return Validation{OK: false, Errors: []string{"skill path is not a directory"}}
	}
	skillPath := filepath.Join(dir, SkillFileName)
	fileInfo, err := os.Lstat(skillPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			errs = append(errs, "SKILL.md is missing")
		} else {
			errs = append(errs, "inspect SKILL.md: "+err.Error())
		}
		return Validation{OK: false, Errors: errs}
	}
	if fileInfo.Mode()&os.ModeSymlink != 0 || !fileInfo.Mode().IsRegular() {
		return Validation{OK: false, Errors: []string{"SKILL.md must be a regular file, not a symlink"}}
	}
	data, err := os.ReadFile(skillPath)
	if err != nil {
		errs = append(errs, "read SKILL.md: "+err.Error())
		return Validation{OK: false, Errors: errs}
	}
	return validateSkillData(data, id)
}

func validateSkillData(data []byte, id string) Validation {
	var errs []string
	var warnings []string
	meta, ok := parseFrontmatter(data)
	if !ok {
		errs = append(errs, "SKILL.md frontmatter is missing or invalid")
	}
	name := strings.TrimSpace(meta.Name)
	if name == "" {
		errs = append(errs, "frontmatter name is required")
	} else if name != id {
		errs = append(errs, "frontmatter name must match the skill directory name")
	}
	description := strings.TrimSpace(meta.Description)
	if description == "" {
		errs = append(errs, "frontmatter description is required")
	} else if len([]rune(description)) < 12 {
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
