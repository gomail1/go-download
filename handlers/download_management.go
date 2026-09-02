package handlers

import (
	"fmt"
	"net/http"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/internal/core"
	"go-download-server/session"
	"go-download-server/utils"
)

// 全局变量，用于存储QuadEngine实例
var (
	QuadEngine core.Engine
)

// 下载管理主页处理函数
func DownloadsHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}

	// 只有管理员和二级管理员可以访问
	if sess.Role != constants.RoleAdmin && sess.Role != constants.RoleSubAdmin {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/?msg=您没有权限访问该页面", http.StatusFound)
		return
	}

	// 获取任务列表并计算统计数据
	var activeCount, completedCount, failedCount int
	if QuadEngine != nil {
		tasks := QuadEngine.ListTasks()
		for _, task := range tasks {
			switch task.Status {
			case core.TaskStatusDownloading, core.TaskStatusWaiting:
				activeCount++
			case core.TaskStatusCompleted:
				completedCount++
			case core.TaskStatusFailed:
				failedCount++
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=5.0, minimum-scale=1.0">
	<title>下载管理 - ` + config.AppConfig.Server.ServerName + `</title>
	` + utils.GenerateCSRFTokenMeta(utils.GetSessionIDFromRequest(r)) + `
	<script src="/static/js/csrf.js"></script>
	<link rel="icon" href="data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 100 100%22><text y=%22.9em%22 font-size=%2290%22>📦</text></svg>">
	<link rel="stylesheet" href="/static/styles.css">

</head>
<body class="v2 admin-layout">
	<div class="admin-layout-wrapper">
		` + utils.GetAdminSidebar(r, config.AppConfig.Server.ServerName) + `
		<main class="admin-main">
			<div class="admin-page-header">
				<h1 class="admin-page-title">下载管理</h1>
				<p class="admin-page-desc">管理BT、磁力链接等下载任务</p>
			</div>
			<!-- 显示消息 -->
			<div class="upload-message-v2">
				` + utils.GetMessage(r) + `
			</div>

			<!-- 下载管理标签页 -->
			<div style="display: flex; gap: 8px; margin-bottom: 24px; flex-wrap: wrap;">
				<a href="/downloads" style="padding: 10px 20px; border-radius: 10px; font-size: 14px; font-weight: 500; text-decoration: none; background: var(--v2-primary); color: white;">概览</a>
				<a href="/new-download" style="padding: 10px 20px; border-radius: 10px; font-size: 14px; font-weight: 500; text-decoration: none; background: var(--v2-bg-elev); color: var(--v2-text); border: 1px solid var(--v2-border);">新建下载</a>
				<a href="/download-tasks" style="padding: 10px 20px; border-radius: 10px; font-size: 14px; font-weight: 500; text-decoration: none; background: var(--v2-bg-elev); color: var(--v2-text); border: 1px solid var(--v2-border);">下载任务</a>
			</div>

			<!-- 统计卡片 -->
			<div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 16px; margin-bottom: 24px;">
				<div style="background: var(--v2-bg-elev); border: 1px solid var(--v2-border); border-radius: 12px; padding: 20px 24px; position: relative; overflow: hidden;">
					<div style="position: absolute; top: 0; left: 0; width: 4px; height: 100%; background: #3b82f6;"></div>
					<div style="font-size: 13px; color: var(--v2-text-muted); margin-bottom: 8px;">活跃下载</div>
					<div style="font-size: 32px; font-weight: 700; color: var(--v2-primary); margin-bottom: 12px;">` + fmt.Sprintf("%d", activeCount) + `</div>
					<a href="/download-tasks" style="display: inline-block; padding: 8px 16px; background: var(--v2-bg); border: 1px solid var(--v2-border); border-radius: 8px; font-size: 13px; color: var(--v2-text); text-decoration: none;">查看详情</a>
				</div>
				<div style="background: var(--v2-bg-elev); border: 1px solid var(--v2-border); border-radius: 12px; padding: 20px 24px; position: relative; overflow: hidden;">
					<div style="position: absolute; top: 0; left: 0; width: 4px; height: 100%; background: #10b981;"></div>
					<div style="font-size: 13px; color: var(--v2-text-muted); margin-bottom: 8px;">已完成</div>
					<div style="font-size: 32px; font-weight: 700; color: #10b981; margin-bottom: 12px;">` + fmt.Sprintf("%d", completedCount) + `</div>
					<a href="/download-tasks?status=completed" style="display: inline-block; padding: 8px 16px; background: var(--v2-bg); border: 1px solid var(--v2-border); border-radius: 8px; font-size: 13px; color: var(--v2-text); text-decoration: none;">查看详情</a>
				</div>
				<div style="background: var(--v2-bg-elev); border: 1px solid var(--v2-border); border-radius: 12px; padding: 20px 24px; position: relative; overflow: hidden;">
					<div style="position: absolute; top: 0; left: 0; width: 4px; height: 100%; background: #f59e0b;"></div>
					<div style="font-size: 13px; color: var(--v2-text-muted); margin-bottom: 8px;">待审核</div>
					<div style="font-size: 32px; font-weight: 700; color: #f59e0b; margin-bottom: 12px;">` + fmt.Sprintf("%d", utils.CountPendingFiles()) + `</div>
					<a href="/review" style="display: inline-block; padding: 8px 16px; background: var(--v2-bg); border: 1px solid var(--v2-border); border-radius: 8px; font-size: 13px; color: var(--v2-text); text-decoration: none;">审核文件</a>
				</div>
				<div style="background: var(--v2-bg-elev); border: 1px solid var(--v2-border); border-radius: 12px; padding: 20px 24px; position: relative; overflow: hidden;">
					<div style="position: absolute; top: 0; left: 0; width: 4px; height: 100%; background: #ef4444;"></div>
					<div style="font-size: 13px; color: var(--v2-text-muted); margin-bottom: 8px;">失败</div>
					<div style="font-size: 32px; font-weight: 700; color: #ef4444; margin-bottom: 12px;">` + fmt.Sprintf("%d", failedCount) + `</div>
					<a href="/download-tasks?status=failed" style="display: inline-block; padding: 8px 16px; background: var(--v2-bg); border: 1px solid var(--v2-border); border-radius: 8px; font-size: 13px; color: var(--v2-text); text-decoration: none;">查看详情</a>
				</div>
			</div>

		</main>
	</div>
</body>
</html>`

	w.Write([]byte(html))
}

// 新建下载页面处理函数
func NewDownloadHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}

	// 只有管理员和二级管理员可以访问
	if sess.Role != constants.RoleAdmin && sess.Role != constants.RoleSubAdmin {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/?msg=您没有权限访问该页面", http.StatusFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 会话ID用于生成/校验CSRF令牌
	sessionID := utils.GetSessionIDFromRequest(r)

	if r.Method == "POST" {
		// 验证CSRF令牌
		if !utils.ValidateCSRFTokenFromRequest(r) {
			http.Redirect(w, r, "/new-download?msg=CSRF验证失败，请刷新页面重试", http.StatusFound)
			return
		}

		// 处理表单提交
		r.ParseForm()
		url := r.Form.Get("url")
		if url == "" {
			http.Redirect(w, r, "/new-download?msg=URL不能为空", http.StatusFound)
			return
		}

		// 创建下载任务请求，使用固定的保存路径，不允许客户端修改
		req := &core.AddTaskRequest{
			URL:      url,
			SavePath: constants.PendingDir + "/download-user", // 固定保存路径，不允许修改
			Config: &core.TaskConfig{
				MaxThreads: 8,
				SpeedLimit: 0,
			},
		}

		// 添加下载任务
		_, err := QuadEngine.AddTask(r.Context(), req)
		if err != nil {
			http.Redirect(w, r, "/new-download?msg=创建下载任务失败: "+err.Error(), http.StatusFound)
			return
		}

		http.Redirect(w, r, "/download-tasks?msg=下载任务已创建", http.StatusFound)
		return
	}

	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=5.0, minimum-scale=1.0">
	<title>新建下载 - ` + config.AppConfig.Server.ServerName + `</title>
	` + utils.GenerateCSRFTokenMeta(sessionID) + `
	<script src="/static/js/csrf.js"></script>
	<link rel="icon" href="data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 100 100%22><text y=%22.9em%22 font-size=%2290%22>📦</text></svg>">
	<link rel="stylesheet" href="/static/styles.css?v=2">

</head>
<body class="v2 admin-layout">
	<div class="admin-layout-wrapper">
		` + utils.GetAdminSidebar(r, config.AppConfig.Server.ServerName) + `
		<main class="admin-main">
			<div class="admin-page-header">
				<h1 class="admin-page-title">新建下载</h1>
				<p class="admin-page-desc">创建BT、磁力链接等下载任务</p>
			</div>
			<!-- 显示消息 -->
			<div class="upload-message-v2">
				` + utils.GetMessage(r) + `
			</div>

			<!-- 下载管理标签页 -->
			<div style="display: flex; gap: 8px; margin-bottom: 24px; flex-wrap: wrap;">
				<a href="/downloads" style="padding: 10px 20px; border-radius: 10px; font-size: 14px; font-weight: 500; text-decoration: none; background: var(--v2-bg-elev); color: var(--v2-text); border: 1px solid var(--v2-border);">概览</a>
				<a href="/new-download" style="padding: 10px 20px; border-radius: 10px; font-size: 14px; font-weight: 500; text-decoration: none; background: var(--v2-primary); color: white;">新建下载</a>
				<a href="/download-tasks" style="padding: 10px 20px; border-radius: 10px; font-size: 14px; font-weight: 500; text-decoration: none; background: var(--v2-bg-elev); color: var(--v2-text); border: 1px solid var(--v2-border);">下载任务</a>
			</div>

			<div style="background: var(--v2-bg-elev); border: 1px solid var(--v2-border); border-radius: 12px; padding: 24px; margin-bottom: 24px;">
				<h2 style="font-size: 18px; font-weight: 600; color: var(--v2-text); margin-bottom: 16px;">新建下载任务</h2>
				
				<div style="background: var(--v2-bg); border: 1px solid var(--v2-border); border-radius: 10px; padding: 16px; margin-bottom: 20px;">
					<h4 style="font-size: 14px; font-weight: 600; color: var(--v2-text); margin-bottom: 12px;">支持的协议</h4>
					<ul style="list-style: none; padding: 0; margin: 0; display: grid; gap: 8px;">
					<li style="font-size: 13px; color: var(--v2-text-muted);">📥 HTTP/HTTPS - 支持多线程加速下载</li>
					<li style="font-size: 13px; color: var(--v2-text-muted);">📂 FTP - 已启用（PASV 模式，明文传输，请勿用于敏感文件）</li>
					<li style="font-size: 13px; color: var(--v2-text-muted);">🧲 BitTorrent - 已启用（磁力链接 / .torrent 种子）</li>
					<li style="font-size: 13px; color: var(--v2-text-muted);">🅴 ED2K - 已启用（ed2k:// 链接，真实 eDonkey2000 客户端）</li>
					<li style="font-size: 13px; color: var(--v2-text-muted);">🔧 SFTP / 流媒体协议 - 暂未实现</li>
					</ul>
				</div>

				<form method="POST" action="/new-download" enctype="multipart/form-data">
					` + utils.GenerateCSRFTokenField(sessionID) + `
					<div style="margin-bottom: 16px;">
						<label for="url" style="display: block; font-size: 13px; font-weight: 500; color: var(--v2-text); margin-bottom: 8px;">下载链接</label>
						<input type="text" id="url" name="url" style="width: 100%; padding: 12px 16px; border: 1px solid var(--v2-border); border-radius: 10px; font-size: 14px; color: var(--v2-text); background: var(--v2-bg); box-sizing: border-box;" placeholder="支持 http(s)://、ftp://、magnet: 磁力链接、ed2k:// 链接">
					</div>

					<div style="margin-bottom: 16px;">
						<label style="display: block; font-size: 13px; font-weight: 500; color: var(--v2-text); margin-bottom: 8px;">或上传本地种子文件</label>
						<div style="border: 2px dashed var(--v2-border); border-radius: 10px; padding: 24px; text-align: center; background: var(--v2-bg); cursor: pointer; transition: all 0.2s;" onmouseover="this.style.borderColor='var(--v2-primary)'; this.style.background='rgba(79, 70, 229, 0.05)';" onmouseout="this.style.borderColor='var(--v2-border)'; this.style.background='var(--v2-bg)';">
							<input type="file" id="torrent_file" name="torrent_file" accept=".torrent" style="display: none;" onchange="document.getElementById('file_name').textContent = this.files[0] ? this.files[0].name : '';">
							<label for="torrent_file" style="cursor: pointer;">
								<div style="font-size: 32px; margin-bottom: 8px;">📄</div>
								<div style="font-size: 14px; font-weight: 500; color: var(--v2-text); margin-bottom: 4px;">点击选择 .torrent 种子文件</div>
								<div id="file_name" style="font-size: 12px; color: var(--v2-text-muted);">支持 .torrent 格式的种子文件</div>
							</label>
						</div>
					</div>

					<div class="form-row">
						<div class="form-group">
						<label for="save_path" style="display: block; font-size: 13px; font-weight: 500; color: var(--v2-text); margin-bottom: 8px;">保存路径</label>
						<input type="text" id="save_path" name="save_path" style="width: 100%; padding: 12px 16px; border: 1px solid var(--v2-border); border-radius: 10px; font-size: 14px; color: var(--v2-text-muted); background: var(--v2-bg); box-sizing: border-box;" value="` + constants.PendingDir + `/download-user" readonly>
					</div>
						
						<div class="form-group">
							<label for="max_threads" style="display: block; font-size: 13px; font-weight: 500; color: var(--v2-text); margin-bottom: 8px;">最大线程数</label>
							<input type="number" id="max_threads" name="max_threads" style="width: 100%; padding: 12px 16px; border: 1px solid var(--v2-border); border-radius: 10px; font-size: 14px; color: var(--v2-text); background: var(--v2-bg); box-sizing: border-box;" value="8" min="1" max="32">
						</div>
					</div>

					<div class="form-row">
						<div class="form-group">
							<label for="speed_limit" style="display: block; font-size: 13px; font-weight: 500; color: var(--v2-text); margin-bottom: 8px;">速度限制 (KB/s)</label>
							<input type="number" id="speed_limit" name="speed_limit" style="width: 100%; padding: 12px 16px; border: 1px solid var(--v2-border); border-radius: 10px; font-size: 14px; color: var(--v2-text); background: var(--v2-bg); box-sizing: border-box;" value="0" min="0" placeholder="0表示无限制">
						</div>
						
						<div class="form-group">
							<label for="overwrite" style="display: block; font-size: 13px; font-weight: 500; color: var(--v2-text); margin-bottom: 8px;">覆盖文件</label>
							<select id="overwrite" name="overwrite" style="width: 100%; padding: 12px 16px; border: 1px solid var(--v2-border); border-radius: 10px; font-size: 14px; color: var(--v2-text); background: var(--v2-bg); box-sizing: border-box;">
								<option value="false">不覆盖</option>
								<option value="true">覆盖</option>
							</select>
						</div>
					</div>

					<div style="display: flex; gap: 12px; margin-top: 20px;">
						<button type="submit" style="padding: 12px 24px; background: var(--v2-primary); color: white; border: none; border-radius: 10px; font-size: 14px; font-weight: 500; cursor: pointer;">创建下载任务</button>
						<a href="/downloads" style="padding: 12px 24px; background: var(--v2-bg); color: var(--v2-text); border: 1px solid var(--v2-border); border-radius: 10px; font-size: 14px; font-weight: 500; text-decoration: none;">取消</a>
					</div>
				</form>
			</div>
		</div>

		<footer>
			<p>版本: ` + constants.Version + ` | 开发者: ` + constants.Developer + ` | <a href="` + constants.RepoURL + `" target="_blank" title="GitHub仓库"><svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" style="vertical-align: middle;"><path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.435.372.825 1.102.825 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12"></path></svg></a></p>
		</footer>
	</div>
</body>
</html>`

	w.Write([]byte(html))
}

// 下载任务列表页面处理函数
func DownloadTasksHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}

	// 只有管理员和二级管理员可以访问
	if sess.Role != constants.RoleAdmin && sess.Role != constants.RoleSubAdmin {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/?msg=您没有权限访问该页面", http.StatusFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 获取查询参数
	status := r.URL.Query().Get("status")

	// 获取任务列表
	var tasks []*core.Task
	if QuadEngine != nil {
		tasks = QuadEngine.ListTasks()

		// 按照创建时间倒序排序，最新的任务在最上面
		for i := 0; i < len(tasks); i++ {
			for j := i + 1; j < len(tasks); j++ {
				if tasks[i].CreatedAt.Before(tasks[j].CreatedAt) {
					tasks[i], tasks[j] = tasks[j], tasks[i]
				}
			}
		}
	}

	// 过滤任务
	var filteredTasks []*core.Task
	for _, task := range tasks {
		if status == "" || string(task.Status) == status {
			filteredTasks = append(filteredTasks, task)
		}
	}

	// 生成过滤选项的active类
	activeAll := ""
	activeDownloading := ""
	activeCompleted := ""
	activePaused := ""
	activeFailed := ""
	activeWaiting := ""

	if status == "" {
		activeAll = ` class="active"`
	} else {
		switch status {
		case "downloading":
			activeDownloading = ` class="active"`
		case "completed":
			activeCompleted = ` class="active"`
		case "paused":
			activePaused = ` class="active"`
		case "failed":
			activeFailed = ` class="active"`
		case "waiting":
			activeWaiting = ` class="active"`
		}
	}

	// 生成任务列表HTML
	tasksHTML := ""
	if len(filteredTasks) == 0 {
		tasksHTML = `<div class="empty-message">
			<div class="empty-icon">📭</div>
			<h3>暂无下载任务</h3>
			<p>点击"新建下载"按钮创建您的第一个下载任务</p>
		</div>`
	} else {
		tasksHTML = `<table class="table">
		<thead>
			<tr>
				<th>任务ID</th>
				<th>URL</th>
				<th>协议</th>
				<th>状态</th>
				<th>进度</th>
				<th>速度</th>
				<th>创建时间</th>
				<th>操作</th>
			</tr>
		</thead>
		<tbody>`

		for _, task := range filteredTasks {
			// 格式化URL，只显示前50个字符
			displayURL := task.URL
			if len(displayURL) > 50 {
				displayURL = displayURL[:50] + "..."
			}

			// 格式化进度
			progress := fmt.Sprintf("%.1f%%", task.Progress.Percentage)

			// 格式化速度
			speed := utils.FormatFileSize(task.Progress.Speed) + "/s"
			if task.Progress.Speed == 0 {
				speed = "0 B/s"
			}

			// 格式化创建时间
			createdAt := task.CreatedAt.Format("2006-01-02 15:04:05")

			// 生成状态样式
			statusClass := ""
			switch task.Status {
			case core.TaskStatusCompleted:
				statusClass = "status-completed"
			case core.TaskStatusDownloading:
				statusClass = "status-downloading"
			case core.TaskStatusFailed:
				statusClass = "status-failed"
			case core.TaskStatusPaused:
				statusClass = "status-paused"
			case core.TaskStatusWaiting:
				statusClass = "status-waiting"
			}

			// 根据任务状态决定显示的按钮
			var pauseBtn, resumeBtn string
			if task.Status == core.TaskStatusDownloading {
				pauseBtn = fmt.Sprintf(`<a href="#" class="btn btn-sm btn-secondary" onclick="pauseTask('%s')">暂停</a>`, task.ID)
				resumeBtn = ""
			} else if task.Status == core.TaskStatusPaused {
				pauseBtn = ""
				resumeBtn = fmt.Sprintf(`<a href="#" class="btn btn-sm btn-secondary" onclick="resumeTask('%s')">恢复</a>`, task.ID)
			} else {
				pauseBtn = ""
				resumeBtn = ""
			}

			tasksHTML += fmt.Sprintf(`<tr>
						<td>%s</td>
						<td title="%s">%s</td>
						<td>%s</td>
						<td><span class="status %s">%s</span></td>
						<td>%s</td>
						<td>%s</td>
						<td>%s</td>
						<td>
							<a href="/download-task?id=%s" class="btn btn-sm btn-primary">查看</a>
							%s
							%s
							<a href="#" class="btn btn-sm btn-danger" onclick="deleteTask('%s')">删除</a>
						</td>
					</tr>`,
				utils.EscapeHTML(task.ID),
				utils.EscapeHTML(task.URL),
				utils.EscapeHTML(displayURL),
				task.Protocol,
				statusClass,
				task.Status,
				progress,
				speed,
				createdAt,
				task.ID,
				pauseBtn,
				resumeBtn,
				task.ID,
			)
		}

		tasksHTML += `</tbody>
	</table>`
	}

	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=5.0, minimum-scale=1.0">
	<title>下载任务 - ` + config.AppConfig.Server.ServerName + `</title>
	` + utils.GenerateCSRFTokenMeta(utils.GetSessionIDFromRequest(r)) + `
	<script src="/static/js/csrf.js"></script>
	<link rel="icon" href="data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 100 100%22><text y=%22.9em%22 font-size=%2290%22>📦</text></svg>">
	<link rel="stylesheet" href="/static/styles.css?v=2">

</head>
<body class="v2 admin-layout">
	<div class="admin-layout-wrapper">
		` + utils.GetAdminSidebar(r, config.AppConfig.Server.ServerName) + `
		<main class="admin-main">
			<div class="admin-page-header">
				<h1 class="admin-page-title">下载任务</h1>
				<p class="admin-page-desc">查看和管理所有下载任务</p>
			</div>
			<!-- 显示消息 -->
			<div class="upload-message-v2">
				` + utils.GetMessage(r) + `
			</div>

			<!-- 下载管理标签页 -->
			<div style="display: flex; gap: 8px; margin-bottom: 24px; flex-wrap: wrap;">
				<a href="/downloads" style="padding: 10px 20px; border-radius: 10px; font-size: 14px; font-weight: 500; text-decoration: none; background: var(--v2-bg-elev); color: var(--v2-text); border: 1px solid var(--v2-border);">概览</a>
				<a href="/new-download" style="padding: 10px 20px; border-radius: 10px; font-size: 14px; font-weight: 500; text-decoration: none; background: var(--v2-bg-elev); color: var(--v2-text); border: 1px solid var(--v2-border);">新建下载</a>
				<a href="/download-tasks" style="padding: 10px 20px; border-radius: 10px; font-size: 14px; font-weight: 500; text-decoration: none; background: var(--v2-primary); color: white;">下载任务</a>
			</div>

			<div style="margin-bottom: 20px;">
				<div style="display: flex; gap: 8px; flex-wrap: wrap;">
							<a href="/download-tasks" ` + activeAll + ` style="padding: 8px 16px; border-radius: 8px; font-size: 13px; font-weight: 500; text-decoration: none; background: var(--v2-bg-elev); color: var(--v2-text); border: 1px solid var(--v2-border);">全部</a>
							<a href="/download-tasks?status=downloading" ` + activeDownloading + ` style="padding: 8px 16px; border-radius: 8px; font-size: 13px; font-weight: 500; text-decoration: none; background: var(--v2-bg-elev); color: var(--v2-text); border: 1px solid var(--v2-border);">下载中</a>
							<a href="/download-tasks?status=completed" ` + activeCompleted + ` style="padding: 8px 16px; border-radius: 8px; font-size: 13px; font-weight: 500; text-decoration: none; background: var(--v2-bg-elev); color: var(--v2-text); border: 1px solid var(--v2-border);">已完成</a>
							<a href="/download-tasks?status=paused" ` + activePaused + ` style="padding: 8px 16px; border-radius: 8px; font-size: 13px; font-weight: 500; text-decoration: none; background: var(--v2-bg-elev); color: var(--v2-text); border: 1px solid var(--v2-border);">已暂停</a>
							<a href="/download-tasks?status=failed" ` + activeFailed + ` style="padding: 8px 16px; border-radius: 8px; font-size: 13px; font-weight: 500; text-decoration: none; background: var(--v2-bg-elev); color: var(--v2-text); border: 1px solid var(--v2-border);">失败</a>
							<a href="/download-tasks?status=waiting" ` + activeWaiting + ` style="padding: 8px 16px; border-radius: 8px; font-size: 13px; font-weight: 500; text-decoration: none; background: var(--v2-bg-elev); color: var(--v2-text); border: 1px solid var(--v2-border);">等待中</a>
							<button id="auto-refresh-btn" style="padding: 8px 16px; border-radius: 8px; font-size: 13px; font-weight: 500; cursor: pointer; background: #ef4444; color: white; border: none;">关闭自动刷新</button>
						</div>
			</div>

			<div class="table-container">
					` + tasksHTML + `
				</div>
		</div>
	</div>

	<script>
		// 自动刷新相关变量
		let autoRefreshInterval = null;
		let isAutoRefreshEnabled = true; // 默认开启自动刷新
		const SHORT_REFRESH_INTERVAL = 5000; // 创建任务后的短刷新间隔（5秒）
		const LONG_REFRESH_INTERVAL = 60000; // 正常刷新间隔（60秒）
		let lastCreatedTime = Date.now(); // 最后一次创建任务的时间

		// 暂停任务
		function pauseTask(taskId) {
			if (confirm('确定要暂停该任务吗？')) {
				fetch('/api/tasks/' + taskId + '/pause', {
					method: 'PUT'
				})
				.then(response => {
					if (response.ok) {
						// 手动刷新任务列表
						refreshTaskList();
					} else {
						alert('暂停任务失败');
					}
				})
				.catch(error => {
					console.error('暂停任务出错:', error);
					alert('暂停任务出错');
				});
			}
		}

		// 恢复任务
		function resumeTask(taskId) {
			if (confirm('确定要恢复该任务吗？')) {
				fetch('/api/tasks/' + taskId + '/resume', {
					method: 'PUT'
				})
				.then(response => {
					if (response.ok) {
						// 手动刷新任务列表
						refreshTaskList();
					} else {
						alert('恢复任务失败');
					}
				})
				.catch(error => {
					console.error('恢复任务出错:', error);
					alert('恢复任务出错');
				});
			}
		}

		// 删除任务
		function deleteTask(taskId) {
			if (confirm('确定要删除该任务吗？此操作不可恢复。')) {
				fetch('/api/tasks/' + taskId, {
					method: 'DELETE'
				})
				.then(response => {
					if (response.ok) {
						// 手动刷新任务列表
						refreshTaskList();
					} else {
						alert('删除任务失败');
					}
				})
				.catch(error => {
					console.error('删除任务出错:', error);
					alert('删除任务出错');
				});
			}
		}

		// 格式化文件大小
		function formatFileSize(bytes) {
			if (bytes === 0) return '0 B';
			const k = 1024;
			const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
			const i = Math.floor(Math.log(bytes) / Math.log(k));
			return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
		}

		// 获取任务列表并更新表格
		async function refreshTaskList() {
			try {
				// 异步获取任务列表
				const response = await fetch('/api/tasks');
				if (!response.ok) {
					throw new Error('获取任务列表失败');
				}
				const tasks = await response.json();
				
				// 获取当前状态过滤条件
				const currentStatus = new URLSearchParams(window.location.search).get('status') || '';
				
				// 过滤任务，将preparing状态也视为waiting状态
				const filteredTasks = currentStatus ? 
					tasks.filter(task => {
						// 如果当前过滤条件是waiting，那么匹配waiting和preparing状态
						if (currentStatus === 'waiting') {
							return task.status === 'waiting' || task.status === 'preparing';
						}
						// 其他情况，精确匹配状态
						return task.status === currentStatus;
					}) : 
					tasks;
				
				// 按照创建时间倒序排序，最新的任务在最上面
				filteredTasks.sort((a, b) => {
					const dateA = new Date(a.created_at);
					const dateB = new Date(b.created_at);
					return dateB - dateA;
				});
				
				// 获取表格元素
				const table = document.querySelector('.table');
				if (!table) {
					console.error('未找到表格元素');
					return;
				}
				
				// 获取tbody元素
				let tbody = table.querySelector('tbody');
				if (!tbody) {
					// 如果tbody不存在，创建一个
					tbody = document.createElement('tbody');
					table.appendChild(tbody);
				}
				
				// 清空tbody
				tbody.innerHTML = '';
				
				// 如果没有任务，显示空消息
				if (filteredTasks.length === 0) {
					tbody.innerHTML = '<tr>' +
						'<td colspan="8" style="text-align: center; padding: 20px;">' +
							'<div class="empty-message">' +
								'<div class="empty-icon">📭</div>' +
								'<h3>暂无下载任务</h3>' +
								'<p>点击"新建下载"按钮创建您的第一个下载任务</p>' +
							'</div>' +
						'</td>' +
					'</tr>';
				} else {
					// 添加任务行
					filteredTasks.forEach(task => {
						// 格式化URL，只显示前50个字符
						let displayURL = task.url;
						if (task.url && task.url.length > 50) {
							displayURL = task.url.substring(0, 50) + '...';
						}

						// 格式化进度
						let progress = '0.0%';
						if (task.progress && typeof task.progress.percentage === 'number') {
							progress = task.progress.percentage.toFixed(1) + '%';
						}

						// 格式化速度
						let speed = '0 B/s';
						if (task.progress && typeof task.progress.speed === 'number') {
							speed = formatFileSize(task.progress.speed) + '/s';
						}

						// 格式化创建时间
						let createdAt = '未知时间';
						if (task.created_at) {
							try {
								createdAt = new Date(task.created_at).format('yyyy-MM-dd HH:mm:ss');
							} catch (e) {
								console.error('格式化时间出错:', e);
							}
						}

						// 状态映射：英文 → 中文
					const statusMap = {
						'completed': '已完成',
						'downloading': '下载中',
						'failed': '失败',
						'paused': '已暂停',
						'waiting': '等待中',
						'preparing': '准备中'
					};

					// 生成状态样式
					let statusClass = '';
					switch (task.status) {
						case 'completed':
							statusClass = 'status-completed';
							break;
						case 'downloading':
							statusClass = 'status-downloading';
							break;
						case 'failed':
							statusClass = 'status-failed';
							break;
						case 'paused':
							statusClass = 'status-paused';
							break;
						case 'waiting':
							statusClass = 'status-waiting';
							break;
						case 'preparing':
							statusClass = 'status-waiting';
							break;
						default:
							statusClass = '';
					}

					// 显示中文状态
					const displayStatus = statusMap[task.status] || task.status;

					// 根据任务状态决定显示的按钮
					let pauseBtn = '';
					let resumeBtn = '';
					if (task.status === 'downloading') {
						pauseBtn = '<a href="#" class="btn btn-sm btn-secondary" onclick="pauseTask(\'' + task.id + '\')">暂停</a>';
						resumeBtn = '';
					} else if (task.status === 'paused') {
						pauseBtn = '';
						resumeBtn = '<a href="#" class="btn btn-sm btn-secondary" onclick="resumeTask(\'' + task.id + '\')">恢复</a>';
					}

						// 获取文件名（从metadata中获取，若不存在则使用任务ID）
					const filename = (task.metadata && task.metadata.filename) ? task.metadata.filename : (task.id || '');
					
					// 创建表格行
					const row = document.createElement('tr');
					row.innerHTML = '<td title="' + (task.id || '') + '">' + filename + '</td>' +
							'<td title="' + (task.url || '') + '">' + (displayURL || '') + '</td>' +
							'<td>' + (task.protocol || '') + '</td>' +
							'<td><span class="status ' + statusClass + '">' + displayStatus + '</span></td>' +
							'<td>' + progress + '</td>' +
							'<td>' + speed + '</td>' +
							'<td>' + createdAt + '</td>' +
							'<td>' +
								'<a href="/download-task?id=' + (task.id || '') + '" class="btn btn-sm btn-primary">查看</a>' +
								pauseBtn +
								resumeBtn +
								'<a href="#" class="btn btn-sm btn-danger" onclick="deleteTask(\'' + (task.id || '') + '\')">删除</a>' +
							'</td>';
						tbody.appendChild(row);
					});
				}
			} catch (error) {
				console.error('刷新任务列表出错:', error);
				// 显示错误提示，帮助调试
				const table = document.querySelector('.table');
				if (table) {
					let tbody = table.querySelector('tbody');
					if (!tbody) {
						tbody = document.createElement('tbody');
						table.appendChild(tbody);
					}
					tbody.innerHTML = '<tr><td colspan="8" style="text-align: center; padding: 20px; color: red;">刷新任务列表时出错: ' + error.message + '</td></tr>';
				}
			}
		}

		// 切换自动刷新状态
		function toggleAutoRefresh() {
			isAutoRefreshEnabled = !isAutoRefreshEnabled;
			const btn = document.getElementById('auto-refresh-btn');
			if (isAutoRefreshEnabled) {
				startAutoRefresh();
				btn.textContent = '关闭自动刷新';
				btn.className = 'btn btn-danger';
			} else {
				stopAutoRefresh();
				btn.textContent = '开启自动刷新';
				btn.className = 'btn btn-primary';
			}
		}

		// 获取当前应该使用的刷新间隔
		function getCurrentRefreshInterval() {
			// 计算距离最后一次创建任务的时间（毫秒）
			const timeSinceLastCreated = Date.now() - lastCreatedTime;
			// 任务创建后的5分钟内使用短间隔，之后使用长间隔
			// 5分钟 = 5 * 60 * 1000 = 300000毫秒
			return timeSinceLastCreated < 300000 ? SHORT_REFRESH_INTERVAL : LONG_REFRESH_INTERVAL;
		}

		// 动态调整刷新间隔
		function adjustRefreshInterval() {
			if (!isAutoRefreshEnabled) return;
			
			// 停止当前的刷新
			if (autoRefreshInterval) {
				clearInterval(autoRefreshInterval);
				autoRefreshInterval = null;
			}
			
			// 获取当前应该使用的刷新间隔
			const currentInterval = getCurrentRefreshInterval();
			// 启动新的刷新，只刷新任务列表，不再递归调用adjustRefreshInterval
			autoRefreshInterval = setInterval(() => {
				refreshTaskList();
			}, currentInterval);
		}

		// 启动自动刷新
		function startAutoRefresh() {
			adjustRefreshInterval();
		}

		// 停止自动刷新
		function stopAutoRefresh() {
			if (autoRefreshInterval) {
				clearInterval(autoRefreshInterval);
				autoRefreshInterval = null;
			}
		}

		// 更新最后创建任务时间
		function updateLastCreatedTime() {
			lastCreatedTime = Date.now();
			// 调整刷新间隔
			adjustRefreshInterval();
		}

		// 添加日期格式化方法
		Date.prototype.format = function(fmt) {
			var o = {
				"M+": this.getMonth() + 1,
				"d+": this.getDate(),
				"H+": this.getHours(),
				"m+": this.getMinutes(),
				"s+": this.getSeconds(),
				"q+": Math.floor((this.getMonth() + 3) / 3),
				"S": this.getMilliseconds()
			};
			if (/(y+)/.test(fmt)) {
				fmt = fmt.replace(RegExp.$1, (this.getFullYear() + "").substr(4 - RegExp.$1.length));
			}
			for (var k in o) {
				if (new RegExp("(" + k + ")").test(fmt)) {
					fmt = fmt.replace(RegExp.$1, (RegExp.$1.length == 1) ? (o[k]) : (("00" + o[k]).substr(("" + o[k]).length)));
				}
			}
			return fmt;
		};

		// 页面加载完成后初始化
		document.addEventListener('DOMContentLoaded', function() {
			// 添加自动刷新按钮事件监听
			const refreshBtn = document.getElementById('auto-refresh-btn');
			if (refreshBtn) {
				refreshBtn.onclick = toggleAutoRefresh;
			}
			
			// 初始加载任务列表
			refreshTaskList();
			
			// 启动自动刷新
			startAutoRefresh();
		});
	</script>

		<footer>
			<p>版本: ` + constants.Version + ` | 开发者: ` + constants.Developer + ` | <a href="` + constants.RepoURL + `" target="_blank" title="GitHub仓库"><svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" style="vertical-align: middle;"><path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.435.372.825 1.102.825 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12"></path></svg></a></p>
		</footer>
	</div>
</body>
</html>`

	w.Write([]byte(html))
}

// 下载任务详情处理函数
func DownloadTaskHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	// 只有管理员和二级管理员可以访问
	if sess.Role != constants.RoleAdmin && sess.Role != constants.RoleSubAdmin {
		http.Redirect(w, r, "/?msg=您没有权限访问该页面", http.StatusFound)
		return
	}

	// 获取任务ID
	taskID := r.URL.Query().Get("id")
	if taskID == "" {
		http.Redirect(w, r, "/download-tasks?msg=任务ID不能为空", http.StatusFound)
		return
	}

	// 获取任务详情
	var task *core.Task
	if QuadEngine != nil {
		tasks := QuadEngine.ListTasks()
		for _, t := range tasks {
			if t.ID == taskID {
				task = t
				break
			}
		}
	}

	if task == nil {
		http.Redirect(w, r, "/download-tasks?msg=未找到该任务", http.StatusFound)
		return
	}

	// 生成任务详情HTML
	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=5.0, minimum-scale=1.0">
	<title>下载任务详情 - ` + config.AppConfig.Server.ServerName + `</title>
	` + utils.GenerateCSRFTokenMeta(utils.GetSessionIDFromRequest(r)) + `
	<script src="/static/js/csrf.js"></script>
	<link rel="icon" href="data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 100 100%22><text y=%22.9em%22 font-size=%2290%22>📦</text></svg>">
	<link rel="stylesheet" href="/static/styles.css">

</head>
<body class="v2 admin-layout">
	<div class="admin-layout-wrapper">
		` + utils.GetAdminSidebar(r, config.AppConfig.Server.ServerName) + `
		<main class="admin-main">
			<div class="admin-page-header">
				<h1 class="admin-page-title">下载管理</h1>
				<p class="admin-page-desc">管理BT、磁力链接等下载任务</p>
			</div>
	<div class="header">
		<div class="logo">📦 ` + constants.ServerName + `</div>
		<div class="user-info">
			` + utils.GetCurrentUserInfo(r) + `
		</div>
	</div>

	<!-- 导航菜单 -->
	<div class="nav-menu">
		<ul>
			<li><a href="/files">文件列表</a></li>
			<li><a href="/upload">上传文件</a></li>
			` + utils.GetAdminLinks(r) + `
		</ul>
	</div>

	<div class="container">
		<div class="content">
			<!-- 显示消息 -->
			` + utils.GetMessage(r) + `

			<div class="back-link">
				<a href="/download-tasks" class="btn btn-secondary">返回下载任务列表</a>
			</div>

			<div class="task-detail">
				<h2>下载任务详情</h2>
				
				<div class="detail-item">
					<span class="detail-label">任务ID:</span>
					<span class="detail-value">` + utils.EscapeHTML(task.ID) + `</span>
				</div>

				<div class="detail-item">
					<span class="detail-label">下载URL:</span>
					<span class="detail-value">` + utils.EscapeHTML(task.URL) + `</span>
				</div>
				
				<div class="detail-item">
					<span class="detail-label">协议:</span>
					<span class="detail-value">` + task.Protocol + `</span>
				</div>
				
				<div class="detail-item">
					<span class="detail-label">状态:</span>
					<span class="detail-value">
						<span class="status status-` + string(task.Status) + `">` + string(task.Status) + `</span>
					</span>
				</div>
				
				<div class="detail-item">
					<span class="detail-label">保存路径:</span>
					<span class="detail-value">` + task.Config.SavePath + `</span>
				</div>
				
				<div class="detail-item">
					<span class="detail-label">进度:</span>
					<div class="detail-value">
						<span>` + fmt.Sprintf("%.1f%%", task.Progress.Percentage) + `</span>
						<div class="progress-bar">
							<div class="progress-fill" style="width: ` + fmt.Sprintf("%.1f%%", task.Progress.Percentage) + `;"></div>
						</div>
					</div>
				</div>
				
				<div class="detail-item">
					<span class="detail-label">已下载:</span>
					<span class="detail-value">` + utils.FormatFileSize(task.Progress.Downloaded) + `</span>
				</div>
				
				<div class="detail-item">
					<span class="detail-label">总大小:</span>
					<span class="detail-value">` + utils.FormatFileSize(task.Progress.TotalSize) + `</span>
				</div>
				
				<div class="detail-item">
					<span class="detail-label">速度:</span>
					<span class="detail-value">` + utils.FormatFileSize(task.Progress.Speed) + `/s</span>
				</div>
				
				<div class="detail-item">
					<span class="detail-label">创建时间:</span>
					<span class="detail-value">` + task.CreatedAt.Format("2006-01-02 15:04:05") + `</span>
				</div>
				
				` + func() string {
		if task.CompletedAt != nil {
			return `<div class="detail-item"><span class="detail-label">完成时间:</span><span class="detail-value">` + task.CompletedAt.Format("2006-01-02 15:04:05") + `</span></div>`
		} else {
			return ""
		}
	}() + `
			</div>
		</div>

		<footer>
			<p>版本: ` + constants.Version + ` | 开发者: ` + constants.Developer + ` | <a href="` + constants.RepoURL + `" target="_blank" title="GitHub仓库"><svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" style="vertical-align: middle;"><path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.435.372.825 1.102.825 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12"></path></svg></a></p>
		</footer>
	</div>
</body>
</html>`

	w.Write([]byte(html))
}

// 注册下载管理相关的路由
func RegisterDownloadManagementRoutes() {
	http.HandleFunc("/downloads", DownloadsHandler)
	http.HandleFunc("/new-download", NewDownloadHandler)
	http.HandleFunc("/download-tasks", DownloadTasksHandler)
	http.HandleFunc("/download-task", DownloadTaskHandler)
}
