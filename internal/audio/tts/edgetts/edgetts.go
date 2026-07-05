// Package edgetts implements TTS with Microsoft Edge read-aloud service.
//
// Edge read-aloud is not an official public API. It works without a user API
// key, but depends on the Edge WebSocket protocol and token constants staying
// compatible with the service.
package edgetts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	mp3 "github.com/hajimehoshi/go-mp3"

	"github.com/teatak/pudding-core/internal/audio/frame"
	"github.com/teatak/pudding-core/internal/audio/tts"
)

const (
	wsBase              = "wss://speech.platform.bing.com/consumer/speech/synthesize/readaloud/edge/v1"
	trustedClientToken  = "6A5AA1D4EAFF4E9FB37E23D68491D6F4"
	chromiumFullVersion = "143.0.3650.75"
	chromiumMajor       = "143"
	secMSGECVersion     = "1-" + chromiumFullVersion

	filetimeEpochSec    = 11644473600
	secMSGECBucketTicks = 300 * 10_000_000

	outputFormat = "audio-24khz-48kbitrate-mono-mp3"
	sampleRate   = 24000
	chunkSamples = 480

	defaultEventQueue   = 64
	defaultDialTimeout  = 15 * time.Second
	defaultReadTimeout  = 20 * time.Second
	defaultWriteTimeout = 10 * time.Second

	defaultVoice = "zh-CN-YunxiaNeural"

	chromeUA  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/" + chromiumMajor + ".0.0.0 Safari/537.36 Edg/" + chromiumMajor + ".0.0.0"
	originHdr = "chrome-extension://jdiccldimpdaibmpdkjnbmckianbfold"
)

type Config struct {
	Voice string
	Rate  string
	Speed float32

	Volume string
	Pitch  string

	DialTimeout    time.Duration
	EventQueueSize int
}

type Client struct {
	cfg Config

	events chan tts.Event
	stopCh chan struct{}

	started atomic.Bool
	stopped atomic.Bool

	speakMu sync.Mutex

	cancelGen atomic.Uint64

	currentMu     sync.Mutex
	currentCancel context.CancelFunc
	currentTurnID string
	currentWS     *websocket.Conn

	closeOnce sync.Once
}

func New(cfg Config) (*Client, error) {
	if cfg.Voice == "" {
		cfg.Voice = defaultVoice
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = defaultDialTimeout
	}
	if cfg.EventQueueSize <= 0 {
		cfg.EventQueueSize = defaultEventQueue
	}
	return &Client{
		cfg:    cfg,
		events: make(chan tts.Event, cfg.EventQueueSize),
		stopCh: make(chan struct{}),
	}, nil
}

func (c *Client) Name() string { return "edgetts" }

func (c *Client) Start(context.Context) error {
	if c.stopped.Load() {
		return errors.New("edgetts: client already stopped")
	}
	c.started.Store(true)
	return nil
}

