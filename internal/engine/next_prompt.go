package engine

import (
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/tool"
)

func onlyTurnMetadataCalls(calls []tool.Call, defs []provider.ToolDef) bool {
	if len(calls) == 0 {
		return false
	}
	for _, call := range calls {
		if (call.Name != tool.SetNextPrompt && call.Name != tool.SetTurnTopic) || !tool.HasDefinition(defs, call.Name) {
			return false
		}
	}
	return true
}
