// puddingd 是 Pudding daemon 的 CLI 入口;Electron shell 通过同一个 daemon
// 二进制提供业务协议。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/teatak/pudding-core/internal/agenteval"
	"github.com/teatak/pudding-core/internal/applog"
	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/daemon"
	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/prompt"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
	"github.com/teatak/pudding-core/internal/tooleval"
	"github.com/teatak/pudding-core/internal/toolreport"
)

func main() {
	if err := run(); err != nil {
		slog.Error("puddingd", "err", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "prompt":
			return runPrompt(os.Args[2:])
		case "tools":
			return runTools(os.Args[2:], os.Stdout, os.Stderr, time.Now())
		case "agent":
			return runAgent(os.Args[2:], os.Stdout, os.Stderr, time.Now())
		}
	}

	var (
		flagHome = flag.String("home", "", "data home (default: channel home, see docs)")
		flagAddr = flag.String("addr", home.DefaultAddr(), "HTTP listen address")
		flagMock = flag.Bool("mock", false, "use mock provider")
		flagLAN  = flag.Bool("lan", false, "listen on LAN interfaces for mobile pairing")
	)
	flag.Parse()
	resolvedHome, err := home.Resolve(*flagHome)
	if err != nil {
		return err
	}
	if err := home.Prepare(resolvedHome); err != nil {
		return err
	}
	if err := applog.Install(filepath.Join(resolvedHome, "logs"), "puddingd"); err != nil {
		slog.Warn("puddingd file logging unavailable", "err", err)
	}

	d, err := daemon.Start(daemon.Options{
		Home:      resolvedHome,
		Addr:      *flagAddr,
		Mock:      *flagMock,
		MobileLAN: *flagLAN,
	})
	if err != nil {
		slog.Error("puddingd startup failed", "addr", *flagAddr, "err", err)
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	parentDone := electronParentDone(os.Stdin)

	select {
	case err := <-d.ServeErr():
		slog.Error("puddingd serve failed", "err", err)
		return err
	case <-ctx.Done():
	case <-parentDone:
		slog.Info("puddingd managed parent exited")
	}

	slog.Info("puddingd shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Shutdown(shutdownCtx); err != nil {
		slog.Error("puddingd shutdown failed", "err", err)
		return err
	}
	return nil
}

func electronParentDone(stdin io.Reader) <-chan struct{} {
	if os.Getenv("PUDDING_ELECTRON_MANAGED") != "1" {
		return nil
	}
	// Electron 异常退出时写端会由 OS 关闭；stdin EOF 是跨平台的父进程
	// 生命周期信号，弥补 before-quit / SIGTERM 可能来不及执行的路径。
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, stdin)
		close(done)
	}()
	return done
}

func runAgent(args []string, stdout, stderr io.Writer, now time.Time) error {
	if len(args) == 0 {
		return errors.New("usage: puddingd agent eval [flags]")
	}
	switch args[0] {
	case "eval":
		return runAgentEval(args[1:], stdout, stderr, now)
	case "help", "-h", "--help":
		_, err := fmt.Fprintln(stdout, "usage: puddingd agent eval [flags]")
		return err
	default:
		return fmt.Errorf("unknown agent command %q; usage: puddingd agent eval [flags]", args[0])
	}
}

func runAgentEval(args []string, stdout, stderr io.Writer, now time.Time) error {
	fs := flag.NewFlagSet("agent eval", flag.ContinueOnError)
	fs.SetOutput(stderr)
	flagHome := fs.String("home", "", "source Pudding home containing provider profiles")
	flagCases := fs.String("cases", "evals/cases", "directory containing eval case YAML files")
	flagCase := fs.String("case", "", "comma-separated case names (default: all)")
	flagProvider := fs.String("provider", "buzzhive", "provider profile ID or display name")
	flagModel := fs.String("model", "", "model ID (default: DeepSeek then MiMo under the provider)")
	flagRuns := fs.Int("runs", 1, "runs per case (1-10)")
	flagKeep := fs.Bool("keep", false, "keep isolated fixtures and include their paths in the report")
	flagMock := fs.Bool("mock", false, "use the local mock provider for runner smoke testing")
	flagJSON := fs.Bool("json", false, "print machine-readable JSON")
	flagOutput := fs.String("output", "", "also write the JSON report to this path")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected agent eval arguments: %v", fs.Args())
	}
	var selectedCases []string
	for _, name := range strings.Split(*flagCase, ",") {
		if name = strings.TrimSpace(name); name != "" {
			selectedCases = append(selectedCases, name)
		}
	}
	report, err := agenteval.Run(context.Background(), agenteval.Options{
		SourceHome: *flagHome,
		CasesDir:   *flagCases,
		Provider:   *flagProvider,
		Model:      *flagModel,
		CaseNames:  selectedCases,
		Runs:       *flagRuns,
		Keep:       *flagKeep,
		Mock:       *flagMock,
		Now:        func() time.Time { return now },
	})
	if err != nil {
		return err
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return err
	}
	if strings.TrimSpace(*flagOutput) != "" {
		path, err := filepath.Abs(*flagOutput)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
			return err
		}
		if !*flagJSON {
			defer fmt.Fprintf(stdout, "\nJSON: %s\n", path)
		}
	}
	if *flagJSON {
		_, err := stdout.Write(encoded.Bytes())
		return err
	}
	return agenteval.WriteText(stdout, report)
}

