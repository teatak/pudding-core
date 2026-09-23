package tool

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
)

type toolArgumentError struct {
	kind          string
	detail        string
	hint          string
	field         string
	expected      string
	offset        int64
	receivedBytes int
}

func (e *toolArgumentError) Error() string { return e.detail }

type toolArgumentField struct {
	name     string
	expected string
}

// decodeToolArgumentObject checks complete JSON before its schema. A syntax EOF
// describes the received arguments; it does not establish a provider size limit.
// Diagnostic errors retain the byte count and parser offset, not the input body.
func decodeToolArgumentObject(raw json.RawMessage, target any, required ...toolArgumentField) (argumentErr *toolArgumentError) {
	defer func() {
		if argumentErr != nil {
			argumentErr.receivedBytes = len(raw)
		}
	}()
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return &toolArgumentError{kind: "missing_arguments", detail: "tool arguments are empty", hint: "Pass one JSON object matching the tool schema."}
	}
	if trimmed[0] != '{' {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return classifyToolArgumentError(err)
		}
		hint := "Pass one JSON object matching the tool schema."
		if trimmed[0] == '"' {
			hint = "Pass the object directly instead of a JSON-encoded string."
		}
		return &toolArgumentError{kind: "expected_object", detail: "tool arguments must be a JSON object", hint: hint, expected: "object"}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return classifyToolArgumentError(err)
	}
	for _, field := range required {
		if _, ok := fields[field.name]; !ok {
			return &toolArgumentError{
				kind: "missing_field", detail: "required field is missing: " + field.name,
				hint: "Add the required " + field.name + " field and retry.", field: field.name, expected: field.expected,
			}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return classifyToolArgumentError(err)
	}
	return nil
}

func classifyToolArgumentError(err error) *toolArgumentError {
	const unknownPrefix = "json: unknown field "
	if strings.HasPrefix(err.Error(), unknownPrefix) {
		field, _ := strconv.Unquote(strings.TrimPrefix(err.Error(), unknownPrefix))
		return &toolArgumentError{kind: "unknown_field", detail: err.Error(), hint: "Remove the unsupported field and use the current tool schema.", field: field}
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		kind := "invalid_json"
		hint := "Correct the JSON syntax near the reported byte offset and retry."
		if strings.Contains(strings.ToLower(syntaxErr.Error()), "unexpected end") {
			kind = "truncated_json"
			hint = "Finish all JSON strings, arrays, and objects, then resend the complete arguments."
		}
		return &toolArgumentError{kind: kind, detail: syntaxErr.Error(), hint: hint, offset: syntaxErr.Offset}
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return &toolArgumentError{
			kind: "invalid_type", detail: typeErr.Error(), hint: "Use the JSON type required by the tool schema and retry.",
			field: typeErr.Field, expected: toolArgumentExpectedType(typeErr.Type), offset: typeErr.Offset,
		}
	}
	return &toolArgumentError{kind: "invalid_json", detail: err.Error(), hint: "Pass one valid JSON object matching the tool schema."}
}

func toolArgumentExpectedType(value reflect.Type) string {
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.String:
		return "string"
	case reflect.Array, reflect.Slice:
		return "array"
	case reflect.Map, reflect.Struct:
		return "object"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	default:
		return "schema-compatible value"
	}
}

func toolArgumentFailure(out Result, argumentErr *toolArgumentError) Result {
	payload := map[string]any{
		"ok": false, "reason": "invalid_arguments", "errorKind": argumentErr.kind,
		"detail": argumentErr.detail, "receivedBytes": argumentErr.receivedBytes,
	}
	if argumentErr.hint != "" {
		payload["hint"] = argumentErr.hint
	}
	if argumentErr.field != "" {
		payload["field"] = argumentErr.field
	}
	if argumentErr.expected != "" {
		payload["expected"] = argumentErr.expected
	}
	if argumentErr.offset > 0 {
		payload["offset"] = argumentErr.offset
	}
	out = toolJSON(out, false, payload)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(payload)
	return out
}
