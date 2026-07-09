package runtimeassets

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/teatak/pudding-core/internal/config"
)

const (
	DefaultRelease         = "runtime-v1"
	DefaultManifestFile    = "runtime-manifest.json"
	DefaultManifestURL     = "https://github.com/teatak/pudding/releases/download/" + DefaultRelease + "/" + DefaultManifestFile
	defaultRuntimeProfile  = "voice"
	markerFileName         = ".pudding-installed.json"
	installProgressSpacing = 250 * time.Millisecond
	downloadDialTimeout    = 15 * time.Second
	downloadHeaderTimeout  = 30 * time.Second
	cancelWaitTimeout      = 2 * time.Second
)

type Installer struct {
	home      string
	client    *http.Client
	onInstall func(context.Context) error

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	state  Status
}

type Status struct {
	OK              bool           `json:"ok"`
	Installed       bool           `json:"installed"`
	Disabled        bool           `json:"disabled,omitempty"`
	Running         bool           `json:"running"`
	State           string         `json:"state"`
	Release         string         `json:"release"`
	Profile         string         `json:"profile"`
	PlatformKey     string         `json:"platformKey"`
	CurrentAsset    string         `json:"currentAsset,omitempty"`
	AssetIndex      int            `json:"assetIndex,omitempty"`
	AssetTotal      int            `json:"assetTotal,omitempty"`
	BytesDownloaded int64          `json:"bytesDownloaded,omitempty"`
	BytesTotal      int64          `json:"bytesTotal,omitempty"`
	Message         string         `json:"message,omitempty"`
	Error           string         `json:"error,omitempty"`
	Required        []RequiredFile `json:"required"`
	Missing         []RequiredFile `json:"missing"`
}

type RequiredFile struct {
	Label  string `json:"label"`
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Exists bool   `json:"exists"`
}

type Manifest struct {
	SchemaVersion int                     `json:"schema_version"`
	RuntimeAPI    int                     `json:"runtime_api"`
	Release       string                  `json:"release"`
	BaseURL       string                  `json:"base_url"`
	Native        map[string]Asset        `json:"native"`
	Models        map[string]Asset        `json:"models"`
	Profiles      map[string]AssetProfile `json:"profiles"`
}

type Asset struct {
	Version    string `json:"version"`
	Asset      string `json:"asset"`
	SHA256     string `json:"sha256"`
	InstallDir string `json:"install_dir"`
}

type AssetProfile struct {
	Native bool     `json:"native"`
	Models []string `json:"models"`
}

type installPlan struct {
	Kind string
	Key  string
	Asset
}

type progressEvent struct {
	Stage           string
	Asset           string
	Index           int
	Total           int
	BytesDownloaded int64
	BytesTotal      int64
}

func NewInstaller(home string, onInstall func(context.Context) error) *Installer {
	return &Installer{
		home:      strings.TrimSpace(home),
		client:    &http.Client{Transport: downloadTransport()},
		onInstall: onInstall,
		state: Status{
			OK:          true,
			State:       "idle",
			Release:     DefaultRelease,
			Profile:     defaultRuntimeProfile,
			PlatformKey: PlatformKey(goruntime.GOOS, goruntime.GOARCH),
		},
	}
}

func downloadTransport() http.RoundTripper {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   downloadDialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   downloadDialTimeout,
		ResponseHeaderTimeout: downloadHeaderTimeout,
		ExpectContinueTimeout: time.Second,
	}
}

func PlatformKey(goos, goarch string) string {
	return goos + "_" + goarch
}

func (i *Installer) Status(ctx context.Context, cfg config.AudioConfig) Status {
	i.mu.Lock()
	status := i.state
	i.mu.Unlock()

	required, missing, disabled := RequiredFiles(i.home, cfg)
	status.OK = true
	status.Required = required
	status.Missing = missing
	status.Disabled = disabled
	status.Installed = !status.Running && !disabled && len(missing) == 0
	status.Release = DefaultRelease
	status.Profile = defaultRuntimeProfile
	status.PlatformKey = PlatformKey(goruntime.GOOS, goruntime.GOARCH)
	if !status.Running {
		if disabled {
			status.State = "disabled"
			status.Message = "ASR is disabled"
		} else if status.Installed {
			status.State = "installed"
			status.Message = ""
			status.Error = ""
		} else if status.State == "" || status.State == "installed" {
			status.State = "idle"
			status.Message = "voice runtime is missing"
		}
	}
	_ = ctx
	return normalizeStatus(status)
}

