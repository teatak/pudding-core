package contracts

import (
	_ "embed"
	"encoding/json"
)

//go:embed widget.json
var widgetJSON []byte

// WidgetPolicy is shared by Core validation and the Desktop compiler/bridge.
type WidgetDistributionPolicy struct {
	Kind               string `json:"kind"`
	SchemaVersion      int    `json:"schemaVersion"`
	MinProtocolVersion int    `json:"minProtocolVersion"`
	MaxBytes           int    `json:"maxBytes"`
	MaxIconBytes       int    `json:"maxIconBytes"`
}

type WidgetPolicy struct {
	Distribution          WidgetDistributionPolicy `json:"distribution"`
	SchemaVersion         int                      `json:"schemaVersion"`
	SDKVersion            string                   `json:"sdkVersion"`
	MaxFiles              int                      `json:"maxFiles"`
	MaxPackageBytes       int                      `json:"maxPackageBytes"`
	MaxFileBytes          int                      `json:"maxFileBytes"`
	MaxSources            int                      `json:"maxSources"`
	MaxOperations         int                      `json:"maxOperations"`
	MaxRequestBytes       int                      `json:"maxRequestBytes"`
	MaxResponseBytes      int                      `json:"maxResponseBytes"`
	MaxConcurrentRequests int                      `json:"maxConcurrentRequests"`
	RequestTimeoutMS      int                      `json:"requestTimeoutMS"`
	BuildTimeoutMS        int                      `json:"buildTimeoutMS"`
	MinRefreshMS          int                      `json:"minRefreshMS"`
	MaxSchemaDepth        int                      `json:"maxSchemaDepth"`
	MaxRequestsPerMinute  int                      `json:"maxRequestsPerMinute"`
	MaxDiagnostics        int                      `json:"maxDiagnostics"`
	MaxStorageBytes       int                      `json:"maxStorageBytes"`
}

var widgetPolicy = func() WidgetPolicy {
	var p WidgetPolicy
	if err := json.Unmarshal(widgetJSON, &p); err != nil {
		panic(err)
	}
	if p.SchemaVersion < 1 || p.MaxFiles < 1 || p.MaxPackageBytes < p.MaxFileBytes || p.MaxConcurrentRequests < 1 {
		panic("invalid widget policy")
	}
	return p
}()

func Widget() WidgetPolicy { return widgetPolicy }
