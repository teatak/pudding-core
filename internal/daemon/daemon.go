// Package daemon 封装 puddingd 的启动/关闭逻辑,供 CLI 和 Electron shell
// 启动的 daemon 二进制复用。
package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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
	"github.com/teatak/pudding-core/internal/audio/runtimeassets"
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
	"github.com/teatak/pudding-core/internal/lsp"
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
	lsp       *lsp.Manager
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
		return nil, err
	}
	listenerOwned := false
	defer func() {
		if !listenerOwned {
			_ = ln.Close()
		}
	}()
	localAddr := localAddrFor(addr, ln.Addr().String(), opts.MobileLAN)
	var lanURLs []string
	if opts.MobileLAN {
		lanURLs = lanURLsFor(ln.Addr().String())
	}
	environmentCtx, cancelEnvironment := context.WithTimeout(context.Background(), 5*time.Second)
	environment, environmentErr := tool.CaptureCommandEnvironment(environmentCtx)
	cancelEnvironment()
	if environmentErr != nil {
		slog.Warn("daemon: user development environment unavailable; using process environment", "err", environmentErr)
	} else if environment.Supported {
		slog.Info("daemon: user development environment captured", "shell", environment.Shell, "variables", environment.VariableCount)
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
	audioCfg = audioCfg.WithDefaults()

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
	browserService, err := newBrowserService(dir, st)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	browserMCP := tool.NewBrowserMCPRunner()
	apps.WithRuntimeSource(browserMCP)
	appMCP := tool.NewAppMCPRunner(apps)
	camera := desktopcamera.New()
	screen := desktopscreen.New()
	languageServers := lsp.NewManager()
	backgroundProcessEvents := func(processEvent tool.BackgroundProcessEvent) {
		kind := event.ProcessFinished
		switch processEvent.Phase {
		case tool.BackgroundProcessStarted:
			kind = event.ProcessStarted
		case tool.BackgroundProcessStopped:
			kind = event.ProcessStopped
		case tool.BackgroundProcessRemoved:
			kind = event.ProcessRemoved
		}
		payload, err := json.Marshal(processEvent.Process)
		if err != nil {
			slog.Warn("daemon: encode background process event", "err", err)
			return
		}
		hub.Publish(event.Event{
			SessionID: processEvent.SessionID,
			Kind:      kind,
			TurnID:    processEvent.Process.TurnID,
			CallID:    processEvent.Process.CallID,
			Payload:   payload,
		})
	}
	tools := tool.NewMultiRunner(
		tool.NewBuiltinRunner(tool.WithWebConfig(cfg), tool.WithAppEndpoints(apps), tool.WithAppAuthoring(apps), tool.WithSkills(skills), tool.WithHistorySearch(st), tool.WithBrowserState(st), tool.WithHomeDir(dir), tool.WithCommandSandbox(dir), tool.WithBrowser(browserService), tool.WithLanguageService(languageServers), tool.WithCamera(camera), tool.WithDesktopScreen(screen), tool.WithBackgroundProcessEvents(backgroundProcessEvents)),
		browserMCP,
		appMCP,
	)
	eng := engine.New(st, hub, resolver, cfg, engine.WithPromptSource(prompt.NewLoaderWithApps(dir, apps, cfg)), engine.WithAttachmentHome(dir), engine.WithTools(tools), engine.WithApps(apps))
	audioDriver := defaultCaptureDriver(audioCfg)
	voiceService := voice.NewService(voice.ServiceConfig{
		Manager:           voice.NewManager(),
		Submitter:         eng,
		Canceler:          eng,
		Events:            hub,
		Driver:            audioDriver,
		ASR:               defaultASR(dir, audioCfg),
		AEC:               defaultAEC(audioCfg, audioDriver),
		NS:                defaultNS(audioCfg, audioDriver),
		TTS:               defaultTTS(audioCfg),
		HomeDir:           dir,
		SaveAudio:         audioCfg.ASRSaveAudio(),
		MinEnergy:         audioCfg.ASR.VAD.MinEnergy,
		PlaybackMinEnergy: audioCfg.ASR.VAD.PlaybackMinEnergy,
	})
	audioRuntime := runtimeassets.NewInstaller(dir, func(ctx context.Context) error {
		currentAudio, err := cfg.Audio(ctx)
		if err != nil {
			return err
		}
		return voiceService.ReplaceASR(defaultASR(dir, currentAudio))
	})
	if err := eng.Recover(context.Background()); err != nil {
		_ = languageServers.Close(context.Background())
		_ = st.Close()
		return nil, fmt.Errorf("recover interrupted turns: %w", err)
	}

	devices, err := mobileauth.OpenDeviceStore(home.MobileDevicesPath(dir))
	if err != nil {
		_ = languageServers.Close(context.Background())
		_ = st.Close()
		return nil, err
	}
	pairing := mobileauth.NewManager(devices, append(lanURLs, "http://"+localAddr+"/"))

	// request ctx 派生自此:Shutdown 时 SSE 长连接立即退出,不拖优雅关闭
	sseCtx, stopSSE := context.WithCancel(context.Background())
	apiServer := api.New(eng, st, cfg, hub).WithHome(dir).WithApps(apps).WithSkills(skills).WithBrowserMCP(browserMCP).WithVoice(voiceService).WithAudioRuntime(audioRuntime).WithBrowser(browserService).WithCamera(camera)
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
		browser:   browserService,
		lsp:       languageServers,
		stopSSE:   stopSSE,
		serveErr:  make(chan error, 1),
	}
	listenerOwned = true
	go apiServer.RunSessionArchiveJanitor(sseCtx)
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
	slog.Info("puddingd ready", "url", fmt.Sprintf("http://%s/", d.Addr()))
	return d, nil
}

