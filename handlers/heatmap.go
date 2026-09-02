package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/session"
	"go-download-server/utils"
)

// 辅助函数：安全地将JSON数据嵌入JavaScript单引号字符串
// 顺序至关重要：先转义反斜杠，再转义单引号，否则已转义的 \' 会被二次转义破坏
func safeJSON(jsonData []byte) string {
	jsonStr := string(jsonData)
	// 1. 转义反斜杠
	jsonStr = strings.ReplaceAll(jsonStr, `\`, `\\`)
	// 2. 转义单引号，防止JavaScript字符串提前结束
	jsonStr = strings.ReplaceAll(jsonStr, `'`, `\'`)
	// 3. 转义 </script>，防止提前关闭脚本标签（\/ 在 JSON 字符串中合法）
	jsonStr = strings.ReplaceAll(jsonStr, "</", `<\/`)
	// 4. 转义换行符
	jsonStr = strings.ReplaceAll(jsonStr, "\n", "\\n")
	// 5. 转义回车符
	jsonStr = strings.ReplaceAll(jsonStr, "\r", "\\r")
	return jsonStr
}

// HeatmapHandler 处理热力图页面请求
func HeatmapHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}
	if sess.Role != constants.RoleAdmin && sess.Role != constants.RoleSubAdmin {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/?msg=您没有权限访问该页面", http.StatusFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 获取热力图数据
	heatmapData := GetHeatmapData()
	if heatmapData == nil {
		heatmapData = []HeatmapPoint{}
	}

	dataJSON, err := json.MarshalIndent(heatmapData, "", "  ")
	if err != nil {
		log.Printf("热力图数据JSON编码失败: %v", err)
		dataJSON = []byte(`[]`)
	}

	// 获取文件统计数据
	fileStats := GetAllFileStats()
	if fileStats == nil {
		fileStats = make(map[string]*FileStats)
	}

	fileStatsJSON, err := json.MarshalIndent(fileStats, "", "  ")
	if err != nil {
		log.Printf("文件统计数据JSON编码失败: %v", err)
		fileStatsJSON = []byte(`{}`)
	}

	// 构建HTML页面
	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=5.0, minimum-scale=1.0">
	<title>热力图 - ` + config.AppConfig.Server.ServerName + `</title>
	<link rel="icon" href="data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 100 100%22><text y=%22.9em%22 font-size=%2290%22>📦</text></svg>">
	<link rel="stylesheet" href="/static/styles.css">
	<script src="/static/lib/chart.umd.min.js"></script>
	<style>
		/* 热力图页面样式 */
		.heatmap-container {
			background: #fff;
			border-radius: 12px;
			padding: 24px;
			box-shadow: 0 1px 3px rgba(0,0,0,0.08);
			border: 1px solid #e5e7eb;
			margin-bottom: 24px;
		}
		.chart-container {
			position: relative;
			height: 400px;
			margin-bottom: 24px;
		}
		.stats-table {
			width: 100%;
			border-collapse: collapse;
			margin-top: 16px;
		}
		.stats-table th {
			background: #f9fafb;
			padding: 12px 16px;
			text-align: left;
			font-weight: 600;
			color: #374151;
			border-bottom: 2px solid #e5e7eb;
			font-size: 14px;
		}
		.stats-table td {
			padding: 12px 16px;
			border-bottom: 1px solid #f3f4f6;
			color: #4b5563;
			font-size: 14px;
		}
		.stats-table tr:hover {
			background: #f9fafb;
		}
		.stats-section-title {
			font-size: 18px;
			font-weight: 600;
			color: #111827;
			margin-bottom: 16px;
			padding-bottom: 12px;
			border-bottom: 1px solid #e5e7eb;
		}
		.empty-state {
			text-align: center;
			padding: 60px 20px;
			color: #9ca3af;
		}
		.empty-state-icon {
			font-size: 48px;
			margin-bottom: 16px;
		}
	</style>
</head>
<body class="v2 admin-layout">
	<div class="admin-layout-wrapper">
		` + utils.GetAdminSidebar(r, config.AppConfig.Server.ServerName) + `
		<main class="admin-main">
			<div class="admin-page-header">
				<h1 class="admin-page-title">活动热力图</h1>
				<p class="admin-page-desc">展示最近7天的文件分享和下载活动</p>
			</div>

			<!-- 分享/下载趋势图 -->
			<div class="heatmap-container">
				<div class="stats-section-title">📊 活动趋势</div>
				<div class="chart-container">
					<canvas id="activityChart"></canvas>
				</div>
			</div>

			<!-- 文件统计表格 -->
			<div class="heatmap-container">
				<div class="stats-section-title">📋 文件统计信息</div>
				<table class="stats-table">
					<thead>
						<tr>
							<th>文件路径</th>
							<th>分享次数</th>
							<th>下载次数</th>
							<th>最后分享时间</th>
							<th>最后下载时间</th>
						</tr>
					</thead>
					<tbody id="fileStatsBody">
						<tr>
							<td colspan="5" class="empty-state">
								<div class="empty-state-icon">📁</div>
								<div>暂无文件统计数据</div>
							</td>
						</tr>
					</tbody>
				</table>
			</div>
		</main>
	</div>

	<script>
		// 解析文件统计数据
		let fileStats = JSON.parse('` + safeJSON(fileStatsJSON) + `') || {};
		if (typeof fileStats !== 'object' || fileStats === null) {
			console.error('文件统计数据格式错误，使用空对象');
			fileStats = {};
		}

		// 渲染文件统计表格
		function renderFileStats() {
			const tbody = document.getElementById('fileStatsBody');
			if (!tbody) return;

			try {
				let fileStatsArray = [];
				for (const path in fileStats) {
					if (!Object.prototype.hasOwnProperty.call(fileStats, path)) continue;
					const stats = fileStats[path];
					if (stats) {
						stats.path = path;
						fileStatsArray.push(stats);
					}
				}

				if (fileStatsArray.length === 0) {
					tbody.innerHTML = '<tr><td colspan="5" class="empty-state"><div class="empty-state-icon">📁</div><div>暂无文件统计数据</div></td></tr>';
					return;
				}

				// 按最后下载时间倒序排序
				fileStatsArray.sort(function(a, b) {
					const timeA = a.last_download_time ? new Date(a.last_download_time).getTime() : 0;
					const timeB = b.last_download_time ? new Date(b.last_download_time).getTime() : 0;
					return timeB - timeA;
				});

				// 生成表格行
				let html = '';
				fileStatsArray.forEach(function(stats) {
					const path = stats.path;
					const shareCount = stats.share_count || 0;
					const downloadCount = stats.download_count || 0;
					const lastShareTime = (shareCount > 0 && stats.last_share_time) ? new Date(stats.last_share_time).toLocaleString() : '-';
					const lastDownloadTime = stats.last_download_time ? new Date(stats.last_download_time).toLocaleString() : '-';

					html += '<tr>';
					html += '<td>' + escapeHtml(path) + '</td>';
					html += '<td>' + shareCount + '</td>';
					html += '<td>' + downloadCount + '</td>';
					html += '<td>' + lastShareTime + '</td>';
					html += '<td>' + lastDownloadTime + '</td>';
					html += '</tr>';
				});
				tbody.innerHTML = html;
			} catch (error) {
				console.error('处理文件统计数据时出错:', error);
				tbody.innerHTML = '<tr><td colspan="5" style="color: #ef4444; text-align: center; padding: 20px;">处理数据时出错</td></tr>';
			}
		}

		// HTML转义函数
		function escapeHtml(text) {
			const div = document.createElement('div');
			div.textContent = text;
			return div.innerHTML;
		}

		// 解析热力图数据
		let heatmapData = JSON.parse('` + safeJSON(dataJSON) + `') || [];
		if (!Array.isArray(heatmapData)) {
			console.error('热力图数据格式错误，使用空数组');
			heatmapData = [];
		}

		// 处理数据，按天分组
		const dailyData = {};
		const today = new Date();

		// 初始化最近7天的数据
		for (let i = 6; i >= 0; i--) {
			const date = new Date(today);
			date.setDate(today.getDate() - i);
			const dateStr = date.toISOString().split('T')[0];
			dailyData[dateStr] = { share: 0, download: 0 };
		}

		// 统计每天的分享和下载次数
		try {
			heatmapData.forEach(point => {
				if (!point || !point.timestamp || !point.type) return;
				const date = new Date(point.timestamp);
				if (isNaN(date.getTime())) return;
				const dateStr = date.toISOString().split('T')[0];
				if (dailyData[dateStr]) {
					if (point.type === 'share' || point.type === 'download') {
						dailyData[dateStr][point.type]++;
					}
				}
			});
		} catch (error) {
			console.error('处理热力图数据时出错:', error);
		}

		// 准备图表数据
		const labels = Object.keys(dailyData);
		const shareData = Object.values(dailyData).map(d => d.share);
		const downloadData = Object.values(dailyData).map(d => d.download);

		// 创建图表
		function createChart() {
			const ctx = document.getElementById('activityChart');
			if (!ctx) {
				console.error('找不到图表画布元素');
				return;
			}

			// 检查Chart.js是否加载
			if (typeof Chart === 'undefined') {
				console.error('Chart.js未加载，使用降级方案');
				showFallbackChart();
				return;
			}

			try {
				new Chart(ctx.getContext('2d'), {
					type: 'bar',
					data: {
						labels: labels,
						datasets: [
							{
								label: '分享次数',
								data: shareData,
								backgroundColor: 'rgba(59, 130, 246, 0.7)',
								borderColor: 'rgba(59, 130, 246, 1)',
								borderWidth: 1,
								borderRadius: 4
							},
							{
								label: '下载次数',
								data: downloadData,
								backgroundColor: 'rgba(16, 185, 129, 0.7)',
								borderColor: 'rgba(16, 185, 129, 1)',
								borderWidth: 1,
								borderRadius: 4
							}
						]
					},
					options: {
						responsive: true,
						maintainAspectRatio: false,
						scales: {
							y: {
								beginAtZero: true,
								ticks: {
									stepSize: 1,
									color: '#6b7280'
								},
								grid: {
									color: '#f3f4f6'
								}
							},
							x: {
								ticks: {
									color: '#6b7280'
								},
								grid: {
									display: false
								}
							}
						},
						plugins: {
							legend: {
								position: 'top',
								labels: {
									color: '#374151',
									usePointStyle: true,
									padding: 20
								}
							},
							tooltip: {
								backgroundColor: '#1f2937',
								titleColor: '#fff',
								bodyColor: '#e5e7eb',
								padding: 12,
								cornerRadius: 8
							}
						}
					}
				});
			} catch (error) {
				console.error('创建图表时出错:', error);
				showFallbackChart();
			}
		}

		// 降级方案：使用HTML表格显示数据
		function showFallbackChart() {
			const container = document.querySelector('.chart-container');
			if (!container) return;

			let html = '<div style="padding: 20px;">';
			html += '<h4 style="margin-bottom: 16px; color: #374151;">📊 活动趋势（图表加载失败，使用表格显示）</h4>';
			html += '<table style="width: 100%; border-collapse: collapse;">';
			html += '<thead><tr style="background: #f9fafb;">';
			html += '<th style="padding: 10px; text-align: left; border-bottom: 2px solid #e5e7eb;">日期</th>';
			html += '<th style="padding: 10px; text-align: left; border-bottom: 2px solid #e5e7eb;">分享次数</th>';
			html += '<th style="padding: 10px; text-align: left; border-bottom: 2px solid #e5e7eb;">下载次数</th>';
			html += '</tr></thead><tbody>';

			labels.forEach((label, index) => {
				html += '<tr>';
				html += '<td style="padding: 10px; border-bottom: 1px solid #f3f4f6;">' + label + '</td>';
				html += '<td style="padding: 10px; border-bottom: 1px solid #f3f4f6;">' + shareData[index] + '</td>';
				html += '<td style="padding: 10px; border-bottom: 1px solid #f3f4f6;">' + downloadData[index] + '</td>';
				html += '</tr>';
			});

			html += '</tbody></table></div>';
			container.innerHTML = html;
		}

		// 页面加载完成后初始化
		document.addEventListener('DOMContentLoaded', function() {
			renderFileStats();
			createChart();
		});

		// 如果DOM已经加载完成，直接执行
		if (document.readyState !== 'loading') {
			renderFileStats();
			createChart();
		}
	</script>
</body>
</html>`

	w.Write([]byte(html))
}
