package browser

import "github.com/teatak/pudding-core/contracts"

// The desktop consumes the same versioned contract during its build.
var tabLimits = contracts.Runtime().BrowserTabLimits
