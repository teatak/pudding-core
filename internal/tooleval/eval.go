// Package tooleval runs deterministic CLI-versus-structured-tool checks.
package tooleval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/tool"
)

type Report struct {
	GeneratedAt          time.Time `json:"generatedAt"`
	Platform             string    `json:"platform"`
	Cases                []Case    `json:"cases"`
	Passed               int       `json:"passed"`
	Failed               int       `json:"failed"`
	Skipped              int       `json:"skipped"`
	DedicatedResultBytes int       `json:"dedicatedResultBytes"`
	CLIResultBytes       int       `json:"cliResultBytes"`
}

type Case struct {
	Name                 string `json:"name"`
	Domain               string `json:"domain"`
	DedicatedTool        string `json:"dedicatedTool"`
	CLI                  string `json:"cli"`
	Passed               bool   `json:"passed"`
	Skipped              bool   `json:"skipped,omitempty"`
	Detail               string `json:"detail,omitempty"`
	DedicatedResultBytes int    `json:"dedicatedResultBytes"`
	CLIResultBytes       int    `json:"cliResultBytes"`
}

type fixture struct {
	root   string
	runner *tool.BuiltinRunner
	git    bool
}

type toolOutput struct {
	result tool.Result
	value  map[string]any
}

func Run(ctx context.Context, now time.Time) (Report, error) {
	root, err := os.MkdirTemp("", "pudding-code-eval-")
	if err != nil {
		return Report{}, err
	}
	defer os.RemoveAll(root)

	f := &fixture{root: root, runner: tool.NewBuiltinRunner()}
	defer f.runner.Close()
	if err := f.prepare(); err != nil {
		return Report{}, err
	}
	report := Report{GeneratedAt: now.UTC(), Platform: runtime.GOOS + "/" + runtime.GOARCH}
	report.Cases = append(report.Cases,
		f.evalFileList(ctx),
		f.evalFileStat(ctx),
		f.evalFileSearch(ctx),
		f.evalFileSlice(ctx),
	)
	if f.git {
		report.Cases = append(report.Cases, f.evalGitStatus(ctx), f.evalGitDiff(ctx), f.evalGitLog(ctx))
	} else {
		for _, name := range []string{"git_status", "git_diff", "git_log"} {
			report.Cases = append(report.Cases, Case{Name: name, Domain: "git", CLI: "git", Skipped: true, Detail: "git executable unavailable"})
		}
	}
	for _, item := range report.Cases {
		report.DedicatedResultBytes += item.DedicatedResultBytes
		report.CLIResultBytes += item.CLIResultBytes
		switch {
		case item.Skipped:
			report.Skipped++
		case item.Passed:
			report.Passed++
		default:
			report.Failed++
		}
	}
	return report, nil
}

func (f *fixture) prepare() error {
	if err := os.MkdirAll(filepath.Join(f.root, "src"), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(f.root, "README.md"), []byte("alpha\nEvalNeedle\nomega\n"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(f.root, "src", "main.go"), []byte("package fixture\n\nconst marker = \"EvalNeedle\"\n\nfunc main() {}\n"), 0o600); err != nil {
		return err
	}
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}
	commands := [][]string{
		{"git", "init", "-q"},
		{"git", "add", "README.md", "src/main.go"},
		{"git", "-c", "user.name=Pudding Eval", "-c", "user.email=eval@pudding.local", "commit", "-q", "-m", "fixture"},
	}
	for _, argv := range commands {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = f.root
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("prepare eval fixture %q: %w: %s", strings.Join(argv, " "), err, strings.TrimSpace(string(output)))
		}
	}
	if err := os.WriteFile(filepath.Join(f.root, "README.md"), []byte("alpha\nEvalNeedle\nomega\nchanged\n"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(f.root, "untracked.txt"), []byte("new\n"), 0o600); err != nil {
		return err
	}
	f.git = true
	return nil
}

func (f *fixture) evalFileList(ctx context.Context) Case {
	return f.compare(ctx, "file_list", "file", tool.FileList, map[string]any{
		"scope": "project", "path": ".", "max_entries": 200,
	}, []string{"find", ".", "-mindepth", "1", "-maxdepth", "1", "-print"}, func(dedicated, cli map[string]any) (bool, string) {
		want := stringSetFromRecords(dedicated["entries"], "name")
		got := lineSet(stringValue(cli, "stdout"), func(value string) string { return filepath.Base(value) })
		return sameStrings(want, got), fmt.Sprintf("entries dedicated=%d cli=%d", len(want), len(got))
	})
}

func (f *fixture) evalFileStat(ctx context.Context) Case {
	return f.compare(ctx, "file_stat", "file", tool.FileStat, map[string]any{
		"scope": "project", "path": "README.md",
	}, []string{"wc", "-c", "README.md"}, func(dedicated, cli map[string]any) (bool, string) {
		fields := strings.Fields(stringValue(cli, "stdout"))
		cliSize := -1
		if len(fields) > 0 {
			cliSize, _ = strconv.Atoi(fields[0])
		}
		want := intValue(dedicated, "size")
		return want == cliSize, fmt.Sprintf("bytes dedicated=%d cli=%d", want, cliSize)
	})
}

