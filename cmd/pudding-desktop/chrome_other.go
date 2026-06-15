//go:build !darwin

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// 非 darwin 平台没有红绿灯 / NSToolbar / zoom 语义,全部 no-op。
// Windows / Linux 的窗口 chrome 后续按 data-shell="win" 等机制另行扩展。

func installZoomSwizzle() {}

func attachDoubleClickToZoom(*application.WebviewWindow) {}
