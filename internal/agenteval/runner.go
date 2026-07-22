package agenteval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/daemon"
	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

const (
	defaultProviderName = "buzzhive"
	maxRuns             = 10
	maxCommandOutput    = 8 * 1024
)

type selection struct {
	profile *store.ProviderProfile
	model   string
}

type apiClient struct {
	baseURL string
	token   string
	client  *http.Client
}

type submitResponse struct {
	TurnID string `json:"turnID"`
}

type approvalList struct {
	Approvals []approvalWire `json:"approvals"`
}

type approvalWire struct {
	Kind       string          `json:"approvalKind"`
	TargetMode string          `json:"targetMode,omitempty"`
	Title      string          `json:"title,omitempty"`
	Reason     string          `json:"reason,omitempty"`
	Risk       string          `json:"risk,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

type approvalError struct {
	detail ApprovalDetail
}

func (e *approvalError) Error() string { return "agent eval: turn requested interactive approval" }

func Run(ctx context.Context, opts Options) (Report, error) {
	if opts.Runs == 0 {
		opts.Runs = 1
	}
	if opts.Runs < 1 || opts.Runs > maxRuns {
		return Report{}, fmt.Errorf("agent eval: runs must be between 1 and %d", maxRuns)
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	cases, err := LoadCases(opts.CasesDir, opts.CaseNames)
	if err != nil {
		return Report{}, err
	}
	selected := selection{
		profile: &store.ProviderProfile{ID: "mock", DisplayName: "mock", Models: []store.ProviderModel{{ID: "mock", Capabilities: &store.ModelCaps{Tools: true}}}},
		model:   "mock",
	}
	if !opts.Mock {
		sourceHome, err := home.Resolve(opts.SourceHome)
		if err != nil {
			return Report{}, err
		}
		selected, err = selectProviderModel(ctx, config.NewManager(sourceHome), opts.Provider, opts.Model)
		if err != nil {
			return Report{}, err
		}
	}
	workRoot, err := os.MkdirTemp("", "pudding-agent-eval-")
	if err != nil {
		return Report{}, fmt.Errorf("agent eval: create work directory: %w", err)
	}
	if !opts.Keep {
		defer os.RemoveAll(workRoot)
	}
	evalHome := filepath.Join(workRoot, "home")
	// The selected profile contains credentials. Remove the eval home even when
	// --keep preserves fixtures for debugging.
	defer os.RemoveAll(evalHome)
	if err := home.Prepare(evalHome); err != nil {
		return Report{}, err
	}
	evalConfig := config.NewManager(evalHome)
	if err := evalConfig.Prepare(); err != nil {
		return Report{}, err
	}
	if err := evalConfig.PutProviderProfile(context.Background(), selected.profile); err != nil {
		return Report{}, fmt.Errorf("agent eval: prepare selected provider: %w", err)
	}
	d, err := daemon.Start(daemon.Options{Home: evalHome, Addr: "127.0.0.1:0", Mock: opts.Mock})
	if err != nil {
		return Report{}, fmt.Errorf("agent eval: start daemon: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = d.Shutdown(shutdownCtx)
	}()
	api := &apiClient{
		baseURL: "http://" + d.Addr(),
		token:   d.Token(),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
	report := Report{
		GeneratedAt: opts.Now().UTC(),
		Provider:    selected.profile.DisplayLabel(),
		Model:       selected.model,
		Runs:        opts.Runs,
		Cases:       len(cases),
	}
	for _, item := range cases {
		for run := 1; run <= opts.Runs; run++ {
			result := runCase(ctx, api, workRoot, selected, item, run, opts.Keep)
			report.Results = append(report.Results, result)
			if result.Passed {
				report.Passed++
			} else {
				report.Failed++
			}
		}
	}
	return report, nil
}

func selectProviderModel(ctx context.Context, cfg *config.Manager, providerName, modelName string) (selection, error) {
	profiles, err := cfg.ListProviderProfiles(ctx)
	if err != nil {
		return selection{}, fmt.Errorf("agent eval: load provider profiles: %w", err)
	}
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		providerName = defaultProviderName
	}
	var profile *store.ProviderProfile
	for _, candidate := range profiles {
		if strings.EqualFold(candidate.ID, providerName) || strings.EqualFold(candidate.DisplayName, providerName) {
			profile = candidate
			break
		}
	}
	if profile == nil {
		return selection{}, fmt.Errorf("agent eval: provider %q not found", providerName)
	}
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		for _, preferred := range []string{"deepseek-v4-flash", "mimo-v2.5", "deepseek-v4-pro", "mimo-v2.5-pro"} {
			if modelSupportsTools(profile, preferred) {
				modelName = preferred
				break
			}
		}
	}
	model, ok := profile.ModelByID(modelName)
	if !ok {
		return selection{}, fmt.Errorf("agent eval: model %q is not configured under provider %q", modelName, profile.DisplayLabel())
	}
	if model.Capabilities != nil && !model.Capabilities.Tools {
		return selection{}, fmt.Errorf("agent eval: model %q does not declare tool support", modelName)
	}
	return selection{profile: profile, model: modelName}, nil
}

func modelSupportsTools(profile *store.ProviderProfile, modelName string) bool {
	model, ok := profile.ModelByID(modelName)
	return ok && (model.Capabilities == nil || model.Capabilities.Tools)
}

func runCase(ctx context.Context, api *apiClient, workRoot string, selected selection, item Case, run int, keep bool) (result Result) {
	started := time.Now()
	result = Result{Case: item.Name, Run: run, Description: item.Description, VerifyExitCode: -1}
	defer func() { result.DurationMS = time.Since(started).Milliseconds() }()
	fixtureDir := filepath.Join(workRoot, "fixtures", fmt.Sprintf("%s-%02d", item.Name, run))
	if keep {
		result.FixtureDir = fixtureDir
	}
	if err := copyFixture(item.FixtureDir, fixtureDir); err != nil {
		result.Failure = err.Error()
		return result
	}
	if err := initGitFixture(ctx, fixtureDir); err != nil {
		result.Failure = err.Error()
		return result
	}
	initialHead, err := commandText(ctx, fixtureDir, []string{"git", "rev-parse", "HEAD"}, 30*time.Second)
	if err != nil {
		result.Failure = err.Error()
		return result
	}
	baseline := runCommand(ctx, fixtureDir, item.Verify.Command, item.Verify.Timeout)
	result.BaselineFailed = baseline.exitCode != 0
	if item.Verify.BaselineMustFail && !result.BaselineFailed {
		result.VerifyExitCode = baseline.exitCode
		result.VerifyOutput = baseline.output
		result.Failure = "fixture baseline unexpectedly passes verification"
		return result
	}

	var project store.Project
	if err := api.do(ctx, http.MethodPost, "/projects", map[string]any{
		"name": item.Name, "rootDirs": []string{fixtureDir}, "approvalMode": store.ApprovalAuto,
	}, &project); err != nil {
		result.Failure = err.Error()
		return result
	}
	var session store.Session
	if err := api.do(ctx, http.MethodPost, "/sessions", map[string]any{
		"title": "Eval: " + item.Name, "provider": selected.profile.ID, "model": selected.model, "projectID": project.ID,
	}, &session); err != nil {
		result.Failure = err.Error()
		return result
	}
	result.SessionID = session.ID
	prompt := evaluationPrompt(item)
	var submitted submitResponse
	if err := api.do(ctx, http.MethodPost, "/sessions/"+session.ID+"/submit", map[string]any{
		"clientMessageID": fmt.Sprintf("eval:%s:%d", item.Name, run), "text": prompt,
	}, &submitted); err != nil {
		result.Failure = err.Error()
		return result
	}
	result.TurnID = submitted.TurnID
	turn, err := api.waitTurn(ctx, session.ID, submitted.TurnID, item.Verify.Timeout)
	if turn != nil {
		result.TurnStatus = string(turn.Status)
		collectTurnStats(&result, turn, item.Verify.Command)
	}
	if err != nil {
		result.Failure = err.Error()
		var approvalErr *approvalError
		if errors.As(err, &approvalErr) {
			approval := approvalErr.detail
			result.Approval = &approval
		}
		_ = api.do(context.Background(), http.MethodPost, "/sessions/"+session.ID+"/cancel", nil, nil)
	}

	verify := runCommand(ctx, fixtureDir, item.Verify.Command, item.Verify.Timeout)
	result.VerifyExitCode = verify.exitCode
	result.VerifyOutput = verify.output
	result.VerifyPassed = verify.exitCode == 0
	result.ChangedPaths, _ = changedPaths(ctx, fixtureDir, strings.TrimSpace(initialHead))
	result.OutOfScopePaths = outOfScopePaths(result.ChangedPaths, item.Verify.AllowedPaths)
	currentHead, headErr := commandText(ctx, fixtureDir, []string{"git", "rev-parse", "HEAD"}, 30*time.Second)
	result.UnexpectedCommit = headErr == nil && strings.TrimSpace(currentHead) != strings.TrimSpace(initialHead)
	var usage store.SessionUsageStat
	if err := api.do(ctx, http.MethodGet, "/sessions/"+session.ID+"/usage", nil, &usage); err == nil {
		result.ProviderRequests = usage.RequestCount
		result.InputTokens = usage.CumulativeInputTokens()
		result.InputUncached = usage.CumulativeInputUncachedTokens
		result.InputCached = usage.CumulativeInputCachedTokens
		result.CacheCreation = usage.CumulativeCacheCreationTokens
		result.OutputTokens = usage.CumulativeOutputTokens()
		result.OutputContent = usage.CumulativeOutputContentTokens
		result.OutputReasoning = usage.CumulativeOutputReasoningTokens
	}
	result.FalsePassClaim = !result.VerifyPassed && claimsVerificationPassed(result.FinalResponse)
	if result.Failure == "" {
		switch {
		case result.TurnStatus != string(store.TurnCompleted):
			result.Failure = "turn did not complete"
		case !result.VerifyPassed:
			result.Failure = "verification failed"
		case len(result.OutOfScopePaths) > 0:
			result.Failure = "changed files outside allowed paths"
		case result.UnexpectedCommit:
			result.Failure = "agent created an unexpected Git commit"
		case result.FalsePassClaim:
			result.Failure = "agent claimed verification passed when it failed"
		}
	}
	result.Passed = result.Failure == ""
	return result
}

func evaluationPrompt(item Case) string {
	return strings.TrimSpace(item.Prompt) + "\n\nEvaluation constraints:\n" +
		"- Work only inside the provided project.\n" +
		"- Do not commit, push, publish, or change Git branches.\n" +
		"- Do not modify tests unless the task explicitly asks for it.\n" +
		"- Run this verification command before finishing: " + strings.Join(item.Verify.Command, " ") + "\n" +
		"- In the final answer, state what changed and what verification actually ran."
}

func (a *apiClient) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("agent eval API %s %s: %w", method, path, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("agent eval API %s %s: status %d: %s", method, path, response.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("agent eval API %s %s: decode response: %w", method, path, err)
	}
	return nil
}

func (a *apiClient) waitTurn(ctx context.Context, sessionID, turnID string, timeout time.Duration) (*store.ConversationTurn, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		var turn store.ConversationTurn
		if err := a.do(ctx, http.MethodGet, "/sessions/"+sessionID+"/turns/"+turnID, nil, &turn); err != nil {
			return nil, err
		}
		if turn.Status != store.TurnRunning {
			return &turn, nil
		}
		var approvals approvalList
		if err := a.do(ctx, http.MethodGet, "/sessions/"+sessionID+"/approvals", nil, &approvals); err != nil {
			return nil, err
		}
		if len(approvals.Approvals) > 0 {
			return &turn, &approvalError{detail: approvalDiagnostic(approvals.Approvals[0])}
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("agent eval: wait for turn: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func collectTurnStats(result *Result, turn *store.ConversationTurn, verifyCommand []string) {
	counts := map[string]int{}
	commands := map[string]string{}
	toolArgs := map[string]json.RawMessage{}
	for _, message := range turn.Messages {
		if message.Role == store.RoleAssistant {
			if text := strings.TrimSpace(store.TextFromParts(message.Parts)); text != "" {
				result.FinalResponse = text
			}
		}
		for _, part := range message.Parts {
			switch part.Type {
			case store.ContentPartToolUse:
				result.ToolCalls++
				result.ToolCallSequence = append(result.ToolCallSequence, part.Name)
				counts[part.Name]++
				toolArgs[part.CallID] = append(json.RawMessage(nil), part.Args...)
				if part.Name == tool.CommandRun {
					commands[part.CallID] = commandLabel(part.Args)
				}
				if part.Name == tool.CommandRun && commandArgsMatch(part.Args, verifyCommand) {
					result.AgentRanVerify = true
				}
			case store.ContentPartToolResult:
				if !part.Ok {
					result.ToolFailures++
					result.ToolFailureDetail = append(result.ToolFailureDetail, toolFailureDetail(part, toolArgs[part.CallID]))
				}
				if part.Name == tool.CommandRun {
					result.CommandAttempts = append(result.CommandAttempts, commandAttempt(commands[part.CallID], part.Content))
				}
			}
		}
	}
	for _, count := range counts {
		if count > 1 {
			result.RepeatedToolCalls += count - 1
		}
	}
}

func commandLabel(raw json.RawMessage) string {
	var args struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(raw, &args) != nil {
		return ""
	}
	return strings.TrimSpace(args.Command)
}

func toolFailureDetail(part store.ContentPart, rawArgs json.RawMessage) ToolFailureDetail {
	var payload map[string]any
	_ = json.Unmarshal([]byte(part.Content), &payload)
	detail := stringMapValue(payload, "detail")
	if detail == "" {
		detail = stringMapValue(payload, "error")
	}
	reason := stringMapValue(payload, "reason")
	var args map[string]json.RawMessage
	_ = json.Unmarshal(rawArgs, &args)
	var scope, path string
	_ = json.Unmarshal(args["scope"], &scope)
	pathRaw, hasPath := args["path"]
	_ = json.Unmarshal(pathRaw, &path)
	failure := ToolFailureDetail{
		Name:   part.Name,
		Reason: reason,
		Detail: truncateDiagnostic(detail, 1000),
		Scope:  strings.TrimSpace(scope),
		Path:   truncateDiagnostic(path, 500),
	}
	if hasPath || strings.HasPrefix(reason, "path_") {
		failure.PathKind = diagnosticPathKind(path)
	}
	return failure
}

func diagnosticPathKind(rawPath string) string {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "empty"
	}
	if filepath.IsAbs(rawPath) {
		return "absolute"
	}
	cleaned := filepath.Clean(rawPath)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "parent_relative"
	}
	return "relative"
}

func approvalDiagnostic(item approvalWire) ApprovalDetail {
	detail := ApprovalDetail{
		Kind:       strings.TrimSpace(item.Kind),
		TargetMode: strings.TrimSpace(item.TargetMode),
		Title:      strings.TrimSpace(item.Title),
		Reason:     strings.TrimSpace(item.Reason),
		Risk:       strings.TrimSpace(item.Risk),
	}
	if detail.Kind != "capability" {
		return detail
	}
	var payload struct {
		ProjectDirs     []string `json:"projectDirs"`
		NeedsProjectDir bool     `json:"needsProjectDir"`
	}
	if json.Unmarshal(item.Payload, &payload) == nil {
		for _, dir := range payload.ProjectDirs {
			if dir = truncateDiagnostic(dir, 500); dir != "" {
				detail.ProjectDirs = append(detail.ProjectDirs, dir)
			}
		}
		detail.NeedsProjectDir = payload.NeedsProjectDir
	}
	return detail
}

func commandAttempt(command, content string) CommandAttempt {
	var payload struct {
		ExitCode int    `json:"exitCode"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		Error    string `json:"error"`
	}
	_ = json.Unmarshal([]byte(content), &payload)
	output := strings.TrimSpace(strings.Join([]string{payload.Stdout, payload.Stderr, payload.Error}, "\n"))
	return CommandAttempt{Command: truncateDiagnostic(command, 500), ExitCode: payload.ExitCode, Output: truncateDiagnostic(output, 2000)}
}

