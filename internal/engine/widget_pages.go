package engine

import (
	"context"
	"encoding/json"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
	"time"
)

// OpenWidgetPage replaces the execution lease, preserving only committed state
// and session bindings. No old notification, turn or tool call is replayed.
func (e *Engine) OpenWidgetPage(ctx context.Context, itemID, scope, revision, target string) (*store.WidgetPage, *WidgetRun, error) {
	e.widgetRuns.Lock()
	defer e.widgetRuns.Unlock()
	if plugin.RuntimeIDFromContext(ctx) == "" {
		return nil, nil, ErrWidgetRun
	}
	page, err := e.store.OpenWidgetPage(ctx, itemID, scope, revision, target)
	if err != nil {
		return nil, nil, err
	}
	for id, run := range e.widgetRuns.entries {
		if run.ItemID != itemID {
			continue
		}
		if _, err := e.store.GetWidgetPageByTarget(ctx, run.TargetID); err != nil {
			e.cancelWidgetRunLocked(run)
			delete(e.widgetRuns.entries, id)
		}
	}
	if len(page.Interaction) == 0 {
		return page, nil, nil
	}
	var definition WidgetRunCreate
	if err = json.Unmarshal(page.Interaction, &definition); err != nil {
		return nil, nil, err
	}
	item, err := e.store.GetStudioItem(ctx, itemID)
	if err != nil {
		return nil, nil, err
	}
	if e.store.AuthorizeWidgetPage(ctx, itemID, revision, target) != nil || item.BindingVersion != definition.BindingVersion {
		return page, nil, nil
	}
	definition.TargetID = target
	run := &WidgetRun{ID: store.NewID("run"), ItemID: itemID, Title: item.Name, WidgetRunCreate: definition, Status: "paused", Reason: "restored", Receipts: []*WidgetReceipt{}, runtimeID: plugin.RuntimeIDFromContext(ctx), started: time.Now(), expires: time.Now().Add(45 * time.Second)}
	if e.widgetRuns.entries == nil {
		e.widgetRuns.entries = map[string]*WidgetRun{}
	}
	e.widgetRuns.entries[run.ID] = run
	e.widgetRuns.once.Do(func() { go e.widgetLoop() })
	return page, cloneWidgetRun(run), nil
}
func (e *Engine) CloseWidgetPages(ctx context.Context, itemID, scope string) error {
	e.widgetRuns.Lock()
	defer e.widgetRuns.Unlock()
	if err := e.store.CloseWidgetPages(ctx, itemID, scope); err != nil {
		return err
	}
	for id, run := range e.widgetRuns.entries {
		if run.ItemID == itemID {
			if _, err := e.store.GetWidgetPageByTarget(ctx, run.TargetID); err != nil {
				e.cancelWidgetRunLocked(run)
				delete(e.widgetRuns.entries, id)
			}
		}
	}
	return nil
}

func (e *Engine) SelectWidgetPage(ctx context.Context, itemID, scope, revision string) (string, error) {
	e.widgetRuns.Lock()
	defer e.widgetRuns.Unlock()
	hash, err := e.store.SelectWidgetPage(ctx, itemID, scope, revision)
	if err != nil {
		return "", err
	}
	for id, run := range e.widgetRuns.entries {
		if run.ItemID == itemID && e.store.AuthorizeWidgetPage(ctx, itemID, run.RevisionHash, run.TargetID) != nil {
			e.cancelWidgetRunLocked(run)
			delete(e.widgetRuns.entries, id)
		}
	}
	return hash, nil
}
