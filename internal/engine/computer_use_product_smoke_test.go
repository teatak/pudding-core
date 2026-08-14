package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/computer"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
)

const (
	computerUseProductSmokeEnv            = "PUDDING_COMPUTER_USE_PRODUCT_SMOKE"
	computerUseCalculatorSmokeEnv         = "PUDDING_COMPUTER_USE_CALCULATOR_SMOKE"
	computerUseCalculatorExistingSmokeEnv = "PUDDING_COMPUTER_USE_CALCULATOR_EXISTING_SMOKE"
	computerUseFixtureAppID               = "com.teatak.pudding.computer-use-fixture"
	computerUseCalculatorAppID            = "com.apple.calculator"
)

// These tests are opt-in because they cross the real macOS TCC, Electron
// bridge, signed Helper, and application boundaries.
func TestComputerUseProductSmoke(t *testing.T) {
	runComputerUseSmoke(t, computerUseProductSmokeEnv, fixtureSmokeScenario())
}

func TestComputerUseCalculatorSmoke(t *testing.T) {
	runComputerUseSmoke(t, computerUseCalculatorSmokeEnv, calculatorSmokeScenario())
}

func TestComputerUseCalculatorExistingSmoke(t *testing.T) {
	runComputerUseSmoke(t, computerUseCalculatorExistingSmokeEnv, calculatorExistingSmokeScenario())
}

type computerUseSmokeScenario struct {
	name          string
	appID         string
	expectOwned   bool
	windowMatches func(computer.CapturableWindow) bool
	actions       []computerUseSmokeAction
}

type computerUseSmokeAction struct {
	callID  string
	action  string
	value   *string
	matches func(computer.Element) bool
	verify  func([]computer.Element) error
}

func fixtureSmokeScenario() computerUseSmokeScenario {
	fixtureValue := fmt.Sprintf("pudding-product-smoke-%d", time.Now().UnixNano())
	return computerUseSmokeScenario{
		name: "fixture", appID: computerUseFixtureAppID, expectOwned: true,
		windowMatches: func(window computer.CapturableWindow) bool {
			return window.Title != nil && *window.Title == "Computer Use Fixture"
		},
		actions: []computerUseSmokeAction{
			{
				callID: "call_computer_set_value", action: computer.ActionSetValue, value: &fixtureValue,
				matches: func(element computer.Element) bool {
					return !element.Secure && element.Role != nil && *element.Role == "AXTextField"
				},
				verify: func(elements []computer.Element) error {
					if !smokeHasValue(elements, fixtureValue) {
						return errors.New("fixture text value did not persist in the action's fresh observation")
					}
					return nil
				},
			},
			{
				callID: "call_computer_increment", action: computer.ActionPress,
				matches: func(element computer.Element) bool { return smokeElementName(element) == "Increment" },
				verify: func(elements []computer.Element) error {
					if !smokeHasNamedValue(elements, "Fixture count", "1") {
						return errors.New("fixture increment did not update the count to 1")
					}
					return nil
				},
			},
		},
	}
}

func calculatorSmokeScenario() computerUseSmokeScenario {
	return computerUseSmokeScenario{
		name: "calculator", appID: computerUseCalculatorAppID, expectOwned: true,
		windowMatches: func(computer.CapturableWindow) bool {
			return true
		},
		actions: []computerUseSmokeAction{
			calculatorPressWithoutDisplayCheck("call_calculator_clear_current", []string{"All Clear", "Clear", "AC", "全部清除", "清除"}),
			calculatorPress("call_calculator_all_clear", []string{"All Clear", "Clear", "AC", "全部清除", "清除"}, "0"),
			calculatorPress("call_calculator_one_first", []string{"1"}, "1"),
			calculatorPress("call_calculator_add", []string{"Add", "Plus", "+", "加"}, "1+"),
			calculatorPress("call_calculator_one_second", []string{"1"}, "1+1"),
			calculatorPress("call_calculator_equals", []string{"Equals", "=", "等于"}, "2"),
		},
	}
}

func calculatorExistingSmokeScenario() computerUseSmokeScenario {
	scenario := calculatorSmokeScenario()
	scenario.name = "calculator-existing"
	scenario.expectOwned = false
	return scenario
}

