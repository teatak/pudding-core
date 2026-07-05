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
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/api"
	appsvc "github.com/teatak/pudding-core/internal/app"
	audioasr "github.com/teatak/pudding-core/internal/audio/asr"
	sherpaasr "github.com/teatak/pudding-core/internal/audio/asr/sherpa"
	audiodriver "github.com/teatak/pudding-core/internal/audio/driver"
	portaudiodriver "github.com/teatak/pudding-core/internal/audio/driver/portaudio"
	aecproc "github.com/teatak/pudding-core/internal/audio/dsp/aec"
	nsproc "github.com/teatak/pudding-core/internal/audio/dsp/ns"
	"github.com/teatak/pudding-core/internal/audio/frame"
	audiotts "github.com/teatak/pudding-core/internal/audio/tts"
	"github.com/teatak/pudding-core/internal/audio/tts/edgetts"
	"github.com/teatak/pudding-core/internal/audio/tts/macsay"
	"github.com/teatak/pudding-core/internal/audio/voice"
	"github.com/teatak/pudding-core/internal/browser"
	"github.com/teatak/pudding-core/internal/buildinfo"
	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/desktopcamera"
	"github.com/teatak/pudding-core/internal/desktopscreen"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/mobileauth"
	"github.com/teatak/pudding-core/internal/prompt"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	skillsvc "github.com/teatak/pudding-core/internal/skill"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
	"github.com/teatak/pudding-core/internal/tool"
	"github.com/teatak/pudding-core/internal/webui"
)

type Options struct {
	Home      string // 空 = 通道默认目录
	Addr      string // 空 = 通道默认地址
	Mock      bool
	MobileLAN bool // true = 同一 HTTP 服务监听局域网接口
}

type Daemon struct {
	store     *sqlitestore.Store
	engine    *engine.Engine
	server    *http.Server
	listener  net.Listener
	localAddr string
	lanURLs   []string
	token     string
	homeDir   string
	voice     *voice.Service
	browser   browser.Service
	stopSSE   context.CancelFunc
	serveErr  chan error
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
	audioCfg, err := cfg.Audio(context.Background())
	if err != nil {
		slog.Warn("daemon: audio config unavailable, using defaults", "err", err)
		audioCfg = config.DefaultAudioConfig()
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
	apps := appsvc.NewService(dir, cfg)
	skills := skillsvc.NewService(dir)
	browserManager := browser.NewManager(browser.Config{HomeDir: dir, Headless: true})
	browserMCP := tool.NewBrowserMCPRunner()
	camera := desktopcamera.New()
	screen := desktopscreen.New()
	tools := tool.NewMultiRunner(
		tool.NewBuiltinRunner(tool.WithWebConfig(cfg), tool.WithAppEndpoints(apps), tool.WithSkills(skills), tool.WithHistorySearch(st), tool.WithBrowserState(st), tool.WithHomeDir(dir), tool.WithBrowser(browserManager), tool.WithCamera(camera), tool.WithDesktopScreen(screen)),
		browserMCP,
	)
	eng := engine.New(st, hub, resolver, cfg, engine.WithPromptSource(prompt.NewLoader(dir)), engine.WithAttachmentHome(dir), engine.WithTools(tools))
	audioDriver := defaultCaptureDriver(audioCfg)
	voiceService := voice.NewService(voice.ServiceConfig{
		Manager:   voice.NewManager(),
		Submitter: eng,
		Canceler:  eng,
		Events:    hub,
		Driver:    audioDriver,
		ASR:       defaultASR(dir, audioCfg),
		AEC:       defaultAEC(audioCfg, audioDriver),
		NS:        defaultNS(audioCfg, audioDriver),
		TTS:       defaultTTS(audioCfg),
	})
	if err := eng.Recover(context.Background()); err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("recover interrupted turns: %w", err)
	}

	addr := opts.Addr
	if addr == "" {
		addr = home.DefaultAddr()
	}
	listenAddr := addr
	if opts.MobileLAN {
		listenAddr = lanListenAddr(addr)
	}
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	localAddr := localAddrFor(addr, ln.Addr().String(), opts.MobileLAN)
	var lanURLs []string
	if opts.MobileLAN {
		lanURLs = lanURLsFor(ln.Addr().String())
	}
	devices, err := mobileauth.OpenDeviceStore(home.MobileDevicesPath(dir))
	if err != nil {
		_ = st.Close()
		_ = ln.Close()
		return nil, err
	}
	pairing := mobileauth.NewManager(devices, append(lanURLs, "http://"+localAddr+"/"))

	// request ctx 派生自此:Shutdown 时 SSE 长连接立即退出,不拖优雅关闭
	sseCtx, stopSSE := context.WithCancel(context.Background())
	apiServer := api.New(eng, st, cfg, hub).WithHome(dir).WithApps(apps).WithSkills(skills).WithBrowserMCP(browserMCP).WithVoice(voiceService).WithBrowser(browserManager).WithCamera(camera)
	server := &http.Server{
		Handler: apiServer.Handler(
			token,
			webui.Handler(),
			api.WithDeviceTokenValidator(devices),
			api.WithPairing(pairing),
		),
		BaseContext: func(net.Listener) context.Context { return sseCtx },
	}

	d := &Daemon{
		store:     st,
		engine:    eng,
		server:    server,
		listener:  ln,
		localAddr: localAddr,
		lanURLs:   lanURLs,
		token:     token,
		homeDir:   dir,
		voice:     voiceService,
		browser:   browserManager,
		stopSSE:   stopSSE,
		serveErr:  make(chan error, 1),
	}
	go func() { d.serveErr <- server.Serve(ln) }()

	slog.Info("puddingd starting",
		"channel", buildinfo.Channel(),
		"home", dir,
		"addr", d.Addr(),
		"listen", ln.Addr().String(),
		"lan", opts.MobileLAN,
		"lanURLs", d.LANURLs(),
		"provider", providerLabel,
		"store", "sqlite")
	slog.Info("open", "url", d.OpenURL())
	return d, nil
}