func (c *Client) Speak(ctx context.Context, req tts.Request) error {
	req.Text = tts.SanitizeText(req.Text)
	if req.Text == "" || !tts.HasSpeakableText(req.Text) {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !c.started.Load() || c.stopped.Load() {
		return errors.New("edgetts: not started")
	}

	c.speakMu.Lock()
	defer c.speakMu.Unlock()
	if c.stopped.Load() {
		return errors.New("edgetts: stopped")
	}

	synthCtx, cancel := context.WithCancel(ctx)
	c.setCurrentRequest(req.TurnID, cancel)
	defer c.clearCurrentRequest(req.TurnID)

	startGen := c.cancelGen.Load()
	c.emit(context.Background(), tts.Event{Kind: tts.EventStarted, SessionID: req.SessionID, TurnID: req.TurnID, SegmentID: req.SegmentID})

	err := c.doSynthesize(synthCtx, req, startGen)
	cancelled := c.cancelGen.Load() != startGen || errors.Is(synthCtx.Err(), context.Canceled)
	if cancelled {
		err = nil
	} else if err != nil {
		c.emit(context.Background(), tts.Event{Kind: tts.EventError, SessionID: req.SessionID, TurnID: req.TurnID, SegmentID: req.SegmentID, Err: err})
	}
	c.emit(context.Background(), tts.Event{Kind: tts.EventEnded, SessionID: req.SessionID, TurnID: req.TurnID, SegmentID: req.SegmentID})
	return err
}

func (c *Client) Cancel(ctx context.Context, turnID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !c.started.Load() || c.stopped.Load() {
		return nil
	}
	cancel, conn, ok := c.currentForCancel(turnID)
	if !ok {
		return nil
	}
	c.cancelGen.Add(1)
	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.Close()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (c *Client) Events() <-chan tts.Event { return c.events }

func (c *Client) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !c.stopped.CompareAndSwap(false, true) {
		return nil
	}
	c.cancelGen.Add(1)
	c.closeOnce.Do(func() { close(c.stopCh) })
	cancel, conn, _ := c.currentForCancel("")
	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.Close()
	}

	done := make(chan struct{})
	go func() {
		c.speakMu.Lock()
		c.speakMu.Unlock()
		close(done)
	}()
	select {
	case <-done:
		close(c.events)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) doSynthesize(ctx context.Context, req tts.Request, startGen uint64) error {
	if c.cancelGen.Load() != startGen {
		return ctx.Err()
	}
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = c.cfg.DialTimeout

	hdr := http.Header{}
	hdr.Set("User-Agent", chromeUA)
	hdr.Set("Origin", originHdr)
	hdr.Set("Pragma", "no-cache")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	hdr.Set("Accept-Language", "en-US,en;q=0.9")

	conn, resp, err := dialer.DialContext(ctx, buildWSURL(time.Now(), newRequestID()), hdr)
	if err != nil {
		if c.cancelGen.Load() != startGen || errors.Is(ctx.Err(), context.Canceled) {
			return ctx.Err()
		}
		if resp != nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			_ = resp.Body.Close()
			return fmt.Errorf("edgetts: ws dial: %w (http %s body=%q)", err, resp.Status, string(body))
		}
		return fmt.Errorf("edgetts: ws dial: %w", err)
	}
	c.setCurrentWS(conn)
	defer func() {
		c.clearCurrentWS(conn)
		_ = conn.Close()
	}()
	if c.cancelGen.Load() != startGen {
		return ctx.Err()
	}

	if err := writeText(conn, buildSpeechConfig()); err != nil {
		return fmt.Errorf("edgetts: write config: %w", err)
	}
	if err := writeText(conn, buildSSMLRequest(newRequestID(), c.ssmlFor(req.Text))); err != nil {
		return fmt.Errorf("edgetts: write ssml: %w", err)
	}

	pr, pw := io.Pipe()
	decErrCh := make(chan error, 1)
	go c.decodeLoop(ctx, pr, req, startGen, decErrCh)

	var (
		readErr    error
		audioBytes int
	)
	for {
		if c.cancelGen.Load() != startGen {
			break
		}
		_ = conn.SetReadDeadline(time.Now().Add(defaultReadTimeout))
		mt, payload, err := conn.ReadMessage()
		if err != nil {
			if c.cancelGen.Load() != startGen || errors.Is(ctx.Err(), context.Canceled) {
				break
			}
			readErr = fmt.Errorf("edgetts: read: %w", err)
			break
		}
		switch mt {
		case websocket.BinaryMessage:
			audio, perr := parseBinaryFrame(payload)
			if perr != nil {
				readErr = fmt.Errorf("edgetts: parse binary: %w", perr)
				break
			}
			if len(audio) > 0 {
				if _, werr := pw.Write(audio); werr != nil {
					readErr = fmt.Errorf("edgetts: pipe write: %w", werr)
					break
				}
				audioBytes += len(audio)
			}
		case websocket.TextMessage:
			if parseTextFramePath(payload) == "turn.end" {
				_ = pw.Close()
				decErr := <-decErrCh
				if audioBytes == 0 {
					return nil
				}
				if decErr != nil {
					return decErr
				}
				return nil
			}
		}
		if readErr != nil {
			break
		}
	}
	_ = pw.Close()
	<-decErrCh
	return readErr
}

func (c *Client) decodeLoop(ctx context.Context, pr *io.PipeReader, req tts.Request, startGen uint64, done chan<- error) {
	defer pr.Close()

	dec, err := mp3.NewDecoder(pr)
	if err != nil {
		done <- fmt.Errorf("edgetts: mp3 init: %w", err)
		return
	}
	sr := dec.SampleRate()
	if sr <= 0 {
		sr = sampleRate
	}

	var pending []int16
	flush := func(final bool) bool {
		for {
			if final {
				if len(pending) == 0 {
					return true
				}
			} else if len(pending) < chunkSamples {
				return true
			}
			n := chunkSamples
			if n > len(pending) {
				n = len(pending)
			}
			buf := make([]byte, n*2)
			for i := 0; i < n; i++ {
				binary.LittleEndian.PutUint16(buf[i*2:], uint16(pending[i]))
			}
			if !c.emit(ctx, tts.Event{
				Kind:      tts.EventAudio,
				SessionID: req.SessionID,
				TurnID:    req.TurnID,
				SegmentID: req.SegmentID,
				Audio: frame.PCM16{
					Format:    frame.Format{SampleRate: sr, Channels: 1},
					Data:      buf,
					Timestamp: time.Now(),
				},
			}) {
				return false
			}
			pending = pending[n:]
		}
	}

	buf := make([]byte, 8192)
	for {
		if c.cancelGen.Load() != startGen {
			done <- nil
			return
		}
		n, err := dec.Read(buf)
		if n > 0 {
			for i := 0; i+3 < n; i += 4 {
				pending = append(pending, int16(binary.LittleEndian.Uint16(buf[i:i+2])))
			}
			if !flush(false) {
				done <- nil
				return
			}
		}
		if err == io.EOF {
			flush(true)
			done <- nil
			return
		}
		if err != nil {
			flush(true)
			done <- fmt.Errorf("edgetts: mp3 decode: %w", err)
			return
		}
	}
}

