// Package macsay implements TTS with macOS /usr/bin/say.
package macsay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/teatak/pudding-core/internal/audio/tts"
)

const (
	defaultJobQueue   = 16
	defaultEventQueue = 64
)

type Config struct {
	Voice      string
	Rate       int
	BinaryPath string

	EventQueueSize int
}

type Client struct {
	cfg Config

	events chan tts.Event

	started atomic.Bool
	stopped atomic.Bool

	speakMu sync.Mutex

	currentMu     sync.Mutex
	currentCmd    *exec.Cmd
	currentTurnID string
	cancelling    atomic.Bool

	closeOnce sync.Once
}

func New(cfg Config) (*Client, error) {
	if cfg.Voice == "" {
		cfg.Voice = "Tingting"
	}
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = defaultBinaryPath()
	}
	if cfg.EventQueueSize <= 0 {
		cfg.EventQueueSize = defaultEventQueue
	}
	return &Client{
		cfg:    cfg,
		events: make(chan tts.Event, cfg.EventQueueSize),
	}, nil
}

func (c *Client) Name() string { return "macsay" }

func (c *Client) Start(context.Context) error {
	if c.stopped.Load() {
		return errors.New("macsay: client already stopped")
	}
	if c.cfg.BinaryPath == "" {
		return errors.New("macsay: binary path is empty")
	}
	if _, err := os.Stat(c.cfg.BinaryPath); err != nil {
		return fmt.Errorf("macsay: stat %s: %w", c.cfg.BinaryPath, err)
	}
	c.started.Store(true)
	return nil
}

func (c *Client) Speak(ctx context.Context, req tts.Request) error {
	req.Text = tts.SanitizeText(req.Text)
	if req.Text == "" || !tts.HasSpeakableText(req.Text) {
		return nil
	}
	if !c.started.Load() || c.stopped.Load() {
		return errors.New("macsay: not started")
	}
	c.speakMu.Lock()
	defer c.speakMu.Unlock()
	if c.stopped.Load() {
		return errors.New("macsay: stopped")
	}
	c.cancelling.Store(false)

	args := []string{"-v", c.cfg.Voice}
	if c.cfg.Rate > 0 {
		args = append(args, "-r", strconv.Itoa(c.cfg.Rate))
	}
	args = append(args, req.Text)

	cmd := exec.CommandContext(ctx, c.cfg.BinaryPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	c.currentMu.Lock()
	c.currentCmd = cmd
	c.currentTurnID = req.TurnID
	c.currentMu.Unlock()

	err := cmd.Start()
	if err == nil {
		c.emit(tts.Event{Kind: tts.EventStarted, SessionID: req.SessionID, TurnID: req.TurnID, SegmentID: req.SegmentID})
		err = cmd.Wait()
	}

	c.currentMu.Lock()
	c.currentCmd = nil
	c.currentTurnID = ""
	c.currentMu.Unlock()

	switch {
	case c.cancelling.Load(), errors.Is(ctx.Err(), context.Canceled):
		err = nil
	case err != nil:
		err = runError(err, stderr.String())
		c.emit(tts.Event{Kind: tts.EventError, SessionID: req.SessionID, TurnID: req.TurnID, SegmentID: req.SegmentID, Err: err})
	}
	c.emit(tts.Event{Kind: tts.EventEnded, SessionID: req.SessionID, TurnID: req.TurnID, SegmentID: req.SegmentID})
	return err
}

func (c *Client) Cancel(ctx context.Context, turnID string) error {
	if !c.started.Load() || c.stopped.Load() {
		return nil
	}
	c.currentMu.Lock()
	defer c.currentMu.Unlock()
	if turnID != "" && c.currentTurnID != "" && c.currentTurnID != turnID {
		return nil
	}
	c.cancelling.Store(true)
	if c.currentCmd != nil && c.currentCmd.Process != nil {
		if err := c.currentCmd.Process.Kill(); err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return err
			}
		}
	}
	return nil
}

func (c *Client) Events() <-chan tts.Event { return c.events }

func (c *Client) Stop(context.Context) error {
	if !c.stopped.CompareAndSwap(false, true) {
		return nil
	}
	_ = c.Cancel(context.Background(), "")
	c.closeOnce.Do(func() { close(c.events) })
	return nil
}

func (c *Client) emit(ev tts.Event) {
	if c.stopped.Load() {
		return
	}
	select {
	case c.events <- ev:
	default:
	}
}

func defaultBinaryPath() string {
	if runtime.GOOS == "darwin" {
		return "/usr/bin/say"
	}
	return "say"
}

func runError(err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return fmt.Errorf("macsay: run: %w", err)
	}
	return fmt.Errorf("macsay: run: %w: %s", err, stderr)
}
