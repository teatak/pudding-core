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
var connectionFieldIDPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
var endpointPlatformPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

type fileDefinition struct {
	Kind        string                 `yaml:"kind,omitempty"`
	ID          string                 `yaml:"id"`
	Name        string                 `yaml:"name"`
	Version     string                 `yaml:"version,omitempty"`
	Description string                 `yaml:"description,omitempty"`
	Icon        IconSpec               `yaml:"icon,omitempty"`
	Auth        *AuthConfig            `yaml:"auth,omitempty"`
	Connection  *ConnectionConfig      `yaml:"connection,omitempty"`
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
	resolvedRoot, err := resolveAppRoot(root, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	root = resolvedRoot
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

func resolveAppRoot(root string, create bool) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", errors.New("app root is required")
	}
	if create {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return "", err
		}
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("app root must be a directory, not a symlink")
	}
	return filepath.Clean(root), nil
}

func LoadDefinitionDir(dir string) (*Definition, error) {
	path := filepath.Join(dir, AppFileName)
	resolvedPath, err := resolveAppRegularFile(dir, AppFileName)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return nil, err
	}
	var raw fileDefinition
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("app: parse %s: %w", path, err)
	}
	def := &Definition{
		Kind:        normalizedDefinitionKind(raw.Kind),
		ID:          strings.TrimSpace(raw.ID),
		Name:        strings.TrimSpace(raw.Name),
		Version:     strings.TrimSpace(raw.Version),
		Description: strings.TrimSpace(raw.Description),
		Icon:        normalizeIconSpec(raw.Icon, dir),
		Auth:        normalizeAuthConfig(raw.Auth),
		Connection:  normalizeConnectionConfig(raw.Connection),
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
	if def.Icon != nil {
		if _, err := resolveAppRegularFile(dir, def.Icon.SVG); err != nil {
			return nil, fmt.Errorf("app: validate %s: icon %q is not a regular file inside the App: %w", path, def.Icon.SVG, err)
		}
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
	return out
}

func normalizeConnectionConfig(raw *ConnectionConfig) *ConnectionConfig {
	if raw == nil {
		return nil
	}
	out := &ConnectionConfig{}
	for _, field := range raw.Fields {
		field.ID = strings.TrimSpace(field.ID)
		field.Label = strings.TrimSpace(field.Label)
		field.Description = strings.TrimSpace(field.Description)
		field.Placeholder = strings.TrimSpace(field.Placeholder)
		rules := make([]ConnectionFieldInject, 0, len(field.Inject))
		for _, rule := range field.Inject {
			rule.Target = strings.TrimSpace(rule.Target)
			rule.Name = strings.TrimSpace(rule.Name)
			methods := make([]string, 0, len(rule.Methods))
			for _, method := range rule.Methods {
				method = strings.ToUpper(strings.TrimSpace(method))
				if method != "" {
					methods = append(methods, method)
				}
			}
			rule.Methods = methods
			rules = append(rules, rule)
		}
		field.Inject = rules
		if field.ID == "" {
			continue
		}
		if field.Label == "" {
			field.Label = field.ID
		}
		out.Fields = append(out.Fields, field)
	}
	if len(out.Fields) == 0 {
		return nil
	}
	return out
}

func defaultAppIconPath(appDir string) string {
	name := "icon.svg"
	if _, err := resolveAppRegularFile(appDir, filepath.ToSlash(filepath.Join("assets", name))); err == nil {
		return filepath.ToSlash(filepath.Join("assets", name))
	}
	return ""
}

func ValidateDefinition(def *Definition) error {
	if def == nil {
		return errors.New("definition is nil")
	}
	def.Kind = normalizedDefinitionKind(def.Kind)
	switch def.Kind {
	case KindApp:
	case KindMCP:
		if len(def.Endpoints) != 1 {
			return errors.New("mcp app must define exactly one endpoint")
		}
		if len(def.Skills) > 0 {
			return errors.New("mcp app cannot define skills")
		}
	default:
		return fmt.Errorf("unsupported app kind %q", def.Kind)
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
		if def.Kind == KindMCP && endpoint.Kind != EndpointKindMCP {
			return fmt.Errorf("mcp app endpoint %s must use kind %q", name, EndpointKindMCP)
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
	if err := ValidateConnectionConfig(def.Connection); err != nil {
		return err
	}
	return nil
}

func normalizedDefinitionKind(kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return KindApp
	}
	return kind
}

func ValidateConnectionConfig(connection *ConnectionConfig) error {
	if connection == nil {
		return nil
	}
	seen := map[string]struct{}{}
	for _, field := range connection.Fields {
		id := strings.TrimSpace(field.ID)
		if !connectionFieldIDPattern.MatchString(id) {
			return fmt.Errorf("invalid connection field id %q", field.ID)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("duplicate connection field %q", id)
		}
		seen[id] = struct{}{}
		for _, rule := range field.Inject {
			target := strings.TrimSpace(rule.Target)
			switch target {
			case "query", "body", "header", "env":
			default:
				return fmt.Errorf("connection field %q has unsupported inject target %q", id, rule.Target)
			}
			if target == "env" {
				name := strings.TrimSpace(rule.Name)
				if name == "" {
					name = id
				}
				if !validEndpointEnvName(name) {
					return fmt.Errorf("connection field %q has invalid env name %q", id, name)
				}
			}
			if target == "header" {
				name := strings.TrimSpace(rule.Name)
				if name == "" {
					name = id
				}
				if !IsAllowedRequestHeaderName(name) {
					return fmt.Errorf("connection field %q has invalid or forbidden header name %q", id, name)
				}
			}
			for _, method := range rule.Methods {
				switch strings.ToUpper(strings.TrimSpace(method)) {
				case "GET", "POST", "PUT", "PATCH", "DELETE":
				default:
					return fmt.Errorf("connection field %q has unsupported inject method %q", id, method)
				}
			}
		}
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
		if method.Type == AuthTypeHeader {
			header := strings.TrimSpace(method.Header)
			if header == "" {
				return fmt.Errorf("auth method %q header is required", id)
			}
			if !IsAllowedRequestHeaderName(header) {
				return fmt.Errorf("auth method %q has invalid or forbidden header %q", id, header)
			}
		}
		if method.Type == AuthTypeToken && !IsAllowedRequestHeaderValue(method.Prefix) {
			return fmt.Errorf("auth method %q has invalid token prefix", id)
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
		if err := validateEndpointURL(endpoint.URL); err != nil {
			return err
		}
	case EndpointKindMCP:
		if err := validateMCPEndpoint(endpoint); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported kind %q", endpoint.Kind)
	}
	for platform, override := range endpoint.Platforms {
		platform = strings.TrimSpace(platform)
		if !endpointPlatformPattern.MatchString(platform) {
			return fmt.Errorf("invalid endpoint platform %q", platform)
		}
		if err := validateEndpointPlatformOverride(override); err != nil {
			return fmt.Errorf("platform %s: %w", platform, err)
		}
	}
	return nil
}

func validateMCPEndpoint(endpoint Endpoint) error {
	switch strings.TrimSpace(endpoint.Transport) {
	case EndpointTransportStreamableHTTP:
		if err := validateEndpointURL(endpoint.URL); err != nil {
			return err
		}
	case EndpointTransportStdio:
		if strings.TrimSpace(endpoint.Command) == "" {
			return errors.New("stdio mcp endpoint command is required")
		}
	case "":
		return errors.New("mcp endpoint transport is required")
	default:
		return fmt.Errorf("unsupported mcp transport %q", endpoint.Transport)
	}
	if err := validateEndpointStringMap("env", endpoint.Env, validEndpointEnvName); err != nil {
		return err
	}
	if err := validateEndpointStringMap("header", endpoint.Headers, IsAllowedRequestHeaderName); err != nil {
		return err
	}
	return nil
}

func validateEndpointPlatformOverride(override EndpointPlatformOverride) error {
	if strings.TrimSpace(override.URL) != "" {
		if err := validateEndpointURL(override.URL); err != nil {
			return err
		}
	}
	if err := validateEndpointStringMap("env", override.Env, validEndpointEnvName); err != nil {
		return err
	}
	if err := validateEndpointStringMap("header", override.Headers, IsAllowedRequestHeaderName); err != nil {
		return err
	}
	return nil
}

func validateEndpointStringMap(label string, values map[string]string, valid func(string) bool) error {
	for name, value := range values {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("endpoint %s name is required", label)
		}
		if !valid(name) {
			return fmt.Errorf("endpoint %s %q is invalid", label, name)
		}
		if strings.ContainsRune(value, 0) {
			return fmt.Errorf("endpoint %s %q contains a NUL byte", label, name)
		}
		if label == "header" && !IsAllowedRequestHeaderValue(value) {
			return fmt.Errorf("endpoint header %q contains a newline", name)
		}
	}
	return nil
}

func validEndpointEnvName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for index, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_' || index > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// IsAllowedRequestHeaderName reports whether an App may inject the header into
// an outbound request. Keep this shared by manifest validation and execution.
func IsAllowedRequestHeaderName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if _, forbidden := forbiddenRequestHeaders[strings.ToLower(name)]; forbidden {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		if strings.ContainsRune("!#$%&'*+-.^_`|~", r) {
			continue
		}
		return false
	}
	return true
}

// IsAllowedRequestHeaderValue reports whether a value can be safely copied
// into an outbound HTTP header.
func IsAllowedRequestHeaderValue(value string) bool {
	return !strings.ContainsAny(value, "\x00\r\n")
}

var forbiddenRequestHeaders = map[string]struct{}{
	"host":                {},
	"content-length":      {},
	"connection":          {},
	"transfer-encoding":   {},
	"expect":              {},
	"upgrade":             {},
	"proxy-connection":    {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
}

func validateEndpointURL(rawURL string) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
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
	cleaned, err := cleanRelativeSlashPath(rel)
	if err != nil {
		return SkillRef{}, fmt.Errorf("invalid skill path %q: %w", rel, err)
	}
	resolvedPath, err := resolveAppRegularFile(appDir, cleaned)
	if err != nil {
		return SkillRef{}, err
	}
	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return SkillRef{}, err
	}
	meta, _ := parseSkillFrontmatter(data)
	path := filepath.Join(appDir, filepath.FromSlash(cleaned))
	id := strings.TrimSuffix(filepath.Base(filepath.Dir(path)), filepath.Ext(filepath.Base(path)))
	if strings.TrimSpace(meta.Name) != "" {
		id = strings.TrimSpace(meta.Name)
	}
	return SkillRef{
		ID:          id,
		Name:        strings.TrimSpace(meta.Name),
		Description: strings.TrimSpace(meta.Description),
		Path:        cleaned,
	}, nil
}

func resolveAppRegularFile(appDir, rel string) (string, error) {
	info, err := os.Lstat(appDir)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("app directory must be a directory, not a symlink")
	}
	cleaned, err := cleanRelativeSlashPath(rel)
	if err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(appDir)
	if err != nil {
		return "", err
	}
	target, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(cleaned)))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("app file escapes the App directory")
	}
	info, err = os.Stat(target)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("app file must be a regular file")
	}
	return target, nil
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
