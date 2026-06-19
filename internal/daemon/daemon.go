// Package daemon 把 puddingd 的启动/关闭逻辑封装为可嵌入形态:
// CLI(cmd/puddingd)与桌面壳(cmd/pudding-desktop)同进程复用,
// 单二进制、token 内存直传。
package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"

	"github.com/teatak/pudding-core/internal/api"
	"github.com/teatak/pudding-core/internal/buildinfo"
	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
	"github.com/teatak/pudding-core/internal/webui"
)

type Options struct {
	Home string // 空 = 通道默认目录
	Addr string // 空 = 通道默认地址
	Mock bool
}

type Daemon struct {
	store    *sqlitestore.Store
	engine   *engine.Engine
	server   *http.Server
	listener net.Listener
	token    string
	homeDir  string
	stopSSE  context.CancelFunc
	serveErr chan error
}

// Start 完成启动并开始 serve;返回时端口已监听、恢复已完成。
func Start(opts Options) (*Daemon, error) {
	dir, err := home.Resolve(opts.Home)
	if err != nil {
		return nil, err
	}
	if err := home.Prepare(dir); err != nil {
		return nil, err
	}
	token, err := loadOrCreateToken(home.TokenPath(dir))
	if err != nil {
		return nil, err
	}

	st, err := sqlitestore.Open(home.DBPath(dir))
	if err != nil {
		return nil, err
	}
	cfg := config.NewManager(dir)
	if err := cfg.Prepare(); err != nil {
		_ = st.Close()
		return nil, err
	}

	var resolver engine.Resolver
	providerLabel := "registry"
	if opts.Mock {
		resolver = registry.Static(mock.New())
		providerLabel = "mock"
	} else {
		resolver = registry.New(cfg)
	}
	hub := event.NewHub()
	eng := engine.New(st, hub, resolver, cfg)
	if err := eng.Recover(context.Background()); err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("recover interrupted turns: %w", err)
	}

	addr := opts.Addr
	if addr == "" {
		addr = home.DefaultAddr()
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		_ = st.Close()
		return nil, err
	}

	// request ctx 派生自此:Shutdown 时 SSE 长连接立即退出,不拖优雅关闭
	sseCtx, stopSSE := context.WithCancel(context.Background())
	server := &http.Server{
		Handler:     api.New(eng, st, cfg, hub).Handler(token, webui.Handler()),
		BaseContext: func(net.Listener) context.Context { return sseCtx },
	}

	d := &Daemon{
		store:    st,
		engine:   eng,
		server:   server,
		listener: ln,
		token:    token,
		homeDir:  dir,
		stopSSE:  stopSSE,
		serveErr: make(chan error, 1),
	}
	go func() { d.serveErr <- server.Serve(ln) }()

	slog.Info("puddingd starting",
		"channel", buildinfo.Channel(),
		"home", dir,
		"addr", d.Addr(),
		"provider", providerLabel,
		"store", "sqlite")
	slog.Info("open", "url", d.OpenURL())
	return d, nil
}

func (d *Daemon) Addr() string { return d.listener.Addr().String() }

func (d *Daemon) Token() string { return d.token }

func (d *Daemon) Home() string { return d.homeDir }

// OpenURL 是带 token 的一键入口;前端读取后会从地址栏清掉。
func (d *Daemon) OpenURL() string {
	return fmt.Sprintf("http://%s/?token=%s", d.Addr(), d.token)
}

// ServeErr 在 serve 异常退出时收到错误(正常 Shutdown 收到 http.ErrServerClosed)。
func (d *Daemon) ServeErr() <-chan error { return d.serveErr }

// Shutdown 优雅关闭:SSE 退出 → server 关闭 → 等 turn 收尾 → 关存储。
func (d *Daemon) Shutdown(ctx context.Context) error {
	d.stopSSE()
	err := d.server.Shutdown(ctx)
	if err != nil {
		if !errors.Is(err, context.DeadlineExceeded) {
			_ = d.store.Close()
			return err
		}
		slog.Warn("daemon: shutdown timeout, closing active connections")
		if cerr := d.server.Close(); cerr != nil {
			_ = d.store.Close()
			return cerr
		}
	}
	// 先取消辅助 goroutine(自动标题等 best-effort 任务),再等所有
	// goroutine 收尾:否则进行中的标题 LLM 调用会把 Wait() 拖到 30s。
	// turn goroutine 仍各自收尾(写完 canonical),由 provider 超时兜底。
	d.engine.Stop()
	d.engine.Wait()
	return d.store.Close()
}

// loadOrCreateToken 读取或生成 daemon token(0600);
// 所有 API 请求都必须带它(docs/technology-decisions.md 第 9 节)。
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
