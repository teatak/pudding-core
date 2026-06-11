package store

// settings 表的约定键,集中定义避免散落;对照表见 docs/contracts-checklist.md。
// 注意:不存在全局默认模型键——模型名只在 profile 下有意义,
// 默认模型是 provider_profiles.default_model。
const (
	// SettingSystemPrompt:contextbuilder 的 system instruction,空则用默认值。
	SettingSystemPrompt = "system_prompt"
	// SettingDefaultProvider:session.provider 为空时的默认 profile 名,
	// 再为空回落内置 "default"。
	SettingDefaultProvider = "provider.default"
)

// DefaultProviderProfile 是内置默认 profile 名。
const DefaultProviderProfile = "default"