func runTools(args []string, stdout, stderr io.Writer, now time.Time) error {
	if len(args) == 0 {
		return errors.New("usage: puddingd tools report [flags]")
	}
	switch args[0] {
	case "report":
		return runToolsReport(args[1:], stdout, stderr, now)
	case "eval":
		return runToolsEval(args[1:], stdout, stderr, now)
	case "help", "-h", "--help":
		_, err := fmt.Fprintln(stdout, "usage: puddingd tools <report|eval> [flags]")
		return err
	default:
		return fmt.Errorf("unknown tools command %q; usage: puddingd tools <report|eval> [flags]", args[0])
	}
}

func runToolsEval(args []string, stdout, stderr io.Writer, now time.Time) error {
	fs := flag.NewFlagSet("tools eval", flag.ContinueOnError)
	fs.SetOutput(stderr)
	flagJSON := fs.Bool("json", false, "print machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected tools eval arguments: %v", fs.Args())
	}
	report, err := tooleval.Run(context.Background(), now)
	if err != nil {
		return err
	}
	if *flagJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	return tooleval.WriteText(stdout, report)
}

func runToolsReport(args []string, stdout, stderr io.Writer, now time.Time) error {
	fs := flag.NewFlagSet("tools report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	flagHome := fs.String("home", "", "data home (default: channel home, see docs)")
	flagDays := fs.Int("days", 30, "number of recent days to include (1-3650)")
	flagAll := fs.Bool("all", false, "include current built-in tools with zero calls")
	flagJSON := fs.Bool("json", false, "print machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected tools report arguments: %v", fs.Args())
	}
	if *flagDays < 1 || *flagDays > 3650 {
		return errors.New("tools report --days must be between 1 and 3650")
	}
	dir, err := home.Resolve(*flagHome)
	if err != nil {
		return err
	}
	until := now.UTC()
	report, err := toolreport.Generate(context.Background(), home.DBPath(dir), toolreport.Options{
		Since:         until.Add(-time.Duration(*flagDays) * 24 * time.Hour),
		Until:         until,
		IncludeUnused: *flagAll,
	})
	if err != nil {
		return err
	}
	if *flagJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	if _, err := fmt.Fprintf(stdout, "Data: %s\n", home.DBPath(dir)); err != nil {
		return err
	}
	return toolreport.WriteText(stdout, report)
}

func runPrompt(args []string) error {
	fs := flag.NewFlagSet("prompt", flag.ExitOnError)
	flagHome := fs.String("home", "", "data home (default: channel home, see docs)")
	flagMode := fs.String("mode", "chat", "agent mode: chat, work, code")
	flagSegments := fs.Bool("segments", false, "print prompt segments with headers")
	flagTools := fs.Bool("tools", false, "print provider-neutral tool schemas for the mode")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := home.Resolve(*flagHome)
	if err != nil {
		return err
	}
	cfg := config.NewManager(dir)
	out, err := prompt.NewLoader(dir, cfg).Prompt(context.Background(), *flagMode)
	if err != nil {
		return err
	}
	mode := store.NormalizeAgentMode(store.AgentMode(*flagMode))
	if !store.ValidAgentMode(mode) {
		mode = store.ModeChat
	}
	toolkitIndex := tool.ToolkitIndex(mode, tool.BuildToolkitCatalog(tool.BuiltinDefinitions()))
	if *flagSegments {
		for i, seg := range out.Segments {
			if i > 0 {
				fmt.Println()
			}
			fmt.Printf("===== %s (%s) =====\n%s\n", seg.ID, seg.Layer, seg.Content)
		}
		if toolkitIndex != "" {
			fmt.Printf("\n===== toolkits (runtime) =====\n%s\n", toolkitIndex)
		}
	} else {
		system := out.SystemInstruction
		if toolkitIndex != "" {
			system = strings.TrimRight(system, "\n") + "\n\n" + toolkitIndex
		}
		fmt.Printf("===== system prompt =====\n%s\n", system)
	}
	if *flagTools {
		printPromptTools(store.AgentMode(*flagMode))
	}
	return nil
}

func printPromptTools(mode store.AgentMode) {
	mode = store.NormalizeAgentMode(mode)
	if mode == "" {
		mode = store.ModeChat
	}
	defs := tool.DefinitionsForMode(mode, tool.BuiltinDefinitions())
	fmt.Printf("\n===== tool schemas (%s, %d) =====\n", mode, len(defs))
	for _, def := range defs {
		fmt.Printf("\n## %s\n", def.Name)
		if def.Capability != "" {
			fmt.Printf("Capability: %s\n", store.NormalizeAgentMode(def.Capability))
		}
		if def.Description != "" {
			fmt.Printf("Description: %s\n", def.Description)
		}
		if len(def.InputSchema) > 0 {
			fmt.Printf("Parameters:\n%s\n", prettyJSON(def.InputSchema))
		}
	}
}

func prettyJSON(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(b)
}
