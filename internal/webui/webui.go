// Package webui 把 web 构建产物内嵌进 daemon 二进制并以 SPA 语义 serve。
// dist 内容由 `make embed` 生成(git 只跟踪占位 .gitkeep,不跟踪构建产物);
// 未执行过 make embed 时返回引导页,不影响 API 与测试。
package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

func FS() fs.FS {
	dist, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	return dist
}

const placeholder = `<!doctype html><meta charset="utf-8"><title>Pudding</title>
<body style="font-family:system-ui;display:grid;place-items:center;height:100vh">
<p>Web UI 未打包:在仓库根目录执行 <code>make embed</code> 后重新构建 daemon。</p>`

// Handler 返回静态资源 handler:命中文件直接 serve,
// 其余路径回落 index.html(SPA 只用查询参数,不用路径路由)。
func Handler() http.Handler {
	dist := FS()
	fileServer := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path != "/" {
			if _, err := fs.Stat(dist, path[1:]); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(placeholder))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}
