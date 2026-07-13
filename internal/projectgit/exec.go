package projectgit

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"sort"
	"strings"
)

const (
	metadataLimitBytes = 4 << 20
	stderrLimitBytes   = 16 << 10
)

type execResult struct {
	stdout    string
	stderr    string
	truncated bool
	err       error
}

type boundedBuffer struct {
	head      bytes.Buffer
	tail      []byte
	headLimit int
	tailLimit int
	total     int
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	written := len(data)
	b.total += written
	if b.head.Len() < b.headLimit {
		count := min(len(data), b.headLimit-b.head.Len())
		b.head.Write(data[:count])
		data = data[count:]
	}
	if len(data) > 0 {
		b.tail = append(b.tail, data...)
		if len(b.tail) > b.tailLimit {
			b.tail = append(b.tail[:0], b.tail[len(b.tail)-b.tailLimit:]...)
		}
	}
	return written, nil
}

func newBoundedBuffer(limit int) *boundedBuffer {
	limit = max(limit, 2)
	return &boundedBuffer{headLimit: limit / 2, tailLimit: limit - limit/2}
}

func (b *boundedBuffer) String() string {
	if !b.Truncated() {
		return b.head.String() + string(b.tail)
	}
	return b.head.String() + "\n... output truncated ...\n" + string(b.tail)
}

func (b *boundedBuffer) Truncated() bool {
	return b.total > b.headLimit+b.tailLimit
}

func run(ctx context.Context, dir string, stdoutLimit int, args ...string) execResult {
	gitArgs := []string{"--no-pager", "-c", "core.fsmonitor=false", "-c", "color.ui=false"}
	gitArgs = append(gitArgs, args...)
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	cmd.Dir = dir
	cmd.Env = gitEnvironment()
	stdout := newBoundedBuffer(stdoutLimit)
	stderr := newBoundedBuffer(stderrLimitBytes)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	return execResult{
		stdout:    stdout.String(),
		stderr:    strings.ToValidUTF8(stderr.String(), "�"),
		truncated: stdout.Truncated(),
		err:       err,
	}
}

func runWithoutExternalFilters(ctx context.Context, dir string, stdoutLimit int, args ...string) execResult {
	config := run(ctx, dir, metadataLimitBytes, "config", "--name-only", "--get-regexp", `^filter\..*\.(clean|smudge|process|required)$`)
	if config.err != nil && exitCode(config.err) != 1 {
		return config
	}
	if config.truncated {
		return execResult{err: errors.New("Git filter configuration exceeded the safety limit")}
	}
	drivers := make(map[string]bool)
	for _, key := range strings.Fields(config.stdout) {
		for _, suffix := range []string{".clean", ".smudge", ".process", ".required"} {
			if strings.HasPrefix(key, "filter.") && strings.HasSuffix(key, suffix) {
				driver := strings.TrimSuffix(strings.TrimPrefix(key, "filter."), suffix)
				if driver != "" {
					drivers[driver] = true
				}
			}
		}
	}
	names := make([]string, 0, len(drivers))
	for driver := range drivers {
		names = append(names, driver)
	}
	sort.Strings(names)
	safeArgs := make([]string, 0, len(names)*8+len(args))
	for _, driver := range names {
		safeArgs = append(safeArgs,
			"-c", "filter."+driver+".clean=cat",
			"-c", "filter."+driver+".smudge=cat",
			"-c", "filter."+driver+".process=",
			"-c", "filter."+driver+".required=false",
		)
	}
	safeArgs = append(safeArgs, args...)
	return run(ctx, dir, stdoutLimit, safeArgs...)
}

func gitEnvironment() []string {
	allowed := map[string]bool{
		"PATH": true, "HOME": true, "USER": true, "LOGNAME": true,
		"TMPDIR": true, "TMP": true, "TEMP": true,
		"SYSTEMROOT": true, "WINDIR": true, "COMSPEC": true, "PATHEXT": true,
		"USERPROFILE": true, "HOMEDRIVE": true, "HOMEPATH": true,
		"APPDATA": true, "LOCALAPPDATA": true, "DEVELOPER_DIR": true,
	}
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok && allowed[strings.ToUpper(key)] {
			values[strings.ToUpper(key)] = key + "=" + value
		}
	}
	for key, value := range map[string]string{
		"GIT_ATTR_NOSYSTEM": "1", "GIT_OPTIONAL_LOCKS": "0", "GIT_PAGER": "cat",
		"GIT_TERMINAL_PROMPT": "0", "LANG": "C", "LC_ALL": "C", "NO_COLOR": "1", "PAGER": "cat",
	} {
		values[key] = key + "=" + value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, values[key])
	}
	return env
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func execDetail(result execResult) string {
	if detail := strings.TrimSpace(result.stderr); detail != "" {
		return detail
	}
	if result.err != nil {
		return result.err.Error()
	}
	return "Git command failed"
}

func commandError(ctx context.Context, fallback string, result execResult) error {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return newError(CodeTimedOut, "Git command timed out", ctx.Err())
	case errors.Is(result.err, exec.ErrNotFound):
		return newError(CodeGitUnavailable, "Git executable is unavailable", result.err)
	default:
		return newError(fallback, execDetail(result), result.err)
	}
}
