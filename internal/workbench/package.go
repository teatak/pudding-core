// Package workbench validates immutable, generated workbench source packages.
// Generated code is never evaluated by Core.
package workbench

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/teatak/pudding-core/contracts"
)

var identifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,79}$`)
var sourceFileName = regexp.MustCompile(`^[a-zA-Z0-9_./ -]+$`)
var revisionID = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Source struct {
	AppID    string `json:"appID"`
	Endpoint string `json:"endpoint"`
}
type Request struct {
	Method        string         `json:"method,omitempty"`
	Path          string         `json:"path,omitempty"`
	PathParams    map[string]any `json:"pathParams,omitempty"`
	Query         map[string]any `json:"query,omitempty"`
	Body          any            `json:"body,omitempty"`
	Document      string         `json:"document,omitempty"`
	OperationName string         `json:"operationName,omitempty"`
	Variables     map[string]any `json:"variables,omitempty"`
}
type Result struct {
	Rows    string         `json:"rows,omitempty"`
	Total   string         `json:"total,omitempty"`
	Cursor  string         `json:"cursor,omitempty"`
	Schema  map[string]any `json:"schema,omitempty"`
	Success *Condition     `json:"success,omitempty"`
}
type Condition struct {
	Pointer string `json:"pointer"`
	Equals  any    `json:"equals"`
}
type Operation struct {
	Source      string         `json:"source"`
	Kind        string         `json:"kind"`
	EffectHint  string         `json:"effectHint"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
	Request     Request        `json:"request"`
	Result      Result         `json:"result,omitempty"`
}
type Manifest struct {
	SchemaVersion int                  `json:"schemaVersion"`
	SDKVersion    string               `json:"sdkVersion"`
	Entry         string               `json:"entry"`
	Sources       map[string]Source    `json:"sources"`
	Operations    map[string]Operation `json:"operations"`
}
type Package struct {
	Files map[string]string `json:"files"`
}

func DecodeStrict(data []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("expected one JSON value")
	}
	return nil
}

