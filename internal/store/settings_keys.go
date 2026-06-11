package store

// settings 表的约定键,集中定义避免散落;对照表见 docs/contracts-checklist.md。
const (
	// SettingSystemPrompt:contextbuilder 的 system instruction,空则用默认值。
	SettingSystemPrompt = "system_prompt"
	// SettingOpenAIBaseURL:OpenAI-compatible 端点;settings 优先,
	// 环境变量 PUDDING_OPENAI_BASE_URL 兜底。
	SettingOpenAIBaseURL = "provider.openai.base_url"
	// SettingOpenAIAPIKey:可为空(本地端点如 Ollama 不需要)。
	SettingOpenAIAPIKey = "provider.openai.api_key"
	// SettingDefaultModel:session.model 为空时的默认模型,优先于 --model flag。
	SettingDefaultModel = "model.default"
)