func (i *Installer) Start(ctx context.Context, cfg config.AudioConfig) Status {
	current := i.Status(ctx, cfg)
	if current.Disabled || current.Installed {
		return current
	}

	i.mu.Lock()
	if i.state.Running {
		status := i.state
		i.mu.Unlock()
		return normalizeStatus(status)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	i.cancel = cancel
	i.done = done
	i.state = Status{
		OK:          true,
		Running:     true,
		State:       "manifest",
		Release:     DefaultRelease,
		Profile:     defaultRuntimeProfile,
		PlatformKey: PlatformKey(goruntime.GOOS, goruntime.GOARCH),
		Message:     "loading runtime manifest",
		Required:    current.Required,
		Missing:     current.Missing,
	}
	status := i.state
	i.mu.Unlock()

	go i.install(runCtx, done)
	return normalizeStatus(status)
}

func (i *Installer) Cancel() Status {
	i.mu.Lock()
	cancel := i.cancel
	done := i.done
	if cancel == nil || !i.state.Running {
		status := i.state
		i.mu.Unlock()
		return normalizeStatus(status)
	}
	i.state.State = "canceling"
	i.state.Message = "canceling download"
	i.mu.Unlock()
	cancel()

	if done != nil {
		select {
		case <-done:
		case <-time.After(cancelWaitTimeout):
		}
	}

	i.mu.Lock()
	status := i.state
	i.mu.Unlock()
	return normalizeStatus(status)
}

func (i *Installer) install(ctx context.Context, done chan struct{}) {
	defer close(done)
	err := i.runInstall(ctx)
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.done == done {
		i.cancel = nil
		i.done = nil
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			i.state.Running = false
			i.state.State = "canceled"
			i.state.Message = "download canceled"
			i.state.Error = ""
			return
		}
		i.state.Running = false
		i.state.State = "error"
		i.state.Message = "runtime download failed"
		i.state.Error = runtimeErrorMessage(err)
		return
	}
	i.state.Running = false
	i.state.State = "installed"
	i.state.Installed = true
	i.state.Message = ""
	i.state.Error = ""
	i.state.BytesDownloaded = 0
	i.state.BytesTotal = 0
}

