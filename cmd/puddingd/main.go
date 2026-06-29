// puddingd 是 Pudding daemon 的 CLI 入口;启动逻辑在 internal/daemon,
// 与桌面壳(cmd/pudding-desktop)同源。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/teatak/pudding-core/internal/daemon"
	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/prompt"
)

func main() {
	if err := run(); err != nil {
		slog.Error("puddingd", "err", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 && os.Args[1] == "prompt" {
		return runPrompt(os.Args[2:])
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

func runPrompt(args []string) error {
	fs := flag.NewFlagSet("prompt", flag.ExitOnError)
	flagHome := fs.String("home", "", "data home (default: channel home, see docs)")
	flagMode := fs.String("mode", "chat", "agent mode: chat, research, workspace")
	flagSegments := fs.Bool("segments", false, "print prompt segments with headers")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := home.Resolve(*flagHome)
	if err != nil {
		return err
	}
	out, err := prompt.NewLoader(dir).Prompt(context.Background(), *flagMode)
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
		return nil
	}
	fmt.Println(out.SystemInstruction)
	return nil
}
