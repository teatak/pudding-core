package main

import (
	"encoding/json"
	"math"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const desktopNoZoomRectsEvent = "desktop:no-zoom-rects"

type noZoomRect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

func bindDesktopNoZoomRects(app *application.App) {
	if app == nil {
		return
	}
	app.Event.On(desktopNoZoomRectsEvent, func(event *application.CustomEvent) {
		setNoZoomRects(decodeNoZoomRects(event.Data))
	})
}

func decodeNoZoomRects(data any) []noZoomRect {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil
	}

	var rects []noZoomRect
	if err := json.Unmarshal(raw, &rects); err != nil {
		return nil
	}

	out := rects[:0]
	for _, rect := range rects {
		if !validNoZoomNumber(rect.X) || !validNoZoomNumber(rect.Y) || !validNoZoomNumber(rect.W) || !validNoZoomNumber(rect.H) {
			continue
		}
		if rect.W <= 0 || rect.H <= 0 {
			continue
		}
		out = append(out, rect)
	}
	return out
}

func validNoZoomNumber(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
