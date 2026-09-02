package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/session"
	"go-download-server/utils"
)

// 文件删除处理函数
func DeleteHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}
	// 管理员和二级管理员都可以删除文件
	if sess.Role != constants.RoleAdmin && sess.Role != constants.RoleSubAdmin {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/?msg=您没有权限执行此操作", http.StatusFound)
		return
	}

	// 仅允许POST请求，防止GET链接形式的CSRF攻击
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 验证CSRF令牌
	if !utils.ValidateCSRFTokenFromRequest(r) {
		http.Error(w, "CSRF令牌验证失败", http.StatusForbidden)
		return
	}

	// 获取文件路径
	path := r.FormValue("path")
	if path == "" {
		http.Error(w, "缺少文件路径", http.StatusBadRequest)
		return
	}

	var err error

	// 兼容旧格式：若路径仍为URL编码形式则解码
	if decoded, decErr := url.QueryUnescape(path); decErr == nil && decoded != path && !strings.Contains(path, "/") {
		path = decoded
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

	// 执行删除操作
	var errMsg string
	if fileInfo.IsDir() {
		err = os.RemoveAll(fullPath)
		errMsg = fmt.Sprintf("目录 '%s' 删除失败", path)
	} else {
		err = os.Remove(fullPath)
		errMsg = fmt.Sprintf("文件 '%s' 删除失败", path)
	}

	if err != nil {
		utils.Log(utils.LogLevelError, sess.Username, "admin", "delete_file", fmt.Sprintf("删除失败: %v", err))
		http.Redirect(w, r, fmt.Sprintf("/files?msg=%s&type=error", url.QueryEscape(errMsg)), http.StatusFound)
		return
	}

	// 清理统计数据
	DeleteStatsForPath(path) // 删除该路径的统计数据

	// 使相关缓存失效
	parentPath := filepath.Dir(fullPath)
	invalidateCache(parentPath) // 使父目录缓存失效
	invalidateCache(fullPath)   // 使被删除项缓存失效
	if fileInfo.IsDir() {
		invalidateCacheRecursive(fullPath) // 如果是目录，递归使子项缓存失效
	}

	// 清理pending目录下所有用户对应路径的内容
	pendingRoot := config.AppConfig.Server.PendingDir
	userDirs, err := os.ReadDir(pendingRoot)
	if err == nil {
		for _, userDir := range userDirs {
			if userDir.IsDir() {
				// 构建该用户pending目录下的对应路径
				pendingPath := filepath.Join(pendingRoot, userDir.Name(), path)
				// 删除该路径下的文件或目录
				os.RemoveAll(pendingPath)
				utils.Log(utils.LogLevelInfo, sess.Username, "admin", "clean_pending", fmt.Sprintf("清理了用户 %s pending目录下的: %s", userDir.Name(), pendingPath))
			}
		}
	}

	// 记录日志
	utils.Log(utils.LogLevelSuccess, sess.Username, "admin", "delete_file", fmt.Sprintf("删除了: %s", path))

	// 重定向回文件列表页面并显示成功消息
	parentPath = filepath.Dir(path)
	if parentPath == "." {
		parentPath = ""
	}
	successMsg := fmt.Sprintf("删除成功: %s", path)
	http.Redirect(w, r, fmt.Sprintf("/files?path=%s&msg=%s&type=success", url.QueryEscape(parentPath), url.QueryEscape(successMsg)), http.StatusFound)
}
