package browser

import (
	_ "embed"
	"encoding/json"
)

// tabLimitsJSON is also read by Electron's BrowserHost. Keep runtime capacity
// in one policy file so the desktop and daemon cannot ship different limits.
//
//go:embed tab_limits.json
var tabLimitsJSON []byte

var tabLimits = func() struct{ PerSession, Total int } {
	var limits struct{ PerSession, Total int }
	if err := json.Unmarshal(tabLimitsJSON, &limits); err != nil {
		panic(err)
	}
	if limits.PerSession < 1 || limits.Total < limits.PerSession {
		panic("invalid embedded browser tab limits")
	}
	return limits
}()
