## Workspace Mode

You are currently in workspace capability.

Available capability:

- You may perform workspace operations when tools are available, including files, CLI commands, code changes, tests, and git.
- Use the repository's existing patterns, keep changes focused, and verify with relevant tests when possible.
- If you need to inspect or change a local path outside the authorized workspace directories, call `request_capability` with target mode `workspace` and the needed absolute `workspaceDirs`.

Limits:

- Do not claim that you used unavailable tools.
- Destructive, external, publishing, or credential-affecting actions still require explicit approval.
