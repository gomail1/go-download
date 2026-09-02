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

// 操作类型中文映射
func getActionNameCN(action string) string {
	actionMap := map[string]string{
		// 认证相关
		"login":               "登录",
		"logout":              "退出登录",
		"login_attempt":       "登录尝试",
		"login_attempt_failed": "登录失败",
		"login_attempt_debug":  "登录调试",
		"register":            "注册",

		// 文件相关
		"upload":              "上传文件",
		"upload_file":         "上传文件",
		"download":            "下载文件",
		"download_file":       "下载文件",
		"delete":              "删除文件",
		"batch_delete":        "批量删除文件",
		"rename":              "重命名文件",
		"mkdir":               "创建目录",
		"view_files":          "查看文件列表",
		"approve":             "审核文件",
		"reject":              "拒绝文件",
		"api_request":         "API请求",

		// 用户管理
		"add_user":            "添加用户",
		"delete_user":         "删除用户",
		"change_password":     "修改密码",
		"update_user":         "更新用户",

		// IP管理
		"block_ip":            "封禁IP",
		"unblock_ip":          "解封IP",
		"update_ip_limit_config": "更新IP限额配置",

		// 分类管理
		"create_category":     "创建分类",
		"update_category":     "更新分类",
		"delete_category":     "删除分类",
		"set_file_category":   "设置文件分类",

		// 系统相关
		"view_server_info":    "查看服务器信息",
		"view_logs":           "查看操作日志",
		"view_heatmap":        "查看热力图",
		"view_ip_management":  "查看IP管理",
		"download_blocked":    "下载被阻止",
		"download_limited":    "下载被限制",
		"ip_auto_blocked":     "IP自动封禁",
		"alert_triggered":     "告警触发",
	}

	if cnName, exists := actionMap[action]; exists {
		return cnName
	}
	return action
}

