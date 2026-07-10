// puddingd 是 Pudding daemon 的 CLI 入口;Electron shell 通过同一个 daemon
// 二进制提供业务协议。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/daemon"
	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/prompt"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
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
		}
	}

	var (
		flagHome = flag.String("home", "", "data home (default: channel home, see docs)")
		flagAddr = flag.String("addr", home.DefaultAddr(), "HTTP listen address")
		flagMock = flag.Bool("mock", false, "use mock provider")
		flagLAN  = flag.Bool("lan", false, "listen on LAN interfaces for mobile pairing")
	)
	flag.Parse()

	d, err := daemon.Start(daemon.Options{
		Home:      *flagHome,
		Addr:      *flagAddr,
		Mock:      *flagMock,
		MobileLAN: *flagLAN,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-d.ServeErr():
		return err
	case <-ctx.Done():
	}

	slog.Info("puddingd shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return d.Shutdown(shutdownCtx)
}

func runTools(args []string, stdout, stderr io.Writer, now time.Time) error {
	if len(args) == 0 {
		return errors.New("usage: puddingd tools report [flags]")
	}
	switch args[0] {
	case "report":
		return runToolsReport(args[1:], stdout, stderr, now)
	case "help", "-h", "--help":
		_, err := fmt.Fprintln(stdout, "usage: puddingd tools report [--days 30] [--home PATH] [--all] [--json]")
		return err
	default:
		return fmt.Errorf("unknown tools command %q; usage: puddingd tools report [flags]", args[0])
	}
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
	flagMode := fs.String("mode", "chat", "agent mode: chat, workspace")
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
	if *flagSegments {
		for i, seg := range out.Segments {
			if i > 0 {
				fmt.Println()
			}
			fmt.Printf("===== %s (%s) =====\n%s\n", seg.ID, seg.Layer, seg.Content)
		}
	} else {
		fmt.Printf("===== system prompt =====\n%s\n", out.SystemInstruction)
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