func calculatorPressWithoutDisplayCheck(callID string, names []string) computerUseSmokeAction {
	return computerUseSmokeAction{
		callID: callID,
		action: computer.ActionPress,
		matches: func(element computer.Element) bool {
			return smokeStringIn(smokeElementName(element), names)
		},
		verify: func([]computer.Element) error { return nil },
	}
}

func calculatorPress(callID string, names []string, expectedDisplay string) computerUseSmokeAction {
	return computerUseSmokeAction{
		callID: callID,
		action: computer.ActionPress,
		matches: func(element computer.Element) bool {
			return smokeStringIn(smokeElementName(element), names)
		},
		verify: func(elements []computer.Element) error {
			if !smokeHasCalculatorDisplay(elements, expectedDisplay) {
				return fmt.Errorf("Calculator display did not become %s", expectedDisplay)
			}
			return nil
		},
	}
}

func runComputerUseSmoke(t *testing.T, enabledEnv string, scenario computerUseSmokeScenario) {
	t.Helper()
	if os.Getenv(enabledEnv) != "1" {
		t.Skipf("set %s=1 and start the Electron Computer Use bridge", enabledEnv)
	}

	bridge, err := computer.NewElectronBridgeService(computer.ElectronBridgeConfig{
		URL:   os.Getenv("PUDDING_ELECTRON_COMPUTER_BRIDGE_URL"),
		Token: os.Getenv("PUDDING_ELECTRON_COMPUTER_BRIDGE_TOKEN"),
	})
	if err != nil {
		t.Fatal(err)
	}
	manager := computer.NewManager(bridge)
	runner := tool.NewBuiltinRunner(tool.WithComputer(manager))
	client := &computerUseSmokeClient{scenario: scenario}
	ms := memstore.New()
	hub := event.NewHub()
	apps := app.NewService(t.TempDir(), nil)
	providerName := "computer-use-" + scenario.name + "-smoke"
	modelName := providerName + "-model"
	eng := New(ms, hub, computerUseSmokeResolver{providerName: client}, ms, WithTools(runner), WithApps(apps))

	sessionID := "sess_" + strings.ReplaceAll(providerName, "-", "_")
	ctx := context.Background()
	if err := ms.CreateSession(ctx, &store.Session{
		ID: sessionID, Title: "Computer Use " + scenario.name + " smoke",
		Provider: providerName, Model: modelName,
		ActiveMode: store.ModeWork, ModeLease: store.ModeLeaseSession,
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: providerName, Protocol: "openai-compatible",
		Models: []store.ProviderModel{{
			ID:           modelName,
			Capabilities: &store.ModelCaps{Tools: true},
			Limits:       &store.ModelLimits{MaxToolLoops: 64},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = manager.ReleaseSession(releaseCtx, sessionID)
		eng.Stop()
		eng.Wait()
	}()

	approvalEvents, unsubscribe := hub.Subscribe(sessionID)
	defer unsubscribe()
	var approvalCount atomic.Int32
	approvalErrors := make(chan error, 64)
	go func() {
		for ev := range approvalEvents {
			if ev.Kind != event.ApprovalRequested {
				continue
			}
			approvalCount.Add(1)
			if ev.ApprovalKind != ApprovalKindToolCall {
				approvalErrors <- fmt.Errorf("unexpected approval kind %q", ev.ApprovalKind)
			}
			var payload struct {
				AppID string `json:"appID"`
			}
			if err := json.Unmarshal(ev.Payload, &payload); err != nil || payload.AppID != scenario.appID {
				approvalErrors <- fmt.Errorf("unexpected approval payload %s: %v", ev.Payload, err)
			}
			if err := eng.ApproveApproval(ctx, sessionID, ev.ApprovalID, ApprovalScopeSession, nil); err != nil {
				approvalErrors <- err
			}
		}
	}()

	if _, err := eng.Submit(ctx, SubmitInput{
		SessionID: sessionID, ClientMessageID: providerName + "-input",
		Text: "Use Computer Use to complete the deterministic smoke scenario and close the app.",
	}); err != nil {
		t.Fatal(err)
	}
	waitComputerUseSmokeTurn(t, ms, sessionID)

	select {
	case err := <-approvalErrors:
		t.Fatal(err)
	default:
	}
	if got := approvalCount.Load(); got != 1 {
		t.Fatalf("Computer Use approvals = %d, want one session+app approval", got)
	}
	granted, err := ms.HasComputerAppGrant(ctx, sessionID, scenario.appID)
	if err != nil || !granted {
		t.Fatalf("Computer Use app grant = %v, err = %v", granted, err)
	}
	if err := client.Err(); err != nil {
		t.Fatal(err)
	}
	if !client.Completed() {
		t.Fatalf("provider stage = %d, want completed stage %d", client.Stage(), client.FinalStage())
	}
	if got := client.RequestCount(); got < client.FinalStage() {
		t.Fatalf("provider requests = %d, want at least %d", got, client.FinalStage())
	}
}

type computerUseSmokeResolver map[string]provider.Client

func (r computerUseSmokeResolver) Resolve(_ context.Context, name string) (provider.Client, error) {
	client, ok := r[name]
	if !ok {
		return nil, errors.New("no such profile: " + name)
	}
	return client, nil
}

type computerUseSmokeClient struct {
	mu           sync.Mutex
	scenario     computerUseSmokeScenario
	requests     int
	stage        int
	listAttempts int
	launchID     string
	windowID     uint32
	err          error
}

func (c *computerUseSmokeClient) Name() string { return "computer-use-" + c.scenario.name + "-smoke" }

func (c *computerUseSmokeClient) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests++
	stage := c.stage + 1
	advance := true

	var name, callID string
	var args any
	switch {
	case stage == 1:
		if !smokeHasToolDef(req.Tools, tool.AppLoad) || smokeHasToolDef(req.Tools, tool.ComputerLaunchApp) {
			return c.fail(fmt.Errorf("Computer Use must begin unloaded; tools = %+v", req.Tools))
		}
		name, callID = tool.AppLoad, "call_computer_app_load"
		args = map[string]any{"app_id": app.BuiltinComputerUseID}
	case stage == 2:
		for _, required := range []string{tool.ComputerListApps, tool.ComputerLaunchApp, tool.ComputerQuitApp, tool.ComputerObserve, tool.ComputerAct} {
			if !smokeHasToolDef(req.Tools, required) {
				return c.fail(fmt.Errorf("Computer Use tool %q missing after app load", required))
			}
		}
		name, callID = tool.ComputerLaunchApp, "call_computer_launch"
		args = map[string]any{"appID": c.scenario.appID}
	case stage == 3:
		var result struct {
			OK     bool                  `json:"ok"`
			Result computer.LaunchResult `json:"result"`
		}
		if err := decodeSmokeToolResult(req, tool.ComputerLaunchApp, &result); err != nil {
			return c.fail(err)
		}
		if !result.OK || result.Result.PID <= 0 {
			return c.fail(fmt.Errorf("%s launch returned an invalid result: %+v", c.scenario.name, result))
		}
		if c.scenario.expectOwned && result.Result.LaunchID == nil {
			return c.fail(fmt.Errorf("%s launch was not newly session-owned: %+v", c.scenario.name, result))
		}
		if !c.scenario.expectOwned && result.Result.LaunchID != nil {
			return c.fail(fmt.Errorf("%s unexpectedly acquired ownership of an already-running app: %+v", c.scenario.name, result))
		}
		if result.Result.LaunchID != nil {
			c.launchID = *result.Result.LaunchID
		}
		name, callID = tool.ComputerListApps, "call_computer_list_apps"
		args = map[string]any{}
	case stage == 4:
		var result struct {
			OK     bool             `json:"ok"`
			Result computer.AppList `json:"result"`
		}
		if err := decodeSmokeToolResult(req, tool.ComputerListApps, &result); err != nil {
			return c.fail(err)
		}
		if !result.OK || !result.Result.Permissions.Accessibility || !result.Result.Permissions.ScreenRecording {
			return c.fail(fmt.Errorf("Computer Use permissions are incomplete: %+v", result.Result.Permissions))
		}
		for _, window := range result.Result.CapturableWindows {
			if window.AppID != nil && *window.AppID == c.scenario.appID && c.scenario.windowMatches(window) {
				c.windowID = window.WindowID
				break
			}
		}
		if c.windowID == 0 {
			c.listAttempts++
			if c.listAttempts >= 40 {
				return c.fail(fmt.Errorf("%s window was not discovered through the product tool", c.scenario.name))
			}
			advance = false
			select {
			case <-ctx.Done():
				return c.fail(ctx.Err())
			case <-time.After(150 * time.Millisecond):
			}
			name, callID = tool.ComputerListApps, fmt.Sprintf("call_computer_list_apps_retry_%d", c.listAttempts)
			args = map[string]any{}
			break
		}
		name, callID = tool.ComputerObserve, "call_computer_observe"
		args = map[string]any{"appID": c.scenario.appID, "windowID": c.windowID, "maxElements": 300}
	case stage >= 5 && stage <= 5+len(c.scenario.actions):
		completedActions := stage - 5
		var observation *computer.ManagedObservation
		if completedActions == 0 {
			var result struct {
				OK          bool                        `json:"ok"`
				Observation computer.ManagedObservation `json:"observation"`
			}
			if err := decodeSmokeToolResult(req, tool.ComputerObserve, &result); err != nil {
				return c.fail(err)
			}
			if !result.OK {
				return c.fail(errors.New("Computer Use observation failed"))
			}
			observation = &result.Observation
		} else {
			result, err := decodeSmokeActionResult(req)
			if err != nil {
				return c.fail(err)
			}
			observation = result.Observation
			if observation == nil {
				return c.fail(errors.New("Computer Use action did not return a fresh observation"))
			}
			if err := c.scenario.actions[completedActions-1].verify(observation.Snapshot.Elements); err != nil {
				return c.fail(err)
			}
		}
		if completedActions == len(c.scenario.actions) {
			if !c.scenario.expectOwned {
				c.stage = stage
				return smokeTextStream("Computer Use " + c.scenario.name + " smoke completed without closing the app."), nil
			}
			name, callID = tool.ComputerQuitApp, "call_computer_quit"
			args = map[string]any{"launchID": c.launchID}
			break
		}
		action := c.scenario.actions[completedActions]
		element := smokeElement(observation.Snapshot.Elements, action.action, action.matches)
		if element == nil {
			return c.fail(fmt.Errorf("%s action target %s was not observed", c.scenario.name, action.callID))
		}
		name, callID = tool.ComputerAct, action.callID
		actionArgs := map[string]any{
			"appID": c.scenario.appID, "windowID": c.windowID,
			"observationID": observation.ObservationID, "elementID": element.ElementID,
			"action": action.action,
		}
		if action.value != nil {
			actionArgs["value"] = *action.value
		}
		args = actionArgs
	case stage == c.finalStage():
		var result struct {
			OK     bool                `json:"ok"`
			Result computer.QuitResult `json:"result"`
		}
		if err := decodeSmokeToolResult(req, tool.ComputerQuitApp, &result); err != nil {
			return c.fail(err)
		}
		if !result.OK || !result.Result.Closed || result.Result.LaunchID != c.launchID {
			return c.fail(fmt.Errorf("%s did not close through its session-owned launch: %+v", c.scenario.name, result))
		}
		c.stage = stage
		return smokeTextStream("Computer Use " + c.scenario.name + " smoke completed."), nil
	default:
		return c.fail(fmt.Errorf("unexpected provider stage %d", stage))
	}
	if advance {
		c.stage = stage
	}

	rawArgs, err := json.Marshal(args)
	if err != nil {
		return c.fail(err)
	}
	return smokeToolStream(callID, name, string(rawArgs)), nil
}

func (c *computerUseSmokeClient) finalStage() int {
	if c.scenario.expectOwned {
		return 6 + len(c.scenario.actions)
	}
	return 5 + len(c.scenario.actions)
}

func (c *computerUseSmokeClient) fail(err error) (<-chan provider.Chunk, error) {
	if c.err == nil {
		c.err = err
	}
	return nil, err
}

func (c *computerUseSmokeClient) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *computerUseSmokeClient) RequestCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests
}

