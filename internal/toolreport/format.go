package toolreport

import (
	"fmt"
	"io"
	"text/tabwriter"
)

func WriteText(w io.Writer, report Report) error {
	if _, err := fmt.Fprintf(w, "Tool usage report (%s to %s)\n", report.Since.Format("2006-01-02"), report.Until.Format("2006-01-02")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Turns: %d (project %d, chat %d) | Calls: %d\n", report.TotalTurns, report.ProjectTurns, report.ChatTurns, report.Calls); err != nil {
		return err
	}
	if report.SkippedMessages > 0 {
		if _, err := fmt.Fprintf(w, "Skipped malformed messages: %d\n", report.SkippedMessages); err != nil {
			return err
		}
	}
	if len(report.Tools) == 0 {
		_, err := fmt.Fprintln(w, "No tool calls found in this period.")
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "TOOL\tGROUP\tCALLS\tTURN%\tOK%\tREPEAT%\tCLI-FB%\tP95 RESULT"); err != nil {
		return err
	}
	for _, stat := range report.Tools {
		if _, err := fmt.Fprintf(
			tw,
			"%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\n",
			stat.Name,
			stat.Group,
			stat.Calls,
			formatRate(stat.TurnCoverageRate),
			formatRate(stat.SuccessRate),
			formatRateValue(stat.RepeatRate),
			formatRate(stat.CLIFallbackRate),
			formatBytes(stat.P95ResultBytes),
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func formatRate(value *float64) string {
	if value == nil {
		return "-"
	}
	return formatRateValue(*value)
}

func formatRateValue(value float64) string {
	return fmt.Sprintf("%.1f%%", value*100)
}

func formatBytes(value int) string {
	switch {
	case value >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(value)/(1<<20))
	case value >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(value)/(1<<10))
	default:
		return fmt.Sprintf("%d B", value)
	}
}
