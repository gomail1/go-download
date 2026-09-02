package handlers

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/session"
	"go-download-server/utils"
)

// IP管理页面
func IPAdminHandler(w http.ResponseWriter, r *http.Request) {
	// 检查管理员权限
	user := session.GetCurrentUser(r)
	if user == nil || user.Role != constants.RoleAdmin {
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 获取CSRF令牌（meta标签，供前端AJAX读取）
	sessionID := utils.GetSessionIDFromRequest(r)
	csrfTokenMeta := utils.GenerateCSRFTokenMeta(sessionID)

	// 获取筛选和分页参数
	statusFilter := r.URL.Query().Get("status")
	if statusFilter == "" {
		statusFilter = "all"
	}
	page := 1
	pageSize := 20
	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 && parsed <= 100 {
			pageSize = parsed
		}
	}

	// 获取所有IP统计
	allIPStats := GetAllIPStats()

	// 根据筛选条件过滤
	var filteredIPStats []IPDownloadStats
	for _, stats := range allIPStats {
		if statusFilter == "blocked" && !stats.Blocked {
			continue
		}
		if statusFilter == "normal" && stats.Blocked {
			continue
		}
		filteredIPStats = append(filteredIPStats, stats)
	}

	// 分页
	totalFiltered := len(filteredIPStats)
	totalPages := (totalFiltered + pageSize - 1) / pageSize
	if page > totalPages && totalPages > 0 {
		page = totalPages
	}
	startIndex := (page - 1) * pageSize
	endIndex := startIndex + pageSize
	if endIndex > totalFiltered {
		endIndex = totalFiltered
	}
	var pagedIPStats []IPDownloadStats
	if startIndex < totalFiltered {
		pagedIPStats = filteredIPStats[startIndex:endIndex]
	}

	// 生成IP列表HTML
	ipListHTML := ""
	if len(pagedIPStats) == 0 {
		ipListHTML = `<div class="empty-state-v2" style="grid-column: 1/-1;">
			<div class="empty-state-icon-v2">📊</div>
			<div class="empty-state-title-v2">暂无IP统计</div>
			<div class="empty-state-desc-v2">还没有任何下载记录</div>
		</div>`
	} else {
		for idx, stats := range pagedIPStats {
			i := startIndex + idx // 全局序号
			// XSS防护：对IP地址进行HTML编码和JavaScript编码
			safeIP := utils.EscapeHTML(stats.IP)
			safeJSIP := strings.ReplaceAll(safeIP, "'", "\\'")
			// XSS防护：对封禁原因进行HTML编码
			safeBlockReason := utils.EscapeHTML(stats.BlockReason)

			blockedBadge := ""
			blockBtn := ""
			if stats.Blocked {
				blockedBadge = `<span class="status-badge status-danger">已封禁</span>`
				blockBtn = fmt.Sprintf(`<button class="btn btn-sm btn-success" onclick="unblockIP('%s')">解封</button>`, safeJSIP)
			} else {
				blockedBadge = `<span class="status-badge status-success">正常</span>`
				blockBtn = fmt.Sprintf(`<button class="btn btn-sm btn-danger" onclick="blockIP('%s')">封禁</button>`, safeJSIP)
			}

			// 计算序号
			rankBadge := ""
			if i < 3 {
				colors := []string{"#ff6b6b", "#ffa94d", "#ffd43b"}
				rankBadge = fmt.Sprintf(`<span class="hot-rank-badge" style="background: %s;">%d</span>`, colors[i], i+1)
			}

			ipListHTML += fmt.Sprintf(`<div class="ip-card">
				<div class="ip-card-header">
					<div class="ip-address">%s %s %s</div>
					%s
				</div>
				<div class="ip-card-stats">
					<div class="ip-stat-item">
						<div class="ip-stat-value">%d</div>
						<div class="ip-stat-label">下载次数</div>
					</div>
					<div class="ip-stat-item">
						<div class="ip-stat-value">%s</div>
						<div class="ip-stat-label">总流量</div>
					</div>
					<div class="ip-stat-item">
						<div class="ip-stat-value">%s</div>
						<div class="ip-stat-label">首次出现</div>
					</div>
					<div class="ip-stat-item">
						<div class="ip-stat-value">%s</div>
						<div class="ip-stat-label">最后下载</div>
					</div>
				</div>
				<div class="ip-card-actions">
					%s
				</div>
			</div>`, rankBadge, safeIP, blockedBadge,
				safeBlockReason,
				stats.DownloadCount,
				utils.FormatFileSize(stats.TotalBandwidth),
				stats.FirstSeen.Format("2006-01-02 15:04"),
				stats.LastDownloadTime.Format("2006-01-02 15:04"),
				blockBtn)
		}
	}

	// 统计数据（基于全部IP）
	totalIPs := len(allIPStats)
	blockedIPs := 0
	normalIPs := 0
	totalDownloads := int64(0)
	totalBandwidth := int64(0)
	todayDownloads := int64(0)
	todayBandwidth := int64(0)
	todayDate := time.Now().Format("2006-01-02")
	for _, stats := range allIPStats {
		if stats.Blocked {
			blockedIPs++
		} else {
			normalIPs++
		}
		totalDownloads += stats.DownloadCount
		totalBandwidth += stats.TotalBandwidth
		// 统计今日数据
		if stats.DailyDate == todayDate {
			todayDownloads += stats.DailyDownloadCount
			todayBandwidth += stats.DailyBandwidth
		}
	}

	// 生成筛选菜单HTML
	filterHTML := fmt.Sprintf(`
		<div class="ip-filter" style="margin-bottom: 16px; display: flex; gap: 8px; align-items: center;">
			<span style="font-size: 14px; color: var(--v2-text-secondary);">筛选：</span>
			<a href="?status=all&page=1&page_size=%d" class="btn btn-sm %s" style="text-decoration: none;">全部 (%d)</a>
			<a href="?status=normal&page=1&page_size=%d" class="btn btn-sm %s" style="text-decoration: none;">正常 (%d)</a>
			<a href="?status=blocked&page=1&page_size=%d" class="btn btn-sm %s" style="text-decoration: none;">已封禁 (%d)</a>
		</div>
	`, pageSize,
		map[bool]string{true: "btn-primary", false: ""}[statusFilter == "all"], totalIPs,
		pageSize,
		map[bool]string{true: "btn-primary", false: ""}[statusFilter == "normal"], normalIPs,
		pageSize,
		map[bool]string{true: "btn-primary", false: ""}[statusFilter == "blocked"], blockedIPs,
	)

	// 生成分页HTML
	paginationHTML := ""
	if totalPages > 1 {
		paginationHTML = `<div class="pagination" style="margin-top: 24px; display: flex; justify-content: center; align-items: center; gap: 8px;">`
		// 上一页
		if page > 1 {
			paginationHTML += fmt.Sprintf(`<a href="?status=%s&page=%d&page_size=%d" class="btn btn-sm" style="text-decoration: none;">上一页</a>`, statusFilter, page-1, pageSize)
		}
		// 页码
		paginationHTML += fmt.Sprintf(`<span style="font-size: 14px; color: var(--v2-text-secondary);">第 %d / %d 页，共 %d 条</span>`, page, totalPages, totalFiltered)
		// 下一页
		if page < totalPages {
			paginationHTML += fmt.Sprintf(`<a href="?status=%s&page=%d&page_size=%d" class="btn btn-sm" style="text-decoration: none;">下一页</a>`, statusFilter, page+1, pageSize)
		}
		paginationHTML += `</div>`
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>IP管理 - Go 下载站</title>
	%s
	<link rel="stylesheet" href="/static/styles.css">
	<style>
		.ip-management { padding: 20px; }
		.ip-stats-cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 16px; margin-bottom: 24px; }
		.ip-stat-card { background: var(--v2-bg-elev); border: 1px solid var(--v2-border); border-radius: var(--v2-radius); padding: 20px; text-align: center; }
		.ip-stat-card .value { font-size: 28px; font-weight: 700; color: var(--v2-primary); margin-bottom: 4px; }
		.ip-stat-card .label { font-size: 13px; color: var(--v2-text-secondary); }
		.ip-list { display: grid; grid-template-columns: repeat(auto-fill, minmax(400px, 1fr)); gap: 16px; }
		.ip-card { background: var(--v2-bg-elev); border: 1px solid var(--v2-border); border-radius: var(--v2-radius); padding: 18px; transition: all 0.2s; }
		.ip-card:hover { box-shadow: var(--v2-shadow-md); transform: translateY(-2px); }
		.ip-card-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 14px; }
		.ip-address { font-size: 16px; font-weight: 600; color: var(--v2-text); display: flex; align-items: center; gap: 8px; }
		.ip-card-stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 10px; margin-bottom: 14px; padding: 12px; background: var(--v2-bg); border-radius: 8px; }
		.ip-stat-item { text-align: center; }
		.ip-stat-value { font-size: 14px; font-weight: 600; color: var(--v2-text); margin-bottom: 2px; word-break: break-all; }
		.ip-stat-label { font-size: 11px; color: var(--v2-text-muted); }
		.ip-card-actions { display: flex; gap: 8px; justify-content: flex-end; }
		.status-badge { padding: 3px 8px; border-radius: 4px; font-size: 12px; font-weight: 500; }
		.status-success { background: #d1fadf; color: #027a48; }
		.status-danger { background: #fee4e2; color: #b42318; }
		.btn { padding: 6px 14px; border: none; border-radius: 6px; font-size: 13px; font-weight: 500; cursor: pointer; transition: all 0.2s; text-decoration: none; display: inline-block; }
		.btn-sm { padding: 4px 10px; font-size: 12px; }
		.btn-primary { background: var(--v2-primary); color: #fff; }
		.btn-primary:hover { opacity: 0.9; }
		.btn-danger { background: #f04438; color: #fff; }
		.btn-danger:hover { background: #d92d20; }
		.btn-success { background: #12b76a; color: #fff; }
		.btn-success:hover { background: #039855; }
		.hot-rank-badge { display: inline-flex; align-items: center; justify-content: center; width: 22px; height: 22px; border-radius: 50%%; color: #fff; font-size: 12px; font-weight: 700; }
		.page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; }
		.page-title { font-size: 24px; font-weight: 700; color: var(--v2-text); }
		.modal-overlay { position: fixed; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0,0,0,0.5); display: none; align-items: center; justify-content: center; z-index: 1000; }
		.modal-overlay.active { display: flex; }
		.modal { background: var(--v2-bg-elev); border-radius: 12px; padding: 24px; width: 90%%; max-width: 500px; max-height: 80vh; overflow-y: auto; }
		.modal-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
		.modal-title { font-size: 18px; font-weight: 600; }
		.modal-close { background: none; border: none; font-size: 20px; cursor: pointer; color: var(--v2-text-secondary); }
		.form-group { margin-bottom: 16px; }
		.form-label { display: block; font-size: 14px; font-weight: 500; margin-bottom: 6px; color: var(--v2-text); }
		.form-input { width: 100%%; padding: 10px 12px; border: 1px solid var(--v2-border); border-radius: 8px; font-size: 14px; box-sizing: border-box; }
		.form-textarea { width: 100%%; padding: 10px 12px; border: 1px solid var(--v2-border); border-radius: 8px; font-size: 14px; min-height: 80px; box-sizing: border-box; resize: vertical; }
		.toast { position: fixed; top: 20px; left: 50%%; transform: translateX(-50%%); padding: 12px 24px; border-radius: 8px; color: #fff; font-size: 14px; z-index: 2000; opacity: 0; transition: opacity 0.3s; }
		.toast.show { opacity: 1; }
		.toast-success { background: #12b76a; }
		.toast-error { background: #f04438; }
	</style>
</head>
<body class="v2 admin-layout">
	<div class="admin-layout-wrapper">
		<!-- 侧边栏 -->
		%s

		<!-- 主内容区 -->
		<main class="admin-main">
			<div class="ip-management">
				<div class="page-header">
					<h1 class="page-title">🌐 IP下载管理</h1>
					<div style="display: flex; gap: 10px;">
						<button class="btn btn-danger" onclick="openAddBlockModal()">🚫 添加封禁IP</button>
						<button class="btn btn-primary" onclick="openLimitConfig()">⚙️ 限额配置</button>
						<button class="btn btn-primary" onclick="refreshIPList()">🔄 刷新</button>
					</div>
				</div>

				<div class="ip-stats-cards">
					<div class="ip-stat-card">
						<div class="value">%d</div>
						<div class="label">总IP数</div>
					</div>
					<div class="ip-stat-card">
						<div class="value">%d</div>
						<div class="label">已封禁IP</div>
					</div>
					<div class="ip-stat-card">
						<div class="value">%d</div>
						<div class="label">总下载次数</div>
					</div>
					<div class="ip-stat-card">
						<div class="value">%s</div>
						<div class="label">总下载流量</div>
					</div>
					<div class="ip-stat-card">
						<div class="value">%d</div>
						<div class="label">今日下载</div>
					</div>
					<div class="ip-stat-card">
						<div class="value">%s</div>
						<div class="label">今日流量</div>
					</div>
				</div>

				%s

				<div class="ip-list" id="ipList">
					%s
				</div>

				%s
			</div>
		</main>
	</div>

	<!-- 封禁IP弹窗 -->
	<div class="modal-overlay" id="blockModal">
		<div class="modal">
			<div class="modal-header">
				<h3 class="modal-title">封禁IP</h3>
				<button class="modal-close" onclick="closeModal('blockModal')">&times;</button>
			</div>
			<div class="form-group">
				<label class="form-label">IP地址</label>
				<input type="text" class="form-input" id="blockIPInput" placeholder="请输入要封禁的IP地址，如：192.168.1.100">
			</div>
			<div class="form-group">
				<label class="form-label">封禁原因</label>
				<textarea class="form-textarea" id="blockReasonInput" placeholder="请输入封禁原因，如：恶意重复下载、爬虫行为等"></textarea>
			</div>
			<div style="display: flex; gap: 10px; justify-content: flex-end;">
				<button class="btn" onclick="closeModal('blockModal')">取消</button>
				<button class="btn btn-danger" onclick="confirmBlockIP()">确认封禁</button>
			</div>
		</div>
	</div>

	<!-- IP限额配置弹窗 -->
	<div class="modal-overlay" id="limitConfigModal">
		<div class="modal" style="max-width: 600px;">
			<div class="modal-header">
				<h3 class="modal-title">⚙️ IP流量限额配置</h3>
				<button class="modal-close" onclick="closeModal('limitConfigModal')">&times;</button>
			</div>
			<div style="line-height: 1.8;">
				<div class="form-group">
					<label class="form-label" style="display: flex; align-items: center; gap: 8px;">
						<input type="checkbox" id="limitEnabled" style="width: auto;">
						启用IP流量限额
					</label>
				</div>
				<hr style="border: none; border-top: 1px solid var(--v2-border); margin: 16px 0;">
				<h4 style="margin: 0 0 12px 0; font-size: 15px;">📅 每日限额</h4>
				<div class="form-group">
					<label class="form-label">每日最大下载次数（0表示不限制）</label>
					<input type="number" class="form-input" id="dailyMaxDownloads" min="0" placeholder="例如：100">
				</div>
				<div class="form-group">
					<label class="form-label">每日最大下载流量（MB，0表示不限制）</label>
					<input type="number" class="form-input" id="dailyMaxBandwidthMB" min="0" placeholder="例如：10240（10GB）">
				</div>
				<hr style="border: none; border-top: 1px solid var(--v2-border); margin: 16px 0;">
				<h4 style="margin: 0 0 12px 0; font-size: 15px;">⏰ 每小时限额</h4>
				<div class="form-group">
					<label class="form-label">每小时最大下载次数（0表示不限制）</label>
					<input type="number" class="form-input" id="hourlyMaxDownloads" min="0" placeholder="例如：20">
				</div>
				<div class="form-group">
					<label class="form-label">每小时最大下载流量（MB，0表示不限制）</label>
					<input type="number" class="form-input" id="hourlyMaxBandwidthMB" min="0" placeholder="例如：1024（1GB）">
				</div>
				<hr style="border: none; border-top: 1px solid var(--v2-border); margin: 16px 0;">
				<h4 style="margin: 0 0 12px 0; font-size: 15px;">🚫 自动封禁</h4>
				<div class="form-group">
					<label class="form-label" style="display: flex; align-items: center; gap: 8px;">
						<input type="checkbox" id="autoBlock" style="width: auto;">
						超过限额自动封禁IP
					</label>
				</div>
				<div class="form-group">
					<label class="form-label">自动封禁原因</label>
					<input type="text" class="form-input" id="autoBlockReason" placeholder="例如：超过流量限额自动封禁">
				</div>
			</div>
			<div style="display: flex; gap: 10px; justify-content: flex-end; margin-top: 20px;">
				<button class="btn" onclick="closeModal('limitConfigModal')">取消</button>
				<button class="btn btn-primary" onclick="saveLimitConfig()">保存配置</button>
			</div>
		</div>
	</div>

	<div class="toast" id="toast"></div>

	<script src="/static/js/csrf.js"></script>
	<script>
		function showToast(message, type) {
			var toast = document.getElementById('toast');
			toast.textContent = message;
			toast.className = 'toast toast-' + type + ' show';
			setTimeout(function() {
				toast.className = 'toast';
			}, 3000);
		}

		function closeModal(modalId) {
			document.getElementById(modalId).classList.remove('active');
		}

		function openAddBlockModal() {
			document.getElementById('blockIPInput').value = '';
			document.getElementById('blockReasonInput').value = '';
			document.getElementById('blockModal').classList.add('active');
		}

		function blockIP(ip) {
			document.getElementById('blockIPInput').value = ip;
			document.getElementById('blockReasonInput').value = '';
			document.getElementById('blockModal').classList.add('active');
		}

		function confirmBlockIP() {
			var ip = document.getElementById('blockIPInput').value;
			var reason = document.getElementById('blockReasonInput').value;
			if (!ip.trim()) {
				showToast('请输入IP地址', 'error');
				return;
			}
			if (!reason.trim()) {
				showToast('请输入封禁原因', 'error');
				return;
			}
			fetch('/api/ip/block', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ ip: ip, reason: reason })
			})
			.then(function(res) { return res.json(); })
			.then(function(data) {
				if (data.success) {
					showToast('IP已封禁', 'success');
					closeModal('blockModal');
					setTimeout(function() { location.reload(); }, 1000);
				} else {
					showToast(data.message || '封禁失败', 'error');
				}
			})
			.catch(function() { showToast('网络错误', 'error'); });
		}

		function unblockIP(ip) {
			if (!confirm('确定要解封此IP吗？')) return;
			fetch('/api/ip/unblock', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ ip: ip })
			})
			.then(function(res) { return res.json(); })
			.then(function(data) {
				if (data.success) {
					showToast('IP已解封', 'success');
					setTimeout(function() { location.reload(); }, 1000);
				} else {
					showToast(data.message || '解封失败', 'error');
				}
			})
			.catch(function() { showToast('网络错误', 'error'); });
		}

		// XSS防护：HTML编码函数
		function escapeHTML(str) {
			if (!str) return '';
			var div = document.createElement('div');
			div.appendChild(document.createTextNode(str));
			return div.innerHTML;
		}

		function formatBytes(bytes) {
			if (bytes === 0) return '0 B';
			var k = 1024;
			var sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
			var i = Math.floor(Math.log(bytes) / Math.log(k));
			return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
		}

		function refreshIPList() {
			location.reload();
		}

		// 打开限额配置弹窗
		function openLimitConfig() {
			fetch('/api/ip/limit-config')
			.then(function(res) { return res.json(); })
			.then(function(data) {
				if (data.success && data.config) {
					var c = data.config;
					document.getElementById('limitEnabled').checked = c.enabled;
					document.getElementById('dailyMaxDownloads').value = c.daily_max_downloads || 0;
					document.getElementById('dailyMaxBandwidthMB').value = c.daily_max_bandwidth ? Math.round(c.daily_max_bandwidth / (1024*1024)) : 0;
					document.getElementById('hourlyMaxDownloads').value = c.hourly_max_downloads || 0;
					document.getElementById('hourlyMaxBandwidthMB').value = c.hourly_max_bandwidth ? Math.round(c.hourly_max_bandwidth / (1024*1024)) : 0;
					document.getElementById('autoBlock').checked = c.auto_block;
					document.getElementById('autoBlockReason').value = c.auto_block_reason || '';
					document.getElementById('limitConfigModal').classList.add('active');
				} else {
					showToast('获取配置失败', 'error');
				}
			})
			.catch(function() { showToast('网络错误', 'error'); });
		}

		// 保存限额配置
		function saveLimitConfig() {
			var dailyBandwidthMB = parseInt(document.getElementById('dailyMaxBandwidthMB').value) || 0;
			var hourlyBandwidthMB = parseInt(document.getElementById('hourlyMaxBandwidthMB').value) || 0;
			var config = {
				enabled: document.getElementById('limitEnabled').checked,
				daily_max_downloads: parseInt(document.getElementById('dailyMaxDownloads').value) || 0,
				daily_max_bandwidth: dailyBandwidthMB * 1024 * 1024,
				hourly_max_downloads: parseInt(document.getElementById('hourlyMaxDownloads').value) || 0,
				hourly_max_bandwidth: hourlyBandwidthMB * 1024 * 1024,
				auto_block: document.getElementById('autoBlock').checked,
				auto_block_reason: document.getElementById('autoBlockReason').value
			};
			fetch('/api/ip/limit-config/update', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify(config)
			})
			.then(function(res) { return res.json(); })
			.then(function(data) {
				if (data.success) {
					showToast('配置保存成功', 'success');
					closeModal('limitConfigModal');
				} else {
					showToast(data.message || '保存失败', 'error');
				}
			})
			.catch(function() { showToast('网络错误', 'error'); });
		}

		// 点击弹窗外部关闭
		document.querySelectorAll('.modal-overlay').forEach(function(overlay) {
			overlay.addEventListener('click', function(e) {
				if (e.target === overlay) {
					overlay.classList.remove('active');
				}
			});
		});
	</script>
</body>
</html>`,
		csrfTokenMeta,
		utils.GetAdminSidebar(r, config.AppConfig.Server.ServerName),
		totalIPs, blockedIPs, totalDownloads, utils.FormatFileSize(totalBandwidth),
		todayDownloads, utils.FormatFileSize(todayBandwidth),
		filterHTML,
		ipListHTML,
		paginationHTML)

	w.Write([]byte(html))
}

// API: 获取所有IP统计
func APIListIPStats(w http.ResponseWriter, r *http.Request) {
	// 检查管理员权限
	user := session.GetCurrentUser(r)
	if user == nil || user.Role != constants.RoleAdmin {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "需要管理员权限",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	allIPStats := GetAllIPStats()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"stats":   allIPStats,
		"total":   len(allIPStats),
	})
}

// API: 获取单个IP统计
func APIGetIPStats(w http.ResponseWriter, r *http.Request) {
	// 检查管理员权限
	user := session.GetCurrentUser(r)
	if user == nil || user.Role != constants.RoleAdmin {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "需要管理员权限",
		})
		return
	}

	ip := r.URL.Query().Get("ip")
	if ip == "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "缺少IP参数",
		})
		return
	}

	stats, exists := GetIPStats(ip)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if !exists {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "IP不存在",
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"stats":   stats,
	})
}

// API: 封禁IP
func APIBlockIP(w http.ResponseWriter, r *http.Request) {
	// 只允许POST请求
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "只允许POST请求",
		})
		return
	}

	// 检查管理员权限
	user := session.GetCurrentUser(r)
	if user == nil || user.Role != constants.RoleAdmin {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "需要管理员权限",
		})
		return
	}

	// 验证CSRF令牌
	if !utils.ValidateCSRFTokenFromRequest(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "CSRF令牌验证失败，请刷新页面后重试",
		})
		return
	}

	var req struct {
		IP     string `json:"ip"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "请求格式错误",
		})
		return
	}

	if req.IP == "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "IP不能为空",
		})
		return
	}

	// 验证IP地址格式
	if net.ParseIP(req.IP) == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "IP地址格式不正确",
		})
		return
	}

	// 限制封禁原因长度，防止过长输入
	if len(req.Reason) > 200 {
		req.Reason = req.Reason[:200]
	}

	if req.Reason == "" {
		req.Reason = "管理员手动封禁"
	}

	if err := BlockIP(req.IP, req.Reason); err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	// 记录操作日志
	utils.LogUserAction(r, "block_ip", fmt.Sprintf("封禁IP: %s, 原因: %s", req.IP, req.Reason))

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "IP已封禁",
	})
}

