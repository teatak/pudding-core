package agenteval

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/store"
)

func TestLoadCasesAndSelection(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(root, "fixture")
	if err := os.MkdirAll(fixture, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte("name: sample\ndescription: test\nfixture: fixture\nprompt: fix it\nverify:\n  command: [go, test, ./...]\n  timeout: 1m\n  baseline_must_fail: true\n  allowed_paths: [main.go]\n")
	if err := os.WriteFile(filepath.Join(root, "sample.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cases, err := LoadCases(root, []string{"sample"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || cases[0].Name != "sample" || cases[0].Verify.Timeout.String() != "1m0s" {
		t.Fatalf("unexpected cases: %+v", cases)
	}
	if _, err := LoadCases(root, []string{"missing"}); err == nil {
		t.Fatal("missing selection should fail")
	}
}

func TestRepositoryCaseCatalog(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "evals", "cases"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 10 {
		t.Fatalf("case count = %d, want 10", len(cases))
	}
}

func TestSelectProviderModelPrefersBuzzHiveDeepSeek(t *testing.T) {
	dir := t.TempDir()
	if err := home.Prepare(dir); err != nil {
		t.Fatal(err)
	}
	cfg := config.NewManager(dir)
	if err := cfg.Prepare(); err != nil {
		t.Fatal(err)
	}
	profile := &store.ProviderProfile{
		ID: "prof_buzz", DisplayName: "BuzzHive", Protocol: "openai-compatible", BaseURL: "https://example.invalid", APIKey: "test",
		Models: []store.ProviderModel{
			{ID: "mimo-v2.5", Capabilities: &store.ModelCaps{Tools: true}},
			{ID: "deepseek-v4-flash", Capabilities: &store.ModelCaps{Tools: true}},
		},
	}
	if err := cfg.PutProviderProfile(context.Background(), profile); err != nil {
		t.Fatal(err)
	}
	selected, err := selectProviderModel(context.Background(), cfg, "buzzhive", "")
	if err != nil {
		t.Fatal(err)
	}
	if selected.profile.ID != "prof_buzz" || selected.model != "deepseek-v4-flash" {
		t.Fatalf("unexpected selection: %+v", selected)
	}
}

func TestScoringHelpers(t *testing.T) {
	changed := []string{"internal/a.go", "README.md", "web/app.ts"}
	allowed := []string{"internal/**", "README.md"}
	if got, want := outOfScopePaths(changed, allowed), []string{"web/app.ts"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("outOfScopePaths = %v, want %v", got, want)
	}
	if !claimsVerificationPassed("All tests passed") || claimsVerificationPassed("Tests were not run") {
		t.Fatal("verification claim detection mismatch")
	}
	if !commandArgsMatch([]byte(`{"scope":"project","command":"go test ./..."}`), []string{"go", "test", "./..."}) {
		t.Fatal("verification command should match")
	}
	failure := toolFailureDetail(
		store.ContentPart{Name: "file", Content: `{"reason":"path_not_authorized","detail":"outside"}`},
		[]byte(`{"scope":"project","path":"../secret.txt"}`),
	)
	if failure.Reason != "path_not_authorized" || failure.Detail != "outside" || failure.Scope != "project" || failure.Path != "../secret.txt" || failure.PathKind != "parent_relative" {
		t.Fatalf("unexpected failure detail: %+v", failure)
	}
	approval := approvalDiagnostic(approvalWire{
		Kind: "capability", TargetMode: "code", Reason: "need another directory",
		Payload: []byte(`{"projectDirs":["/tmp/extra"],"needsProjectDir":true}`),
	})
	if approval.TargetMode != "code" || !approval.NeedsProjectDir || !reflect.DeepEqual(approval.ProjectDirs, []string{"/tmp/extra"}) {
		t.Fatalf("unexpected approval detail: %+v", approval)
	}
	attempt := commandAttempt("go test ./...", `{"exitCode":1,"stdout":"failed"}`)
	if attempt.ExitCode != 1 || attempt.Command != "go test ./..." || attempt.Output != "failed" {
		t.Fatalf("unexpected command attempt: %+v", attempt)
	}
}
