package prompt

import (
	"strings"
	"testing"
)

func TestCodeCommandPromptMatchesPlatform(t *testing.T) {
	windows := codeModePromptForOS("windows")
	for _, want := range []string{"PowerShell 7 x64", "Ask and Auto require a fresh approval", "No reusable Windows command grants"} {
		if !strings.Contains(windows, want) {
			t.Errorf("Windows prompt missing %q", want)
		}
	}
	for _, wrong := range []string{"{{COMMAND_PLATFORM}}", "Auto trusts code execution within", "Use $TMPDIR", "another `sh -c`", "default sandbox for complex scripts"} {
		if strings.Contains(windows, wrong) {
			t.Errorf("Windows prompt includes %q", wrong)
		}
	}
	mac := codeModePromptForOS("darwin")
	if !strings.Contains(mac, "Auto trusts code execution within") || strings.Contains(mac, "PowerShell 7") || strings.Contains(mac, "{{COMMAND_PLATFORM}}") {
		t.Fatal("Mac command instructions changed")
	}
}
