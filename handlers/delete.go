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
	if sess == nil || sess.Role != constants.RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
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
	parentPath := filepath.Dir(path)
	if parentPath == "." {
		parentPath = ""
	}
	successMsg := fmt.Sprintf("删除成功: %s", path)
	http.Redirect(w, r, fmt.Sprintf("/files?path=%s&msg=%s&type=success", url.QueryEscape(parentPath), url.QueryEscape(successMsg)), http.StatusFound)
}
