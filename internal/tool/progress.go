package tool

import "context"

const (
	ProgressStdout = "stdout"
	ProgressStderr = "stderr"
)

// Progress is ephemeral tool output. It is sent to the live transcript only and
// never becomes part of canonical message history.
type Progress struct {
	Stream  string
	Content string
}

type progressSink func(Progress)
type progressSinkContextKey struct{}

func WithProgressSink(ctx context.Context, sink func(Progress)) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, progressSinkContextKey{}, progressSink(sink))
}

func EmitProgress(ctx context.Context, progress Progress) {
	if progress.Content == "" || (progress.Stream != ProgressStdout && progress.Stream != ProgressStderr) {
		return
	}
	if sink, ok := ctx.Value(progressSinkContextKey{}).(progressSink); ok {
		sink(progress)
	}
}
