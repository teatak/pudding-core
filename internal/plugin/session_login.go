package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Login inputs are ephemeral and never become connection fields.
type LoginInputField struct {
	ID       string `json:"id" yaml:"id"`
	Label    string `json:"label,omitempty" yaml:"label,omitempty"`
	Secret   bool   `json:"secret,omitempty" yaml:"secret,omitempty"`
	Required bool   `json:"required,omitempty" yaml:"required,omitempty"`
}
type SessionLoginSpec struct {
	Endpoint       string              `json:"endpoint" yaml:"endpoint"`
	Path           string              `json:"path" yaml:"path"`
	Encoding       string              `json:"encoding" yaml:"encoding"` // form | json
	Inputs         []LoginInputField   `json:"inputs" yaml:"inputs"`
	Parameters     map[string]string   `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	BodyFields     map[string]string   `json:"bodyFields" yaml:"body_fields"`
	Outputs        map[string]string   `json:"outputs" yaml:"outputs"` // connection field -> response JSON path
	AccountInput   string              `json:"accountInput,omitempty" yaml:"account_input,omitempty"`
	SuccessField   string              `json:"successField,omitempty" yaml:"success_field,omitempty"`
	ErrorCodeField string              `json:"errorCodeField,omitempty" yaml:"error_code_field,omitempty"`
	Challenges     map[string][]string `json:"challenges,omitempty" yaml:"challenges,omitempty"`
	ExpiresInField string              `json:"expiresInField,omitempty" yaml:"expires_in_field,omitempty"`
}
type SessionLoginResult struct {
	Status    string             `json:"status"` // success | challenge | error
	Code      string             `json:"code,omitempty"`
	Inputs    []string           `json:"inputs,omitempty"`
	Fields    map[string]string  `json:"-"`
	Account   *ConnectionAccount `json:"-"`
	ExpiresAt time.Time          `json:"-"`
}

func cloneSessionLogin(in *SessionLoginSpec) *SessionLoginSpec {
	if in == nil {
		return nil
	}
	out := *in
	out.Inputs = append([]LoginInputField(nil), in.Inputs...)
	out.Parameters = cloneStringMap(in.Parameters)
	out.BodyFields = cloneStringMap(in.BodyFields)
	out.Outputs = cloneStringMap(in.Outputs)
	out.Challenges = make(map[string][]string, len(in.Challenges))
	for code, fields := range in.Challenges {
		out.Challenges[code] = append([]string(nil), fields...)
	}
	return &out
}

func validateSessionLogins(def *Definition) error {
	if def.Auth == nil {
		return nil
	}
	for _, method := range def.Auth.Methods {
		if method.Type != AuthTypeSession {
			continue
		}
		spec := method.Session
		endpoint, ok := def.Endpoints[spec.Endpoint]
		if !ok || endpoint.Kind != EndpointKindREST {
			return fmt.Errorf("session login requires a REST endpoint")
		}
		path, err := url.Parse(spec.Path)
		if err != nil || path.IsAbs() || path.Host != "" || path.RawQuery != "" || path.Fragment != "" || !strings.HasPrefix(spec.Path, "/") || strings.Contains(path.Path, "\\") {
			return fmt.Errorf("session login path must be an endpoint-relative path")
		}
		for _, part := range strings.Split(path.Path, "/") {
			if part == ".." {
				return fmt.Errorf("session login path cannot escape endpoint")
			}
		}
		if spec.Encoding != "form" && spec.Encoding != "json" {
			return fmt.Errorf("session login encoding must be form or json")
		}
		inputs := map[string]LoginInputField{}
		for _, field := range spec.Inputs {
			if !endpointNamePattern.MatchString(field.ID) {
				return fmt.Errorf("invalid login input %q", field.ID)
			}
			if _, exists := inputs[field.ID]; exists {
				return fmt.Errorf("duplicate login input %q", field.ID)
			}
			inputs[field.ID] = field
		}
		for key, field := range spec.BodyFields {
			if key == "" {
				return fmt.Errorf("login body key is required")
			}
			if _, exists := inputs[field]; !exists {
				return fmt.Errorf("unknown login input %q", field)
			}
			if _, exists := spec.Parameters[key]; exists {
				return fmt.Errorf("duplicate login parameter %q", key)
			}
		}
		if spec.AccountInput != "" {
			if f, ok := inputs[spec.AccountInput]; !ok || f.Secret {
				return fmt.Errorf("account input must reference a non-secret input")
			}
		}
		for _, fields := range spec.Challenges {
			if spec.ErrorCodeField == "" || len(fields) == 0 {
				return fmt.Errorf("challenge requires error code and inputs")
			}
			for _, field := range fields {
				if _, ok := inputs[field]; !ok {
					return fmt.Errorf("unknown challenge input %q", field)
				}
			}
		}
		fields := map[string]ConnectionField{}
		if def.Connection != nil {
			for _, field := range def.Connection.Fields {
				fields[field.ID] = field
			}
		}
		if len(spec.Outputs) == 0 {
			return fmt.Errorf("session login outputs are required")
		}
		required := false
		for id, path := range spec.Outputs {
			field, ok := fields[id]
			if !ok || !field.Secret || !validTokenExchangeJSONPath(path) {
				return fmt.Errorf("session output %q must map a JSON path to a declared secret connection field", id)
			}
			required = required || field.Required
		}
		if !required {
			return fmt.Errorf("session login needs at least one required credential output")
		}
		for id, field := range fields {
			if field.Required && spec.Outputs[id] == "" {
				return fmt.Errorf("required session field %q has no output", id)
			}
		}
		for _, path := range []string{spec.SuccessField, spec.ErrorCodeField, spec.ExpiresInField} {
			if path != "" && !validTokenExchangeJSONPath(path) {
				return fmt.Errorf("invalid session response path")
			}
		}
	}
	return nil
}

func loginValue(data map[string]any, path string) any {
	var value any = data
	for _, key := range strings.Split(path, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = object[key]
	}
	return value
}

// AcquireSession performs only the declared login request, never follows redirects,
// and returns sanitized failures. The caller validates and atomically stores outputs.
func AcquireSession(ctx context.Context, client *http.Client, spec *SessionLoginSpec, baseURL string, inputs map[string]string) SessionLoginResult {
	failure := func(code string) SessionLoginResult { return SessionLoginResult{Status: "error", Code: code} }
	known := map[string]bool{}
	for _, field := range spec.Inputs {
		known[field.ID] = true
		if field.Required && inputs[field.ID] == "" {
			return failure("missing_input")
		}
	}
	for id, value := range inputs {
		if !known[id] || len(value) > 8192 {
			return failure("invalid_input")
		}
	}
	body := cloneStringMap(spec.Parameters)
	if body == nil {
		body = map[string]string{}
	}
	for key, id := range spec.BodyFields {
		if inputs[id] != "" {
			body[key] = inputs[id]
		}
	}
	var payload []byte
	contentType := "application/json"
	if spec.Encoding == "form" {
		values := url.Values{}
		for key, value := range body {
			values.Set(key, value)
		}
		payload = []byte(values.Encode())
		contentType = "application/x-www-form-urlencoded"
	} else {
		payload, _ = json.Marshal(body)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+spec.Path, bytes.NewReader(payload))
	if err != nil {
		return failure("invalid_endpoint")
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	transport := *client
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := transport.Do(req)
	if err != nil {
		return failure("login_unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return failure("login_http_error")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(data) > 65536 {
		return failure("invalid_response")
	}
	var decoded map[string]any
	if json.Unmarshal(data, &decoded) != nil || decoded == nil {
		return failure("invalid_response")
	}
	if spec.ErrorCodeField != "" {
		code := fmt.Sprint(loginValue(decoded, spec.ErrorCodeField))
		if fields, ok := spec.Challenges[code]; ok {
			return SessionLoginResult{Status: "challenge", Code: "additional_input_required", Inputs: append([]string(nil), fields...)}
		}
	}
	if spec.SuccessField != "" && loginValue(decoded, spec.SuccessField) != true {
		return failure("login_rejected")
	}
	result := SessionLoginResult{Status: "success", Fields: map[string]string{}}
	for id, path := range spec.Outputs {
		value := loginValue(decoded, path)
		if value == nil {
			continue
		}
		text, ok := value.(string)
		if !ok || len(text) > 8192 {
			return failure("invalid_response")
		}
		result.Fields[id] = text
	}
	if spec.AccountInput != "" {
		result.Account = &ConnectionAccount{Login: inputs[spec.AccountInput], Name: inputs[spec.AccountInput]}
	}
	if spec.ExpiresInField != "" {
		value, ok := loginValue(decoded, spec.ExpiresInField).(float64)
		if !ok || value <= 0 || value > 31536000 {
			return failure("invalid_response")
		}
		result.ExpiresAt = time.Now().Add(time.Duration(value * float64(time.Second)))
	}
	return result
}
