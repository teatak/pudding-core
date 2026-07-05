package main

import (
	"errors"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const (
	defaultDesktopWindowWidth  = 1200
	defaultDesktopWindowHeight = 800
	minDesktopWindowWidth      = 520
	minDesktopWindowHeight     = 600
)

type desktopWindowPreference struct {
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
}

type desktopWindowPreferenceManager struct {
	window *application.WebviewWindow
	path   string

	mu    sync.Mutex
	timer *time.Timer
}

func defaultDesktopWindowPreference() desktopWindowPreference {
	return desktopWindowPreference{
		Width:  defaultDesktopWindowWidth,
		Height: defaultDesktopWindowHeight,
	}
}

func normalizeDesktopWindowPreference(pref desktopWindowPreference) desktopWindowPreference {
	if pref.Width < minDesktopWindowWidth {
		pref.Width = defaultDesktopWindowWidth
	}
	if pref.Height < minDesktopWindowHeight {
		pref.Height = defaultDesktopWindowHeight
	}
	return pref
}

func loadDesktopWindowPreference(path string) (desktopWindowPreference, error) {
	prefs, err := loadDesktopPreferences(path)
	return prefs.Window, err
}

func saveDesktopWindowPreference(path string, pref desktopWindowPreference) error {
	desktopPreferencesMu.Lock()
	defer desktopPreferencesMu.Unlock()

	prefs, err := loadDesktopPreferences(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	prefs.Window = normalizeDesktopWindowPreference(pref)
	return saveDesktopPreferences(path, prefs)
}

func newDesktopWindowPreferenceManager(window *application.WebviewWindow, path string) *desktopWindowPreferenceManager {
	return &desktopWindowPreferenceManager{
		window: window,
		path:   path,
	}
}

func (m *desktopWindowPreferenceManager) bind() {
	if m == nil || m.window == nil {
		return
	}
	m.window.RegisterHook(events.Common.WindowDidResize, func(*application.WindowEvent) {
		m.scheduleSave()
	})
}

func (m *desktopWindowPreferenceManager) scheduleSave() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.timer != nil {
		m.timer.Stop()
	}
	m.timer = time.AfterFunc(600*time.Millisecond, m.saveCurrentSize)
}

func (m *desktopWindowPreferenceManager) saveCurrentSize() {
	if m == nil || m.window == nil || m.window.IsFullscreen() || m.window.IsMaximised() {
		return
	}
	width, height := m.window.Size()
	pref := normalizeDesktopWindowPreference(desktopWindowPreference{
		Width:  width,
		Height: height,
	})
	if pref.Width != width || pref.Height != height {
		return
	}
	if err := saveDesktopWindowPreference(m.path, pref); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("pudding-desktop: save window preference", "path", m.path, "err", err)
	}
}
