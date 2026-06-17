package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const desktopThemeSetEvent = "desktop:theme-set"

type desktopThemePreference string

const (
	desktopThemeSystem desktopThemePreference = "system"
	desktopThemeLight  desktopThemePreference = "light"
	desktopThemeDark   desktopThemePreference = "dark"
)

type desktopThemeResolved string

const (
	desktopThemeResolvedLight desktopThemeResolved = "light"
	desktopThemeResolvedDark  desktopThemeResolved = "dark"
)

type desktopPreferences struct {
	Theme desktopThemePreference `json:"theme"`
}

type desktopThemeState struct {
	Theme    desktopThemePreference `json:"theme"`
	Resolved desktopThemeResolved   `json:"resolved"`
}

type desktopThemeManager struct {
	app    *application.App
	window *application.WebviewWindow
	path   string
	theme  desktopThemePreference
}

func desktopPreferencesPath(homeDir string) string {
	return filepath.Join(homeDir, "config", "desktop.json")
}

func loadDesktopThemePreference(path string) (desktopThemePreference, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return desktopThemeSystem, nil
		}
		return desktopThemeSystem, err
	}
	var prefs desktopPreferences
	if err := json.Unmarshal(b, &prefs); err != nil {
		return desktopThemeSystem, err
	}
	return normalizeDesktopThemePreference(prefs.Theme), nil
}

func saveDesktopThemePreference(path string, theme desktopThemePreference) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(desktopPreferences{Theme: normalizeDesktopThemePreference(theme)}, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o600)
}

func normalizeDesktopThemePreference(theme desktopThemePreference) desktopThemePreference {
	switch theme {
	case desktopThemeLight, desktopThemeDark, desktopThemeSystem:
		return theme
	default:
		return desktopThemeSystem
	}
}

func newDesktopThemeManager(app *application.App, window *application.WebviewWindow, path string, theme desktopThemePreference) *desktopThemeManager {
	return &desktopThemeManager{
		app:    app,
		window: window,
		path:   path,
		theme:  normalizeDesktopThemePreference(theme),
	}
}

func (m *desktopThemeManager) bind() {
	if m == nil || m.app == nil {
		return
	}
	m.app.Event.On(desktopThemeSetEvent, func(event *application.CustomEvent) {
		theme, ok := decodeDesktopThemePreference(event.Data)
		if !ok {
			return
		}
		if err := m.setTheme(theme); err != nil {
			slog.Warn("pudding-desktop: set theme preference", "theme", theme, "err", err)
		}
	})
	m.app.Event.OnApplicationEvent(events.Common.ThemeChanged, func(*application.ApplicationEvent) {
		if m.theme == desktopThemeSystem {
			m.apply(true)
		}
	})
	if runtime.GOOS == "darwin" && m.window != nil {
		m.window.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, func(*application.WindowEvent) {
			m.notifyPage(m.state())
		})
	}
}

func decodeDesktopThemePreference(data any) (desktopThemePreference, bool) {
	if theme, ok := data.(string); ok {
		return parseDesktopThemePreference(theme)
	}
	b, err := json.Marshal(data)
	if err != nil {
		return desktopThemeSystem, false
	}
	var theme string
	if err := json.Unmarshal(b, &theme); err != nil {
		return desktopThemeSystem, false
	}
	return parseDesktopThemePreference(theme)
}

func parseDesktopThemePreference(value string) (desktopThemePreference, bool) {
	switch desktopThemePreference(value) {
	case desktopThemeLight, desktopThemeDark, desktopThemeSystem:
		return desktopThemePreference(value), true
	default:
		return desktopThemeSystem, false
	}
}

func (m *desktopThemeManager) setTheme(theme desktopThemePreference) error {
	if m == nil {
		return nil
	}
	theme = normalizeDesktopThemePreference(theme)
	m.theme = theme
	if err := saveDesktopThemePreference(m.path, theme); err != nil {
		return err
	}
	m.apply(true)
	return nil
}

func (m *desktopThemeManager) apply(notifyPage bool) {
	if m == nil || m.window == nil {
		return
	}
	state := m.state()
	if state.Resolved == desktopThemeResolvedDark {
		m.window.SetBackgroundColour(application.NewRGB(28, 28, 28))
	} else {
		m.window.SetBackgroundColour(application.NewRGB(255, 255, 255))
	}
	if runtime.GOOS == "darwin" {
		setWindowAppearance(m.window, string(state.Theme))
	}
	if notifyPage {
		m.notifyPage(state)
	}
}

func (m *desktopThemeManager) notifyPage(state desktopThemeState) {
	if m == nil || m.window == nil {
		return
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return
	}
	m.window.ExecJS(fmt.Sprintf("window.__puddingSetThemeState && window.__puddingSetThemeState(%s)", payload))
}

func (m *desktopThemeManager) state() desktopThemeState {
	theme := desktopThemeSystem
	systemIsDark := false
	if m != nil {
		theme = normalizeDesktopThemePreference(m.theme)
		systemIsDark = m.systemIsDark()
	}
	return desktopThemeState{
		Theme:    theme,
		Resolved: resolveDesktopTheme(theme, systemIsDark),
	}
}

func (m *desktopThemeManager) systemIsDark() bool {
	if m == nil || m.app == nil {
		return false
	}
	return m.app.Env.IsDarkMode()
}

func resolveDesktopTheme(theme desktopThemePreference, systemIsDark bool) desktopThemeResolved {
	switch normalizeDesktopThemePreference(theme) {
	case desktopThemeDark:
		return desktopThemeResolvedDark
	case desktopThemeLight:
		return desktopThemeResolvedLight
	default:
		if systemIsDark {
			return desktopThemeResolvedDark
		}
		return desktopThemeResolvedLight
	}
}

func macAppearanceForTheme(theme desktopThemePreference) application.MacAppearanceType {
	switch normalizeDesktopThemePreference(theme) {
	case desktopThemeDark:
		return application.NSAppearanceNameDarkAqua
	case desktopThemeLight:
		return application.NSAppearanceNameAqua
	default:
		return application.DefaultAppearance
	}
}
