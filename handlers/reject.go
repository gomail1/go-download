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

// 文件审核拒绝处理函数
func RejectHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil || sess.Role != constants.RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
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

	// 删除文件
	if err := os.Remove(filePath); err != nil {
		utils.Log(utils.LogLevelError, sess.Username, "admin", "reject_file", fmt.Sprintf("删除文件失败: %v", err))
		http.Redirect(w, r, fmt.Sprintf("/review?path=%s&msg=%s&type=error", url.QueryEscape(currentPath), url.QueryEscape("审核拒绝失败")), http.StatusFound)
		return
	}

	// 记录日志
	utils.Log(utils.LogLevelSuccess, sess.Username, "admin", "reject_file", fmt.Sprintf("文件 '%s' 审核拒绝，用户: %s，路径: %s", filename, username, currentPath))

	// 重定向回审核页面
	http.Redirect(w, r, fmt.Sprintf("/review?path=%s&msg=%s&type=success", url.QueryEscape(currentPath), url.QueryEscape(fmt.Sprintf("文件 '%s' 审核拒绝", filename))), http.StatusFound)
}
