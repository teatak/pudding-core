// puddingd 是 Pudding 的 daemon 入口。
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/teatak/pudding-core/internal/api"
	"github.com/teatak/pudding-core/internal/buildinfo"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/store/memstore"
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
		flagModel = flag.String("model", "mock-model", "default model")
	)
	flag.Parse()

	dir, err := home.Resolve(*flagHome)
	if err != nil {
		return err
	}
	if err := home.Prepare(dir); err != nil {
		return err
	}
	token, err := loadOrCreateToken(home.TokenPath(dir))
	if err != nil {
		return err
	}

	var client provider.Client
	if *flagMock {
		client = mock.New()
	} else {
		// OpenAI provider 由轨道 B 交付(docs/phase-1-plan.md 第 3 节)
		return errors.New("only --mock is available in M0")
	}

	// SQLite store 由轨道 A 交付后替换;memstore 重启即清空
	st := memstore.New()
	defer st.Close()
	hub := event.NewHub()
	eng := engine.New(st, hub, client, *flagModel)
	server := &http.Server{
		Addr:    *flagAddr,
		Handler: api.New(eng, st, hub).Handler(token),
	}

	slog.Info("puddingd starting",
		"channel", buildinfo.Channel(),
		"home", dir,
		"addr", *flagAddr,
		"provider", client.Name(),
		"store", "memory")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	slog.Info("puddingd shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	eng.Wait()
	return nil
}

// loadOrCreateToken 读取或生成 daemon token(0600);
// 所有 HTTP/SSE 请求都必须带它(docs/technology-decisions.md 第 9 节)。
func loadOrCreateToken(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		return string(b), nil
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		return "", fmt.Errorf("write token: %w", err)
	}
	return token, nil
}
