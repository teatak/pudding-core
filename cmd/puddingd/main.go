// puddingd 是 Pudding daemon 的 CLI 入口;启动逻辑在 internal/daemon,
// 与桌面壳(cmd/pudding-desktop)同源。
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/teatak/pudding-core/internal/daemon"
	"github.com/teatak/pudding-core/internal/home"
)

func main() {
	if err := run(); err != nil {
		slog.Error("puddingd", "err", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		flagHome  = flag.String("home", "", "data home (default: channel home, see docs)")
		flagAddr  = flag.String("addr", home.DefaultAddr(), "HTTP listen address")
		flagMock  = flag.Bool("mock", false, "use mock provider")
		flagModel = flag.String("model", "mock-model", "fallback model (mock/dev)")
	)
	flag.Parse()

	d, err := daemon.Start(daemon.Options{
		Home:         *flagHome,
		Addr:         *flagAddr,
		Mock:         *flagMock,
		DefaultModel: *flagModel,
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
