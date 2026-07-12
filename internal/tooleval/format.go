package tooleval

import (
	"fmt"
	"io"
	"text/tabwriter"
)

func WriteText(w io.Writer, report Report) error {
	if _, err := fmt.Fprintf(w, "Code CLI eval (%s)\n", report.Platform); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Cases: %d passed, %d failed, %d skipped | Result bytes: dedicated %d, CLI %d\n\n", report.Passed, report.Failed, report.Skipped, report.DedicatedResultBytes, report.CLIResultBytes); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "CASE\tDOMAIN\tSTATUS\tDEDICATED\tCLI\tDETAIL"); err != nil {
		return err
	}
	for _, item := range report.Cases {
		status := "failed"
		if item.Skipped {
			status = "skipped"
		} else if item.Passed {
			status = "passed"
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%d B\t%d B\t%s\n", item.Name, item.Domain, status, item.DedicatedResultBytes, item.CLIResultBytes, item.Detail); err != nil {
			return err
		}
	}
	return tw.Flush()
}