func (c *Client) ssmlFor(text string) string {
	rate := c.cfg.Rate
	if rate == "" {
		rate = speedToRate(c.cfg.Speed)
	}
	volume := c.cfg.Volume
	if volume == "" {
		volume = "+0%"
	}
	pitch := c.cfg.Pitch
	if pitch == "" {
		pitch = "+0Hz"
	}
	return fmt.Sprintf(
		`<speak version='1.0' xmlns='http://www.w3.org/2001/10/synthesis' xml:lang='en-US'>`+
			`<voice name='%s'><prosody pitch='%s' rate='%s' volume='%s'>%s</prosody></voice></speak>`,
		c.cfg.Voice, pitch, rate, volume, xmlEscape(text),
	)
}

func speedToRate(speed float32) string {
	if speed <= 0 || speed == 1.0 {
		return "+0%"
	}
	pct := int((speed - 1.0) * 100)
	if pct >= 0 {
		return fmt.Sprintf("+%d%%", pct)
	}
	return fmt.Sprintf("%d%%", pct)
}

func xmlEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"'", "&apos;",
		"\"", "&quot;",
	).Replace(s)
}

func (c *Client) setCurrentRequest(turnID string, cancel context.CancelFunc) {
	c.currentMu.Lock()
	c.currentTurnID = turnID
	c.currentCancel = cancel
	c.currentMu.Unlock()
}

func (c *Client) clearCurrentRequest(turnID string) {
	c.currentMu.Lock()
	if c.currentTurnID == turnID {
		c.currentTurnID = ""
		c.currentCancel = nil
		c.currentWS = nil
	}
	c.currentMu.Unlock()
}

func (c *Client) setCurrentWS(conn *websocket.Conn) {
	c.currentMu.Lock()
	c.currentWS = conn
	c.currentMu.Unlock()
}

func (c *Client) clearCurrentWS(conn *websocket.Conn) {
	c.currentMu.Lock()
	if c.currentWS == conn {
		c.currentWS = nil
	}
	c.currentMu.Unlock()
}

func (c *Client) currentForCancel(turnID string) (context.CancelFunc, *websocket.Conn, bool) {
	c.currentMu.Lock()
	defer c.currentMu.Unlock()
	if c.currentTurnID == "" {
		return nil, nil, false
	}
	if turnID != "" && c.currentTurnID != turnID {
		return nil, nil, false
	}
	return c.currentCancel, c.currentWS, true
}

func (c *Client) emit(ctx context.Context, ev tts.Event) bool {
	if c.stopped.Load() {
		return false
	}
	select {
	case c.events <- ev:
		return true
	case <-ctx.Done():
		return false
	case <-c.stopCh:
		return false
	}
}

func writeText(conn *websocket.Conn, payload string) error {
	_ = conn.SetWriteDeadline(time.Now().Add(defaultWriteTimeout))
	return conn.WriteMessage(websocket.TextMessage, []byte(payload))
}

func buildSpeechConfig() string {
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	body := `{"context":{"synthesis":{"audio":{"metadataoptions":{"sentenceBoundaryEnabled":"false","wordBoundaryEnabled":"false"},"outputFormat":"` + outputFormat + `"}}}}`
	return "X-Timestamp:" + ts + "\r\n" +
		"Content-Type:application/json; charset=utf-8\r\n" +
		"Path:speech.config\r\n\r\n" +
		body
}

func buildSSMLRequest(requestID, ssml string) string {
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	return "X-RequestId:" + requestID + "\r\n" +
		"Content-Type:application/ssml+xml\r\n" +
		"X-Timestamp:" + ts + "\r\n" +
		"Path:ssml\r\n\r\n" +
		ssml
}

func parseBinaryFrame(b []byte) ([]byte, error) {
	if len(b) < 2 {
		return nil, errors.New("frame too short")
	}
	hdrLen := int(binary.BigEndian.Uint16(b[:2]))
	if 2+hdrLen > len(b) {
		return nil, errors.New("header length exceeds frame")
	}
	return b[2+hdrLen:], nil
}

func parseTextFramePath(b []byte) string {
	head := string(b)
	if end := strings.Index(head, "\r\n\r\n"); end > 0 {
		head = head[:end]
	}
	for _, line := range strings.Split(head, "\r\n") {
		if strings.HasPrefix(line, "Path:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Path:"))
		}
	}
	return ""
}

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func buildWSURL(now time.Time, connectionID string) string {
	params := url.Values{}
	params.Set("TrustedClientToken", trustedClientToken)
	params.Set("Sec-MS-GEC", secMSGEC(now))
	params.Set("Sec-MS-GEC-Version", secMSGECVersion)
	params.Set("ConnectionId", connectionID)
	return wsBase + "?" + params.Encode()
}

func secMSGEC(now time.Time) string {
	ticks := (now.UTC().Unix() + filetimeEpochSec) * 10_000_000
	ticks -= ticks % secMSGECBucketTicks
	raw := fmt.Sprintf("%d%s", ticks, trustedClientToken)
	sum := sha256.Sum256([]byte(raw))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}