// API: 解封IP
func APIUnblockIP(w http.ResponseWriter, r *http.Request) {
	// 只允许POST请求
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "只允许POST请求",
		})
		return
	}

	// 检查管理员权限
	user := session.GetCurrentUser(r)
	if user == nil || user.Role != constants.RoleAdmin {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "需要管理员权限",
		})
		return
	}

	// 验证CSRF令牌
	if !utils.ValidateCSRFTokenFromRequest(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "CSRF令牌验证失败，请刷新页面后重试",
		})
		return
	}

	var req struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "请求格式错误",
		})
		return
	}

	if req.IP == "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "IP不能为空",
		})
		return
	}

	if err := UnblockIP(req.IP); err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	// 记录操作日志
	utils.LogUserAction(r, "unblock_ip", fmt.Sprintf("解封IP: %s", req.IP))

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "IP已解封",
	})
}

// API: 获取IP限额配置
func APIGetIPLimitConfig(w http.ResponseWriter, r *http.Request) {
	// 检查管理员权限
	user := session.GetCurrentUser(r)
	if user == nil || user.Role != constants.RoleAdmin {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "需要管理员权限",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"config":  config.AppConfig.IPLimit,
	})
}

// API: 更新IP限额配置
func APIUpdateIPLimitConfig(w http.ResponseWriter, r *http.Request) {
	// 只允许POST请求
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "只允许POST请求",
		})
		return
	}

	// 检查管理员权限
	user := session.GetCurrentUser(r)
	if user == nil || user.Role != constants.RoleAdmin {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "需要管理员权限",
		})
		return
	}

	// 验证CSRF令牌
	if !utils.ValidateCSRFTokenFromRequest(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "CSRF令牌验证失败，请刷新页面后重试",
		})
		return
	}

	var req config.IPLimitConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "请求格式错误",
		})
		return
	}

	// 更新配置
	config.AppConfig.IPLimit = req

	// 保存配置到文件
	if err := config.SaveConfig(); err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "保存配置失败: " + err.Error(),
		})
		return
	}

	// 记录操作日志
	utils.LogUserAction(r, "update_ip_limit_config", "更新IP流量限额配置")

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "IP限额配置已更新",
		"config":  config.AppConfig.IPLimit,
	})
}
