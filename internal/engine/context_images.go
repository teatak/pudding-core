package engine

import (
	"context"
	"log/slog"

	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

func (e *Engine) tryPruneToolImages(ctx context.Context, sessionID, turnID string, resolved *resolvedModel, req *provider.Request, factor float64) (bool, error) {
	through, err := contextbuilder.ToolImageCandidate(req.Messages, contextbuilder.DefaultToolImageBudget(req.Config.ContextWindow), false)
	if err != nil || through == "" {
		return false, err
	}
	previous, err := e.store.ContextCooling(ctx, sessionID)
	if err != nil {
		return false, err
	}
	all, err := e.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return false, err
	}
	next := previous
	next.SummaryMessageID = contextbuilder.ImageEpoch(all)
	next.ImageThroughID = through
	candidate := *req
	candidate.Messages = contextbuilder.ApplyToolImageWatermark(req.Messages, through)
	before, err := contextbuilder.EstimateRequest(*req, resolved.protocol)
	if err != nil {
		return false, err
	}
	after, err := contextbuilder.EstimateRequest(candidate, resolved.protocol)
	if err != nil {
		return false, err
	}
	tokens := calibratedTokenEstimate(after.Total(), factor)
	if after.Total() >= before.Total() {
		return false, ErrContextBudget
	}
	if err := limitRequestOutput(&candidate, resolved.protocol, tokens); err != nil {
		return false, err
	}
	if err := e.store.AdvanceContextCooling(ctx, store.AdvanceContextCoolingInput{
		SessionID: sessionID, RunningTurnID: turnID, ExpectedLastMessageID: all[len(all)-1].ID,
		Expected: previous, Next: next,
	}); err != nil {
		return false, err
	}
	slog.Info("engine: screenshot budget cleaned", "sessionID", sessionID, "beforeInputEstimate", calibratedTokenEstimate(before.Total(), factor), "afterInputEstimate", tokens)
	return true, nil
}
