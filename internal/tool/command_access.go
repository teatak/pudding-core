package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CommandProjectAccess binds a one-invocation sandbox extension to directory
// identities. It is computed by the backend, never supplied by the model/UI.
type CommandProjectAccess struct {
	Dirs       []string
	identities []os.FileInfo
}

func PrepareCommandProjectAccess(call Call, risk ToolRisk, managedHome string) (*CommandProjectAccess, Call, error) {
	args, err := decodeCommandRunArgs(call.Args)
	if err != nil || args.Execution == CommandExecutionHost || risk.hostAccessRequired || len(risk.requiredProjectPaths) == 0 {
		return nil, call, err
	}
	cwd, err := resolveCommandCWD(call.ProjectDirs, args.CWD)
	if err != nil {
		return nil, call, err
	}
	// Runtime-owned files (including other sessions' artifacts) cannot be
	// promoted to general project directories by an operation approval.
	if managedHome != "" {
		root, err := filepath.EvalSymlinks(managedHome)
		if err != nil {
			return nil, call, err
		}
		for _, path := range risk.requiredProjectPaths {
			resolved, err := resolveExistingParent(path)
			if err != nil {
				return nil, call, err
			}
			if pathInsideRoot(resolved, root) {
				return nil, call, nil
			}
		}
	}
	plan := &CommandProjectAccess{}
	for _, dir := range commandProjectDirSuggestions(risk.requiredProjectPaths) {
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return nil, call, err
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, call, err
		}
		if !info.IsDir() {
			return nil, call, fmt.Errorf("not a directory: %s", dir)
		}
		plan.Dirs = append(plan.Dirs, resolved)
		plan.identities = append(plan.identities, info)
	}
	if len(plan.Dirs) == 0 {
		return nil, call, nil
	}
	// Adding roots must not change the implicit working directory of the command
	// that the model requested and that the user is about to review.
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(call.Args, &payload); err != nil {
		return nil, call, err
	}
	payload["cwd"], _ = json.Marshal(cwd)
	call.Args, err = json.Marshal(payload)
	call.ProjectDirs = normalizeProjectDirs(append(append([]string(nil), call.ProjectDirs...), plan.Dirs...))
	return plan, call, err
}

func (p *CommandProjectAccess) Validate() error {
	for i, dir := range p.Dirs {
		info, err := os.Lstat(dir)
		if err != nil {
			return err
		}
		if !info.IsDir() || !os.SameFile(info, p.identities[i]) {
			return fmt.Errorf("approved directory changed: %s", dir)
		}
	}
	return nil
}
