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
		// 如果是跨设备移动失败，使用异步复制+删除的方式（避免大文件同步复制导致长时间等待）
		if strings.Contains(err.Error(), "invalid cross-device link") {
			// 启动异步复制
			go func(src, dst, fname, user string) {
				// 打开源文件
				srcFile, err := os.Open(src)
				if err != nil {
					utils.Log(utils.LogLevelError, "system", "admin", "approve_file_async", fmt.Sprintf("异步复制-打开源文件失败: %v", err))
					return
				}
				defer srcFile.Close()

				// 创建目标文件
				dstFile, err := os.Create(dst)
				if err != nil {
					utils.Log(utils.LogLevelError, "system", "admin", "approve_file_async", fmt.Sprintf("异步复制-创建目标文件失败: %v", err))
					return
				}

				// 复制文件内容
				if _, err := io.Copy(dstFile, srcFile); err != nil {
					dstFile.Close()
					utils.Log(utils.LogLevelError, "system", "admin", "approve_file_async", fmt.Sprintf("异步复制-复制文件内容失败: %v", err))
					return
				}

				// 关闭并校验目标文件，确保复制完整后才删除源文件
				if err := dstFile.Close(); err != nil {
					utils.Log(utils.LogLevelError, "system", "admin", "approve_file_async", fmt.Sprintf("异步复制-关闭目标文件失败: %v", err))
					return
				}
				srcInfo, srcErr := srcFile.Stat()
				if srcErr != nil {
					utils.Log(utils.LogLevelError, "system", "admin", "approve_file_async", fmt.Sprintf("异步复制-获取源文件信息失败: %v", srcErr))
					return
				}
				dstInfo, dstErr := os.Stat(dst)
				if dstErr != nil || dstInfo.Size() != srcInfo.Size() {
					utils.Log(utils.LogLevelError, "system", "admin", "approve_file_async", "异步复制-复制文件不完整，拒绝删除源文件")
					return
				}

				// 删除源文件
				if err := os.Remove(src); err != nil {
					utils.Log(utils.LogLevelError, "system", "admin", "approve_file_async", fmt.Sprintf("异步复制-删除源文件失败: %v", err))
				}

				// 清理空目录
				utils.CleanupEmptyDirectories(config.AppConfig.Server.PendingDir, user, filepath.Dir(src))
				
				// 使目标目录缓存失效
				invalidateCache(filepath.Dir(dst))

				utils.Log(utils.LogLevelSuccess, "system", "admin", "approve_file_async", fmt.Sprintf("文件 '%s' 异步复制完成", fname))
			}(sourcePath, destPath, filename, username)

			// 立即返回，不等待异步复制完成
			utils.Log(utils.LogLevelInfo, sess.Username, "admin", "approve_file", fmt.Sprintf("文件 '%s' 审核通过，正在后台异步移动（跨设备）", filename))
			http.Redirect(w, r, fmt.Sprintf("/review?path=%s&msg=%s&type=success", url.QueryEscape(currentPath), url.QueryEscape(fmt.Sprintf("文件 '%s' 审核通过，正在后台移动", filename))), http.StatusFound)
			return
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
