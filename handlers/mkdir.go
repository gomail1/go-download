package handlers

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/session"
	"go-download-server/utils"
)

// 创建目录处理函数
func MkdirHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}
	// 管理员和二级管理员都可以创建目录
	if sess.Role != constants.RoleAdmin && sess.Role != constants.RoleSubAdmin {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/?msg=您没有权限执行此操作", http.StatusFound)
		return
	}

	// POST请求：处理目录创建
	if r.Method == "POST" {
		// 验证CSRF令牌
		if !utils.ValidateCSRFTokenFromRequest(r) {
			http.Error(w, "CSRF令牌验证失败", http.StatusForbidden)
			return
		}

		// 解析表单
		r.ParseForm()
		parentDir := r.FormValue("parent_dir")
		dirName := r.FormValue("dir_name")

		// 检查目录名称
		if dirName == "" {
			http.Error(w, "目录名称不能为空", http.StatusBadRequest)
			return
		}

		// 清理目录名称
		dirName = utils.SanitizeFilename(dirName)

		// 安全检查
		parentDir = filepath.Clean(parentDir)
		if strings.HasPrefix(parentDir, "..") {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}

		// 构建完整路径
		fullPath := filepath.Join(config.AppConfig.Server.DownloadDir, parentDir, dirName)

		// 检查目录是否已存在
		if _, err := os.Stat(fullPath); err == nil {
			http.Error(w, "目录已存在", http.StatusBadRequest)
			return
		}

		// 创建目录
		err := os.MkdirAll(fullPath, 0755)
		if err != nil {
			log.Printf("创建目录失败: %v", err)
			http.Error(w, fmt.Sprintf("目录创建失败: %v", err), http.StatusInternalServerError)
			return
		}

		// 记录日志
		log.Printf("管理员 %s 创建了目录: %s", sess.Username, fullPath)

		// 使父目录缓存失效，这样新创建的目录就能立即显示
		parentCachePath := filepath.Join(config.AppConfig.Server.DownloadDir, parentDir)
		invalidateCache(parentCachePath)

		// 返回成功状态
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("目录创建成功"))
	} else {
		// GET请求：直接返回404，因为我们不再使用这个页面
		http.NotFound(w, r)
	}
}