func (c *computerUseSmokeClient) Stage() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stage
}

func (c *computerUseSmokeClient) FinalStage() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.finalStage()
}

func (c *computerUseSmokeClient) Completed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stage == c.finalStage()
}

func decodeSmokeToolResult(req provider.Request, name string, out any) error {
	for messageIndex := len(req.Messages) - 1; messageIndex >= 0; messageIndex-- {
		parts := req.Messages[messageIndex].Parts
		for partIndex := len(parts) - 1; partIndex >= 0; partIndex-- {
			part := parts[partIndex]
			if part.Type != provider.PartToolResult || part.Name != name {
				continue
			}
			if !part.Ok {
				return fmt.Errorf("tool %s failed: %s", name, part.Content)
			}
			if err := json.Unmarshal([]byte(part.Content), out); err != nil {
				return fmt.Errorf("decode %s result: %w", name, err)
			}
			return nil
		}
	}
	return fmt.Errorf("tool result %s not found", name)
}

func decodeSmokeActionResult(req provider.Request) (computer.ActionResult, error) {
	var result struct {
		OK     bool                  `json:"ok"`
		Result computer.ActionResult `json:"result"`
	}
	if err := decodeSmokeToolResult(req, tool.ComputerAct, &result); err != nil {
		return computer.ActionResult{}, err
	}
	if !result.OK || !result.Result.Action.Completed {
		return computer.ActionResult{}, fmt.Errorf("Computer Use action was not completed: %+v", result)
	}
	return result.Result, nil
}

