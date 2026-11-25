package tool

import "runtime"

func commandRunDescription() string {
	const result = " Check exitCode, signal, timedOut and cancelled; ok alone does not verify success. Run tests directly: pipelines, negation or masked exits can hide failures (verificationWarning). For large output read outputFiles via file_slice/file_search; result_ref contains the bounded result. Poll yielded foreground work via builtin_command_session until done before ending the turn. Use background=true for work that must survive the turn."
	if runtime.GOOS == "windows" {
		return "Run one PowerShell 7 command with execution=host and a concrete host_access_reason. Ask/Auto require approval per call. Use $env:NAME and & for quoted executables. No shell nesting, profiles, tty or shell background operators. Save long commands as project scripts; never switch shells to evade a refusal." + result
	}
	return "Run one POSIX /bin/sh command in the project sandbox. Ask for the directory when another local path is needed. Use host only for an invocation needing host services or credentials, with a concrete reason and separate Ask/Auto approval. Use $TMPDIR and python3 -m pip install --user for sandbox dependencies. Homebrew and common toolchain directories are already on PATH; try the executable directly before overriding PATH. Keep TLS verification enabled." + result
}
