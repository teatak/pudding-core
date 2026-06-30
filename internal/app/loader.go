package app

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const AppFileName = "app.yaml"

var appIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
var endpointNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type fileDefinition struct {
	ID          string                 `yaml:"id"`
	Name        string                 `yaml:"name"`
	Description string                 `yaml:"description,omitempty"`
	Endpoints   map[string]Endpoint    `yaml:"endpoints,omitempty"`
	Skills      []fileSkillRef         `yaml:"skills,omitempty"`
	Extra       map[string]interface{} `yaml:",inline"`
}

type fileSkillRef struct {
	Path string `yaml:"path,omitempty"`
}

func (r *fileSkillRef) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		r.Path = value.Value
		return nil
	case yaml.MappingNode:
		type alias fileSkillRef
		var out alias
		if err := value.Decode(&out); err != nil {
			return err
		}
		*r = fileSkillRef(out)
		return nil
	default:
		return fmt.Errorf("skill ref must be a path string or object")
	}
}

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func LoadUserDefinitions(root string) ([]*Definition, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]*Definition, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		def, err := LoadDefinitionDir(filepath.Join(root, entry.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, def)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func LoadDefinitionDir(dir string) (*Definition, error) {
	path := filepath.Join(dir, AppFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw fileDefinition
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("app: parse %s: %w", path, err)
	}
	def := &Definition{
		ID:          strings.TrimSpace(raw.ID),
		Name:        strings.TrimSpace(raw.Name),
		Description: strings.TrimSpace(raw.Description),
		Endpoints:   raw.Endpoints,
		Path:        path,
	}
	if def.Name == "" {
		def.Name = def.ID
	}
	for _, ref := range raw.Skills {
		skillPath := strings.TrimSpace(ref.Path)
		if skillPath == "" {
			continue
		}
		skill, err := loadSkillRef(dir, skillPath)
		if err != nil {
			return nil, err
		}
		def.Skills = append(def.Skills, skill)
	}
	if err := ValidateDefinition(def); err != nil {
		return nil, fmt.Errorf("app: validate %s: %w", path, err)
	}
	return def, nil
}

func ValidateDefinition(def *Definition) error {
	if def == nil {
		return errors.New("definition is nil")
	}
	if !appIDPattern.MatchString(strings.TrimSpace(def.ID)) {
		return fmt.Errorf("invalid id %q", def.ID)
	}
	for name, endpoint := range def.Endpoints {
		if !endpointNamePattern.MatchString(strings.TrimSpace(name)) {
			return fmt.Errorf("invalid endpoint name %q", name)
		}
		if err := ValidateEndpoint(endpoint); err != nil {
			return fmt.Errorf("endpoint %s: %w", name, err)
		}
	}
	for _, skill := range def.Skills {
		if strings.TrimSpace(skill.Path) == "" {
			return errors.New("skill path is required")
		}
	}
	return nil
}

func ValidateEndpoint(endpoint Endpoint) error {
	switch strings.TrimSpace(endpoint.Kind) {
	case EndpointKindREST, EndpointKindGraphQL:
	default:
		return fmt.Errorf("unsupported kind %q", endpoint.Kind)
	}
	u, err := url.Parse(strings.TrimSpace(endpoint.URL))
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q not allowed", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("missing host")
	}
	return nil
}

func loadSkillRef(appDir, rel string) (SkillRef, error) {
	if filepath.IsAbs(rel) || strings.Contains(rel, "..") {
		return SkillRef{}, fmt.Errorf("invalid skill path %q", rel)
	}
	path := filepath.Join(appDir, filepath.FromSlash(rel))
	data, err := os.ReadFile(path)
	if err != nil {
		return SkillRef{}, err
	}
	meta, _ := parseSkillFrontmatter(data)
	id := strings.TrimSuffix(filepath.Base(filepath.Dir(path)), filepath.Ext(filepath.Base(path)))
	if strings.TrimSpace(meta.Name) != "" {
		id = strings.TrimSpace(meta.Name)
	}
	return SkillRef{
		ID:          id,
		Name:        strings.TrimSpace(meta.Name),
		Description: strings.TrimSpace(meta.Description),
		Path:        filepath.ToSlash(rel),
	}, nil
}

func parseSkillFrontmatter(data []byte) (skillFrontmatter, bool) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if !bytes.HasPrefix(trimmed, []byte("---\n")) && !bytes.HasPrefix(trimmed, []byte("---\r\n")) {
		return skillFrontmatter{}, false
	}
	trimmed = trimmed[3:]
	trimmed = bytes.TrimPrefix(trimmed, []byte("\r"))
	trimmed = bytes.TrimPrefix(trimmed, []byte("\n"))
	end := bytes.Index(trimmed, []byte("\n---"))
	if end < 0 {
		return skillFrontmatter{}, false
	}
	var meta skillFrontmatter
	if err := yaml.Unmarshal(trimmed[:end], &meta); err != nil {
		return skillFrontmatter{}, false
	}
	return meta, true
}
