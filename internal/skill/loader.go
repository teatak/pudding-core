package skill

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const SkillFileName = "SKILL.md"

//go:embed all:embed
var builtinFS embed.FS

var skillIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func LoadBuiltinSkills() ([]Skill, error) {
	return loadEmbeddedSkills(builtinFS, "embed")
}

func LoadUserSkills(root string) ([]Skill, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Skill, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		skill, err := loadUserSkillDir(root, entry.Name())
		if err != nil {
			slog.Warn("skill: skip user skill", "dir", entry.Name(), "error", err)
			continue
		}
		out = append(out, skill)
	}
	sortSkills(out)
	return out, nil
}

func loadEmbeddedSkills(fsys fs.FS, root string) ([]Skill, error) {
	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return nil, err
	}
	out := make([]Skill, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		skill, err := loadEmbeddedSkillDir(fsys, root, entry.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, skill)
	}
	sortSkills(out)
	return out, nil
}

func loadEmbeddedSkillDir(fsys fs.FS, root, dir string) (Skill, error) {
	rel := path.Join(root, dir, SkillFileName)
	data, err := fs.ReadFile(fsys, rel)
	if err != nil {
		return Skill{}, err
	}
	virtualDir := path.Join(SystemSubdir, dir)
	return skillFromData(data, dir, path.Join(virtualDir, SkillFileName), probeIconFS(fsys, root, dir, virtualDir), SourceBuiltin, true)
}

func loadUserSkillDir(root, dir string) (Skill, error) {
	rel := filepath.Join(root, dir, SkillFileName)
	data, err := os.ReadFile(rel)
	if err != nil {
		return Skill{}, err
	}
	return skillFromData(data, dir, path.Join(dir, SkillFileName), probeIconDisk(filepath.Join(root, dir), dir), SourceUser, false)
}

func skillFromData(data []byte, fallbackID, filePath, iconPath, source string, system bool) (Skill, error) {
	meta, _ := parseFrontmatter(data)
	id := strings.TrimSpace(meta.Name)
	if id == "" {
		id = strings.TrimSpace(fallbackID)
	}
	if !skillIDPattern.MatchString(id) {
		return Skill{}, fmt.Errorf("skill: invalid id %q", id)
	}
	name := strings.TrimSpace(meta.Name)
	if name == "" {
		name = id
	}
	return Skill{
		ID:          id,
		Name:        name,
		Description: strings.TrimSpace(meta.Description),
		Scope:       ScopeGlobal,
		Source:      source,
		System:      system,
		Path:        filePath,
		IconPath:    iconPath,
	}, nil
}

func parseFrontmatter(data []byte) (frontmatter, bool) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if !bytes.HasPrefix(trimmed, []byte("---\n")) && !bytes.HasPrefix(trimmed, []byte("---\r\n")) {
		return frontmatter{}, false
	}
	trimmed = trimmed[3:]
	trimmed = bytes.TrimPrefix(trimmed, []byte("\r"))
	trimmed = bytes.TrimPrefix(trimmed, []byte("\n"))
	end := bytes.Index(trimmed, []byte("\n---"))
	if end < 0 {
		return frontmatter{}, false
	}
	var meta frontmatter
	if err := yaml.Unmarshal(trimmed[:end], &meta); err != nil {
		return frontmatter{}, false
	}
	return meta, true
}

func sortSkills(skills []Skill) {
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Source != skills[j].Source {
			return skills[i].Source < skills[j].Source
		}
		return skills[i].ID < skills[j].ID
	})
}

func probeIconDisk(skillDir, relDir string) string {
	for _, ext := range []string{"png", "jpg", "svg"} {
		name := "icon." + ext
		if _, err := os.Stat(filepath.Join(skillDir, "assets", name)); err == nil {
			return path.Join(relDir, "assets", name)
		}
	}
	return ""
}

func probeIconFS(fsys fs.FS, root, dir, relDir string) string {
	for _, ext := range []string{"png", "jpg", "svg"} {
		name := "icon." + ext
		if _, err := fs.Stat(fsys, path.Join(root, dir, "assets", name)); err == nil {
			return path.Join(relDir, "assets", name)
		}
	}
	return ""
}
