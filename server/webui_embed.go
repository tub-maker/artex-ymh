//go:build embedui

package server

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// webuiDist holds the statically-exported frontend (built with
// `cd web && npm run build:static`, then copied into server/webui/dist). Compiled
// into the binary only when building with `-tags embedui`. The `all:` prefix is
// required so Next's `_next/` asset dir (leading underscore) is included.
//
//go:embed all:webui/dist
var webuiDist embed.FS

// webuiHandler serves the embedded SPA. Public (no JWT) — auth is enforced
// client-side and on /api. Serves the exported per-route index.html files and
// falls back to index.html so client-side routing still resolves unknown paths.
func (s *Server) webuiHandler() http.Handler {
	root, err := fs.Sub(webuiDist, "webui/dist")
	if err != nil {
		panic(err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

		// 移动端 SPA（artex-mobile 仓库）：从挂载目录提供，不在二进制里。
		// 这样移动端改一行代码只要 `npm run build`，刷新手机就生效，不用重建镜像。
		if p == "m" || strings.HasPrefix(p, "m/") {
			if serveMobile(w, r, p) {
				return
			}
			http.NotFound(w, r)
			return
		}

		// 客户端路由要的 RSC 载荷：trailingSlash 导出下，路由 /a/b 的载荷
		// 实际文件是 a/b/index.txt，但 Next 客户端请求的是 /a/b.txt。
		// 不把两者对上，请求会落到下面的 index.html 兜底，客户端拿到 HTML
		// 解析 RSC 失败 → 每次点导航都报 "This page couldn't load"。
		if strings.HasSuffix(p, ".txt") {
			base := strings.TrimSuffix(p, ".txt")
			for _, cand := range []string{base + "/index.txt", base + ".txt"} {
				if serveFileIfExists(w, r, root, cand) {
					return
				}
			}
			// 载荷确实不存在时明确 404，不要再回落成 HTML。
			http.NotFound(w, r)
			return
		}

		if p == "" {
			p = "index.html"
		}
		// 1) exact file (assets: _next/*, favicon.ico, ...)
		// 2) route dir → <p>/index.html (trailingSlash export)
		// 3) <p>.html
		// 4) SPA 兜底 → index.html（交给客户端路由）
		for _, cand := range []string{p, p + "/index.html", p + ".html"} {
			if serveFileIfExists(w, r, root, cand) {
				return
			}
		}
		http.ServeFileFS(w, r, root, "index.html")
	})
}

// serveMobile 从挂载目录提供移动端 SPA（artex-mobile 仓库的 dist）。
//
// 为什么不用 embed：移动端迭代频繁，打进二进制意味着每改一次样式都要重建镜像。
// 挂载目录后只需 `npm run build`，刷新手机即可生效。
func serveMobile(w http.ResponseWriter, r *http.Request, p string) bool {
	dir := strings.TrimSpace(os.Getenv("ARTEX_MOBILE_DIR"))
	if dir == "" {
		dir = "/app/mobile"
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return false
	}

	// p 形如 "m" / "m/" / "m/assets/index.js"
	rel := strings.TrimPrefix(strings.TrimPrefix(p, "m"), "/")
	if rel == "" {
		rel = "index.html"
	}
	// 防目录穿越
	if strings.Contains(rel, "..") {
		return false
	}

	full := filepath.Join(dir, filepath.FromSlash(rel))
	if st, err := os.Stat(full); err == nil && !st.IsDir() {
		setMobileCacheHeaders(w, rel)
		http.ServeFile(w, r, full)
		return true
	}

	// SPA 兜底：未知路径一律回 index.html，交给前端路由
	idx := filepath.Join(dir, "index.html")
	if _, err := os.Stat(idx); err != nil {
		return false
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, idx)
	return true
}

// setMobileCacheHeaders 为移动端静态文件设缓存策略。
//
// 关键：sw.js 必须 no-cache —— 否则 CDN 或浏览器会缓存旧版本，
// Service Worker 永远不更新，离线/推送逻辑就卡在旧代码上。
// 带 hash 的 /m/assets/* 可以长缓存（内容变了文件名就变）。
func setMobileCacheHeaders(w http.ResponseWriter, rel string) {
	switch {
	case rel == "sw.js" || rel == "index.html" || strings.HasSuffix(rel, ".webmanifest"):
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	case strings.HasPrefix(rel, "assets/"):
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	default:
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
}

// serveFileIfExists serves name from fsys when it exists as a regular file.
func serveFileIfExists(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) bool {
	f, err := fsys.Open(name)
	if err != nil {
		return false
	}
	st, statErr := f.Stat()
	_ = f.Close()
	if statErr != nil || st.IsDir() {
		return false
	}
	http.ServeFileFS(w, r, fsys, name)
	return true
}
