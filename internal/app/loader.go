package app

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	AppFileName = "app.yaml"
)

var appIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
var endpointNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type fileDefinition struct {
	ID          string                 `yaml:"id"`
	Name        string                 `yaml:"name"`
	Version     string                 `yaml:"version,omitempty"`
	Description string                 `yaml:"description,omitempty"`
	Icon        IconSpec               `yaml:"icon,omitempty"`
	Auth        *AuthConfig            `yaml:"auth,omitempty"`
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
		Version:     strings.TrimSpace(raw.Version),
		Description: strings.TrimSpace(raw.Description),
		Icon:        normalizeIconSpec(raw.Icon, dir),
		Auth:        normalizeAuthConfig(raw.Auth),
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
	applyDefinitionLock(dir, def)
	return def, nil
}

func normalizeIconSpec(raw IconSpec, appDir string) *IconSpec {
	icon := IconSpec{SVG: strings.TrimSpace(raw.SVG)}
	if raw.Color != nil {
		color := normalizeThemeColor(*raw.Color)
		if color.Light != "" || color.Dark != "" {
			icon.Color = &color
		}
	}
	if raw.Background != nil {
		background := normalizeThemeColor(*raw.Background)
		if background.Light != "" || background.Dark != "" {
			icon.Background = &background
		}
	}
	if icon.SVG == "" {
		icon.SVG = defaultAppIconPath(appDir)
	}
	if icon.SVG == "" && icon.Color == nil && icon.Background == nil {
		return nil
	}
	return &icon
}

func normalizeThemeColor(raw ThemeColor) ThemeColor {
	return ThemeColor{
		Light: strings.TrimSpace(raw.Light),
		Dark:  strings.TrimSpace(raw.Dark),
	}
}

func normalizeAuthConfig(raw *AuthConfig) *AuthConfig {
	if raw == nil {
		return nil
	}
	out := &AuthConfig{Required: raw.Required}
	for _, method := range raw.Methods {
		method.ID = strings.TrimSpace(method.ID)
		method.Type = strings.TrimSpace(method.Type)
		method.Provider = strings.TrimSpace(method.Provider)
		method.Label = strings.TrimSpace(method.Label)
		method.Prefix = strings.TrimSpace(method.Prefix)
		method.Header = strings.TrimSpace(method.Header)
		if method.ID == "" {
			method.ID = method.Type
		}
		if method.Type == "" {
			continue
		}
		out.Methods = append(out.Methods, method)
	}
	if len(out.Methods) == 0 && !out.Required {
		return nil
	}
	return out
}

func defaultAppIconPath(appDir string) string {
	name := "icon.svg"
	if _, err := os.Stat(filepath.Join(appDir, "assets", name)); err == nil {
		return filepath.ToSlash(filepath.Join("assets", name))
	}
	return ""
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
	if def.Icon != nil {
		if err := ValidateIcon(*def.Icon); err != nil {
			return err
		}
	}
	if err := ValidateAuthConfig(def.Auth); err != nil {
		return err
	}
	return nil
}

func ValidateAuthConfig(auth *AuthConfig) error {
	if auth == nil {
		return nil
	}
	if auth.Required && len(auth.Methods) == 0 {
		return errors.New("auth methods are required")
	}
	seen := map[string]struct{}{}
	for _, method := range auth.Methods {
		id := strings.TrimSpace(method.ID)
		if id == "" {
			return errors.New("auth method id is required")
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("duplicate auth method %q", id)
		}
		seen[id] = struct{}{}
		switch strings.TrimSpace(method.Type) {
		case AuthTypeNone:
			if auth.Required {
				return fmt.Errorf("auth method %q cannot be none when auth is required", id)
			}
		case AuthTypeBearer, AuthTypeToken, AuthTypeBasic, AuthTypeHeader, AuthTypeOAuth2:
		default:
			return fmt.Errorf("unsupported auth method type %q", method.Type)
		}
		if method.Type == AuthTypeHeader && strings.TrimSpace(method.Header) == "" {
			return fmt.Errorf("auth method %q header is required", id)
		}
	}
	return nil
}

func ValidateIcon(icon IconSpec) error {
	if strings.TrimSpace(icon.SVG) == "" {
		return errors.New("icon svg is required")
	}
	cleaned, err := cleanRelativeSlashPath(icon.SVG)
	if err != nil {
		return fmt.Errorf("invalid icon svg: %w", err)
	}
	parts := strings.Split(cleaned, "/")
	if len(parts) != 2 || parts[0] != "assets" {
		return fmt.Errorf("icon svg must be assets/<file>")
	}
	if contentType, ok := iconContentType(parts[1]); !ok || contentType != "image/svg+xml" {
		return fmt.Errorf("icon svg must point to an svg file")
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

func cleanRelativeSlashPath(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("invalid relative path %q", rel)
	}
	slashed := filepath.ToSlash(rel)
	for _, part := range strings.Split(slashed, "/") {
		if part == ".." {
			return "", fmt.Errorf("invalid relative path %q", rel)
		}
	}
	cleaned := path.Clean(slashed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("invalid relative path %q", rel)
	}
	return cleaned, nil
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
