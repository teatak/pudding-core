package engine

import "github.com/teatak/pudding-core/internal/tool"

func onlyNextPromptCalls(calls []tool.Call) bool {
	if len(calls) == 0 {
		return false
	}
	for _, call := range calls {
		if call.Name != tool.SetNextPrompt {
			return false
		}
	}
	return true
}
