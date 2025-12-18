package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/session"
	"go-download-server/utils"
)

// 日志查看处理函数
func LogsHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil || sess.Role != constants.RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 记录日志
	utils.LogUserAction(r, "view_logs", "查看服务器日志")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 读取日志文件
	logFilePath := filepath.Join(config.AppConfig.Server.LogDir, config.AppConfig.Server.LogFile)
	logContent, err := os.ReadFile(logFilePath)
	if err != nil {
		// 构建错误日志条目
		logContent = []byte(fmt.Sprintf("[%s] [error] [system] [system] failed_to_read_log_file %v\n",
			time.Now().Format("2006-01-02 15:04:05"), err))
	}

	// 解析日志内容为结构化格式
	logEntries := parseLogContent(string(logContent))

	// 构建HTML页面
	html := `<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>服务器日志 - ` + constants.ServerName + `</title>
	<style>
		body {
			font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
			background-color: #f8f9fa;
			margin: 0;
			padding: 0;
			color: #333;
		}
		.container {
			max-width: 1200px;
			margin: 0 auto;
			padding: 20px;
		}
		header {
			background-color: #4CAF50;
			color: white;
			padding: 15px;
			border-radius: 8px;
			margin-bottom: 20px;
			box-shadow: 0 2px 10px rgba(0,0,0,0.1);
		}
		.header-content {
			display: flex;
			justify-content: space-between;
			align-items: center;
		}
		nav {
			background-color: white;
			padding: 15px;
			border-radius: 8px;
			margin-bottom: 20px;
			box-shadow: 0 2px 10px rgba(0,0,0,0.05);
			border: 1px solid #e9ecef;
		}
		.nav-links {
			display: flex;
			gap: 15px;
		}
		.nav-links a {
			text-decoration: none;
			color: #333;
			padding: 8px 16px;
			border-radius: 6px;
			transition: all 0.3s ease;
			font-weight: 500;
		}
		.nav-links a:hover {
			background-color: #e0e0e0;
			color: #4CAF50;
			transform: translateY(-1px);
		}
		.logs-panel {
			background-color: white;
			padding: 30px;
			border-radius: 8px;
			box-shadow: 0 2px 10px rgba(0,0,0,0.05);
			border: 1px solid #e9ecef;
		}
		.logs-panel h2 {
			color: #4CAF50;
			margin-bottom: 25px;
			font-size: 24px;
			border-bottom: 2px solid #e9ecef;
			padding-bottom: 10px;
		}
		.logs-content {
			background-color: #f8f9fa;
			border: 1px solid #e9ecef;
			border-radius: 8px;
			padding: 20px;
			font-family: 'Monaco', 'Consolas', 'Courier New', Courier, monospace;
			font-size: 13px;
			line-height: 1.8;
			overflow: auto;
			max-height: 600px;
			color: #333333;
			box-shadow: inset 0 2px 4px rgba(0, 0, 0, 0.05);
		}
		/* 日志行高亮样式 */
		.log-entry {
			margin-bottom: 8px;
			padding: 12px 16px;
			border-radius: 6px;
			transition: all 0.3s ease;
			border-left: 3px solid transparent;
			background-color: white;
			box-shadow: 0 1px 3px rgba(0,0,0,0.05);
			margin-left: 0;
		}
		.log-entry:hover {
			background-color: #f0f8ff;
			transform: translateX(5px);
			box-shadow: 0 2px 6px rgba(0,0,0,0.1);
		}
		/* 日志级别颜色 */
		.log-entry.info {
			border-left-color: #2c7ad2;
		}
		.log-entry.success {
			border-left-color: #27ae60;
		}
		.log-entry.warning {
			border-left-color: #f39c12;
		}
		.log-entry.error {
			border-left-color: #e74c3c;
		}
		.log-entry.debug {
			border-left-color: #9b59b6;
		}
		/* 日志时间样式 */
		.log-time {
			color: #2c7ad2;
			font-weight: 600;
			font-family: 'Monaco', 'Consolas', 'Courier New', Courier, monospace;
			margin-right: 15px;
		}
		/* 日志消息样式 */
		.log-message {
			color: #333333;
			font-family: 'Monaco', 'Consolas', 'Courier New', Courier, monospace;
			font-weight: 500;
		}
		/* 日志级别样式 */
		.log-level {
			font-weight: bold;
			margin-right: 10px;
			padding: 2px 8px;
			border-radius: 3px;
			font-size: 11px;
			text-transform: uppercase;
		}
		.log-level.info {
			color: #2c7ad2;
			background-color: #e3f2fd;
		}
		.log-level.success {
			color: #27ae60;
			background-color: #e8f5e8;
		}
		.log-level.warning {
			color: #f39c12;
			background-color: #fff3e0;
		}
		.log-level.error {
			color: #e74c3c;
			background-color: #ffebee;
		}
		.log-level.debug {
			color: #9b59b6;
			background-color: #f3e5f5;
		}
		/* 日志用户名样式 */
		.log-username {
			color: #d35400;
			font-weight: bold;
			margin-right: 10px;
		}
		/* 日志角色样式 */
		.log-role {
			color: #8e44ad;
			font-weight: bold;
			margin-right: 10px;
			padding: 2px 8px;
			border-radius: 3px;
			font-size: 11px;
			background-color: #f0e6fa;
		}
		/* 日志操作样式 */
		.log-action {
			color: #3498db;
			font-weight: bold;
			margin-right: 10px;
		}
		/* 日志详情样式 */
		.log-details {
			color: #666;
			font-weight: normal;
		}
		/* 按钮样式 */
		.btn {
			padding: 10px 20px;
			border: none;
			border-radius: 8px;
			cursor: pointer;
			text-decoration: none;
			font-size: 14px;
			font-weight: 500;
			transition: all 0.3s ease;
			text-align: center;
			display: inline-block;
			margin-right: 10px;
		}
		.btn-secondary {
			background-color: #6c757d;
			color: white;
			box-shadow: 0 2px 4px rgba(108, 117, 125, 0.2);
		}
		.btn-secondary:hover {
			background-color: #5a6268;
			transform: translateY(-1px);
			box-shadow: 0 4px 8px rgba(108, 117, 125, 0.3);
		}
		.btn-primary {
			background-color: #4CAF50;
			color: white;
			box-shadow: 0 2px 4px rgba(76, 175, 80, 0.2);
		}
		.btn-primary:hover {
			background-color: #45a049;
			transform: translateY(-1px);
			box-shadow: 0 4px 8px rgba(76, 175, 80, 0.3);
		}
		/* 控制面板样式 */
		.logs-controls {
			display: flex;
			justify-content: space-between;
			align-items: center;
			margin-bottom: 20px;
			padding: 20px;
			background-color: #f8f9fa;
			border-radius: 8px;
			border: 1px solid #e9ecef;
		}
		.logs-controls .filter-group {
			display: flex;
			gap: 15px;
			align-items: center;
		}
		.logs-controls label {
			font-weight: 600;
			color: #555;
			font-size: 14px;
		}
		.logs-controls select {
			padding: 10px 15px;
			border: 1px solid #ced4da;
			border-radius: 6px;
			font-size: 14px;
			background-color: white;
			box-shadow: 0 2px 4px rgba(0,0,0,0.05);
			transition: all 0.3s ease;
		}
		.logs-controls select:focus {
			outline: none;
			border-color: #4CAF50;
			box-shadow: 0 0 0 3px rgba(76, 175, 80, 0.1);
		}
		/* 页脚样式 */
		.footer {
			margin-top: 20px;
			text-align: center;
			color: #666;
			font-size: 14px;
			padding: 15px;
			border-top: 1px solid #e9ecef;
		}
		/* 加载状态 */
		.loading {
			text-align: center;
			padding: 20px;
			color: #666;
		}
		/* 日志搜索功能 */
		.search-box {
			margin-bottom: 20px;
			position: relative;
		}
		.search-box input {
			width: 100%;
			padding: 14px 20px;
			border: 1px solid #ced4da;
			border-radius: 8px;
			font-size: 14px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.05);
			transition: all 0.3s ease;
		}
		.search-box input:focus {
			outline: none;
			border-color: #4CAF50;
			box-shadow: 0 0 0 3px rgba(76, 175, 80, 0.1);
		}
		/* 日志统计 */
		.logs-stats {
			display: flex;
			gap: 20px;
			margin-bottom: 20px;
			flex-wrap: wrap;
		}
		.stat-item {
			background-color: white;
			padding: 15px 20px;
			border-radius: 8px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.05);
			border-left: 3px solid #4CAF50;
			min-width: 120px;
			text-align: center;
		}
		.stat-number {
			font-size: 24px;
			font-weight: bold;
			color: #4CAF50;
		}
		.stat-label {
			font-size: 12px;
			color: #666;
			margin-top: 5px;
		}
		/* 分页控件 */
		.pagination {
			margin-top: 20px;
			text-align: center;
		}
		.pagination button {
			margin: 0 5px;
			padding: 8px 16px;
			border: 1px solid #ced4da;
			border-radius: 6px;
			background-color: white;
			cursor: pointer;
			transition: all 0.3s ease;
		}
		.pagination button:hover {
			background-color: #f8f9fa;
			border-color: #4CAF50;
		}
		.pagination button.active {
			background-color: #4CAF50;
			color: white;
			border-color: #4CAF50;
		}
	</style>
	<script>
		// 页面加载完成后执行
		document.addEventListener('DOMContentLoaded', function() {
			// 日志搜索功能
			const searchInput = document.getElementById('logSearch');
			const logEntries = document.querySelectorAll('.log-entry');
			const totalLogs = logEntries.length;
			
			// 更新日志总数显示
			document.getElementById('totalLogs').textContent = totalLogs;
			
			if (searchInput && logEntries.length > 0) {
				searchInput.addEventListener('input', function(e) {
					const searchTerm = e.target.value.toLowerCase();
					let visibleCount = 0;
					logEntries.forEach(entry => {
						const text = entry.textContent.toLowerCase();
						if (text.includes(searchTerm)) {
							entry.style.display = 'block';
							visibleCount++;
						} else {
							entry.style.display = 'none';
						}
					});
					// 更新可见日志数量
					document.getElementById('visibleLogs').textContent = visibleCount;
				});
			}
			
			// 日志级别筛选功能
			const levelFilter = document.getElementById('logLevel');
			if (levelFilter && logEntries.length > 0) {
				levelFilter.addEventListener('change', function(e) {
					const selectedLevel = e.target.value;
					let visibleCount = 0;
					logEntries.forEach(entry => {
						if (selectedLevel === 'all') {
							entry.style.display = 'block';
							visibleCount++;
						} else {
							if (entry.classList.contains(selectedLevel)) {
								entry.style.display = 'block';
								visibleCount++;
							} else {
								entry.style.display = 'none';
							}
						}
					});
					// 更新可见日志数量
					document.getElementById('visibleLogs').textContent = visibleCount;
				});
			}
			
			// 确保页面加载完成后滚动到顶部，显示最新日志
			const logsContent = document.querySelector('.logs-content');
			if (logsContent) {
				logsContent.scrollTop = 0;
			}
		});
	</script>
</head>
<body>
	<div class="container">
		<header>
			<div class="header-content">
				<h1>📦 ` + constants.ServerName + `</h1>
				<div>
					` + utils.GetCurrentUserInfo(r) + `
				</div>
			</div>
		</header>

		<nav>
			<div class="nav-links">
				<a href="/files">文件列表</a>
				<a href="/upload">上传文件</a>
				<a href="/admin">管理员</a>
			</div>
		</nav>

		<div class="logs-panel">
			<h2>服务器日志</h2>
			
			<!-- 日志统计 -->
			<div class="logs-stats">
				<div class="stat-item">
					<div class="stat-number" id="totalLogs">0</div>
					<div class="stat-label">总日志数</div>
				</div>
				<div class="stat-item">
					<div class="stat-number" id="visibleLogs">0</div>
					<div class="stat-label">可见日志</div>
				</div>
			</div>
			
			<!-- 日志搜索 -->
			<div class="search-box">
				<input type="text" id="logSearch" placeholder="搜索日志内容...">
			</div>
			
			<!-- 日志筛选 -->
			<div class="logs-controls">
				<div class="filter-group">
					<label for="logLevel">筛选级别：</label>
					<select id="logLevel">
						<option value="all">所有级别</option>
						<option value="info">信息</option>
						<option value="success">成功</option>
						<option value="warning">警告</option>
						<option value="error">错误</option>
						<option value="debug">调试</option>
					</select>
				</div>
				
				<div class="filter-group">
					<a href="/logs" class="btn btn-primary">刷新日志</a>
					<a href="/admin" class="btn btn-secondary">返回管理员面板</a>
				</div>
			</div>
			
			<!-- 日志内容 -->
			<div class="logs-content">` + logEntries + `</div>
		</div>

		<div class="footer">
			<p>版本: ` + constants.Version + ` | 开发者: ` + constants.Developer + `</p>
		</div>
	</div>
</body>
</html>`

	w.Write([]byte(html))
}

