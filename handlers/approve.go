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

// 文件审核通过处理函数
func ApproveHandler(w http.ResponseWriter, r *http.Request) {
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
	targetDir := r.FormValue("target_dir")
	username := r.FormValue("username")

	// URL解码
	filename, _ = url.QueryUnescape(filename)
	currentPath, _ = url.QueryUnescape(currentPath)
	targetDir, _ = url.QueryUnescape(targetDir)
	username, _ = url.QueryUnescape(username)

	// 安全检查
	currentPath = filepath.Clean(currentPath)
	targetDir = filepath.Clean(targetDir)
	username = filepath.Clean(username)
	if strings.HasPrefix(currentPath, "..") || strings.HasPrefix(targetDir, "..") || strings.HasPrefix(username, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// 构建源文件和目标文件路径
	sourcePath := filepath.Join(config.AppConfig.Server.PendingDir, username, currentPath, filename)
	destPath := filepath.Join(config.AppConfig.Server.DownloadDir, targetDir, filename)

	// 确保目标目录存在
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		utils.Log(utils.LogLevelError, sess.Username, "admin", "approve_file", fmt.Sprintf("创建目标目录失败: %v", err))
		http.Redirect(w, r, fmt.Sprintf("/review?path=%s&msg=%s&type=error", url.QueryEscape(currentPath), url.QueryEscape("创建目标目录失败")), http.StatusFound)
		return
	}

	// 移动文件
	if err := os.Rename(sourcePath, destPath); err != nil {
		utils.Log(utils.LogLevelError, sess.Username, "admin", "approve_file", fmt.Sprintf("移动文件失败: %v", err))
		http.Redirect(w, r, fmt.Sprintf("/review?path=%s&msg=%s&type=error", url.QueryEscape(currentPath), url.QueryEscape("审核通过失败")), http.StatusFound)
		return
	}

	// 记录日志
	utils.Log(utils.LogLevelSuccess, sess.Username, "admin", "approve_file", fmt.Sprintf("文件 '%s' 审核通过，从 %s/%s 移动到 %s", filename, username, currentPath, targetDir))

	// 重定向回审核页面
	http.Redirect(w, r, fmt.Sprintf("/review?path=%s&msg=%s&type=success", url.QueryEscape(currentPath), url.QueryEscape(fmt.Sprintf("文件 '%s' 审核通过", filename))), http.StatusFound)
}