func smokeToolStream(callID, name, args string) <-chan provider.Chunk {
	out := make(chan provider.Chunk, 2)
	out <- provider.Chunk{Tool: &provider.ToolCallChunk{Index: 0, CallID: callID, Name: name, ArgsDelta: args}}
	out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	close(out)
	return out
}

func smokeTextStream(value string) <-chan provider.Chunk {
	out := make(chan provider.Chunk, 2)
	out <- provider.Chunk{Part: provider.PartText, Delta: value}
	out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	close(out)
	return out
}

func smokeHasToolDef(defs []provider.ToolDef, name string) bool {
	for _, definition := range defs {
		if definition.Name == name {
			return true
		}
	}
	return false
}

func smokeElement(elements []computer.Element, action string, predicate func(computer.Element) bool) *computer.Element {
	for index := range elements {
		if smokeStringIn(action, elements[index].Actions) && predicate(elements[index]) {
			return &elements[index]
		}
	}
	return nil
}

func smokeStringIn(target string, values []string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func smokeHasValue(elements []computer.Element, value string) bool {
	for _, element := range elements {
		if element.Value != nil && *element.Value == value {
			return true
		}
	}
	return false
}

func smokeHasNamedValue(elements []computer.Element, name, value string) bool {
	for _, element := range elements {
		if smokeElementName(element) == name && element.Value != nil && *element.Value == value {
			return true
		}
	}
	return false
}

func smokeHasCalculatorDisplay(elements []computer.Element, value string) bool {
	for _, element := range elements {
		if element.Role == nil || *element.Role != "AXStaticText" || element.Value == nil {
			continue
		}
		if normalizeCalculatorDisplay(*element.Value) == value {
			return true
		}
	}
	return false
}

func normalizeCalculatorDisplay(value string) string {
	return strings.Map(func(char rune) rune {
		if unicode.IsSpace(char) || unicode.Is(unicode.Cf, char) {
			return -1
		}
		return char
	}, value)
}

func smokeElementName(element computer.Element) string {
	if element.Label != nil && strings.TrimSpace(*element.Label) != "" {
		return strings.TrimSpace(*element.Label)
	}
	if element.Description != nil {
		return strings.TrimSpace(*element.Description)
	}
	return ""
}

func waitComputerUseSmokeTurn(t *testing.T, ms *memstore.Memstore, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := ms.RunningTurn(context.Background(), sessionID); errors.Is(err, store.ErrNotFound) {
			events, eventErr := ms.EventsAfter(context.Background(), sessionID, 0, 0)
			if eventErr != nil {
				t.Fatal(eventErr)
			}
			for _, ev := range events {
				switch ev.Kind {
				case event.TurnCompleted:
					return
				case event.TurnFailed, event.TurnCancelled:
					t.Fatalf("Computer Use smoke ended with %s: %s", ev.Kind, ev.Error)
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("Computer Use smoke did not finish in time")
}
