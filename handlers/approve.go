package handlers

import (
	"fmt"
	"io"
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
	if sess == nil {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}
	// 管理员和二级管理员都可以批准文件
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
		// 如果是跨设备移动失败，使用复制+删除的方式
		if strings.Contains(err.Error(), "invalid cross-device link") {
			// 打开源文件
			srcFile, err := os.Open(sourcePath)
			if err != nil {
				utils.Log(utils.LogLevelError, sess.Username, "admin", "approve_file", fmt.Sprintf("打开源文件失败: %v", err))
				http.Redirect(w, r, fmt.Sprintf("/review?path=%s&msg=%s&type=error", url.QueryEscape(currentPath), url.QueryEscape("审核通过失败")), http.StatusFound)
				return
			}
			defer srcFile.Close()

			// 创建目标文件
			dstFile, err := os.Create(destPath)
			if err != nil {
				utils.Log(utils.LogLevelError, sess.Username, "admin", "approve_file", fmt.Sprintf("创建目标文件失败: %v", err))
				http.Redirect(w, r, fmt.Sprintf("/review?path=%s&msg=%s&type=error", url.QueryEscape(currentPath), url.QueryEscape("审核通过失败")), http.StatusFound)
				return
			}
			defer dstFile.Close()

			// 复制文件内容
			if _, err := io.Copy(dstFile, srcFile); err != nil {
				utils.Log(utils.LogLevelError, sess.Username, "admin", "approve_file", fmt.Sprintf("复制文件内容失败: %v", err))
				http.Redirect(w, r, fmt.Sprintf("/review?path=%s&msg=%s&type=error", url.QueryEscape(currentPath), url.QueryEscape("审核通过失败")), http.StatusFound)
				return
			}

			// 关闭并校验目标文件，确保复制完整后才删除源文件
			if err := dstFile.Close(); err != nil {
				utils.Log(utils.LogLevelError, sess.Username, "admin", "approve_file", fmt.Sprintf("关闭目标文件失败: %v", err))
				http.Redirect(w, r, fmt.Sprintf("/review?path=%s&msg=%s&type=error", url.QueryEscape(currentPath), url.QueryEscape("审核通过失败")), http.StatusFound)
				return
			}
			srcInfo, srcErr := srcFile.Stat()
			if srcErr != nil {
				utils.Log(utils.LogLevelError, sess.Username, "admin", "approve_file", fmt.Sprintf("获取源文件信息失败: %v", srcErr))
				http.Redirect(w, r, fmt.Sprintf("/review?path=%s&msg=%s&type=error", url.QueryEscape(currentPath), url.QueryEscape("审核通过失败")), http.StatusFound)
				return
			}
			dstInfo, dstErr := os.Stat(destPath)
			if dstErr != nil || dstInfo.Size() != srcInfo.Size() {
				utils.Log(utils.LogLevelError, sess.Username, "admin", "approve_file", "复制文件不完整，拒绝删除源文件")
				http.Redirect(w, r, fmt.Sprintf("/review?path=%s&msg=%s&type=error", url.QueryEscape(currentPath), url.QueryEscape("复制文件不完整，请重试")), http.StatusFound)
				return
			}

			// 删除源文件
			if err := os.Remove(sourcePath); err != nil {
				utils.Log(utils.LogLevelError, sess.Username, "admin", "approve_file", fmt.Sprintf("删除源文件失败: %v", err))
				// 这里不返回错误，因为文件已经成功复制到目标位置
			}
		} else {
			// 其他错误直接返回
			utils.Log(utils.LogLevelError, sess.Username, "admin", "approve_file", fmt.Sprintf("移动文件失败: %v", err))
			http.Redirect(w, r, fmt.Sprintf("/review?path=%s&msg=%s&type=error", url.QueryEscape(currentPath), url.QueryEscape("审核通过失败")), http.StatusFound)
			return
		}
	}

	// 清理空目录
	utils.CleanupEmptyDirectories(config.AppConfig.Server.PendingDir, username, currentPath)

	// 使目标目录缓存失效
	targetCachePath := filepath.Join(config.AppConfig.Server.DownloadDir, targetDir)
	invalidateCache(targetCachePath)

	// 记录日志
	utils.Log(utils.LogLevelSuccess, sess.Username, "admin", "approve_file", fmt.Sprintf("文件 '%s' 审核通过，从 %s/%s 移动到 %s", filename, username, currentPath, targetDir))

	// 重定向回审核页面
	http.Redirect(w, r, fmt.Sprintf("/review?path=%s&msg=%s&type=success", url.QueryEscape(currentPath), url.QueryEscape(fmt.Sprintf("文件 '%s' 审核通过", filename))), http.StatusFound)
}