func (d *Daemon) Addr() string { return d.localAddr }

func (d *Daemon) LANURLs() []string { return append([]string(nil), d.lanURLs...) }

func (d *Daemon) Token() string { return d.token }

func (d *Daemon) Home() string { return d.homeDir }

// OpenURL 是带 token 的一键入口;前端读取后会从地址栏清掉。
func (d *Daemon) OpenURL() string {
	return fmt.Sprintf("http://%s/?token=%s", d.Addr(), d.token)
}

func lanListenAddr(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return addr
	}
	return net.JoinHostPort("0.0.0.0", port)
}

func localAddrFor(configuredAddr, actualAddr string, mobileLAN bool) string {
	if !mobileLAN {
		if host, port, err := net.SplitHostPort(actualAddr); err == nil && isUnspecifiedHost(host) {
			return net.JoinHostPort("127.0.0.1", port)
		}
		return actualAddr
	}
	_, port, err := net.SplitHostPort(actualAddr)
	if err != nil || port == "" {
		return configuredAddr
	}
	return net.JoinHostPort("127.0.0.1", port)
}

func lanURLsFor(actualAddr string) []string {
	_, port, err := net.SplitHostPort(actualAddr)
	if err != nil || port == "" {
		return nil
	}
	var urls []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := addrIP(addr)
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			if ip4 := ip.To4(); ip4 != nil {
				ip = ip4
			}
			host := ip.String()
			if seen[host] {
				continue
			}
			seen[host] = true
			urls = append(urls, "http://"+net.JoinHostPort(host, port)+"/")
		}
	}
	return urls
}

func addrIP(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}

func defaultTTS(cfg config.AudioConfig) audiotts.Client {
	cfg = cfg.WithDefaults()
	if !cfg.TTSEnabled() {
		slog.Info("daemon: tts disabled by config")
		return nil
	}
	profileName, profile := cfg.ActiveTTSProfile()
	switch strings.ToLower(profileName) {
	case "edge":
		client, err := edgetts.New(edgetts.Config{
			Voice: profile.Voice,
			Speed: float32(profile.Speed),
		})
		if err == nil {
			slog.Info("daemon: using edge tts", "tts", client.Name(), "profile", profileName)
			return client
		}
		slog.Warn("daemon: edge tts unavailable", "err", err)
	default:
		slog.Warn("daemon: unsupported tts profile", "profile", profileName)
	}
	if runtime.GOOS == "darwin" {
		client, err := macsay.New(macsay.Config{Rate: 230})
		if err == nil {
			return client
		}
		slog.Warn("daemon: macsay unavailable", "err", err)
	}
	return audiotts.NewNoop()
}

func defaultCaptureDriver(cfg config.AudioConfig) audiodriver.Driver {
	cfg = cfg.WithDefaults()
	if strings.ToLower(strings.TrimSpace(cfg.Driver.Type)) != "portaudio" {
		slog.Warn("daemon: unsupported audio driver", "driver", cfg.Driver.Type)
		return nil
	}
	return portaudiodriver.New(portaudiodriver.Config{
		InputFormat:  frame.Format{SampleRate: cfg.Driver.CaptureSampleRate, Channels: cfg.Driver.Channels},
		OutputFormat: frame.Format{SampleRate: cfg.Driver.PlaybackSampleRate, Channels: cfg.Driver.Channels},
		FrameMillis:  cfg.Driver.PeriodMillis,
	})
}

