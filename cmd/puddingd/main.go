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
	"net"
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
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
	"github.com/teatak/pudding-core/internal/webui"
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

	st, err := sqlitestore.Open(home.DBPath(dir))
	if err != nil {
		return err
	}
	defer st.Close()

	// provider 解析走 registry:profile 表 → legacy settings 键 → env。
	// 未配置也允许启动,submit 会以 turn.failed 提示去 /providers 配置。
	var resolver engine.Resolver
	providerLabel := "registry"
	if *flagMock {
		resolver = registry.Static(mock.New())
		providerLabel = "mock"
	} else {
		resolver = registry.New(st)
	}
	hub := event.NewHub()
	eng := engine.New(st, hub, resolver, *flagModel)
	if err := eng.Recover(context.Background()); err != nil {
		return fmt.Errorf("recover interrupted turns: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := &http.Server{
		Addr:    *flagAddr,
		Handler: api.New(eng, st, hub).Handler(token, webui.Handler()),
		// request ctx 派生自 signal ctx:收到信号后 SSE 长连接立即退出,
		// Shutdown 不再被流式请求拖满超时。
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	slog.Info("puddingd starting",
		"channel", buildinfo.Channel(),
		"home", dir,
		"addr", *flagAddr,
		"provider", providerLabel,
		"store", "sqlite")
	// 浏览器一键入口:URL 带 token,前端读取后存 sessionStorage 并清掉地址栏
	slog.Info("open", "url", fmt.Sprintf("http://%s/?token=%s", *flagAddr, token))

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
		if !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		slog.Warn("puddingd shutdown timeout, closing active connections")
		if err := server.Close(); err != nil {
			return err
		}
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
