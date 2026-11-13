package tool

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
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
	allowedFields []string
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
	decoder.UseNumber()
	if err := validateToolArgumentValue(decoder, reflect.TypeOf(target), ""); err != nil {
		return err
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return classifyToolArgumentError(err)
	}
	return nil
}

// encoding/json accepts case-insensitive field names and null scalar values.
// Inspect every raw value against the target's JSON tags before decoding so
// neither can silently replace a requested mutation with a zero value.
func validateToolArgumentValue(decoder *json.Decoder, valueType reflect.Type, field string) *toolArgumentError {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	token, err := decoder.Token()
	if err != nil {
		return classifyToolArgumentError(err)
	}
	typeError := func(value string) *toolArgumentError {
		return classifyToolArgumentError(&json.UnmarshalTypeError{Value: value, Type: valueType, Field: field, Offset: decoder.InputOffset()})
	}
	switch token {
	case nil:
		return typeError("null")
	case json.Delim('{'):
		if valueType.Kind() != reflect.Struct {
			return typeError("object")
		}
		fields := make(map[string]reflect.Type)
		for _, declared := range reflect.VisibleFields(valueType) {
			jsonName, _, _ := strings.Cut(declared.Tag.Get("json"), ",")
			if declared.IsExported() && jsonName != "" && jsonName != "-" {
				fields[jsonName] = declared.Type
			}
		}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return classifyToolArgumentError(err)
			}
			name := key.(string)
			fieldType := fields[name]
			if fieldType == nil {
				argumentErr := classifyToolArgumentError(errors.New("json: unknown field " + strconv.Quote(name)))
				if field != "" {
					argumentErr.field = field + "." + name
				}
				for allowed := range fields {
					argumentErr.allowedFields = append(argumentErr.allowedFields, allowed)
				}
				sort.Strings(argumentErr.allowedFields)
				argumentErr.hint = "Replace or remove the unsupported field. Allowed fields in this object: " + strings.Join(argumentErr.allowedFields, ", ") + "."
				return argumentErr
			}
			childField := name
			if field != "" {
				childField = field + "." + name
			}
			if err := validateToolArgumentValue(decoder, fieldType, childField); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
	case json.Delim('['):
		if valueType.Kind() != reflect.Array && valueType.Kind() != reflect.Slice {
			return typeError("array")
		}
		for index := 0; decoder.More(); index++ {
			if err := validateToolArgumentValue(decoder, valueType.Elem(), field+"["+strconv.Itoa(index)+"]"); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
	}
	// Validate scalar types here too, so errors name JSON fields rather than
	// encoding/json's internal Go embedding path (e.g. codePositionArgs.scope).
	switch value := token.(type) {
	case string:
		if valueType.Kind() != reflect.String {
			return typeError("string")
		}
	case bool:
		if valueType.Kind() != reflect.Bool {
			return typeError("bool")
		}
	case json.Number:
		var numberErr error
		switch valueType.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			_, numberErr = strconv.ParseInt(string(value), 10, valueType.Bits())
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			_, numberErr = strconv.ParseUint(string(value), 10, valueType.Bits())
		case reflect.Float32, reflect.Float64:
			_, numberErr = strconv.ParseFloat(string(value), valueType.Bits())
		default:
			return typeError("number")
		}
		if numberErr != nil {
			return typeError("number")
		}
	}
	if err != nil {
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
	if len(argumentErr.allowedFields) != 0 {
		payload["allowedFields"] = argumentErr.allowedFields
	}
	if argumentErr.offset > 0 {
		payload["offset"] = argumentErr.offset
	}
	out = toolJSON(out, false, payload)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(payload)
	return out
}
