package tooleval

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	report, err := Run(context.Background(), time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Cases) != 7 || report.Failed != 0 || report.Passed+report.Skipped != 7 {
		t.Fatalf("unexpected eval report: %+v", report)
	}
	var output bytes.Buffer
	if err := WriteText(&output, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Code CLI eval") || !strings.Contains(output.String(), "file_search") {
		t.Fatalf("unexpected text report: %s", output.String())
	}
}