func defaultAEC(cfg config.AudioConfig, drv audiodriver.Driver) aecproc.Processor {
	cfg = cfg.WithDefaults()
	if !cfg.AECEnabled() {
		return nil
	}
	if drv == nil {
		return nil
	}
	if strings.ToLower(strings.TrimSpace(cfg.AEC.Model)) != "webrtc" {
		slog.Warn("daemon: unsupported aec model", "model", cfg.AEC.Model)
		return nil
	}
	format := drv.InputFormat()
	client, err := aecproc.NewWebRTCAEC(aecproc.WebRTCAECConfig{
		SampleRate:   format.SampleRate,
		Channels:     format.Channels,
		PeriodMillis: cfg.Driver.PeriodMillis,
	})
	if err != nil {
		slog.Warn("daemon: WebRTC AEC unavailable", "err", err)
		return nil
	}
	slog.Info("daemon: WebRTC AEC configured", "name", client.Name(), "sampleRate", format.SampleRate, "channels", format.Channels)
	return client
}

func defaultNS(cfg config.AudioConfig, drv audiodriver.Driver) nsproc.Processor {
	cfg = cfg.WithDefaults()
	if !cfg.NSEnabled() {
		return nil
	}
	if drv == nil {
		return nil
	}
	if strings.ToLower(strings.TrimSpace(cfg.NS.Model)) != "webrtc" {
		slog.Warn("daemon: unsupported ns model", "model", cfg.NS.Model)
		return nil
	}
	format := drv.InputFormat()
	client, err := nsproc.NewWebRTCNS(nsproc.WebRTCNSConfig{
		SampleRate:   format.SampleRate,
		Channels:     format.Channels,
		PeriodMillis: cfg.Driver.PeriodMillis,
		Level:        cfg.NS.Level,
	})
	if err != nil {
		slog.Warn("daemon: WebRTC NS unavailable", "err", err)
		return nil
	}
	slog.Info("daemon: WebRTC NS configured", "name", client.Name(), "level", client.Level(), "sampleRate", format.SampleRate, "channels", format.Channels)
	return client
}

func defaultASR(homeDir string, audioCfg config.AudioConfig) audioasr.Client {
	audioCfg = audioCfg.WithDefaults()
	if !audioCfg.ASREnabled() {
		slog.Info("daemon: asr disabled by config")
		return nil
	}
	cfg, ok := defaultSherpaConfig(homeDir, audioCfg)
	if !ok {
		slog.Warn("daemon: sherpa ASR models unavailable")
		return nil
	}
	client, err := sherpaasr.New(cfg)
	if err != nil {
		slog.Warn("daemon: sherpa ASR unavailable", "err", err)
		return nil
	}
	return client
}

func defaultSherpaConfig(homeDir string, audioCfg config.AudioConfig) (sherpaasr.Config, bool) {
	audioCfg = audioCfg.WithDefaults()
	asrCfg := audioCfg.ASR
	cfg := sherpaasr.Config{
		ModelPath:                   resolveAudioPath(homeDir, asrCfg.ModelPath, "asr"),
		TokensPath:                  resolveAudioPath(homeDir, asrCfg.TokensPath, "asr"),
		VADModelPath:                resolveAudioPath(homeDir, asrCfg.VAD.ModelPath, "vad"),
		Language:                    asrCfg.Language,
		UseInverseTextNormalization: audioCfg.ASRUseITN(),
		VADThreshold:                asrCfg.VAD.Threshold,
		MinSilenceDuration:          time.Duration(asrCfg.VAD.MinSilenceMillis) * time.Millisecond,
		MinSpeechDuration:           time.Duration(asrCfg.VAD.MinSpeechMillis) * time.Millisecond,
		VADWindowSize:               asrCfg.VAD.WindowSize,
		PrerollDuration:             time.Duration(asrCfg.VAD.PrerollMillis) * time.Millisecond,
		Provider:                    asrCfg.Provider,
		NumThreads:                  asrCfg.NumThreads,
	}
	if strings.ToLower(strings.TrimSpace(asrCfg.Engine)) != "sherpa-sensevoice" {
		slog.Warn("daemon: unsupported asr engine", "engine", asrCfg.Engine)
		return cfg, false
	}
	if fileExists(cfg.ModelPath) && fileExists(cfg.TokensPath) && fileExists(cfg.VADModelPath) {
		return cfg, true
	}
	return cfg, false
}

func resolveAudioPath(homeDir, raw, modelSubdir string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || filepath.IsAbs(raw) {
		return raw
	}
	clean := filepath.Clean(raw)
	if clean == "." {
		return ""
	}
	if strings.ContainsRune(clean, filepath.Separator) || strings.HasPrefix(clean, "runtime") {
		return filepath.Join(homeDir, clean)
	}
	return filepath.Join(homeDir, "runtime", "models", modelSubdir, clean)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isUnspecifiedHost(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}

// ServeErr 在 serve 异常退出时收到错误(正常 Shutdown 收到 http.ErrServerClosed)。
func (d *Daemon) ServeErr() <-chan error { return d.serveErr }

// Shutdown 优雅关闭:SSE 退出 → server 关闭 → 等 turn 收尾 → 关存储。
func (d *Daemon) Shutdown(ctx context.Context) error {
	if d.voice != nil {
		_ = d.voice.Close()
	}
	if d.browser != nil {
		_ = d.browser.Close()
	}
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
