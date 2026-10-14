package api

import (
	"strings"

	"github.com/teatak/pudding-core/internal/store"
)

// Keep these families aligned with the model brand keywords in desktop's
// provider/presets.ts. A relay profile's brand does not identify its models.
var modelFamilyKeywords = [][]string{
	{"deepseek"},
	{"anthropic", "claude"},
	{"gpt", "chatgpt", "o1", "o3", "o4", "o5"},
	{"gemini", "gemma"},
	{"qwen"},
	{"mimo"},
	{"moonshot", "kimi"},
	{"zhipu", "glm", "zai"},
	{"grok"},
}

func modelFamily(value string) string {
	tokens := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	for _, keywords := range modelFamilyKeywords {
		for _, token := range tokens {
			for _, keyword := range keywords {
				if strings.HasPrefix(token, keyword) {
					return keywords[0]
				}
			}
		}
	}
	return ""
}

// Group at each family's first occurrence without sorting within a family.
// Appending models before grouping puts new members after the existing ones.
// Unknown models remain separate entries, rather than one unrelated family.
func groupModelsByFamily(models []store.ProviderModel) []store.ProviderModel {
	groups := make([][]store.ProviderModel, 0, len(models))
	indices := make(map[string]int)
	for _, model := range models {
		family := modelFamily(model.ID)
		if family == "" {
			family = modelFamily(model.DisplayName)
		}
		if index, ok := indices[family]; family != "" && ok {
			groups[index] = append(groups[index], model)
			continue
		}
		if family != "" {
			indices[family] = len(groups)
		}
		groups = append(groups, []store.ProviderModel{model})
	}
	ordered := make([]store.ProviderModel, 0, len(models))
	for _, group := range groups {
		ordered = append(ordered, group...)
	}
	return ordered
}
