// Package contracts owns the public wire contract consumed by daemon clients.
package contracts

import (
	_ "embed"
	"encoding/json"
)

//go:embed runtime.json
var runtimeJSON []byte

type Limits struct {
	PerSession int `json:"perSession"`
	Total      int `json:"total"`
}

type RuntimeContract struct {
	ProtocolVersion  int    `json:"protocolVersion"`
	BrowserTabLimits Limits `json:"browserTabLimits"`
}

var runtimeContract = func() RuntimeContract {
	var contract RuntimeContract
	if err := json.Unmarshal(runtimeJSON, &contract); err != nil {
		panic(err)
	}
	if contract.ProtocolVersion < 1 || contract.BrowserTabLimits.PerSession < 1 || contract.BrowserTabLimits.Total < contract.BrowserTabLimits.PerSession {
		panic("invalid public runtime contract")
	}
	return contract
}()

// Runtime returns a value copy of the authoritative policy.
func Runtime() RuntimeContract { return runtimeContract }