func (p Package) Validate() (Manifest, string, error) {
	var m Manifest
	policy := contracts.Workbench()
	if len(p.Files) == 0 || len(p.Files) > policy.MaxFiles {
		return m, "", errors.New("invalid package file count")
	}
	total := 0
	for name, content := range p.Files {
		if !ValidFilePath(name) || !utf8.ValidString(content) {
			return m, "", fmt.Errorf("invalid source file %q", name)
		}
		total += len(content)
		if len(content) > policy.MaxFileBytes || total > policy.MaxPackageBytes {
			return m, "", errors.New("package exceeds size limit")
		}
	}
	if err := DecodeStrict([]byte(p.Files["workbench.json"]), &m); err != nil {
		return m, "", fmt.Errorf("workbench.json: %w", err)
	}
	if m.SchemaVersion != policy.SchemaVersion || m.SDKVersion != policy.SDKVersion {
		return m, "", errors.New("unsupported workbench or SDK version")
	}
	if !strings.HasPrefix(m.Entry, "src/") || !strings.HasSuffix(m.Entry, ".tsx") || p.Files[m.Entry] == "" {
		return m, "", errors.New("entry must reference an existing src/*.tsx file")
	}
	if len(m.Sources) > policy.MaxSources || len(m.Operations) > policy.MaxOperations {
		return m, "", errors.New("too many sources or operations")
	}
	for id, source := range m.Sources {
		if !identifier.MatchString(id) || !identifier.MatchString(source.AppID) || !identifier.MatchString(source.Endpoint) {
			return m, "", fmt.Errorf("invalid source %q", id)
		}
	}
	for id, op := range m.Operations {
		if !identifier.MatchString(id) {
			return m, "", fmt.Errorf("invalid operation %q", id)
		}
		if _, ok := m.Sources[op.Source]; !ok {
			return m, "", fmt.Errorf("operation %q references missing source", id)
		}
		if err := op.Validate(); err != nil {
			return m, "", fmt.Errorf("operation %s: %w", id, err)
		}
	}
	data, err := json.Marshal(p)
	if err != nil {
		return m, "", err
	}
	return m, fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func ValidFilePath(name string) bool {
	if !sourceFileName.MatchString(name) || len(name) > 240 || strings.ContainsAny(name, "\\\x00") || path.Clean(name) != name || strings.HasPrefix(name, "/") {
		return false
	}
	if name == "workbench.json" {
		return true
	}
	if !strings.HasPrefix(name, "src/") && !strings.HasPrefix(name, "fixtures/") && !strings.HasPrefix(name, "assets/") {
		return false
	}
	switch path.Ext(name) {
	case ".ts", ".tsx", ".css", ".json", ".svg":
		return true
	}
	return false
}

func (o Operation) Validate() error {
	if o.EffectHint != "read" && o.EffectHint != "write" && o.EffectHint != "unknown" {
		return errors.New("effectHint must be read, write or unknown")
	}
	if err := ValidateSchema(o.InputSchema); err != nil {
		return fmt.Errorf("inputSchema: %w", err)
	}
	if o.InputSchema["type"] != "object" {
		return errors.New("inputSchema must describe an object")
	}
	if len(o.Result.Schema) != 0 {
		if err := ValidateSchema(o.Result.Schema); err != nil {
			return err
		}
	}
	for _, pointer := range []string{o.Result.Rows, o.Result.Total, o.Result.Cursor} {
		if !ValidPointer(pointer) {
			return errors.New("invalid result JSON pointer")
		}
	}
	if o.Result.Success != nil && !ValidPointer(o.Result.Success.Pointer) {
		return errors.New("invalid success JSON pointer")
	}
	switch o.Kind {
	case "rest":
		if o.Request.Method != "GET" && o.Request.Method != "POST" && o.Request.Method != "PUT" && o.Request.Method != "PATCH" && o.Request.Method != "DELETE" {
			return errors.New("unsupported REST method")
		}
		if err := validateRelativePath(o.Request.Path); err != nil {
			return err
		}
		if o.Request.Document != "" || o.Request.OperationName != "" || len(o.Request.Variables) > 0 {
			return errors.New("GraphQL fields in REST operation")
		}
	case "graphql":
		if strings.TrimSpace(o.Request.Document) == "" || strings.TrimSpace(o.Request.OperationName) == "" {
			return errors.New("GraphQL document and operationName required")
		}
		if o.Request.Path != "" || o.Request.Method != "" || o.Request.Body != nil || len(o.Request.PathParams)+len(o.Request.Query) > 0 {
			return errors.New("REST fields in GraphQL operation")
		}
	default:
		return errors.New("unsupported operation kind")
	}
	_, err := transform(o.Request, nil, true)
	return err
}

func validateRelativePath(value string) error {
	u, err := url.Parse(value)
	if err != nil || value == "" || u.IsAbs() || u.Host != "" || u.RawQuery != "" || u.Fragment != "" || strings.HasPrefix(value, "//") || strings.Contains(value, "\\") {
		return errors.New("path must stay within the App endpoint")
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == ".." || segment == "." {
			return errors.New("path traversal is not allowed")
		}
	}
	return nil
}

func (o Operation) Hash() string { b, _ := json.Marshal(o); return fmt.Sprintf("%x", sha256.Sum256(b)) }

// ResolveRequest only substitutes declared values; it never evaluates expressions.
func (o Operation) ResolveRequest(input map[string]any) (Request, error) {
	if err := ValidateInput(o.InputSchema, input); err != nil {
		return Request{}, err
	}
	value, err := transform(o.Request, input, false)
	if err != nil {
		return Request{}, err
	}
	b, _ := json.Marshal(value)
	var r Request
	if err = json.Unmarshal(b, &r); err != nil {
		return r, err
	}
	for key, value := range r.PathParams {
		text, ok := value.(string)
		if !ok || text == "" || text == "." || text == ".." {
			return r, errors.New("path parameters must be nonempty strings without dot segments")
		}
		r.Path = strings.ReplaceAll(r.Path, "{"+key+"}", url.PathEscape(text))
	}
	if o.Kind == "rest" {
		if strings.ContainsAny(r.Path, "{}") {
			return r, errors.New("unresolved path parameter")
		}
		if err = validateRelativePath(r.Path); err != nil {
			return r, err
		}
	}
	return r, nil
}

func transform(request Request, input map[string]any, validate bool) (any, error) {
	b, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	var value any
	if err = json.Unmarshal(b, &value); err != nil {
		return nil, err
	}
	return transformValue(value, input, validate, 0)
}

func transformValue(value any, input map[string]any, validate bool, depth int) (any, error) {
	if depth > contracts.Workbench().MaxSchemaDepth {
		return nil, errors.New("request template too deep")
	}
	switch v := value.(type) {
	case map[string]any:
		if pointer, ok := v["$input"]; ok {
			p, ok := pointer.(string)
			if !ok || !ValidPointer(p) || len(v) != 1 {
				return nil, errors.New("invalid $input reference")
			}
			if validate {
				return v, nil
			}
			return Pointer(input, p)
		}
		for k, child := range v {
			out, err := transformValue(child, input, validate, depth+1)
			if err != nil {
				return nil, err
			}
			v[k] = out
		}
	case []any:
		for i, child := range v {
			out, err := transformValue(child, input, validate, depth+1)
			if err != nil {
				return nil, err
			}
			v[i] = out
		}
	}
	return value, nil
}
