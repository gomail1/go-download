package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"go-download-server/config"
	"go-download-server/utils"
)

// IconHandler 图标处理器
func IconHandler(w http.ResponseWriter, r *http.Request) {
	// 获取文件路径参数
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "缺少文件路径参数", http.StatusBadRequest)
		return
	}

	// 验证路径安全
	safePath := utils.ValidateSafePath(config.AppConfig.Server.DownloadDir, filePath)
	if !safePath.IsSafe || safePath.Error != nil {
		http.Error(w, "路径不安全", http.StatusForbidden)
		return
	}

	// 检查文件是否存在
	if _, err := os.Stat(safePath.FullPath); os.IsNotExist(err) {
		http.Error(w, "文件不存在", http.StatusNotFound)
		return
	}

	// 检查是否是可执行文件
	ext := strings.ToLower(filepath.Ext(safePath.FullPath))
	if !utils.IsExecutableFile(ext) {
		// 返回默认图标
		returnDefaultIcon(w)
		return
	}

	// 检查图标缓存
	if utils.GlobalIconCache != nil {
		iconPath, err := utils.GlobalIconCache.GetFileIcon(safePath.FullPath)
		if err == nil && iconPath != "" {
			// 读取图标文件并返回
			iconData, err := os.ReadFile(iconPath)
			if err == nil {
				w.Header().Set("Content-Type", "image/png")
				w.Header().Set("Cache-Control", "public, max-age=86400") // 缓存1天
				w.Write(iconData)
				return
			}
		}
	}

	// 直接提取图标到内存
	iconData, err := utils.ExtractIconToBuffer(safePath.FullPath)
	if err != nil {
		// 提取失败，返回默认图标
		returnDefaultIcon(w)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(iconData)
}

// returnDefaultIcon 返回默认图标
func returnDefaultIcon(w http.ResponseWriter) {
	// 这里可以返回一个默认的文件图标
	// 暂时返回404，前端会使用默认图标
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	defaultIcon := `<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path><polyline points="14 2 14 8 20 8"></polyline></svg>`
	fmt.Fprint(w, defaultIcon)
}

// GetFileIconURL 获取文件图标URL
func GetFileIconURL(filePath string, r *http.Request) string {
	if filePath == "" {
		return ""
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	if !utils.IsExecutableFile(ext) {
		return ""
	}

	// 构建基础URL
	baseURL := fmt.Sprintf("%s://%s", utils.GetRequestScheme(r), r.Host)

	return fmt.Sprintf("%s/icon?path=%s", baseURL, filePath)
}
