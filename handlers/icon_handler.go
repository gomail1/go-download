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

// IconHandler 图标处理器（v1.3.1 起图标体系覆盖全部文件类型，对所有人公开）
//
// 返回策略（均 HTTP 200，统一为图片响应，避免暴露额外信息）：
//   - 目录                    → 文件夹类型 SVG
//   - .ico                    → 直接返回该文件本身（它天然就是图标）
//   - 可执行文件(PE,exe/dll等) → 真实内嵌图标 PNG（后台预提取/首次访问提取+落盘缓存）；
//     提取失败时兜底为「程序」类型 SVG，而非默认文件图标
//   - 其它扩展名              → 按文件类型返回品牌色类型 SVG（压缩包/PDF/Office/图片/视频…）
//   - 缺参/路径越界/不存在    → 统一默认 SVG（避免借此探测下载目录中是否存在文件）
func IconHandler(w http.ResponseWriter, r *http.Request) {
	// 获取文件路径参数
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		returnDefaultIcon(w)
		return
	}

	// 验证路径安全（仅允许访问下载目录内的文件）
	safePath := utils.ValidateSafePath(config.AppConfig.Server.DownloadDir, filePath)
	if !safePath.IsSafe || safePath.Error != nil {
		returnDefaultIcon(w)
		return
	}

	// 检查文件是否存在
	info, err := os.Stat(safePath.FullPath)
	if err != nil {
		returnDefaultIcon(w)
		return
	}

	// 目录 → 文件夹类型图标
	if info.IsDir() {
		writeSVG(w, utils.GetFileTypeIconSVG("", true))
		return
	}

	ext := strings.ToLower(filepath.Ext(safePath.FullPath))

	// .ico 文件本身即图标，直接原样返回
	if ext == ".ico" {
		if iconData, err := os.ReadFile(safePath.FullPath); err == nil && len(iconData) > 0 && len(iconData) < 5*1024*1024 {
			w.Header().Set("Content-Type", "image/x-icon")
			w.Header().Set("Cache-Control", "public, max-age=86400") // 缓存1天
			w.Write(iconData)
			return
		}
	}

	// 可执行文件：真实图标（缓存优先 → 提取兜底）
	if utils.IsExecutableFile(ext) {
		if utils.GlobalIconCache != nil {
			iconPath, err := utils.GlobalIconCache.GetFileIcon(safePath.FullPath)
			if err == nil && iconPath != "" {
				if iconData, err := os.ReadFile(iconPath); err == nil {
					w.Header().Set("Content-Type", "image/png")
					w.Header().Set("Cache-Control", "public, max-age=86400")
					w.Write(iconData)
					return
				}
			}
		}
		iconData, err := utils.ExtractIconToBuffer(safePath.FullPath)
		if err == nil {
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "public, max-age=86400")
			w.Write(iconData)
			return
		}
		// 提取失败：兜底为「程序」类型图标
		writeSVG(w, utils.GetFileTypeIconSVG(".exe", false))
		return
	}

	// 其它文件 → 对应类型的彩色 SVG
	writeSVG(w, utils.GetFileTypeIconSVG(ext, false))
}

// writeSVG 输出一张类型 SVG 图标
func writeSVG(w http.ResponseWriter, svg string) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	fmt.Fprint(w, svg)
}

// returnDefaultIcon 返回默认图标（通用文件轮廓，用于一切无法判定/拒绝的场景）
func returnDefaultIcon(w http.ResponseWriter) {
	writeSVG(w, utils.GetFileTypeIconSVG("", false))
}

// GetFileIconURL 获取文件图标的公开 URL（任意文件类型均可，含目录）
func GetFileIconURL(filePath string, r *http.Request) string {
	if filePath == "" {
		return ""
	}
	baseURL := fmt.Sprintf("%s://%s", utils.GetRequestScheme(r), r.Host)
	return fmt.Sprintf("%s/icon?path=%s", baseURL, utils.EncodePath(filePath))
}
