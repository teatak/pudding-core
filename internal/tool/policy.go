package tool

import (
	"encoding/json"
	"strings"
)

type RiskClass string

const (
	RiskClassWrite       RiskClass = "write"
	RiskClassDestructive RiskClass = "destructive"
)

type ToolRisk struct {
	Class     RiskClass `json:"class"`
	Operation string    `json:"operation"`
	Scope     string    `json:"scope"`
	Paths     []string  `json:"paths,omitempty"`
	Summary   string    `json:"summary"`
}

func ClassifyToolCall(name string, raw json.RawMessage) (ToolRisk, bool) {
	var args struct {
		Scope     string `json:"scope"`
		Path      string `json:"path"`
		FromPath  string `json:"from_path"`
		ToPath    string `json:"to_path"`
		Recursive bool   `json:"recursive"`
		Overwrite bool   `json:"overwrite"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil {
		return ToolRisk{}, false
	}
	args.Scope = strings.TrimSpace(args.Scope)
	if !isProjectFileScope(args.Scope) {
		return ToolRisk{}, false
	}
	args.Scope = managedScopeProject
	switch name {
	case FileWrite:
		path := strings.TrimSpace(args.Path)
		return ToolRisk{Class: RiskClassWrite, Operation: "write", Scope: args.Scope, Paths: compactRiskPaths(path), Summary: "Overwrite a project file."}, true
	case FilePatch:
		path := strings.TrimSpace(args.Path)
		return ToolRisk{Class: RiskClassWrite, Operation: "patch", Scope: args.Scope, Paths: compactRiskPaths(path), Summary: "Modify a project file."}, true
	case FileDelete:
		path := strings.TrimSpace(args.Path)
		summary := "Delete a project file."
		if args.Recursive {
			summary = "Delete a project path recursively."
		}
		return ToolRisk{Class: RiskClassDestructive, Operation: "delete", Scope: args.Scope, Paths: compactRiskPaths(path), Summary: summary}, true
	case FileMove:
		return ToolRisk{Class: RiskClassWrite, Operation: "move", Scope: args.Scope, Paths: compactRiskPaths(args.FromPath, args.ToPath), Summary: "Move or rename a project path."}, true
	case FileCopy:
		summary := "Copy a project path."
		if args.Overwrite {
			summary = "Copy a project path and overwrite the destination if it exists."
		}
		return ToolRisk{Class: RiskClassWrite, Operation: "copy", Scope: args.Scope, Paths: compactRiskPaths(args.FromPath, args.ToPath), Summary: summary}, true
	default:
		return ToolRisk{}, false
	}
}

func compactRiskPaths(paths ...string) []string {
	out := make([]string, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}