func newBrowserService(homeDir string, projectFiles browser.ProjectFileScope) (browser.Service, error) {
	fileURLs := browser.ProjectFileURLAuthorizer(projectFiles)
	bridgeURL := strings.TrimSpace(os.Getenv("PUDDING_ELECTRON_BROWSER_BRIDGE_URL"))
	bridgeToken := strings.TrimSpace(os.Getenv("PUDDING_ELECTRON_BROWSER_BRIDGE_TOKEN"))
	if bridgeURL != "" || bridgeToken != "" {
		service, err := browser.NewElectronBridgeService(browser.ElectronBridgeConfig{
			URL:               bridgeURL,
			Token:             bridgeToken,
			FileURLAuthorizer: fileURLs,
		})
		if err != nil {
			return nil, err
		}
		slog.Info("browser service", "kind", "electron-bridge")
		return service, nil
	}
	slog.Info("browser service", "kind", "chrome-manager")
	return browser.NewManager(browser.Config{HomeDir: homeDir, Headless: true, FileURLAuthorizer: fileURLs}), nil
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
	if d.lsp != nil {
		if err := d.lsp.Close(ctx); err != nil {
			_ = d.store.Close()
			return err
		}
	}
	return d.store.Close()
}

// loadOrCreateToken 读取或生成 daemon token(0600);
// 所有 API 请求都必须带它(docs/technology-decisions.md 第 9 节)。
var tokenFileMu sync.Mutex

func loadOrCreateToken(path string) (string, error) {
	tokenFileMu.Lock()
	defer tokenFileMu.Unlock()

	const maxTokenFileBytes = 4 << 10
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, info, err := readTokenFile(path, maxTokenFileBytes)
		if err == nil {
			if token := strings.TrimSpace(string(b)); token != "" {
				return token, nil
			}
			if time.Since(info.ModTime()) >= 2*time.Second {
				if removeErr := removeTokenFileIfSame(path, info); removeErr != nil {
					return "", fmt.Errorf("remove stale token: %w", removeErr)
				}
				continue
			}
			if time.Now().After(deadline) {
				return "", errors.New("daemon token remained empty during startup")
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if errors.Is(err, errTokenFileChanged) {
			if time.Now().After(deadline) {
				return "", errors.New("daemon token kept changing during startup")
			}
			time.Sleep(time.Millisecond)
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("read token: %w", err)
		}

		candidate, token, err := createTokenCandidate(filepath.Dir(path))
		if err != nil {
			return "", err
		}
		if err := os.Link(candidate, path); errors.Is(err, os.ErrExist) {
			_ = os.Remove(candidate)
			continue
		} else if err != nil {
			_ = os.Remove(candidate)
			return "", fmt.Errorf("publish token: %w", err)
		}
		_ = os.Remove(candidate)
		return token, nil
	}
}

var errTokenFileChanged = errors.New("daemon token changed while opening")

func readTokenFile(path string, maxBytes int64) ([]byte, os.FileInfo, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() {
		return nil, nil, errors.New("daemon token must be a regular file, not a symlink")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	openInfo, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !openInfo.Mode().IsRegular() {
		return nil, nil, errors.New("daemon token must be a regular file")
	}
	if !os.SameFile(pathInfo, openInfo) {
		return nil, nil, errTokenFileChanged
	}
	if err := f.Chmod(0o600); err != nil {
		return nil, nil, fmt.Errorf("secure token permissions: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, nil, errors.New("daemon token file is too large")
	}
	return data, openInfo, nil
}

func removeTokenFileIfSame(path string, expected os.FileInfo) error {
	current, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.Mode()&os.ModeSymlink != 0 || !os.SameFile(current, expected) {
		return nil
	}
	return os.Remove(path)
}

func createTokenCandidate(dir string) (string, string, error) {
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(random)
	f, err := os.CreateTemp(dir, ".daemon-token-")
	if err != nil {
		return "", "", fmt.Errorf("create token candidate: %w", err)
	}
	path := f.Name()
	committed := false
	defer func() {
		if !committed {
			_ = f.Close()
			_ = os.Remove(path)
		}
	}()
	if _, err := f.WriteString(token); err != nil {
		return "", "", fmt.Errorf("write token: %w", err)
	}
	if err := f.Sync(); err != nil {
		return "", "", fmt.Errorf("sync token: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", "", fmt.Errorf("close token: %w", err)
	}
	committed = true
	return path, token, nil
}
