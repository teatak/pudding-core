package canvas

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/teatak/pudding-core/contracts"
)

func ValidPointer(pointer string) bool {
	if pointer != "" && !strings.HasPrefix(pointer, "/") {
		return false
	}
	for i := 0; i < len(pointer); i++ {
		if pointer[i] == '~' {
			i++
			if i >= len(pointer) || (pointer[i] != '0' && pointer[i] != '1') {
				return false
			}
		}
	}
	return true
}

func Pointer(value any, pointer string) (any, error) {
	if !ValidPointer(pointer) {
		return nil, errors.New("invalid JSON pointer")
	}
	if pointer == "" {
		return value, nil
	}
	for _, token := range strings.Split(pointer[1:], "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		switch current := value.(type) {
		case map[string]any:
			var found bool
			value, found = current[token]
			if !found {
				return nil, fmt.Errorf("missing field at %s", pointer)
			}
		case []any:
			i, err := strconv.Atoi(token)
			if err != nil || i < 0 || i >= len(current) || strconv.Itoa(i) != token {
				return nil, fmt.Errorf("invalid array index at %s", pointer)
			}
			value = current[i]
		default:
			return nil, fmt.Errorf("missing field at %s", pointer)
		}
	}
	return value, nil
}

func ValidateSchema(schema map[string]any) error { return validateSchema(schema, 0) }

func validateSchema(s map[string]any, depth int) error {
	if depth > contracts.Canvas().MaxSchemaDepth {
		return errors.New("schema too deep")
	}
	t, _ := s["type"].(string)
	allowed := map[string]bool{"type": true, "description": true, "enum": true}
	switch t {
	case "object":
		allowed["properties"], allowed["required"], allowed["additionalProperties"] = true, true, true
		props, ok := s["properties"].(map[string]any)
		if !ok {
			return errors.New("object properties must be an object")
		}
		if _, ok := s["additionalProperties"].(bool); !ok {
			return errors.New("object additionalProperties must be explicit boolean")
		}
		for _, child := range props {
			sub, ok := child.(map[string]any)
			if !ok {
				return errors.New("property schema must be an object")
			}
			if err := validateSchema(sub, depth+1); err != nil {
				return err
			}
		}
		if raw, exists := s["required"]; exists {
			required, ok := raw.([]any)
			if !ok {
				return errors.New("required must be an array")
			}
			seen := map[string]bool{}
			for _, item := range required {
				key, ok := item.(string)
				if !ok || props[key] == nil || seen[key] {
					return errors.New("required references missing or duplicate property")
				}
				seen[key] = true
			}
		}
	case "array":
		allowed["items"], allowed["minItems"], allowed["maxItems"] = true, true, true
		items, ok := s["items"].(map[string]any)
		if !ok {
			return errors.New("array items schema required")
		}
		if err := validateSchema(items, depth+1); err != nil {
			return err
		}
	case "string":
		allowed["minLength"], allowed["maxLength"] = true, true
	case "number", "integer":
		allowed["minimum"], allowed["maximum"] = true, true
	case "boolean", "null":
	default:
		return errors.New("unsupported schema type")
	}
	for key := range s {
		if !allowed[key] {
			return fmt.Errorf("unsupported schema keyword %q", key)
		}
	}
	if raw, ok := s["description"]; ok {
		if _, ok := raw.(string); !ok {
			return errors.New("description must be a string")
		}
	}
	if raw, ok := s["enum"]; ok {
		items, ok := raw.([]any)
		if !ok || len(items) == 0 {
			return errors.New("enum must be a nonempty array")
		}
	}
	for _, pair := range [][2]string{{"minLength", "maxLength"}, {"minItems", "maxItems"}, {"minimum", "maximum"}} {
		for _, key := range pair {
			if raw, exists := s[key]; exists {
				number, ok := raw.(float64)
				if !ok || math.IsNaN(number) || math.IsInf(number, 0) || (key != "minimum" && key != "maximum" && (number < 0 || math.Trunc(number) != number)) {
					return fmt.Errorf("invalid %s", key)
				}
			}
		}
		min, minOK := s[pair[0]].(float64)
		max, maxOK := s[pair[1]].(float64)
		if minOK && maxOK && min > max {
			return errors.New("schema minimum exceeds maximum")
		}
	}
	return nil
}

func ValidateInput(schema map[string]any, value any) error {
	if err := ValidateSchema(schema); err != nil {
		return err
	}
	return validateValue(schema, value, "", 0)
}

func validateValue(s map[string]any, value any, at string, depth int) error {
	if depth > contracts.Canvas().MaxSchemaDepth {
		return errors.New("input too deep")
	}
	if items, ok := s["enum"].([]any); ok {
		found := false
		for _, item := range items {
			if reflect.DeepEqual(item, value) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s: value outside enum", at)
		}
	}
	var size float64
	var minKey, maxKey string
	switch s["type"] {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected object", at)
		}
		props := s["properties"].(map[string]any)
		if required, ok := s["required"].([]any); ok {
			for _, key := range required {
				if _, ok := object[key.(string)]; !ok {
					return fmt.Errorf("%s: missing %s", at, key)
				}
			}
		}
		for key, item := range object {
			if child, ok := props[key].(map[string]any); ok {
				if err := validateValue(child, item, at+"/"+key, depth+1); err != nil {
					return err
				}
			} else if s["additionalProperties"] != true {
				return fmt.Errorf("%s: undeclared field %s", at, key)
			}
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s: expected array", at)
		}
		for i, child := range items {
			if err := validateValue(s["items"].(map[string]any), child, fmt.Sprintf("%s/%d", at, i), depth+1); err != nil {
				return err
			}
		}
		size, minKey, maxKey = float64(len(items)), "minItems", "maxItems"
	case "string":
		v, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s: expected string", at)
		}
		size, minKey, maxKey = float64(utf8.RuneCountInString(v)), "minLength", "maxLength"
	case "number", "integer":
		v, ok := value.(float64)
		if n, isNumber := value.(json.Number); isNumber {
			var err error
			v, err = n.Float64()
			ok = err == nil
		}
		if !ok || math.IsNaN(v) || math.IsInf(v, 0) || (s["type"] == "integer" && math.Trunc(v) != v) {
			return fmt.Errorf("%s: invalid number", at)
		}
		size, minKey, maxKey = v, "minimum", "maximum"
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s: expected boolean", at)
		}
	case "null":
		if value != nil {
			return fmt.Errorf("%s: expected null", at)
		}
	}
	if min, ok := s[minKey].(float64); ok && size < min {
		return fmt.Errorf("%s: below %s", at, minKey)
	}
	if max, ok := s[maxKey].(float64); ok && size > max {
		return fmt.Errorf("%s: above %s", at, maxKey)
	}
	return nil
}
