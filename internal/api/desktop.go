package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kbinani/screenshot"
	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/buildinfo"
	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/store"
)

const desktopSaveBodyLimit = 80 * 1024 * 1024

var (
	errDesktopScreenshotUnsupported = errors.New("desktop screenshot unsupported")
	runDesktopScreenshots           = defaultRunDesktopScreenshots
)

type desktopSaveFileRequest struct {
	Filename string `json:"filename"`
	Mime     string `json:"mime"`
	Data     string `json:"data"`
}

type desktopRevealFileRequest struct {
	Path string `json:"path"`
}

type desktopAboutResponse struct {
	Sections []desktopAboutSection `json:"sections"`
}

type desktopAboutSection struct {
	ID    string            `json:"id"`
	Title string            `json:"title"`
	Rows  []desktopAboutRow `json:"rows"`
}

type desktopAboutRow struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type audioConfigReader interface {
	Audio(context.Context) (config.AudioConfig, error)
}

func (s *Server) desktopAbout(c *cart.Context) error {
	audioCfg := config.DefaultAudioConfig()
	if reader, ok := s.config.(audioConfigReader); ok {
		cfg, err := reader.Audio(c.Request.Context())
		if err == nil {
			audioCfg = cfg
		}
	}
	audioCfg = audioCfg.WithDefaults()

	bindings := map[string]string{
		"input_owner":  "—",
		"output_owner": "—",
	}
	if s.voice != nil {
		snap := s.voice.Snapshot()
		if snap.InputOwner != "" {
			bindings["input_owner"] = snap.InputOwner
		}
		if snap.OutputOwner != "" {
			bindings["output_owner"] = snap.OutputOwner
		}
	}

	ttsProfileName, ttsProfile := audioCfg.ActiveTTSProfile()
	sections := []desktopAboutSection{
		aboutSection("version", "版本", aboutRows(
			"pudding", buildinfo.Channel(),
			"go", runtime.Version(),
			"platform", runtime.GOOS+"/"+runtime.GOARCH,
		)),
		aboutSection("service", "服务", aboutRows(
			"endpoint", requestBaseURL(c.Request),
			"phase", "active",
			"home", s.home,
		)),
		aboutSection("health", "健康自检", aboutRows(
			"capture", enabledText(audioCfg.ASREnabled() && strings.EqualFold(audioCfg.Driver.Type, "portaudio")),
			"playback", enabledText(audioCfg.TTSEnabled() && strings.EqualFold(ttsProfileName, "edge")),
		)),
		aboutSection("audio_bindings", "语音绑定", aboutRows(
			"input_owner", bindings["input_owner"],
			"output_owner", bindings["output_owner"],
		)),
		aboutSection("audio_config", "语音配置文件", aboutRows(
			"path", filepath.Join(s.home, "config", "audio.yaml"),
		)),
		aboutSection("driver", "驱动 (Driver)", aboutRows(
			"type", audioCfg.Driver.Type,
			"capture_sample_rate", intText(audioCfg.Driver.CaptureSampleRate),
			"playback_sample_rate", intText(audioCfg.Driver.PlaybackSampleRate),
			"channels", intText(audioCfg.Driver.Channels),
			"period_millis", intText(audioCfg.Driver.PeriodMillis),
		)),
		aboutSection("asr", "语音识别 (ASR)", aboutRows(
			"enabled", onOff(audioCfg.ASREnabled()),
			"save_audio", onOff(audioCfg.ASRSaveAudio()),
			"engine", audioCfg.ASR.Engine,
			"language", audioCfg.ASR.Language,
			"model_path", baseNameOrDash(audioCfg.ASR.ModelPath),
			"tokens_path", baseNameOrDash(audioCfg.ASR.TokensPath),
			"num_threads", intText(audioCfg.ASR.NumThreads),
			"provider", audioCfg.ASR.Provider,
			"use_itn", onOff(audioCfg.ASRUseITN()),
		)),
		aboutSection("asr_vad", "ASR 内置切句 VAD", aboutRows(
			"model_path", baseNameOrDash(audioCfg.ASR.VAD.ModelPath),
			"threshold", floatText(audioCfg.ASR.VAD.Threshold),
			"min_energy", floatText(audioCfg.ASR.VAD.MinEnergy),
			"playback_min_energy", floatText(audioCfg.ASR.VAD.PlaybackMinEnergy),
			"min_silence_millis", intText(audioCfg.ASR.VAD.MinSilenceMillis),
			"min_speech_millis", intText(audioCfg.ASR.VAD.MinSpeechMillis),
			"window_size", intText(audioCfg.ASR.VAD.WindowSize),
			"preroll_millis", intText(audioCfg.ASR.VAD.PrerollMillis),
		)),
		aboutSection("aec", "回声消除 (AEC)", aboutRows(
			"enabled", onOff(audioCfg.AECEnabled()),
			"model", audioCfg.AEC.Model,
		)),
		aboutSection("ns", "噪声抑制 (NS)", aboutRows(
			"enabled", onOff(audioCfg.NSEnabled()),
			"model", audioCfg.NS.Model,
			"level", audioCfg.NS.Level,
		)),
		aboutSection("tts", "语音合成 (TTS)", aboutRows(
			"enabled", onOff(audioCfg.TTSEnabled()),
			"profile", ttsProfileName,
			"backend", ttsProfileName,
			"voice", ttsProfile.Voice,
			"speed", floatText(ttsProfile.Speed),
		)),
	}
	c.JSON(http.StatusOK, desktopAboutResponse{Sections: sections})
	return nil
}

