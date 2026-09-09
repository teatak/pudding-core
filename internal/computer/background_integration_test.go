package computer

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Opt-in native smoke, launched only by computer-use-background-integration.cjs.
// A Chinese-locale Calendar month view is required; navigate next then previous,
// never click an event or create data. No production daemon/database is involved.
func TestBackgroundCalendarIntegration(t *testing.T) {
	url := os.Getenv("PUDDING_CU_BACKGROUND_URL")
	if url == "" {
		t.Skip("native smoke only")
	}
	window, err := strconv.ParseUint(os.Getenv("PUDDING_CU_BACKGROUND_WINDOW"), 10, 32)
	if err != nil || window == 0 {
		t.Fatal("explicit Calendar window required")
	}
	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: url, Token: os.Getenv("PUDDING_CU_BACKGROUND_TOKEN")})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(service)
	ctx := context.Background()
	const session = "background-integration"
	const app = "com.apple.iCal"
	observe := func() Observation {
		o, err := service.Observe(ctx, session, app, uint32(window), 60)
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	month := func(o Observation) string {
		for _, element := range o.Elements {
			if element.Role != nil && *element.Role == "AXStaticText" && element.Value != nil && strings.Contains(*element.Value, "年") && strings.HasSuffix(*element.Value, "月") {
				return *element.Value
			}
		}
		t.Fatal("Calendar must be in month view; no input sent")
		return ""
	}
	before := observe()
	initialMonth := month(before)
	if len(before.Windows) != 1 || before.Windows[0].Frame == nil {
		t.Fatal("exact Calendar frame required")
	}
	frame := *before.Windows[0].Frame
	point := func(description string) ActionInput {
		var found []Element
		for _, element := range before.Elements {
			if element.Description != nil && *element.Description == description && element.Frame != nil && !element.Secure {
				found = append(found, element)
			}
		}
		if len(found) != 1 {
			t.Fatalf("expected unique %s toolbar control", description)
		}
		e := *found[0].Frame
		x, y := (e.X+e.Width/2-frame.X)/frame.Width, (e.Y+e.Height/2-frame.Y)/frame.Height
		return ActionInput{Type: ActionClick, X: &x, Y: &y, Delivery: "background"}
	}
	// Both positions are known from one observation; results are inspected for
	// assertions, not because an observation token was consumed or expired.
	next, previous := point("下一月"), point("上一月")
	for index, action := range []ActionInput{next, previous} {
		result, err := manager.Act(ctx, session, app, uint32(window), []ActionInput{action})
		if err != nil || result.Failure != nil || result.CompletedCount != 1 {
			t.Fatalf("step=%d result=%+v err=%v; no replay", index, result, err)
		}
		after := observe()
		current := month(after)
		if (index == 0 && current == initialMonth) || (index == 1 && current != initialMonth) {
			t.Fatalf("step=%d calendar view effect not verified", index)
		}
		t.Logf("step=%d delivered=background month=%s", index, current)
	}
}
