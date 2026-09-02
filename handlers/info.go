package handlers

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/session"
	"go-download-server/utils"
)

// 服务器信息处理函数
func InfoHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}
	// 管理员和二级管理员都可以访问服务器信息页面
	if sess.Role != constants.RoleAdmin && sess.Role != constants.RoleSubAdmin {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/?msg=您没有权限访问该页面", http.StatusFound)
		return
	}

	// 记录日志
	utils.LogUserAction(r, "view_server_info", "查看服务器信息")

	// 获取监控数据
	monitor := utils.GetMonitor()
	systemMetrics := monitor.GetSystemMetrics()
	appMetrics := monitor.GetAppMetrics()
	activeAlerts := monitor.GetActiveAlerts()

	// 构建告警HTML
	alertsHTML := ""
	if len(activeAlerts) == 0 {
		alertsHTML = `<div class="no-alerts">暂无活动告警</div>`
	} else {
		for _, alert := range activeAlerts {
			alertClass := "alert-info"
			if alert.Level == utils.AlertLevelWarning {
				alertClass = "alert-warning"
			} else if alert.Level == utils.AlertLevelCritical {
				alertClass = "alert-critical"
			}
			alertsHTML += fmt.Sprintf(`
				<div class="alert-item %s">
					<div class="alert-level">%s</div>
					<div class="alert-message">%s</div>
					<div class="alert-time">%s</div>
				</div>`, alertClass, alert.Level, alert.Message, alert.Timestamp.Format("2006-01-02 15:04:05"))
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 构建HTML页面
	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=5.0, minimum-scale=1.0">
	<title>服务器信息 - ` + config.AppConfig.Server.ServerName + `</title>
	<link rel="icon" href="data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 100 100%22><text y=%22.9em%22 font-size=%2290%22>📦</text></svg>">
	<link rel="stylesheet" href="/static/styles.css">
	<style>
		/* 监控告警样式 */
		.monitor-section {
			margin-top: 24px;
		}
		.monitor-grid {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
			gap: 16px;
			margin-top: 16px;
		}
		.monitor-card {
			background: #fff;
			border-radius: 12px;
			padding: 20px;
			box-shadow: 0 1px 3px rgba(0,0,0,0.08);
			border: 1px solid #e5e7eb;
		}
		.monitor-card h4 {
			margin: 0 0 12px 0;
			font-size: 14px;
			color: #6b7280;
			font-weight: 500;
		}
		.monitor-value {
			font-size: 28px;
			font-weight: 700;
			color: #111827;
			margin-bottom: 8px;
		}
		.monitor-bar {
			height: 8px;
			background: #e5e7eb;
			border-radius: 4px;
			overflow: hidden;
		}
		.monitor-bar-fill {
			height: 100%;
			border-radius: 4px;
			transition: width 0.3s ease;
		}
		.monitor-bar-fill.normal { background: #10b981; }
		.monitor-bar-fill.warning { background: #f59e0b; }
		.monitor-bar-fill.critical { background: #ef4444; }

		/* 告警样式 */
		.alerts-section {
			margin-top: 24px;
		}
		.alerts-list {
			margin-top: 16px;
			display: flex;
			flex-direction: column;
			gap: 12px;
		}
		.alert-item {
			background: #fff;
			border-radius: 10px;
			padding: 16px;
			border-left: 4px solid;
			box-shadow: 0 1px 3px rgba(0,0,0,0.08);
		}
		.alert-item.alert-info { border-left-color: #3b82f6; }
		.alert-item.alert-warning { border-left-color: #f59e0b; }
		.alert-item.alert-critical { border-left-color: #ef4444; }
		.alert-level {
			display: inline-block;
			padding: 2px 8px;
			border-radius: 4px;
			font-size: 12px;
			font-weight: 600;
			margin-bottom: 8px;
		}
		.alert-info .alert-level { background: #dbeafe; color: #1d4ed8; }
		.alert-warning .alert-level { background: #fef3c7; color: #b45309; }
		.alert-critical .alert-level { background: #fee2e2; color: #b91c1c; }
		.alert-message {
			font-size: 14px;
			color: #374151;
			margin-bottom: 4px;
		}
		.alert-time {
			font-size: 12px;
			color: #9ca3af;
		}
		.no-alerts {
			text-align: center;
			padding: 40px;
			color: #9ca3af;
			font-size: 14px;
		}

		/* 应用性能样式 */
		.perf-grid {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
			gap: 16px;
			margin-top: 16px;
		}
		.perf-item {
			text-align: center;
			padding: 16px;
			background: #f9fafb;
			border-radius: 8px;
		}
		.perf-item .perf-value {
			font-size: 24px;
			font-weight: 700;
			color: #111827;
		}
		.perf-item .perf-label {
			font-size: 12px;
			color: #6b7280;
			margin-top: 4px;
		}
	</style>
</head>
<body class="v2 admin-layout">
		<div class="admin-layout-wrapper">
			` + utils.GetAdminSidebar(r, config.AppConfig.Server.ServerName) + `
			<main class="admin-main">
				<div class="admin-page-header">
					<h1 class="admin-page-title">服务器信息</h1>
					<p class="admin-page-desc">查看服务器详细信息、运行状态和监控告警</p>
				</div>
		<div class="upload-card-v2">
		<div class="info-panel-v2">

			<!-- 统计卡片 -->
			<div class="stats-grid">
				<div class="stat-card">
					<div class="stat-number">` + fmt.Sprintf("%d", config.GetUserCount()) + `</div>
					<div class="stat-label">总用户数</div>
				</div>
				<div class="stat-card">
					<div class="stat-number">` + time.Now().Format("15:04:05") + `</div>
					<div class="stat-label">当前时间</div>
				</div>
				<div class="stat-card">
					<div class="stat-number">` + time.Now().Format("2006-01-02") + `</div>
					<div class="stat-label">当前日期</div>
				</div>
				<div class="stat-card">
					<div class="stat-number">` + utils.FormatDuration(time.Since(StartTime)) + `</div>
					<div class="stat-label">运行时间</div>
				</div>
			</div>

			<!-- 系统资源监控 -->
			<div class="monitor-section">
				<h3>系统资源监控</h3>
				<div class="monitor-grid">
					<div class="monitor-card">
						<h4>CPU使用率</h4>
						<div class="monitor-value">` + fmt.Sprintf("%.1f%%", systemMetrics.CPUUsage) + `</div>
						<div class="monitor-bar">
							<div class="monitor-bar-fill ` + getMonitorBarClass(systemMetrics.CPUUsage) + `" style="width: ` + fmt.Sprintf("%.1f%%", systemMetrics.CPUUsage) + `"></div>
						</div>
					</div>
					<div class="monitor-card">
						<h4>内存使用率</h4>
						<div class="monitor-value">` + fmt.Sprintf("%.1f%%", systemMetrics.MemoryUsage) + `</div>
						<div class="monitor-bar">
							<div class="monitor-bar-fill ` + getMonitorBarClass(systemMetrics.MemoryUsage) + `" style="width: ` + fmt.Sprintf("%.1f%%", systemMetrics.MemoryUsage) + `"></div>
						</div>
					</div>
					<div class="monitor-card">
						<h4>磁盘使用率</h4>
						<div class="monitor-value">` + fmt.Sprintf("%.1f%%", systemMetrics.DiskUsage) + `</div>
						<div class="monitor-bar">
							<div class="monitor-bar-fill ` + getMonitorBarClass(systemMetrics.DiskUsage) + `" style="width: ` + fmt.Sprintf("%.1f%%", systemMetrics.DiskUsage) + `"></div>
						</div>
					</div>
					<div class="monitor-card">
						<h4>Goroutine数量</h4>
						<div class="monitor-value">` + fmt.Sprintf("%d", systemMetrics.GoroutineNum) + `</div>
						<div style="font-size: 12px; color: #6b7280; margin-top: 8px;">
							已分配内存: ` + utils.FormatFileSize(int64(systemMetrics.AllocMem)) + `
						</div>
					</div>
				</div>
			</div>

			<!-- 应用性能监控 -->
			<div class="monitor-section">
				<h3>应用性能监控</h3>
				<div class="perf-grid">
					<div class="perf-item">
						<div class="perf-value">` + fmt.Sprintf("%d", appMetrics.TotalRequests) + `</div>
						<div class="perf-label">总请求数</div>
					</div>
					<div class="perf-item">
						<div class="perf-value">` + fmt.Sprintf("%d", appMetrics.SuccessRequests) + `</div>
						<div class="perf-label">成功请求</div>
					</div>
					<div class="perf-item">
						<div class="perf-value">` + fmt.Sprintf("%d", appMetrics.ErrorRequests) + `</div>
						<div class="perf-label">错误请求</div>
					</div>
					<div class="perf-item">
						<div class="perf-value">` + fmt.Sprintf("%.1f%%", appMetrics.ErrorRate) + `</div>
						<div class="perf-label">错误率</div>
					</div>
					<div class="perf-item">
						<div class="perf-value">` + appMetrics.AvgResponseTime.String() + `</div>
						<div class="perf-label">平均响应时间</div>
					</div>
					<div class="perf-item">
						<div class="perf-value">` + appMetrics.MaxResponseTime.String() + `</div>
						<div class="perf-label">最大响应时间</div>
					</div>
				</div>
			</div>

			<!-- 活动告警 -->
			<div class="alerts-section">
				<h3 style="cursor: pointer; user-select: none;" onclick="toggleAlerts()">
					<span id="alertsToggleIcon">▼</span> 活动告警 (` + fmt.Sprintf("%d", len(activeAlerts)) + `)
				</h3>
				<div class="alerts-list" id="alertsList" style="display: none;">
					` + alertsHTML + `
				</div>
			</div>

			<!-- 服务器基本信息 -->
			<div class="info-section">
				<h3>服务器基本信息</h3>
				<div class="info-grid">
					<div class="info-card">
						<h3>系统信息</h3>
						<div class="info-item">
							<div class="info-label">操作系统</div>
							<div class="info-value">` + runtime.GOOS + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">架构</div>
							<div class="info-value">` + runtime.GOARCH + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">Go版本</div>
							<div class="info-value">` + runtime.Version() + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">CPU核心数</div>
							<div class="info-value">` + fmt.Sprintf("%d", runtime.NumCPU()) + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">当前进程ID</div>
							<div class="info-value">` + fmt.Sprintf("%d", os.Getpid()) + `</div>
						</div>
					</div>

					<div class="info-card">
						<h3>服务器配置</h3>
						<div class="info-item">
							<div class="info-label">HTTP端口</div>
							<div class="info-value">` + fmt.Sprintf("%d", config.AppConfig.Server.Port) + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">HTTPS端口</div>
							<div class="info-value">` + fmt.Sprintf("%d", config.AppConfig.Server.HttpsPort) + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">下载目录</div>
							<div class="info-value">` + config.AppConfig.Server.DownloadDir + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">待审核目录</div>
							<div class="info-value">` + config.AppConfig.Server.PendingDir + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">日志目录</div>
							<div class="info-value">` + config.AppConfig.Server.LogDir + `</div>
						</div>
						<div class="info-item">
								<div class="info-label">日志文件</div>
								<div class="info-value">` + config.AppConfig.Server.LogFile + `</div>
							</div>
							<div class="info-item">
								<div class="info-label">配置文件</div>
								<div class="info-value">config/config.json</div>
							</div>
					</div>

					<div class="info-card">
						<h3>项目信息</h3>
						<div class="info-item">
			<div class="info-label">项目名称</div>
			<div class="info-value">` + config.AppConfig.Server.ServerName + `</div>
		</div>
						<div class="info-item">
							<div class="info-label">版本</div>
							<div class="info-value">` + constants.Version + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">开发者</div>
							<div class="info-value">` + constants.Developer + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">仓库链接</div>
							<div class="info-value"><a href="` + constants.RepoURL + `" target="_blank" title="GitHub仓库"><svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" style="vertical-align: middle;"><path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.435.372.825 1.102.825 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12"></path></svg></a></div>
						</div>
						<div class="info-item">
							<div class="info-label">启动时间</div>
							<div class="info-value">` + StartTime.Format("2006-01-02 15:04:05") + `</div>
						</div>
					</div>


				</div>
			</div>

		</div>
	</div>
	<script>
		function toggleAlerts() {
			var list = document.getElementById('alertsList');
			var icon = document.getElementById('alertsToggleIcon');
			if (list.style.display === 'none') {
				list.style.display = 'block';
				icon.textContent = '▼';
			} else {
				list.style.display = 'none';
				icon.textContent = '▶';
			}
		}
	</script>
</body>
</html>`

	w.Write([]byte(html))
}

// getMonitorBarClass 根据使用率返回进度条样式类
func getMonitorBarClass(usage float64) string {
	if usage >= 90 {
		return "critical"
	} else if usage >= 70 {
		return "warning"
	}
	return "normal"
}
