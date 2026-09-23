package engine

import (
	"fmt"

	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/tool"
)

func (e *Engine) classifyToolCall(sessionID string, call tool.Call) (tool.ToolRisk, bool, error) {
	var managedDirs []string
	if call.Name == tool.CommandRun {
		path, exists, err := home.ExistingSessionArtifacts(e.attachmentHome, sessionID)
		if err != nil {
			return tool.ToolRisk{}, false, fmt.Errorf("resolve command artifacts: %w", err)
		}
		if exists {
			managedDirs = append(managedDirs, path)
		}
	}
	risk, classified := tool.ClassifyToolCallForProject(call.Name, call.Args, call.ProjectDirs, managedDirs...)
	return risk, classified, nil
}
