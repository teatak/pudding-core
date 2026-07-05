package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const desktopLocaleSetEvent = "desktop:locale-set"

type desktopLocalePreference string

const (
	desktopLocaleZHCN desktopLocalePreference = "zh-CN"
	desktopLocaleZHTW desktopLocalePreference = "zh-TW"
	desktopLocaleEN   desktopLocalePreference = "en"
)

type desktopLocaleManager struct {
	app    *application.App
	tray   *application.SystemTray
	window *application.WebviewWindow
	path   string
	locale desktopLocalePreference
}

func loadDesktopLocalePreference(path string) (desktopLocalePreference, error) {
	prefs, err := loadDesktopPreferences(path)
	return prefs.Locale, err
}

func saveDesktopLocalePreference(path string, locale desktopLocalePreference) error {
	desktopPreferencesMu.Lock()
	defer desktopPreferencesMu.Unlock()

	prefs, err := loadDesktopPreferences(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	prefs.Locale = normalizeDesktopLocalePreference(locale)
	return saveDesktopPreferences(path, prefs)
}

func newDesktopLocaleManager(app *application.App, tray *application.SystemTray, window *application.WebviewWindow, path string, locale desktopLocalePreference) *desktopLocaleManager {
	return &desktopLocaleManager{
		app:    app,
		tray:   tray,
		window: window,
		path:   path,
		locale: normalizeDesktopLocalePreference(locale),
	}
}

func (m *desktopLocaleManager) bind() {
	if m == nil || m.app == nil {
		return
	}
	m.app.Event.On(desktopLocaleSetEvent, func(event *application.CustomEvent) {
		locale, ok := decodeDesktopLocalePreference(event.Data)
		if !ok {
			slog.Warn("pudding-desktop: ignore invalid locale request", "payload", event.Data)
			return
		}
		slog.Info("pudding-desktop: locale request", "locale", locale)
		if err := m.setLocale(locale); err != nil {
			slog.Warn("pudding-desktop: set locale preference", "locale", locale, "err", err)
		}
	})
}

func (m *desktopLocaleManager) setLocale(locale desktopLocalePreference) error {
	if m == nil {
		return nil
	}
	m.locale = normalizeDesktopLocalePreference(locale)
	if err := saveDesktopLocalePreference(m.path, m.locale); err != nil {
		return err
	}
	slog.Info("pudding-desktop: locale preference saved", "locale", m.locale, "path", m.path)
	m.apply()
	return nil
}

func (m *desktopLocaleManager) apply() {
	if m == nil || m.tray == nil {
		return
	}
	m.tray.SetTooltip("Pudding")
	m.tray.SetMenu(buildDesktopTrayMenu(m.app, m.window, m.locale))
}

func buildDesktopTrayMenu(app *application.App, window *application.WebviewWindow, locale desktopLocalePreference) *application.Menu {
	menu := application.NewMenu()
	menu.Add(desktopLocaleText(locale, "tray.show")).OnClick(func(*application.Context) {
		if window == nil {
			return
		}
		window.Show()
		window.Focus()
	})
	menu.AddSeparator()
	menu.Add(desktopLocaleText(locale, "tray.quit")).OnClick(func(*application.Context) {
		if app != nil {
			app.Quit()
		}
	})
	return menu
}

func decodeDesktopLocalePreference(data any) (desktopLocalePreference, bool) {
	if locale, ok := data.(string); ok {
		return parseDesktopLocalePreference(locale)
	}
	b, err := json.Marshal(data)
	if err != nil {
		return desktopLocaleEN, false
	}
	var locale string
	if err := json.Unmarshal(b, &locale); err != nil {
		return desktopLocaleEN, false
	}
	return parseDesktopLocalePreference(locale)
}

func parseDesktopLocalePreference(value string) (desktopLocalePreference, bool) {
	locale := desktopLocalePreference(strings.TrimSpace(value))
	switch locale {
	case desktopLocaleZHCN, desktopLocaleZHTW, desktopLocaleEN:
		return locale, true
	default:
		return desktopLocaleEN, false
	}
}

func normalizeDesktopLocalePreference(locale desktopLocalePreference) desktopLocalePreference {
	switch locale {
	case desktopLocaleZHCN, desktopLocaleZHTW, desktopLocaleEN:
		return locale
	default:
		return detectDesktopLocalePreference()
	}
}

func detectDesktopLocalePreference() desktopLocalePreference {
	language := strings.ToLower(os.Getenv("LC_ALL"))
	if language == "" {
		language = strings.ToLower(os.Getenv("LC_MESSAGES"))
	}
	if language == "" {
		language = strings.ToLower(os.Getenv("LANG"))
	}
	if strings.Contains(language, "zh_tw") || strings.Contains(language, "zh-hant") || strings.Contains(language, "zh_hk") {
		return desktopLocaleZHTW
	}
	if strings.HasPrefix(language, "zh") {
		return desktopLocaleZHCN
	}
	return desktopLocaleEN
}

func desktopLocaleText(locale desktopLocalePreference, key string) string {
	switch normalizeDesktopLocalePreference(locale) {
	case desktopLocaleZHCN:
		switch key {
		case "tray.show":
			return "显示 Pudding"
		case "tray.quit":
			return "退出"
		}
	case desktopLocaleZHTW:
		switch key {
		case "tray.show":
			return "顯示 Pudding"
		case "tray.quit":
			return "結束"
		}
	default:
		switch key {
		case "tray.show":
			return "Show Pudding"
		case "tray.quit":
			return "Quit"
		}
	}
	return key
}