// 解析日志内容为结构化HTML
func parseLogContent(content string) string {
	if content == "" {
		return "<div class='log-entry info'>日志文件为空</div>"
	}

	lines := strings.Split(content, "\n")
	var html strings.Builder

	// 倒序遍历日志行，使最新的日志显示在前面
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// 解析日志格式: [时间] [级别] [用户名] [角色] 操作 详情
		// 示例: [2025-12-18 15:30:00] [info] [admin] [admin] login 登录成功
		var timestamp, level, username, role, action, details string

		// 使用正则表达式解析日志行
		re := regexp.MustCompile(`^\[(.*?)\]\s*\[(.*?)\]\s*\[(.*?)\]\s*\[(.*?)\]\s*(\w+)\s*(.*)$`)
		matches := re.FindStringSubmatch(line)

		if len(matches) >= 7 {
			timestamp = matches[1]
			level = matches[2]
			username = matches[3]
			role = matches[4]
			action = matches[5]
			details = matches[6]
		} else {
			// 旧格式日志或不符合预期的日志，直接显示
			html.WriteString(fmt.Sprintf(`<div class="log-entry info"><span class="log-time">%s</span><span class="log-message">%s</span></div>\n`,
				time.Now().Format("2006-01-02 15:04:05"), line))
			continue
		}

		// 转换角色为中文名称
		roleName := utils.GetRoleNameByString(role)

		// 转换操作名称为中文（如果需要）
		actionName := action

		// 构建日志HTML条目
		html.WriteString(fmt.Sprintf(`<div class="log-entry %s">`, strings.ToLower(level)))
		html.WriteString(fmt.Sprintf(`<span class="log-time">%s</span>`, timestamp))
		html.WriteString(fmt.Sprintf(`<span class="log-level %s">%s</span>`, strings.ToLower(level), strings.ToUpper(level)))
		html.WriteString(fmt.Sprintf(`<span class="log-username">%s</span>`, username))
		html.WriteString(fmt.Sprintf(`<span class="log-role">%s</span>`, roleName))
		html.WriteString(fmt.Sprintf(`<span class="log-action">%s</span>`, actionName))
		html.WriteString(fmt.Sprintf(`<span class="log-details">%s</span>`, details))
		html.WriteString(`</div>`)
	}

	return html.String()
}