func (s *Server) desktopScreenshot(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return badRequest(c, "invalid session id")
	}
	if sessionID != attachment.DraftSessionID {
		if _, err := s.store.GetSession(c.Request.Context(), sessionID); err != nil {
			return s.fail(c, err)
		}
	}
	if strings.TrimSpace(s.home) == "" {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "attachment_home_unavailable"})
		return nil
	}

	tmpDir, err := os.MkdirTemp("", "pudding-screenshot-*")
	if err != nil {
		return s.fail(c, err)
	}
	defer os.RemoveAll(tmpDir)

	filename := "Screenshot " + time.Now().Format("2006-01-02 15.04.05")
	paths, err := runDesktopScreenshots(c.Request.Context(), tmpDir, filename)
	if err != nil {
		if errors.Is(err, errDesktopScreenshotUnsupported) {
			c.JSON(http.StatusNotImplemented, map[string]string{"error": "screenshot_unsupported"})
			return nil
		}
		return s.fail(c, err)
	}
	if len(paths) == 0 {
		c.JSON(http.StatusConflict, map[string]string{"error": "screenshot_cancelled"})
		return nil
	}

	service := attachment.NewService(s.home)
	stored := make([]store.Attachment, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) || (err == nil && info.Size() == 0) {
			continue
		}
		if err != nil {
			return s.fail(c, err)
		}
		item, err := service.StorePath(sessionID, path)
		if errors.Is(err, attachment.ErrTooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "attachment_too_large"})
			return nil
		}
		if err != nil {
			if strings.Contains(err.Error(), "not allowed") {
				c.JSON(http.StatusUnsupportedMediaType, map[string]string{"error": "attachment_type_not_allowed"})
				return nil
			}
			return s.fail(c, err)
		}
		stored = append(stored, item)
	}
	if len(stored) == 0 {
		c.JSON(http.StatusConflict, map[string]string{"error": "screenshot_cancelled"})
		return nil
	}
	c.JSON(http.StatusOK, map[string]any{"attachments": stored})
	return nil
}

func aboutSection(id, title string, rows []desktopAboutRow) desktopAboutSection {
	return desktopAboutSection{ID: id, Title: title, Rows: rows}
}

func aboutRows(kv ...string) []desktopAboutRow {
	rows := make([]desktopAboutRow, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		rows = append(rows, desktopAboutRow{Key: kv[i], Value: fallbackDash(kv[i+1])})
	}
	return rows
}

func fallbackDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "—"
	}
	return value
}

