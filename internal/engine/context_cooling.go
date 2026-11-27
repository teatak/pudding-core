package engine

import (
	"context"
	"errors"
	"log/slog"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func (e *Engine) tryCoolContext(ctx context.Context, sessionID, turnID string, resolved *resolvedModel, mode store.AgentMode, req *provider.Request, before, threshold int, factor float64) (bool, error) {
	settings, err := e.config.Settings(ctx)
	if err != nil {
		return false, err
	}
	if settings[config.SettingToolResultCoolingEnabled] == "false" || turnID == "" || !tool.HasDefinition(req.Tools, tool.HistoryGetMessage) {
		return false, nil
	}
	all, err := e.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return false, err
	}
	previous, err := e.store.ContextCooling(ctx, sessionID)
	if err != nil {
		return false, err
	}
	keep := e.intSetting(ctx, config.SettingToolResultCoolingKeepTurns, config.DefaultToolResultCoolingKeepTurns, 0, 50)
	next := contextbuilder.CoolingCandidate(all, turnID, keep)
	next.ImageThroughID = previous.ImageThroughID
	if next.ThroughTurnID == "" || next == previous {
		return false, nil
	}
	pressure, err := summaryInputPressure(resolved, all, factor, true)
	if err != nil || pressure {
		return false, err
	}
	candidate, err := e.builder.BuildForProviderWithHistory(ctx, sessionID, resolved.providerName, resolved.model, string(mode), req.Tools, all, resolved.config, next)
	if err != nil {
		return false, err
	}
	candidate.Config, candidate.Tools = resolved.config, req.Tools
	if err := e.appendDirectoryAccess(ctx, sessionID, turnID, mode, &candidate); err != nil {
		return false, err
	}
	candidate, err = e.builder.ResolveSkillReferences(ctx, sessionID, string(mode), candidate)
	if err != nil {
		return false, err
	}
	through, err := contextbuilder.ToolImageCandidate(candidate.Messages, contextbuilder.DefaultToolImageBudget(candidate.Config.ContextWindow), true)
	if err != nil {
		return false, err
	}
	if through != "" {
		next.ImageThroughID = through
		candidate.Messages = contextbuilder.ApplyToolImageWatermark(candidate.Messages, through)
	}
	estimate, err := contextbuilder.EstimateRequest(candidate, resolved.protocol)
	if err != nil {
		return false, err
	}
	after := calibratedTokenEstimate(estimate.Total(), factor)
	// The trial is local: an ineffective projection never changes the watermark
	// or reaches a provider. Use the existing Compact path in that case.
	if after >= threshold || after >= before {
		return false, nil
	}
	if err := e.store.AdvanceContextCooling(ctx, store.AdvanceContextCoolingInput{
		SessionID: sessionID, RunningTurnID: turnID, ExpectedLastMessageID: all[len(all)-1].ID,
		Expected: previous, Next: next,
	}); err != nil {
		return false, err
	}
	slog.Info("engine: tool results and notifications cooled", "sessionID", sessionID, "throughTurnID", next.ThroughTurnID, "keepEndedTurns", keep, "beforeInputEstimate", before, "afterInputEstimate", after, "savedTokens", before-after)
	return true, nil
}

func (e *Engine) cooledSummaryPressure(ctx context.Context, sessionID string, resolved *resolvedModel, factor float64, canReadHistory bool) (bool, error) {
	settings, err := e.config.Settings(ctx)
	if err != nil {
		return false, err
	}
	if settings[config.SettingToolResultCoolingEnabled] == "false" {
		return false, nil
	}
	state, err := e.store.ContextCooling(ctx, sessionID)
	if err != nil || state.ThroughTurnID == "" {
		return false, err
	}
	all, err := e.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return false, err
	}
	return summaryInputPressure(resolved, all, factor, canReadHistory)
}

// Compact still reads the L1 history. Check its summary budget before
// postponing compaction through cooling.
func summaryInputPressure(resolved *resolvedModel, all []*store.Message, factor float64, canReadHistory bool) (bool, error) {
	effective := contextbuilder.EffectiveMessages(all)
	history, err := compactHistoryText(effective, effective, compactColdTurns(effective, canReadHistory))
	if errors.Is(err, ErrCompactEmpty) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	req, err := compactSummaryRequest(resolved, history, "", compactSummaryMaxTokens)
	if errors.Is(err, ErrContextBudget) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	estimate, err := contextbuilder.EstimateRequest(req, resolved.protocol)
	if err != nil {
		return false, err
	}
	input := max(compactInputTokens(req, estimate.Total()), calibratedTokenEstimate(estimate.Total(), factor))
	return input+compactMinOutputTokens >= compactRequestBudget(resolved.config.ContextWindow), nil
}
