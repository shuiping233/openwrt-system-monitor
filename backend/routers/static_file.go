package routers

import (
	"embed"
	"net/http"
	"path"
)

var (
	staticFS      embed.FS
	dirPrefixPath string
)

func InitFs(fs embed.FS, dirPrefix string) {
	staticFS = fs
	dirPrefixPath = dirPrefix
}

func FileServer(w http.ResponseWriter, r *http.Request) {
	filePath := path.Clean(r.URL.Path)
	if filePath == "/" {
		filePath = "/index.html"
	}

	data, err := staticFS.ReadFile(path.Join(dirPrefixPath, filePath+".gz"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Vary", "Accept-Encoding")

	// 根据原文件后缀设置 Content-Type（去掉 .gz）
	switch path.Ext(filePath) {
	case ".html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case ".js":
		w.Header().Set("Content-Type", "application/javascript")
	case ".css":
		w.Header().Set("Content-Type", "text/css")
	case ".json":
		w.Header().Set("Content-Type", "application/json")
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	w.Write(data)
}