func enabledText(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func onOff(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func intText(value int) string {
	return fmt.Sprintf("%d", value)
}

func floatText(value float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", value), "0"), ".")
}

func baseNameOrDash(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "—"
	}
	return filepath.Base(path)
}

func (s *Server) desktopSaveFile(c *cart.Context) error {
	var req desktopSaveFileRequest
	if err := json.NewDecoder(io.LimitReader(c.Request.Body, desktopSaveBodyLimit)).Decode(&req); err != nil {
		return badRequest(c, "invalid json body")
	}
	filename := safeDesktopFilename(req.Filename)
	if filename == "" {
		filename = "pudding-export.bin"
	}
	if filepath.Ext(filename) == "" && strings.Contains(req.Mime, "presentation") {
		filename += ".pptx"
	}
	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		return badRequest(c, "bad base64")
	}
	dir, err := desktopDownloadsDir()
	if err != nil {
		return s.fail(c, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return s.fail(c, err)
	}
	path := uniqueDesktopDownloadPath(dir, filename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{
		"ok":       true,
		"path":     path,
		"filename": filepath.Base(path),
	})
	return nil
}

func (s *Server) desktopRevealFile(c *cart.Context) error {
	var req desktopRevealFileRequest
	if err := json.NewDecoder(io.LimitReader(c.Request.Body, 8*1024)).Decode(&req); err != nil {
		return badRequest(c, "invalid json body")
	}
	path := strings.TrimSpace(req.Path)
	if path == "" || !filepath.IsAbs(path) {
		return badRequest(c, "path must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil {
		c.JSON(http.StatusNotFound, map[string]string{"error": "file_not_found"})
		return nil
	}
	var cmd *exec.Cmd
	if info.IsDir() {
		switch runtime.GOOS {
		case "windows":
			cmd = exec.Command("explorer", path)
		default:
			cmd = exec.Command("open", path)
			if runtime.GOOS != "darwin" {
				cmd = exec.Command("xdg-open", path)
			}
		}
	} else {
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", "-R", path)
		case "windows":
			cmd = exec.Command("explorer", "/select,"+path)
		default:
			cmd = exec.Command("xdg-open", filepath.Dir(path))
		}
	}
	if err := cmd.Start(); err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"ok": true})
	return nil
}

func desktopDownloadsDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv("PUDDING_DESKTOP_DOWNLOADS_DIR")); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("home dir not found")
	}
	dir := filepath.Join(home, "Downloads")
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return dir, nil
	}
	return home, nil
}

func uniqueDesktopDownloadPath(dir, filename string) string {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	ext := filepath.Ext(filename)
	path := filepath.Join(dir, filename)
	for i := 1; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
		path = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
	}
}

func safeDesktopFilename(raw string) string {
	name := strings.TrimSpace(raw)
	name = strings.Map(func(r rune) rune {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
			return '-'
		default:
			return r
		}
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	runes := []rune(name)
	if len(runes) > 120 {
		name = string(runes[:120])
	}
	return strings.TrimSpace(name)
}

func defaultRunDesktopScreenshots(ctx context.Context, dir, filenamePrefix string) ([]string, error) {
	if runtime.GOOS == "linux" && os.Getenv("WAYLAND_DISPLAY") != "" && os.Getenv("DISPLAY") == "" {
		return nil, errDesktopScreenshotUnsupported
	}
	count := screenshot.NumActiveDisplays()
	if count <= 0 {
		return nil, nil
	}
	paths := make([]string, 0, count)
	for display := 0; display < count; display++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		img, err := screenshot.CaptureDisplay(display)
		if err != nil {
			return nil, err
		}
		name := filenamePrefix + ".png"
		if count > 1 {
			name = fmt.Sprintf("%s Display %d.png", filenamePrefix, display+1)
		}
		path := filepath.Join(dir, name)
		file, err := os.Create(path)
		if err != nil {
			return nil, err
		}
		err = png.Encode(file, img)
		closeErr := file.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		paths = append(paths, path)
	}
	return paths, nil
}
