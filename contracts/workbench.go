package contracts

import (
	_ "embed"
	"encoding/json"
)

//go:embed workbench.json
var workbenchJSON []byte

// WorkbenchPolicy is shared by Core validation and the Desktop compiler/bridge.
type WorkbenchPolicy struct {
	SchemaVersion         int    `json:"schemaVersion"`
	SDKVersion            string `json:"sdkVersion"`
	MaxFiles              int    `json:"maxFiles"`
	MaxPackageBytes       int    `json:"maxPackageBytes"`
	MaxFileBytes          int    `json:"maxFileBytes"`
	MaxSources            int    `json:"maxSources"`
	MaxOperations         int    `json:"maxOperations"`
	MaxRequestBytes       int    `json:"maxRequestBytes"`
	MaxResponseBytes      int    `json:"maxResponseBytes"`
	MaxConcurrentRequests int    `json:"maxConcurrentRequests"`
	RequestTimeoutMS      int    `json:"requestTimeoutMS"`
	BuildTimeoutMS        int    `json:"buildTimeoutMS"`
	MinRefreshMS          int    `json:"minRefreshMS"`
	MaxSchemaDepth        int    `json:"maxSchemaDepth"`
	MaxRequestsPerMinute  int    `json:"maxRequestsPerMinute"`
	MaxDiagnostics        int    `json:"maxDiagnostics"`
}

var workbenchPolicy = func() WorkbenchPolicy {
	var p WorkbenchPolicy
	if err := json.Unmarshal(workbenchJSON, &p); err != nil {
		panic(err)
	}
	if p.SchemaVersion < 1 || p.MaxFiles < 1 || p.MaxPackageBytes < p.MaxFileBytes || p.MaxConcurrentRequests < 1 {
		panic("invalid workbench policy")
	}
	return p
}()

func Workbench() WorkbenchPolicy { return workbenchPolicy }
