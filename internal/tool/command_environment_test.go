package tool

import (
	"slices"
	"strings"
	"testing"
)

func TestParseLoginShellEnvironmentIgnoresStartupOutput(t *testing.T) {
	output := []byte("startup noise\n" +
		commandEnvironmentBeginMarker + "\x00" +
		"PATH=/snapshot/bin\x00" +
		"JAVA_HOME=/snapshot/java\x00" +
		"PUDDING_SECRET=hidden\x00" +
		commandEnvironmentEndMarker + "\x00" +
		"trailing noise\n")

	entries, err := parseLoginShellEnvironment(output)
	if err != nil {
		t.Fatal(err)
	}
	filtered := filterCommandEnvironmentEntries(entries)
	if !slices.Contains(filtered, "PATH=/snapshot/bin") {
		t.Fatalf("filtered environment = %v, want PATH", filtered)
	}
	if !slices.Contains(filtered, "JAVA_HOME=/snapshot/java") {
		t.Fatalf("filtered environment = %v, want JAVA_HOME", filtered)
	}
	for _, entry := range filtered {
		if strings.HasPrefix(entry, "PUDDING_SECRET=") {
			t.Fatalf("filtered environment leaked disallowed variable: %q", entry)
		}
	}
}

func TestParseLoginShellEnvironmentRequiresMarkers(t *testing.T) {
	if _, err := parseLoginShellEnvironment([]byte("PATH=/bin\x00")); err == nil {
		t.Fatal("expected missing marker error")
	}
}

func TestLoginShellEnvironmentUsesInteractiveLoginShell(t *testing.T) {
	args := loginShellEnvironmentArgs()
	if len(args) != 4 || args[0] != "-i" || args[1] != "-l" || args[2] != "-c" {
		t.Fatalf("login shell args = %q, want interactive login invocation", args)
	}
	if args[3] != loginShellEnvironmentScript() {
		t.Fatalf("login shell script = %q, want environment capture script", args[3])
	}
}

func TestCommandEnvironmentPrecedence(t *testing.T) {
	setCommandEnvironmentSnapshotForTest(t, []string{
		"PATH=/snapshot/bin",
		"JAVA_HOME=/snapshot/java",
	})
	t.Setenv("PATH", "/process/bin")
	t.Setenv("JAVA_HOME", "/process/java")

	environment, err := commandEnvironment(map[string]string{
		"JAVA_HOME": "/custom/java",
	})
	if err != nil {
		t.Fatal(err)
	}
	values := environmentValues(environment)
	if values["JAVA_HOME"] != "/custom/java" {
		t.Fatalf("JAVA_HOME = %q, want custom value", values["JAVA_HOME"])
	}
	if path := values["PATH"]; path != "/snapshot/bin" && !strings.HasPrefix(path, "/snapshot/bin:") {
		t.Fatalf("PATH = %q, want snapshot path first", path)
	}
}

func setCommandEnvironmentSnapshotForTest(t *testing.T, entries []string) {
	t.Helper()
	previous := commandEnvironmentSnapshot()
	setCapturedCommandEnvironment(entries)
	t.Cleanup(func() {
		setCapturedCommandEnvironment(previous)
	})
}

func environmentValues(entries []string) map[string]string {
	values := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}
