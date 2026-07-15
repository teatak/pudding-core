package agenteval

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"
)

func WriteText(w io.Writer, report Report) error {
	if _, err := fmt.Fprintf(w, "Agent eval (%s / %s)\n", report.Provider, report.Model); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Cases: %d x %d run(s) | %d passed, %d failed\n\n", report.Cases, report.Runs, report.Passed, report.Failed); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "CASE\tRUN\tSTATUS\tTURN\tVERIFY\tTOOLS\tREPEAT\tDURATION\tDETAIL"); err != nil {
		return err
	}
	for _, item := range report.Results {
		status := "failed"
		if item.Passed {
			status = "passed"
		}
		verify := "failed"
		if item.VerifyPassed {
			verify = "passed"
		}
		detail := item.Failure
		if detail == "" && len(item.OutOfScopePaths) > 0 {
			detail = "out-of-scope changes"
		}
		if _, err := fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%d/%d failed\t%d\t%s\t%s\n",
			item.Case, item.Run, status, item.TurnStatus, verify, item.ToolCalls, item.ToolFailures,
			item.RepeatedToolCalls, (time.Duration(item.DurationMS) * time.Millisecond).String(), detail); err != nil {
			return err
		}
	}
	return tw.Flush()
}
