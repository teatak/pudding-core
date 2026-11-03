package contracts

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

type IconSpec struct {
	SVG        string          `json:"svg,omitempty" yaml:"svg,omitempty"`
	Color      *IconThemeColor `json:"color,omitempty" yaml:"color,omitempty"`
	Background *IconThemeColor `json:"background,omitempty" yaml:"background,omitempty"`
}
type IconThemeColor struct {
	Light string `json:"light,omitempty" yaml:"light,omitempty"`
	Dark  string `json:"dark,omitempty" yaml:"dark,omitempty"`
}

var iconColorLiteral = regexp.MustCompile(`(?i)^(#[0-9a-fA-F]{3,4}|#[0-9a-fA-F]{6}|#[0-9a-fA-F]{8}|[a-zA-Z]+|(rgb|rgba|hsl|hsla|hwb|lab|lch|oklab|oklch|color)\([a-zA-Z0-9\s.,%+\-/]+\))$`)

func ValidateIconColors(spec IconSpec) error {
	for _, theme := range []*IconThemeColor{spec.Color, spec.Background} {
		if theme == nil {
			continue
		}
		for _, value := range []string{theme.Light, theme.Dark} {
			value = strings.TrimSpace(value)
			if len(value) > 128 || (value != "" && !iconColorLiteral.MatchString(value)) {
				return errors.New("invalid icon color")
			}
		}
	}
	return nil
}

// Icon preserves legacy strings and structured IconSpec values in the existing
// icon TEXT column. Structured values are JSON on the wire and canonical JSON
// inside the column; named icons and embedded images remain ordinary strings.
type Icon string

func (icon Icon) Spec() (*IconSpec, error) {
	if !strings.HasPrefix(string(icon), "{") {
		return nil, nil
	}
	var spec IconSpec
	decoder := json.NewDecoder(strings.NewReader(string(icon)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("invalid icon object")
	}
	spec.SVG = strings.TrimSpace(spec.SVG)
	if spec.SVG == "" {
		return nil, errors.New("icon svg is required")
	}
	if err := ValidateIconColors(spec); err != nil {
		return nil, err
	}
	for _, theme := range []*IconThemeColor{spec.Color, spec.Background} {
		if theme != nil {
			theme.Light = strings.TrimSpace(theme.Light)
			theme.Dark = strings.TrimSpace(theme.Dark)
		}
	}
	return &spec, nil
}

func (icon Icon) MarshalJSON() ([]byte, error) {
	spec, err := icon.Spec()
	if err != nil {
		return nil, err
	}
	if spec != nil {
		return json.Marshal(spec)
	}
	return json.Marshal(string(icon))
}
func (icon *Icon) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		*icon = ""
		return nil
	}
	if len(data) > 0 && data[0] == '{' {
		spec, err := Icon(data).Spec()
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(spec)
		if err != nil {
			return err
		}
		*icon = Icon(encoded)
		return nil
	}
	var legacy string
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	if strings.HasPrefix(legacy, "{") {
		return errors.New("icon object must not be encoded as a string")
	}
	*icon = Icon(legacy)
	return nil
}