// 日志查看处理函数
func LogsHandler(w http.ResponseWriter, r *http.Request) {
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

	// 记录日志
	utils.LogUserAction(r, "view_logs", "查看服务器日志")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 读取最新的日志文件（支持日志轮转）
	logDir := config.AppConfig.Server.LogDir
	logFileName := config.AppConfig.Server.LogFile
	logBaseName := strings.TrimSuffix(logFileName, ".log")

	// 查找最新的日志文件
	var targetLogPath string
	var latestLogTime time.Time
	var selectedDate time.Time
	var logContent []byte

	// 检查是否有日期参数
	dateParam := r.URL.Query().Get("date")
	if dateParam != "" {
		// 解析日期参数（格式：yyyy-mm-dd）
		parsedDate, err := time.Parse("2006-01-02", dateParam)
		if err == nil {
			selectedDate = parsedDate
		}
	}

	files, err := os.ReadDir(logDir)
	if err != nil {
		// 构建错误日志条目
		logContentBytes := []byte(fmt.Sprintf("[%s] [error] [system] [system] 读取日志目录失败 %v\n",
			time.Now().Format("2006-01-02 15:04:05"), err))
		// 解析并输出错误日志
		logEntries := parseLogContent(string(logContentBytes))
		// 构建HTML页面
		html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=5.0, minimum-scale=1.0">
	<title>服务器日志 - %s</title>
	<link rel="icon" href="data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><text y=".9em" font-size="90">📦</text></svg>">
	<link rel="stylesheet" href="/static/styles.css">

	<script>
		function loadDateLogs(date) {
			if (date) {
				window.location.href = '/logs?date=' + date;
			}
		}
	</script>
</head>
<body class="v2 admin-layout">
		<div class="admin-layout-wrapper">
			%s
			<main class="admin-main">
				<div class="admin-page-header">
					<h1 class="admin-page-title">服务器日志</h1>
					<p class="admin-page-desc">查看和筛选服务器运行日志</p>
				</div>
		<div class="upload-card-v2">
			<div class="logs-stats-v2" style="display: flex; gap: 24px; margin-bottom: 20px;">
				<div style="text-align: center;">
					<div style="font-size: 24px; font-weight: 700; color: var(--v2-primary);" id="totalLogs">1</div>
					<div style="font-size: 12px; color: var(--v2-text-muted);">总日志数</div>
				</div>
				<div style="text-align: center;">
					<div style="font-size: 24px; font-weight: 700; color: var(--v2-success);" id="visibleLogs">1</div>
					<div style="font-size: 12px; color: var(--v2-text-muted);">可见日志</div>
				</div>
			</div>
			<div class="logs-controls-v2" style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; flex-wrap: wrap; gap: 12px;">
				<div class="filter-group-v2" style="display: flex; align-items: center; gap: 12px; flex-wrap: wrap;">
					<label for="logLevel" style="font-size: 13px; color: var(--v2-text-secondary);">筛选级别：</label>
					<select id="logLevel" style="padding: 8px 12px; border: 1px solid var(--v2-border); border-radius: 8px; font-size: 13px; color: var(--v2-text); background: var(--v2-bg-elev);">
						<option value="all">所有级别</option>
						<option value="info">信息</option>
						<option value="success">成功</option>
						<option value="warning">警告</option>
						<option value="error">错误</option>
						<option value="debug">调试</option>
					</select>
					<label for="logDate" style="font-size: 13px; color: var(--v2-text-secondary);">选择日期：</label>
					<input type="date" id="logDate" onchange="loadDateLogs(this.value)" value="%s" style="padding: 8px 12px; border: 1px solid var(--v2-border); border-radius: 8px; font-size: 13px; color: var(--v2-text);">
				</div>
				<div class="filter-group-v2" style="display: flex; gap: 8px;">
					<a href="/logs" class="btn-v2 btn-primary-v2 btn-sm-v2">刷新日志</a>
					<a href="/admin" class="btn-v2 btn-secondary-v2 btn-sm-v2">返回管理员面板</a>
				</div>
			</div>
			<div class="logs-content-v2">%s</div>
		</div>
	</main>
		</div>
	</body>
</html>`, config.AppConfig.Server.ServerName, utils.GetAdminSidebar(r, config.AppConfig.Server.ServerName), time.Now().Format("2006-01-02"), logEntries)
		w.Write([]byte(html))
		return
	}

	// 收集所有存在日志的日期
	availableDates := []string{}
	latestLogTime = time.Time{}

	// 遍历所有日志文件，收集可用日期
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		name := file.Name()
		if !strings.HasPrefix(name, logBaseName+"_") || !strings.HasSuffix(name, ".log") {
			continue
		}

		// 解析文件名中的日期
		dateStr := strings.TrimPrefix(strings.TrimSuffix(name, ".log"), logBaseName+"_")
		fileTime, err := time.Parse("20060102", dateStr)
		if err != nil {
			continue
		}

		// 添加到可用日期列表
		formattedDate := fileTime.Format("2006-01-02")
		availableDates = append(availableDates, formattedDate)

		// 更新最新日志文件信息
		if fileTime.After(latestLogTime) {
			latestLogTime = fileTime
		}
	}

	// 然后查找目标日期的日志文件
	targetLogPath = ""
	if !selectedDate.IsZero() {
		// 用户选择了日期，查找对应的日志文件
		for _, file := range files {
			if file.IsDir() {
				continue
			}

			name := file.Name()
			if !strings.HasPrefix(name, logBaseName+"_") || !strings.HasSuffix(name, ".log") {
				continue
			}

			// 解析文件名中的日期
			dateStr := strings.TrimPrefix(strings.TrimSuffix(name, ".log"), logBaseName+"_")
			fileTime, err := time.Parse("20060102", dateStr)
			if err != nil {
				continue
			}

			// 检查是否匹配用户选择的日期
			if fileTime.Year() == selectedDate.Year() && fileTime.Month() == selectedDate.Month() && fileTime.Day() == selectedDate.Day() {
				targetLogPath = filepath.Join(logDir, name)
				break
			}
		}
	} else {
		// 用户未选择日期，查找最新的日志文件
		for _, file := range files {
			if file.IsDir() {
				continue
			}

			name := file.Name()
			if !strings.HasPrefix(name, logBaseName+"_") || !strings.HasSuffix(name, ".log") {
				continue
			}

			// 解析文件名中的日期
			dateStr := strings.TrimPrefix(strings.TrimSuffix(name, ".log"), logBaseName+"_")
			fileTime, err := time.Parse("20060102", dateStr)
			if err != nil {
				continue
			}

			// 检查是否是最新的日志文件
			if fileTime.Equal(latestLogTime) {
				targetLogPath = filepath.Join(logDir, name)
				break
			}
		}
	}

	// 如果没有找到目标日期的日志文件，或者没有选择日期且没有找到带日期的日志文件，尝试读取默认的日志文件
	if targetLogPath == "" {
		// 如果用户选择了日期但没有找到对应的日志文件，显示错误信息
		if !selectedDate.IsZero() {
			logContent = []byte(fmt.Sprintf("[%s] [error] [system] [system] 未找到 %s 的日志文件\n",
				time.Now().Format("2006-01-02 15:04:05"), selectedDate.Format("2006-01-02")))
		} else {
			// 尝试读取默认的日志文件
			targetLogPath = filepath.Join(logDir, logFileName)
			logContent, err = os.ReadFile(targetLogPath)
			if err != nil {
				logContent = []byte(fmt.Sprintf("[%s] [error] [system] [system] 读取日志文件失败 %v\n",
					time.Now().Format("2006-01-02 15:04:05"), err))
			}
		}
	} else {
		// 读取日志内容
		logContent, err = os.ReadFile(targetLogPath)
		if err != nil {
			logContent = []byte(fmt.Sprintf("[%s] [error] [system] [system] 读取日志文件失败 %v\n",
				time.Now().Format("2006-01-02 15:04:05"), err))
		}
	}

	// 解析日志内容为结构化格式
	logEntries := parseLogContent(string(logContent))

	// 生成可用日期的JavaScript数组
	availableDatesJS := ""
	if len(availableDates) > 0 {
		availableDatesJS = "'" + strings.Join(availableDates, "','") + "'"
	} else {
		availableDatesJS = "'" + time.Now().Format("2006-01-02") + "'"
	}

	// 获取当前选中的日期
	currentDate := ""
	if !selectedDate.IsZero() {
		currentDate = selectedDate.Format("2006-01-02")
	} else if len(availableDates) > 0 {
		// 默认选择最新的日期
		currentDate = availableDates[len(availableDates)-1]
	} else {
		currentDate = time.Now().Format("2006-01-02")
	}

	// 构建HTML页面
	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=5.0, minimum-scale=1.0">
	<title>服务器日志 - ` + config.AppConfig.Server.ServerName + `</title>
	<link rel="icon" href="data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 100 100%22><text y=%22.9em%22 font-size=%2290%22>📦</text></svg>">
	<link rel="stylesheet" href="/static/styles.css?v=2">

	<script>
		// 可用的日志日期列表
		const availableDates = [` + availableDatesJS + `];
		
		// 页面加载完成后执行
		document.addEventListener('DOMContentLoaded', function() {
			// 日志搜索功能
			const searchInput = document.getElementById('logSearch');
			const logEntries = document.querySelectorAll('.log-entry');
			const totalLogs = logEntries.length;
			
			// 更新日志总数显示
			document.getElementById('totalLogs').textContent = totalLogs;
			// 初始化可见日志数量
			document.getElementById('visibleLogs').textContent = totalLogs;
			
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
			
			// 日期选择器功能
			const dateInput = document.getElementById('logDate');
			
			// 设置日期选择器的最小和最大日期
			if (availableDates.length > 0) {
				availableDates.sort();
				dateInput.min = availableDates[0];
				dateInput.max = availableDates[availableDates.length - 1];
				
				// 确保初始值在可用日期列表中
				const initialValue = dateInput.value;
				if (!availableDates.includes(initialValue)) {
					dateInput.value = availableDates[availableDates.length - 1];
					dateInput.setAttribute('data-last-valid', availableDates[availableDates.length - 1]);
				} else {
					dateInput.setAttribute('data-last-valid', initialValue);
				}
			}
			
			// 监听日期改变事件
			dateInput.addEventListener('change', function(e) {
				const date = e.target.value;
				if (!date) return;
				
				// 直接跳转到对应日志页面
				window.location.href = '/logs?date=' + date;
			});
		});
		
		function loadDateLogs(date) {
			if (date && availableDates.includes(date)) {
				window.location.href = '/logs?date=' + date;
			}
		}
	</script>
</head>
<body class="v2 admin-layout">
		<div class="admin-layout-wrapper">
			` + utils.GetAdminSidebar(r, config.AppConfig.Server.ServerName) + `
			<main class="admin-main">
				<div class="admin-page-header">
					<h1 class="admin-page-title">服务器日志</h1>
					<p class="admin-page-desc">查看和筛选服务器运行日志</p>
				</div>
				<div class="admin-content-card">
			
			<!-- 日志统计 -->
			<div style="display: flex; gap: 16px; margin-bottom: 20px;">
				<div style="flex: 1; background: linear-gradient(135deg, #f8fafc 0%, #ffffff 100%); border: 1px solid #e2e8f0; border-radius: 12px; padding: 20px 24px; position: relative; overflow: hidden;">
					<div style="position: absolute; top: 0; left: 0; width: 4px; height: 100%; background: #3b82f6;"></div>
					<div style="font-size: 32px; font-weight: 700; color: #3b82f6; line-height: 1.2; margin-bottom: 4px;" id="totalLogs">0</div>
					<div style="font-size: 13px; color: #64748b; font-weight: 500;">总日志数</div>
				</div>
				<div style="flex: 1; background: linear-gradient(135deg, #f8fafc 0%, #ffffff 100%); border: 1px solid #e2e8f0; border-radius: 12px; padding: 20px 24px; position: relative; overflow: hidden;">
					<div style="position: absolute; top: 0; left: 0; width: 4px; height: 100%; background: #10b981;"></div>
					<div style="font-size: 32px; font-weight: 700; color: #10b981; line-height: 1.2; margin-bottom: 4px;" id="visibleLogs">0</div>
					<div style="font-size: 13px; color: #64748b; font-weight: 500;">可见日志</div>
				</div>
			</div>
			
			<!-- 日志筛选 -->
			<div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; flex-wrap: wrap; gap: 12px; padding: 16px 20px; background: #f8fafc; border: 1px solid #e2e8f0; border-radius: 12px;">
				<div style="display: flex; align-items: center; gap: 12px; flex-wrap: wrap;">
					<label for="logLevel" style="font-size: 13px; color: #475569; font-weight: 500;">筛选级别：</label>
					<select id="logLevel" style="padding: 8px 12px; border: 1px solid #e2e8f0; border-radius: 8px; font-size: 13px; color: #1e293b; background: #ffffff; cursor: pointer; outline: none;">
						<option value="all">所有级别</option>
						<option value="info">信息</option>
						<option value="success">成功</option>
						<option value="warning">警告</option>
						<option value="error">错误</option>
						<option value="debug">调试</option>
					</select>
					
					<label for="logDate" style="font-size: 13px; color: #475569; font-weight: 500;">选择日期：</label>
					<input type="date" id="logDate" value="` + currentDate + `" data-last-valid="` + currentDate + `" style="padding: 8px 12px; border: 1px solid #e2e8f0; border-radius: 8px; font-size: 13px; color: #1e293b; background: #ffffff; cursor: pointer; outline: none;">
				</div>
				
				<div style="display: flex; gap: 8px;">
					<a href="/logs" class="btn-v2 btn-primary-v2 btn-sm-v2">刷新日志</a>
					<a href="/admin" class="btn-v2 btn-secondary-v2 btn-sm-v2">返回管理员面板</a>
				</div>
			</div>
			
			<!-- 日志内容 -->
		<div class="logs-content-v2">` + logEntries + `</div>
	</div>

	
	</div>
</div>
<script>
// 立即执行，确保可见日志数量正确初始化
(function() {
	const logEntries = document.querySelectorAll('.log-entry');
	const totalLogs = logEntries.length;
	const totalEl = document.getElementById('totalLogs');
	const visibleEl = document.getElementById('visibleLogs');
	if (totalEl) totalEl.textContent = totalLogs;
	if (visibleEl) visibleEl.textContent = totalLogs;
})();
</script>
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

		// 解析日志格式: 时间 [级别] [用户名] [角色] 操作 详情
		// 示例: 2025-12-18 15:30:00 [info] [admin] [admin] login 登录成功
		var timestamp, level, username, role, action, details string

		// 使用正则表达式解析日志行
		re := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})\s*\[(.*?)\]\s*\[(.*?)\]\s*\[(.*?)\]\s*(\w+)\s*(.*)$`)
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
			// XSS防护：对日志内容进行HTML编码
			safeLine := utils.EscapeHTML(line)
			html.WriteString(fmt.Sprintf(`<div class="log-entry info"><span class="log-time">%s</span><span class="log-message">%s</span></div>`,
				time.Now().Format("2006-01-02 15:04:05"), safeLine))
			continue
		}

		// 转换角色为中文名称
		roleName := utils.GetRoleNameByString(role)

		// 转换操作名称为中文
		actionName := getActionNameCN(action)

		// XSS防护：对用户可控的输出进行HTML编码
		safeUsername := utils.EscapeHTML(username)
		safeDetails := utils.EscapeHTML(details)
		safeLevel := strings.ToLower(level)

		// 构建日志HTML条目
		html.WriteString(fmt.Sprintf(`<div class="log-entry %s">`, safeLevel))
		html.WriteString(fmt.Sprintf(`<span class="log-time">%s</span>`, timestamp))
		html.WriteString(fmt.Sprintf(`<span class="log-level %s">%s</span>`, safeLevel, strings.ToUpper(level)))
		html.WriteString(fmt.Sprintf(`<span class="log-username">%s</span>`, safeUsername))
		html.WriteString(fmt.Sprintf(`<span class="log-role">%s</span>`, roleName))
		html.WriteString(fmt.Sprintf(`<span class="log-action">%s</span>`, actionName))
		html.WriteString(fmt.Sprintf(`<span class="log-details">%s</span>`, safeDetails))
		html.WriteString(`</div>`)
	}

	return html.String()
}
