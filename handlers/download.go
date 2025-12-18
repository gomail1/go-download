package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go-download-server/config"
	"go-download-server/session"
	"go-download-server/utils"
)

// 文件下载处理函数
func DownloadHandler(w http.ResponseWriter, r *http.Request) {
	// 获取当前用户
	sess := session.GetCurrentUser(r)
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	// 获取文件路径
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "缺少文件路径", http.StatusBadRequest)
		return
	}

	// URL解码路径
	path, err := url.QueryUnescape(path)
	if err != nil {
		http.Error(w, "路径解码失败", http.StatusBadRequest)
		return
	}

	// 安全检查：防止路径遍历
	path = filepath.Clean(path)
	if strings.HasPrefix(path, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// 构建完整路径
	fullPath := filepath.Join(config.AppConfig.Server.DownloadDir, path)

	// 检查文件是否存在
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		http.Error(w, "文件不存在", http.StatusNotFound)
		return
	}

	// 检查是否为目录
	if fileInfo.IsDir() {
		http.Error(w, "不能下载目录", http.StatusBadRequest)
		return
	}

	// 记录日志
	utils.LogUserAction(r, "download_file", fmt.Sprintf("下载文件: %s", path))

	// 设置响应头
	w.Header().Set("Content-Disposition", "attachment; filename="+filepath.Base(fullPath))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))

	// 发送文件
	http.ServeFile(w, r, fullPath)
}
