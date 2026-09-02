package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/session"
	"go-download-server/utils"
)

// 服务器启动时间
var StartTime time.Time

// 统计待审核文件数量
func countPendingFiles() int {
	pendingRootDir := config.AppConfig.Server.PendingDir
	count := 0

	// 遍历所有用户子目录
	userDirs, err := os.ReadDir(pendingRootDir)
	if err != nil {
		return count
	}

	// 遍历每个用户目录
	for _, userDir := range userDirs {
		if !userDir.IsDir() {
			continue
		}

		userPendingDir := filepath.Join(pendingRootDir, userDir.Name())

		// 递归统计当前用户目录下的所有文件
		err := filepath.Walk(userPendingDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				// 过滤掉BT客户端的临时文件和未完成的下载文件
				fileName := info.Name()
				if fileName == ".torrent.bolt.db" {
					// 跳过BT临时数据库文件
					return nil
				}
				if strings.HasSuffix(fileName, ".part") {
					// 跳过未完成的下载文件
					return nil
				}
				count++
			}
			return nil
		})
		if err != nil {
			continue
		}
	}

	return count
}

// 获取待处理文件数量HTML标记
func getPendingCountHTML(r *http.Request) string {
	count := countPendingFiles()
	if count > 0 {
		return fmt.Sprintf(`<span class="pending-count">%d</span>`, count)
	}
	return ""
}

// 管理员页面处理函数
func AdminHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil {
		// 确保没有缓存
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}
	// 管理员和二级管理员都可以访问管理员页面
	if sess.Role != constants.RoleAdmin && sess.Role != constants.RoleSubAdmin {
		// 确保没有缓存
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/?msg=您没有权限访问该页面", http.StatusFound)
		return
	}

	// 检查协议同意状态
	if !sess.AgreedToTerms {
		http.Redirect(w, r, "/terms", http.StatusFound)
		return
	}

	// 生成统计信息HTML（管理员和二级管理员都能看到完整统计信息）
	statsHTML := `
		<div class="admin-stat-card">
			<div class="admin-stat-label">🕐 当前时间</div>
			<div class="admin-stat-value">` + time.Now().Format("2006-01-02 15:04:05") + `</div>
		</div>
		<div class="admin-stat-card">
			<div class="admin-stat-label">⚡ 运行时间</div>
			<div class="admin-stat-value">` + utils.FormatDuration(time.Since(StartTime)) + `</div>
		</div>
		<div class="admin-stat-card">
			<div class="admin-stat-label">📥 总下载次数</div>
			<div class="admin-stat-value">` + fmt.Sprintf("%d", GetTotalDownloadCount()) + `</div>
		</div>
		<div class="admin-stat-card">
			<div class="admin-stat-label">💾 总流量</div>
			<div class="admin-stat-value">` + utils.FormatFileSize(GetTotalDownloadSize()) + `</div>
		</div>
		<div class="admin-stat-card">
			<div class="admin-stat-label">📊 热力图数据点</div>
			<div class="admin-stat-value">` + fmt.Sprintf("%d", GetHeatmapDataCount()) + `</div>
		</div>`

	// 生成最近操作日志HTML
	recentLogsHTML := utils.GetRecentLogs(10)

	// 主HTML
	html := `
	<!DOCTYPE html>
	<html lang="zh-CN">
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<title>管理中心 - ` + config.AppConfig.Server.ServerName + `</title>
		<link rel="icon" href="data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 100 100%22><text y=%22.9em%22 font-size=%2290%22>📦</text></svg>">
		<link rel="stylesheet" href="/static/styles.css">

	</head>
	<body class="v2 admin-layout">
		<div class="admin-layout-wrapper">
			<!-- 侧边栏 -->
			` + utils.GetAdminSidebar(r, config.AppConfig.Server.ServerName) + `

			<!-- 主内容区 -->
			<main class="admin-main">
				<div class="admin-page-header">
					<h1 class="admin-page-title">管理控制面板</h1>
					<p class="admin-page-desc">服务器状态概览与最近活动</p>
				</div>

				<!-- 统计卡片 -->
				<div class="admin-stats-row">
					` + statsHTML + `
				</div>

				<!-- 最近操作日志 -->
				<div class="admin-content-card">
					<div class="admin-content-card-title">
						<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/><polyline points="10 9 9 9 8 9"/></svg>
						最近操作
						<a href="/logs" style="margin-left: auto; font-size: 13px; color: var(--v2-primary); text-decoration: none;">查看全部 →</a>
					</div>
					<div style="padding: 0 20px 20px 20px;">
						` + recentLogsHTML + `
					</div>
				</div>
			</main>
		</div>
	</body>
</html>
	`

	// 写入响应
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}
