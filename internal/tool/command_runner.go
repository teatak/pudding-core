package tool

import "os/exec"

// commandSpec is shared by foreground and background command tools. A runner
// may wrap the executable, while the caller continues to own its lifecycle.
type commandSpec struct {
	Executable  string
	Args        []string
	CWD         string
	Env         []string
	ProjectDirs []string
	SandboxMode CommandSandboxMode
}

type commandExecution struct {
	Cmd         *exec.Cmd
	Sandboxed   bool
	SandboxKind string
}

type commandRunner interface {
	Prepare(commandSpec) (*commandExecution, error)
}

type directCommandRunner struct{}

func newDirectCommandRunner() commandRunner {
	return directCommandRunner{}
}

func (r *BuiltinRunner) setCommandRunner(commands commandRunner) {
	if commands == nil {
		commands = newDirectCommandRunner()
	}
	r.commands = commands
	r.processes.commands = commands
}

func (directCommandRunner) Prepare(spec commandSpec) (*commandExecution, error) {
	cmd := exec.Command(spec.Executable, spec.Args...)
	cmd.Dir = spec.CWD
	cmd.Env = append([]string(nil), spec.Env...)
	configureCommandProcess(cmd)
	return &commandExecution{Cmd: cmd}, nil
}
