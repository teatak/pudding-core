package tool

import (
	"context"
	"errors"
	"time"
)

// A yielded foreground command keeps its original process, approval, output
// capture and mutation baseline. Only delivery of the initial result ends.
type foregroundCommand struct {
	stdout, stderr *commandProgressWriter
	complete       func(int, string, error, time.Duration) Result
}

type commandCompletionKey struct{}

func WithCommandCompletion(ctx context.Context, complete func()) context.Context {
	return context.WithValue(ctx, commandCompletionKey{}, complete)
}

func (r *BuiltinRunner) commandRunManaged(ctx context.Context, call Call, args commandRunArgs, shell, cwd string, execution *commandExecution, timeout time.Duration) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	if err := ctx.Err(); err != nil {
		return toolJSONError(out, "cancelled", err.Error())
	}
	stdout, stderr := newCommandOutputBuffers(args, cwd, call.ProjectDirs)
	capture := &commandOutputCapture{homeDir: r.homeDir, sessionID: call.SessionID}
	stdout.artifact = &commandOutputArtifact{capture: capture, stream: "stdout"}
	stderr.artifact = &commandOutputArtifact{capture: capture, stream: "stderr"}
	foreground := &foregroundCommand{
		stdout: newCommandProgressWriter(ctx, ProgressStdout, stdout),
		stderr: newCommandProgressWriter(ctx, ProgressStderr, stderr),
	}
	defer foreground.stdout.Close()
	defer foreground.stderr.Close()
	foreground.complete = func(exit int, reason string, runErr error, duration time.Duration) Result {
		foreground.stdout.Close()
		foreground.stderr.Close()
		if reason == "exited" {
			reason = ""
		}
		cancelled := reason == "cancelled" || reason == "stopped" || reason == "session_closed" || reason == "daemon_closed" || reason == "turn_finished"
		result := commandResult(out, args, shell, cwd, call.ProjectDirs, execution, exit, stdout, stderr, reason == "timed_out", cancelled, duration, reason, runErr)
		capture.close()
		if complete, ok := ctx.Value(commandCompletionKey{}).(func()); ok {
			complete()
		}
		return result
	}
	process, err := r.processes.startPrepared(call.SessionID, call.TurnID, call.CallID, cwd, args.Command, shell, false, execution, foreground)
	if err != nil {
		capture.close()
		return backgroundProcessError(out, err)
	}
	go func() {
		timer := time.NewTimer(time.Until(process.startedAt.Add(timeout)))
		defer timer.Stop()
		select {
		case <-process.done:
		case <-ctx.Done():
			_ = process.stop("cancelled")
		case <-timer.C:
			_ = process.stop("timed_out")
		}
	}()
	wait := commandDefaultYield
	if args.YieldMS != nil {
		wait = time.Duration(*args.YieldMS) * time.Millisecond
	}
	// Initial execution still waits for completion or yield_ms. Only later
	// polls return early on output, so short commands keep their final result.
	waitForCommandUpdate(ctx, wait, process.done, nil)
	if ctx.Err() != nil {
		_ = process.stop("cancelled")
	}
	process.mu.Lock()
	if process.finalResult != nil {
		result := *process.finalResult
		process.mu.Unlock()
		process.cancelExpiry()
		r.processes.remove(process)
		return result
	}
	payload := process.statePayloadLocked()
	chunks, nextOffset, truncated, hasMore := process.output.Read(0, backgroundProcessPollDefault)
	payload["output"], payload["nextOffset"] = chunks, nextOffset
	payload["truncated"], payload["hasMore"] = truncated, hasMore
	payload["yielded"] = true
	payload["hint"] = "The command is still running. Continue with command_session(action=poll, process_id=processID, offset=nextOffset, wait_ms=10000); do not restart it."
	process.mu.Unlock()
	return toolJSON(out, true, payload)
}

// FinishForegroundCommands joins foreground work before the turn diff is
// finalized. Explicit background processes retain their existing lifetime.
func (r *BuiltinRunner) FinishForegroundCommands(sessionID, turnID string) error {
	r.processes.mu.Lock()
	var processes []*backgroundProcess
	for _, process := range r.processes.processes {
		if process.foreground != nil && process.sessionID == sessionID && process.turnID == turnID {
			processes = append(processes, process)
		}
	}
	r.processes.mu.Unlock()
	var errs []error
	for _, process := range processes {
		errs = append(errs, process.stop("turn_finished"))
	}
	return errors.Join(errs...)
}