func stringMapValue(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

func truncateDiagnostic(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "...[truncated]"
}

func commandArgsMatch(raw json.RawMessage, verify []string) bool {
	var args struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(raw, &args) != nil {
		return false
	}
	want := strings.Join(verify, " ")
	return strings.TrimSpace(args.Command) == want || strings.Contains(args.Command, want)
}

type commandResult struct {
	exitCode int
	output   string
}

func runCommand(parent context.Context, dir string, argv []string, timeout time.Duration) commandResult {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	raw, err := cmd.CombinedOutput()
	result := commandResult{exitCode: 0, output: truncateOutput(string(raw))}
	if err == nil {
		return result
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.exitCode = exitErr.ExitCode()
	} else if ctx.Err() != nil {
		result.exitCode = 124
		result.output = truncateOutput(result.output + "\n" + ctx.Err().Error())
	} else {
		result.exitCode = -1
		result.output = truncateOutput(result.output + "\n" + err.Error())
	}
	return result
}

func commandText(ctx context.Context, dir string, argv []string, timeout time.Duration) (string, error) {
	result := runCommand(ctx, dir, argv, timeout)
	if result.exitCode != 0 {
		return "", fmt.Errorf("agent eval: command %q failed: %s", strings.Join(argv, " "), result.output)
	}
	return result.output, nil
}

func truncateOutput(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxCommandOutput {
		return value
	}
	return value[:maxCommandOutput/2] + "\n...[truncated]...\n" + value[len(value)-maxCommandOutput/2:]
}

func claimsVerificationPassed(text string) bool {
	text = strings.ToLower(text)
	for _, phrase := range []string{"tests pass", "tests passed", "test suite passes", "all tests pass", "测试通过", "測試通過", "验证通过", "驗證通過"} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func outOfScopePaths(changed, allowed []string) []string {
	if len(allowed) == 0 {
		return append([]string(nil), changed...)
	}
	var out []string
	for _, path := range changed {
		matched := false
		for _, pattern := range allowed {
			if pathAllowed(path, pattern) {
				matched = true
				break
			}
		}
		if !matched {
			out = append(out, path)
		}
	}
	return out
}

func pathAllowed(value, pattern string) bool {
	value = filepath.ToSlash(filepath.Clean(value))
	pattern = filepath.ToSlash(filepath.Clean(pattern))
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return value == prefix || strings.HasPrefix(value, prefix+"/")
	}
	matched, _ := filepath.Match(pattern, value)
	return matched
}

func changedPaths(ctx context.Context, dir, baselineHead string) ([]string, error) {
	tracked, err := commandBytes(ctx, dir, []string{"git", "diff", "--name-only", "-z", baselineHead}, 30*time.Second)
	if err != nil {
		return nil, err
	}
	untracked, err := commandBytes(ctx, dir, []string{"git", "ls-files", "--others", "--exclude-standard", "-z"}, 30*time.Second)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, raw := range append(bytes.Split(tracked, []byte{0}), bytes.Split(untracked, []byte{0})...) {
		path := filepath.ToSlash(strings.TrimSpace(string(raw)))
		if path != "" && !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out, nil
}

func commandBytes(ctx context.Context, dir string, argv []string, timeout time.Duration) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, argv[0], argv[1:]...)
	cmd.Dir = dir
	raw, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("agent eval: command %q: %w", strings.Join(argv, " "), err)
	}
	return raw, nil
}

func copyFixture(source, target string) error {
	if err := os.MkdirAll(target, 0o700); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil || rel == "." {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("agent eval: fixture symlink is not allowed: %s", path)
		}
		destination := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("agent eval: fixture must contain only regular files: %s", path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mode := info.Mode().Perm()
		if mode == 0 {
			mode = 0o600
		}
		return os.WriteFile(destination, raw, mode)
	})
}

func initGitFixture(ctx context.Context, dir string) error {
	commands := [][]string{
		{"git", "init", "-q"},
		{"git", "add", "--all"},
		{"git", "-c", "user.name=Pudding Eval", "-c", "user.email=eval@pudding.local", "commit", "-q", "-m", "fixture baseline"},
	}
	for _, argv := range commands {
		if _, err := commandText(ctx, dir, argv, 30*time.Second); err != nil {
			return err
		}
	}
	return nil
}
