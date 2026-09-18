package provider

// ModelCandidate carries only metadata explicitly supplied by a discovery endpoint.
// Missing capabilities remain absent so the importer can fill them from presets;
// false means the endpoint explicitly reports that a capability is unsupported.
type ModelCandidate struct {
	ID             string          `json:"id"`
	DisplayName    string          `json:"displayName,omitempty"`
	ContextWindow  int             `json:"contextWindow,omitempty"`
	CostMultiplier *float64        `json:"costMultiplier,omitempty"`
	Capabilities   map[string]bool `json:"capabilities,omitempty"`
	Limits         *ModelLimits    `json:"limits,omitempty"`
}