func (i *Installer) runInstall(ctx context.Context) error {
	manifest, err := loadManifest(ctx, DefaultManifestURL, i.client)
	if err != nil {
		return err
	}
	plans, err := voiceInstallPlan(manifest, goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		return err
	}
	tempRoot := filepath.Join(i.home, "temp", "runtime-install")
	if err := os.MkdirAll(tempRoot, 0o755); err != nil {
		return err
	}
	for idx, plan := range plans {
		if err := ctx.Err(); err != nil {
			return err
		}
		i.setProgress(progressEvent{
			Stage: "asset_start",
			Asset: plan.Asset.Asset,
			Index: idx + 1,
			Total: len(plans),
		})
		if err := i.installAsset(ctx, manifest.BaseURL, tempRoot, plan, idx+1, len(plans)); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := writeMarker(i.home, manifest); err != nil {
		return err
	}
	if i.onInstall != nil {
		i.setProgress(progressEvent{Stage: "loading", Index: len(plans), Total: len(plans)})
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := i.onInstall(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (i *Installer) installAsset(ctx context.Context, baseURL, tempRoot string, plan installPlan, index, total int) error {
	if plan.Asset.Asset == "" {
		return fmt.Errorf("%s %q has empty asset", plan.Kind, plan.Key)
	}
	if plan.InstallDir == "" {
		return fmt.Errorf("%s %q has empty install_dir", plan.Kind, plan.Key)
	}
	downloadPath := filepath.Join(tempRoot, plan.Key+"-"+filepath.Base(plan.Asset.Asset))
	defer os.Remove(downloadPath)
	if err := fetch(ctx, i.client, resolveAssetSource(baseURL, plan.Asset.Asset), downloadPath, plan, index, total, i.setProgress); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if isSHA256(plan.SHA256) {
		i.setProgress(progressEvent{Stage: "verifying", Asset: plan.Asset.Asset, Index: index, Total: total})
		if err := verifySHA256(downloadPath, plan.SHA256); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	target := filepath.Join(i.home, filepath.FromSlash(plan.InstallDir))
	stage := target + ".staging-" + fmt.Sprint(time.Now().UnixNano())
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	i.setProgress(progressEvent{Stage: "extracting", Asset: plan.Asset.Asset, Index: index, Total: total})
	if err := extractTarGz(downloadPath, stage); err != nil {
		_ = os.RemoveAll(stage)
		return err
	}
	if err := ctx.Err(); err != nil {
		_ = os.RemoveAll(stage)
		return err
	}
	if err := os.RemoveAll(target); err != nil {
		_ = os.RemoveAll(stage)
		return err
	}
	if err := os.Rename(stage, target); err != nil {
		_ = os.RemoveAll(stage)
		return err
	}
	return nil
}

func (i *Installer) setProgress(ev progressEvent) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.state.Running = true
	i.state.State = ev.Stage
	i.state.CurrentAsset = ev.Asset
	i.state.AssetIndex = ev.Index
	i.state.AssetTotal = ev.Total
	i.state.BytesDownloaded = ev.BytesDownloaded
	i.state.BytesTotal = ev.BytesTotal
	switch ev.Stage {
	case "asset_start":
		i.state.Message = "starting download"
	case "downloading":
		i.state.Message = "downloading runtime"
	case "downloaded":
		i.state.Message = "download complete"
	case "verifying":
		i.state.Message = "verifying runtime"
	case "extracting":
		i.state.Message = "installing runtime"
	case "loading":
		i.state.Message = "loading voice runtime"
	}
}

func RequiredFiles(home string, cfg config.AudioConfig) ([]RequiredFile, []RequiredFile, bool) {
	cfg = cfg.WithDefaults()
	if !cfg.ASREnabled() {
		return []RequiredFile{}, []RequiredFile{}, true
	}
	files := []RequiredFile{
		{Label: "asr_model", Path: resolveAudioPath(home, cfg.ASR.ModelPath, "asr"), Kind: "file"},
		{Label: "asr_tokens", Path: resolveAudioPath(home, cfg.ASR.TokensPath, "asr"), Kind: "file"},
		{Label: "vad_model", Path: resolveAudioPath(home, cfg.ASR.VAD.ModelPath, "vad"), Kind: "file"},
	}
	missing := make([]RequiredFile, 0, len(files))
	for idx := range files {
		files[idx].Exists = requiredExists(files[idx])
		if !files[idx].Exists {
			missing = append(missing, files[idx])
		}
	}
	return files, missing, false
}

func normalizeStatus(status Status) Status {
	if status.Required == nil {
		status.Required = []RequiredFile{}
	}
	if status.Missing == nil {
		status.Missing = []RequiredFile{}
	}
	return status
}

func runtimeErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return "download failed"
}

func requiredExists(file RequiredFile) bool {
	info, err := os.Stat(file.Path)
	if err != nil {
		return false
	}
	if file.Kind == "dir" {
		return info.IsDir()
	}
	return !info.IsDir()
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

func loadManifest(ctx context.Context, source string, client *http.Client) (Manifest, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return Manifest{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Manifest{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Manifest{}, fmt.Errorf("download manifest: %s", resp.Status)
	}
	var manifest Manifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return Manifest{}, err
	}
	if manifest.BaseURL == "" {
		manifest.BaseURL = manifestBaseURL(source)
	}
	if manifest.Release == "" {
		manifest.Release = DefaultRelease
	}
	return manifest, nil
}

func manifestBaseURL(source string) string {
	u, err := url.Parse(source)
	if err != nil {
		return ""
	}
	u.Path = strings.TrimSuffix(u.Path, filepath.Base(u.Path))
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/")
}

func voiceInstallPlan(manifest Manifest, goos, goarch string) ([]installPlan, error) {
	var plans []installPlan
	_ = goos
	_ = goarch
	for _, key := range []string{"vad_silero", "asr_sensevoice"} {
		asset, ok := manifest.Models[key]
		if !ok {
			return nil, fmt.Errorf("model runtime %q not found", key)
		}
		plans = append(plans, installPlan{Kind: "model", Key: key, Asset: asset})
	}
	return plans, nil
}

func fetch(ctx context.Context, client *http.Client, source, dst string, plan installPlan, index, total int, progress func(progressEvent)) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".download"
	defer os.Remove(tmp)
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer out.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("download %s: %s", source, resp.Status)
	}
	if _, err := copyWithProgress(ctx, out, resp.Body, maxInt64(resp.ContentLength, 0), plan, index, total, progress); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func copyWithProgress(ctx context.Context, dst io.Writer, src io.Reader, totalBytes int64, plan installPlan, index, total int, progress func(progressEvent)) (int64, error) {
	progress(progressEvent{Stage: "downloading", Asset: plan.Asset.Asset, Index: index, Total: total, BytesTotal: totalBytes})
	var copied int64
	buf := make([]byte, 256*1024)
	lastEmit := time.Now()
	for {
		if err := ctx.Err(); err != nil {
			return copied, err
		}
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[:nr])
			if nw < 0 || nw > nr {
				return copied, io.ErrShortWrite
			}
			copied += int64(nw)
			now := time.Now()
			if now.Sub(lastEmit) >= installProgressSpacing || copied == totalBytes {
				lastEmit = now
				progress(progressEvent{Stage: "downloading", Asset: plan.Asset.Asset, Index: index, Total: total, BytesDownloaded: copied, BytesTotal: totalBytes})
			}
			if ew != nil {
				return copied, ew
			}
			if nr != nw {
				return copied, io.ErrShortWrite
			}
		}
		if er != nil {
			if errors.Is(er, io.EOF) {
				break
			}
			return copied, er
		}
	}
	progress(progressEvent{Stage: "downloaded", Asset: plan.Asset.Asset, Index: index, Total: total, BytesDownloaded: copied, BytesTotal: totalBytes})
	return copied, nil
}

func resolveAssetSource(base, asset string) string {
	if strings.HasPrefix(asset, "http://") || strings.HasPrefix(asset, "https://") {
		return asset
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(asset, "/")
}

func verifySHA256(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("sha256 mismatch for %s", filepath.Base(path))
	}
	return nil
}

func isSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func extractTarGz(path, target string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if skipArchiveEntry(hdr.Name) {
			continue
		}
		dst, err := safeJoin(target, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, os.FileMode(hdr.Mode)&0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		}
	}
}

func skipArchiveEntry(name string) bool {
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean == "." {
		return true
	}
	parts := strings.Split(clean, "/")
	for _, part := range parts {
		if part == "__MACOSX" || strings.HasPrefix(part, "._") {
			return true
		}
	}
	return false
}

func safeJoin(root, name string) (string, error) {
	cleanName := filepath.Clean(filepath.FromSlash(name))
	if cleanName == "." {
		return root, nil
	}
	if filepath.IsAbs(cleanName) || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe archive path: %s", name)
	}
	dst := filepath.Join(root, cleanName)
	rel, err := filepath.Rel(root, dst)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe archive path: %s", name)
	}
	return dst, nil
}

func writeMarker(home string, manifest Manifest) error {
	data, err := json.MarshalIndent(map[string]any{
		"installed_at": time.Now().UTC().Format(time.RFC3339),
		"release":      manifest.Release,
		"runtime_api":  manifest.RuntimeAPI,
		"profile":      defaultRuntimeProfile,
	}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	for _, dir := range []string{
		filepath.Join(home, "runtime"),
		filepath.Join(home, "runtime", "models"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, markerFileName), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
