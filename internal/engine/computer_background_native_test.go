package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/computer"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/tool"
)

// Opt-in: same source Engine/tool/Manager/bridge/Helper/worker as production,
// with a scripted model and temporary store. Only Calendar month navigation.
// Observe after each native reply is a test assertion, not a product precondition.
type backgroundCalendarEngineService struct {
	computer.Service
	after func(computer.Observation) error
}

func (s *backgroundCalendarEngineService) Pointer(ctx context.Context, sessionID, appID string, windowID uint32, input computer.PointerInput) (computer.NativeAction, error) {
	result, err := s.Service.Pointer(ctx, sessionID, appID, windowID, input)
	if err != nil {
		return result, err
	}
	observed, err := s.Service.Observe(ctx, sessionID, appID, windowID, 60)
	if err == nil {
		err = s.after(observed)
	}
	return result, err
}

func backgroundCalendarMonth(observed computer.Observation) (time.Time, error) {
	for _, element := range observed.Elements {
		if element.Role == nil || *element.Role != "AXStaticText" || element.Value == nil {
			continue
		}
		var year, month int
		if n, err := fmt.Sscanf(*element.Value, "%d年%d月", &year, &month); err == nil && n == 2 && year > 2000 && month >= 1 && month <= 12 {
			return time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC), nil
		}
	}
	return time.Time{}, fmt.Errorf("Chinese Calendar month view required; no replay")
}

func backgroundCalendarBridge(t *testing.T) (*computer.ElectronBridgeService, uint32) {
	t.Helper()
	url := os.Getenv("PUDDING_CU_BACKGROUND_URL")
	if url == "" {
		t.Skip("opt-in native smoke via computer-use-background-integration.cjs engine")
	}
	window, err := strconv.ParseUint(os.Getenv("PUDDING_CU_BACKGROUND_WINDOW"), 10, 32)
	if err != nil || window == 0 {
		t.Fatal("explicit Calendar window required")
	}
	bridge, err := computer.NewElectronBridgeService(computer.ElectronBridgeConfig{URL: url, Token: os.Getenv("PUDDING_CU_BACKGROUND_TOKEN")})
	if err != nil {
		t.Fatal(err)
	}
	return bridge, uint32(window)
}

func backgroundCalendarActions(observed computer.Observation) ([]computer.ActionInput, error) {
	if len(observed.Windows) != 1 || observed.Windows[0].Frame == nil {
		return nil, fmt.Errorf("exact Calendar frame required")
	}
	frame := *observed.Windows[0].Frame
	actions := []computer.ActionInput{}
	for _, description := range []string{"下一月", "上一月"} {
		var matches []computer.Element
		for _, element := range observed.Elements {
			if element.Description != nil && *element.Description == description && element.Frame != nil && !element.Secure {
				matches = append(matches, element)
			}
		}
		if len(matches) != 1 {
			return nil, fmt.Errorf("unique %s toolbar button required", description)
		}
		button := *matches[0].Frame
		x, y := (button.X+button.Width/2-frame.X)/frame.Width, (button.Y+button.Height/2-frame.Y)/frame.Height
		actions = append(actions, computer.ActionInput{Type: computer.ActionClick, Delivery: "background", X: &x, Y: &y})
	}
	return actions, nil
}

func TestBackgroundCalendarEngineIntegration(t *testing.T) {
	bridge, window := backgroundCalendarBridge(t)
	const appID = "com.apple.iCal"
	var initialMonth time.Time
	var verified []string
	service := &backgroundCalendarEngineService{Service: bridge, after: func(observed computer.Observation) error {
		month, err := backgroundCalendarMonth(observed)
		if err != nil {
			return err
		}
		want := initialMonth
		if len(verified) == 0 {
			want = initialMonth.AddDate(0, 1, 0)
		}
		if month != want {
			return fmt.Errorf("month = %s, expected %s; stop, no replay", month, want)
		}
		verified = append(verified, month.Format("2006-01"))
		return nil
	}}
	client := &backgroundEngineClient{step: func(_ context.Context, req provider.Request, step int) (<-chan provider.Chunk, error) {
		switch step {
		case 1:
			return smokeToolStream("load", tool.AppLoad, `{"app_id":"computer-use"}`), nil
		case 2:
			return smokeToolStream("use", tool.ComputerUseApp, `{"appID":"com.apple.iCal","foreground":false}`), nil
		case 3:
			var use struct {
				Result computer.UseResult `json:"result"`
			}
			if err := decodeSmokeToolResult(req, tool.ComputerUseApp, &use); err != nil {
				return nil, err
			}
			if use.Result.LaunchID != nil || len(use.Result.Windows) != 1 || use.Result.Windows[0].WindowID != uint32(window) {
				return nil, fmt.Errorf("expected the existing Calendar window, no launch ownership")
			}
			args, _ := json.Marshal(map[string]any{"appID": appID, "windowID": window, "maxElements": 60, "includeScreenshot": false})
			return smokeToolStream("observe", tool.ComputerObserve, string(args)), nil
		case 4:
			var observed struct {
				Observation computer.Observation `json:"observation"`
			}
			if err := decodeSmokeToolResult(req, tool.ComputerObserve, &observed); err != nil {
				return nil, err
			}
			month, err := backgroundCalendarMonth(observed.Observation)
			if err != nil {
				return nil, err
			}
			initialMonth = month
			actions, err := backgroundCalendarActions(observed.Observation)
			if err != nil {
				return nil, err
			}
			args, _ := json.Marshal(map[string]any{"appID": appID, "windowID": window, "actions": actions})
			return smokeToolStream("batch", tool.ComputerAct, string(args)), nil
		case 5:
			var result struct {
				Result computer.ActionsResult `json:"result"`
			}
			if err := decodeSmokeToolResult(req, tool.ComputerAct, &result); err != nil {
				return nil, err
			}
			if result.Result.CompletedCount != 2 || len(result.Result.Actions) != 2 || len(verified) != 2 {
				return nil, fmt.Errorf("batch did not complete both verified navigations")
			}
			for _, action := range result.Result.Actions {
				if !action.Completed || action.Delivery != "background" {
					return nil, fmt.Errorf("unexpected delivery result")
				}
			}
			return smokeTextStream("Calendar returned to its original month."), nil
		default:
			return nil, fmt.Errorf("unexpected model step; no replay")
		}
	}}
	h := newBackgroundEngineHarness(t, service, map[string]provider.Client{"background-engine-native": client})
	h.submit(t, "background-engine-native")
	if ev := awaitBackground(t, h.terminal["background-engine-native"]); ev.Kind != event.TurnCompleted {
		t.Fatalf("native turn = %+v", ev)
	}
	h.engine.Wait()
	content := h.result(t, "background-engine-native")
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.approvals["background-engine-native"] != 1 || client.calls.Load() != 5 {
		t.Fatalf("approvals=%v model calls=%d", h.approvals, client.calls.Load())
	}
	t.Logf("Engine session+app approval=1; one batch; verified months=%v; canonical result=%s", verified, content)
}
