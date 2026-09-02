package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/session"
	"go-download-server/utils"
)

// 文件审核拒绝处理函数
func RejectHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}
	// 管理员和二级管理员都可以拒绝文件
	if sess.Role != constants.RoleAdmin && sess.Role != constants.RoleSubAdmin {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/?msg=您没有权限执行此操作", http.StatusFound)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 验证CSRF令牌
	if !utils.ValidateCSRFTokenFromRequest(r) {
		http.Error(w, "CSRF令牌验证失败", http.StatusForbidden)
		return
	}

	// 解析表单
	r.ParseForm()
	filename := r.FormValue("file")
	currentPath := r.FormValue("current_path")
	username := r.FormValue("username")

	// URL解码
	filename, _ = url.QueryUnescape(filename)
	currentPath, _ = url.QueryUnescape(currentPath)
	username, _ = url.QueryUnescape(username)

	// 安全检查
	currentPath = filepath.Clean(currentPath)
	username = filepath.Clean(username)
	if strings.HasPrefix(currentPath, "..") || strings.HasPrefix(username, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// 构建文件路径
	filePath := filepath.Join(config.AppConfig.Server.PendingDir, username, currentPath, filename)

	// 调试日志
	utils.Log(utils.LogLevelDebug, sess.Username, "admin", "reject_file", fmt.Sprintf("尝试删除文件: %s", filePath))

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		utils.Log(utils.LogLevelError, sess.Username, "admin", "reject_file", fmt.Sprintf("文件不存在: %s", filePath))
		http.Redirect(w, r, fmt.Sprintf("/review?path=%s&msg=%s&type=error", url.QueryEscape(currentPath), url.QueryEscape("审核拒绝失败: 文件不存在")), http.StatusFound)
		return
	}

	// 删除文件，添加重试机制
	const maxRetries = 3
	const retryDelay = 500 * time.Millisecond
	var err error
	for i := 0; i < maxRetries; i++ {
		err = os.Remove(filePath)
		if err == nil {
			break // 删除成功
		}

		// 检查是否是文件被占用错误
		if i < maxRetries-1 {
			utils.Log(utils.LogLevelDebug, sess.Username, "admin", "reject_file", fmt.Sprintf("删除文件失败，正在重试 (%d/%d): %v, 文件路径: %s", i+1, maxRetries, err, filePath))
			time.Sleep(retryDelay)
		}
	}

	if err != nil {
		utils.Log(utils.LogLevelError, sess.Username, "admin", "reject_file", fmt.Sprintf("删除文件失败: %v, 文件路径: %s", err, filePath))
		http.Redirect(w, r, fmt.Sprintf("/review?path=%s&msg=%s&type=error", url.QueryEscape(currentPath), url.QueryEscape(fmt.Sprintf("审核拒绝失败: %v", err))), http.StatusFound)
		return
	}

	// 清理空目录
	utils.CleanupEmptyDirectories(config.AppConfig.Server.PendingDir, username, currentPath)

	// 记录日志
	utils.Log(utils.LogLevelSuccess, sess.Username, "admin", "reject_file", fmt.Sprintf("文件 '%s' 审核拒绝，用户: %s，路径: %s", filename, username, currentPath))

	// 重定向回审核页面
	http.Redirect(w, r, fmt.Sprintf("/review?path=%s&msg=%s&type=success", url.QueryEscape(currentPath), url.QueryEscape(fmt.Sprintf("文件 '%s' 审核拒绝", filename))), http.StatusFound)
}