func (f *fixture) evalFileSearch(ctx context.Context) Case {
	return f.compare(ctx, "file_search", "file", tool.FileSearch, map[string]any{
		"scope": "project", "path": ".", "query": "EvalNeedle", "mode": "literal", "max_results": 100,
	}, []string{"grep", "-n", "-F", "EvalNeedle", "README.md", "src/main.go"}, func(dedicated, cli map[string]any) (bool, string) {
		want := intValue(dedicated, "matchCount")
		got := nonEmptyLineCount(stringValue(cli, "stdout"))
		return want == got, fmt.Sprintf("matches dedicated=%d cli=%d", want, got)
	})
}

func (f *fixture) evalFileSlice(ctx context.Context) Case {
	return f.compare(ctx, "file_slice", "file", tool.FileSlice, map[string]any{
		"scope": "project", "path": "src/main.go", "origin": "start", "start": 2, "end": 3,
	}, []string{"sed", "-n", "2,3p", "src/main.go"}, func(dedicated, cli map[string]any) (bool, string) {
		want := stringValue(dedicated, "content")
		got := stringValue(cli, "stdout")
		return strings.TrimSuffix(want, "\n") == strings.TrimSuffix(got, "\n"), fmt.Sprintf("chars dedicated=%d cli=%d", len(want), len(got))
	})
}

func (f *fixture) evalGitStatus(ctx context.Context) Case {
	return f.compare(ctx, "git_status", "git", tool.GitStatus, map[string]any{
		"scope": "project",
	}, []string{"git", "status", "--porcelain", "--untracked-files=all"}, func(dedicated, cli map[string]any) (bool, string) {
		want := intValue(dedicated, "fileCount")
		got := nonEmptyLineCount(stringValue(cli, "stdout"))
		return want == got, fmt.Sprintf("changed files dedicated=%d cli=%d", want, got)
	})
}

func (f *fixture) evalGitDiff(ctx context.Context) Case {
	return f.compare(ctx, "git_diff", "git", tool.GitDiff, map[string]any{
		"scope": "project",
	}, []string{"git", "diff", "--numstat", "--no-ext-diff", "--no-textconv"}, func(dedicated, cli map[string]any) (bool, string) {
		lines := nonEmptyLineCount(stringValue(cli, "stdout"))
		want := intValue(dedicated, "fileCount")
		return want == lines, fmt.Sprintf("diff files dedicated=%d cli=%d", want, lines)
	})
}

func (f *fixture) evalGitLog(ctx context.Context) Case {
	return f.compare(ctx, "git_log", "git", tool.GitLog, map[string]any{
		"scope": "project", "limit": 20,
	}, []string{"git", "log", "--oneline", "-20"}, func(dedicated, cli map[string]any) (bool, string) {
		want := intValue(dedicated, "count")
		got := nonEmptyLineCount(stringValue(cli, "stdout"))
		return want == got, fmt.Sprintf("commits dedicated=%d cli=%d", want, got)
	})
}

func (f *fixture) compare(ctx context.Context, name, domain, dedicatedName string, dedicatedArgs map[string]any, argv []string, compare func(map[string]any, map[string]any) (bool, string)) Case {
	item := Case{Name: name, Domain: domain, DedicatedTool: dedicatedName, CLI: strings.Join(argv, " ")}
	if _, err := exec.LookPath(argv[0]); err != nil {
		item.Skipped = true
		item.Detail = argv[0] + " executable unavailable"
		return item
	}
	dedicated, err := f.call(ctx, dedicatedName, dedicatedArgs)
	if err != nil {
		item.Detail = "dedicated tool: " + err.Error()
		return item
	}
	cli, err := f.call(ctx, tool.CommandRun, map[string]any{"scope": "project", "command": strings.Join(argv, " ")})
	item.DedicatedResultBytes = len(dedicated.result.Content)
	if err != nil {
		item.Detail = "CLI: " + err.Error()
		return item
	}
	item.CLIResultBytes = len(cli.result.Content)
	if intValue(cli.value, "exitCode") != 0 {
		item.Detail = fmt.Sprintf("CLI exit %d: %s", intValue(cli.value, "exitCode"), stringValue(cli.value, "stderr"))
		return item
	}
	item.Passed, item.Detail = compare(dedicated.value, cli.value)
	return item
}

func (f *fixture) call(ctx context.Context, name string, args map[string]any) (toolOutput, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return toolOutput{}, err
	}
	result := f.runner.Call(ctx, tool.Call{
		SessionID: "sess_eval", CallID: "call_" + name, Name: name, Args: raw, ProjectDirs: []string{f.root},
	})
	var value map[string]any
	if err := json.Unmarshal([]byte(result.Content), &value); err != nil {
		return toolOutput{}, fmt.Errorf("decode result: %w", err)
	}
	if !result.Ok {
		return toolOutput{}, errors.New(stringValue(value, "detail"))
	}
	return toolOutput{result: result, value: value}, nil
}

func stringSetFromRecords(value any, key string) []string {
	items, _ := value.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		record, _ := item.(map[string]any)
		if text := stringValue(record, key); text != "" {
			out = append(out, text)
		}
	}
	sort.Strings(out)
	return out
}

func lineSet(value string, transform func(string) string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(value), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, transform(line))
		}
	}
	sort.Strings(out)
	return out
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func nonEmptyLineCount(value string) int {
	count := 0
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func stringValue(value map[string]any, key string) string {
	text, _ := value[key].(string)
	return text
}

func intValue(value map[string]any, key string) int {
	number, _ := value[key].(float64)
	return int(number)
}
