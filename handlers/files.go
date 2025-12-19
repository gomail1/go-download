package handlers

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/session"
	"go-download-server/utils"
)

// 主页处理函数
func IndexHandler(w http.ResponseWriter, r *http.Request) {
	// 重定向到文件列表页面
	http.Redirect(w, r, "/files", http.StatusFound)
}

// 文件列表处理函数
func FilesHandler(w http.ResponseWriter, r *http.Request) {
	// 获取当前路径
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "."
	}

	// URL解码路径
	var err error
	path, err = url.QueryUnescape(path)
	if err != nil {
		path = "."
	}

	// 安全检查：防止路径遍历
	path = filepath.Clean(path)
	if strings.HasPrefix(path, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// 记录日志
	utils.LogUserAction(r, "view_files", fmt.Sprintf("访问文件列表，路径: %s", path))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 构建完整路径
	fullPath := filepath.Join(config.AppConfig.Server.DownloadDir, path)

	// 获取文件列表
	files, err := os.ReadDir(fullPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("无法读取目录: %v", err), http.StatusInternalServerError)
		return
	}

	// 对文件列表进行排序：目录在前，文件在后，按修改时间倒序
	sort.Slice(files, func(i, j int) bool {
		// 获取文件信息
		infoI, errI := files[i].Info()
		infoJ, errJ := files[j].Info()

		// 错误处理：出错的文件放在后面
		if errI != nil {
			return false
		}
		if errJ != nil {
			return true
		}

		// 目录在前，文件在后
		if files[i].IsDir() && !files[j].IsDir() {
			return true
		}
		if !files[i].IsDir() && files[j].IsDir() {
			return false
		}

		// 同类型按修改时间倒序排列（最新的在前）
		return infoI.ModTime().After(infoJ.ModTime())
	})

	// 构建HTML页面
	html := `<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>文件列表 - ` + constants.ServerName + `</title>
	<style>
		body {
			font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
			background-color: #f5f5f5;
			margin: 0;
			padding: 0;
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
			border-radius: 5px;
			margin-bottom: 20px;
		}
		.header-content {
			display: flex;
			justify-content: space-between;
			align-items: center;
		}
		nav {
			background-color: white;
			padding: 15px;
			border-radius: 5px;
			margin-bottom: 20px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.nav-links {
			display: flex;
			gap: 15px;
			align-items: center;
		}
		.nav-links a {
			text-decoration: none;
			color: #333;
			padding: 8px 12px;
			border-radius: 3px;
			transition: background-color 0.3s;
		}
		.nav-links a:hover {
			background-color: #e0e0e0;
		}
		/* 导航栏管理员链接徽章样式 */
		.admin-link {
			position: relative;
			padding-right: 20px;
		}
		.admin-link .pending-count {
			position: absolute;
			top: -8px;
			right: -8px;
			background-color: #dc3545;
			color: white;
			font-size: 12px;
			font-weight: bold;
			width: 20px;
			height: 20px;
			border-radius: 50%;
			display: flex;
			align-items: center;
			justify-content: center;
			box-shadow: 0 2px 4px rgba(0,0,0,0.2);
		}
		.file-list {
			background-color: white;
			padding: 20px;
			border-radius: 5px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.file-item {
			display: flex;
			align-items: center;
			padding: 10px;
			border-bottom: 1px solid #eee;
			transition: background-color 0.3s;
		}
		.file-item:hover {
			background-color: #f9f9f9;
		}
		.file-icon {
			font-size: 24px;
			margin-right: 15px;
			width: 30px;
			text-align: center;
		}
		.file-info {
			flex-grow: 1;
		}
		.file-name {
			font-weight: bold;
			margin-bottom: 3px;
		}
		.file-meta {
			font-size: 12px;
			color: #666;
		}
		.file-actions {
			display: flex;
			gap: 10px;
		}
		.btn {
			padding: 5px 10px;
			border: none;
			border-radius: 3px;
			cursor: pointer;
			text-decoration: none;
			font-size: 14px;
			transition: background-color 0.3s;
		}
		.btn-primary {
			background-color: #4CAF50;
			color: white;
		}
		.btn-primary:hover {
			background-color: #45a049;
		}
		.btn-secondary {
			background-color: #2196F3;
			color: white;
		}
		.btn-secondary:hover {
			background-color: #0b7dda;
		}
		.btn-danger {
			background-color: #f44336;
			color: white;
		}
		.btn-danger:hover {
			background-color: #da190b;
		}
		.btn-info {
			background-color: #2196F3;
			color: white;
		}
		.btn-info:hover {
			background-color: #0b7dda;
		}
		.btn-success {
			background-color: #4CAF50;
			color: white;
		}
		.btn-success:hover {
			background-color: #45a049;
		}
		.empty-message {
			text-align: center;
			padding: 60px 20px;
			color: #666;
		}
		.empty-icon {
			font-size: 64px;
			margin-bottom: 20px;
		}
		.path-bar {
			background-color: #f5f5f5;
			padding: 10px;
			border-radius: 3px;
			margin-bottom: 20px;
			font-size: 14px;
		}
		.path-item {
			display: inline-block;
			margin-right: 5px;
		}
		.path-separator {
			color: #999;
			margin-right: 5px;
		}
		.pagination {
			margin-top: 20px;
			text-align: center;
		}
		.page-link {
			display: inline-block;
			padding: 5px 10px;
			margin: 0 2px;
			border: 1px solid #ddd;
			border-radius: 3px;
			text-decoration: none;
			color: #333;
			transition: background-color 0.3s;
		}
		.page-link:hover {
			background-color: #e0e0e0;
		}
		.page-link.active {
			background-color: #4CAF50;
			color: white;
			border-color: #4CAF50;
		}
		footer {
			margin-top: 20px;
			text-align: center;
			color: #666;
			font-size: 12px;
			padding: 10px;
			border-top: 1px solid #eee;
		}
	</style>
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
				` + utils.GetAdminLinks(r) + `
			</div>
		</nav>

		<!-- 显示消息 -->
		` + utils.GetMessage(r) + `

		<div class="file-list">
			<!-- 路径导航 -->
			<div class="path-bar">
				<div class="path-item">
					<a href="/files?path=./" class="path-link">📁 根目录</a>
				</div>
				` + utils.GeneratePathNavigation(path) + `
			</div>

			<!-- 文件列表 -->
			` + generateFileList(r, files, path) + `
		</div>
	
		<footer>
			<p>版本: ` + constants.Version + ` | 开发者: ` + constants.Developer + `</p>
		</footer>
	</div>
</body>
</html>`

	w.Write([]byte(html))
}

// 辅助函数：生成文件列表
func generateFileList(r *http.Request, files []os.DirEntry, currentPath string) string {
	var fileList string

	// 获取当前用户
	sess := session.GetCurrentUser(r)

	// 为管理员添加批量操作按钮
	var batchActions string
	if sess != nil && sess.Role == constants.RoleAdmin {
		// 获取所有目录列表，用于目标路径选择
		dirList := utils.GetDirectoryList(config.AppConfig.Server.DownloadDir)

		// 构建目录选择下拉框
		selectHTML := `<select id="target-path" style="padding: 8px; margin-right: 10px; border-radius: 3px; border: 1px solid #ddd;">`
		for _, dir := range dirList {
			displayName := dir
			if dir == "." {
				displayName = "根目录"
			}
			selectHTML += fmt.Sprintf(`<option value="%s">%s</option>`, url.QueryEscape(dir), displayName)
		}
		selectHTML += `</select>`

		batchActions = fmt.Sprintf(`<div class="batch-actions" style="margin-bottom: 20px;">
				<h3>批量操作</h3>
				<div style="display: flex; gap: 10px; align-items: center;">
					<button type="button" id="select-all" class="btn btn-info">全选</button>
					<button type="button" id="select-none" class="btn btn-info">取消全选</button>
					<button type="button" id="batch-delete" class="btn btn-danger">删除</button>
					<button type="button" id="batch-move" class="btn btn-primary">移动</button>
					<button type="button" id="batch-copy" class="btn btn-secondary">复制</button>
					<div style="display: none; margin-left: 10px;" id="move-copy-form">
						` + selectHTML + `
						<button type="button" id="confirm-action" class="btn btn-success">确认</button>
						<button type="button" id="cancel-action" class="btn btn-danger">取消</button>
					</div>
				</div>
			</div>`)
	}

	// 添加批量操作脚本
	var batchScript string
	if sess != nil && sess.Role == constants.RoleAdmin {
		batchScript = `<script>
				// 批量操作脚本
		document.addEventListener('DOMContentLoaded', function() {
			const batchDeleteBtn = document.getElementById('batch-delete');
			const batchMoveBtn = document.getElementById('batch-move');
			const batchCopyBtn = document.getElementById('batch-copy');
			const selectAllBtn = document.getElementById('select-all');
			const selectNoneBtn = document.getElementById('select-none');
			const moveCopyForm = document.getElementById('move-copy-form');
			const confirmBtn = document.getElementById('confirm-action');
			const cancelBtn = document.getElementById('cancel-action');
			let currentAction = '';

			// 全选功能
			selectAllBtn.addEventListener('click', function() {
				const checkboxes = document.querySelectorAll('input[name="selected-files"]');
				checkboxes.forEach(cb => {
					cb.checked = true;
				});
			});

			// 取消全选功能
			selectNoneBtn.addEventListener('click', function() {
				const checkboxes = document.querySelectorAll('input[name="selected-files"]');
				checkboxes.forEach(cb => {
					cb.checked = false;
				});
			});

			// 显示移动/复制表单
			function showMoveCopyForm(action) {
				currentAction = action;
				moveCopyForm.style.display = 'flex';
			}

			// 隐藏移动/复制表单
			function hideMoveCopyForm() {
				moveCopyForm.style.display = 'none';
				currentAction = '';
				// 清空输入框
				document.getElementById('target-path').value = '';
			}

			// 获取选中的文件
			function getSelectedFiles() {
				const checkboxes = document.querySelectorAll('input[name="selected-files"]:checked');
				const files = [];
				checkboxes.forEach(cb => {
					files.push(cb.value);
				});
				return files;
			}

			// 批量删除
			batchDeleteBtn.addEventListener('click', function() {
				const files = getSelectedFiles();
				if (files.length === 0) {
					alert('请选择要删除的文件');
					return;
				}
				if (confirm('确定要删除选中的 ' + files.length + ' 个文件/目录吗？')) {
					const form = document.createElement('form');
					form.method = 'POST';
					form.action = '/batch-delete';
					files.forEach(file => {
						const input = document.createElement('input');
						input.type = 'hidden';
						input.name = 'files';
						input.value = file;
						form.appendChild(input);
					});
					document.body.appendChild(form);
					form.submit();
				}
			});

			// 批量移动
			batchMoveBtn.addEventListener('click', function() {
				const files = getSelectedFiles();
				if (files.length === 0) {
					alert('请选择要移动的文件');
					return;
				}
				showMoveCopyForm('move');
			});

			// 批量复制
			batchCopyBtn.addEventListener('click', function() {
				const files = getSelectedFiles();
				if (files.length === 0) {
					alert('请选择要复制的文件');
					return;
				}
				showMoveCopyForm('copy');
			});

			// 确认移动/复制
			confirmBtn.addEventListener('click', function() {
				const files = getSelectedFiles();
				const targetPath = document.getElementById('target-path').value;
				if (targetPath === '') {
					alert('请输入目标路径');
					return;
				}

				const form = document.createElement('form');
				form.method = 'POST';
				if (currentAction === 'move') {
					form.action = '/batch-move';
				} else {
					form.action = '/batch-copy';
				}

				// 添加选中的文件
				files.forEach(file => {
					const input = document.createElement('input');
					input.type = 'hidden';
					input.name = 'files';
					input.value = file;
					form.appendChild(input);
				});

				// 添加目标路径
				const targetInput = document.createElement('input');
				targetInput.type = 'hidden';
				targetInput.name = 'target_path';
				targetInput.value = targetPath;
				form.appendChild(targetInput);

				document.body.appendChild(form);
				form.submit();
			});

			// 取消移动/复制
			cancelBtn.addEventListener('click', function() {
				hideMoveCopyForm();
			});
		});
	</script>`
	}

	// 先添加返回上一级目录的选项（如果不是根目录）
	if currentPath != "." {
		parentPath := filepath.Dir(currentPath)
		if parentPath == "." {
			parentPath = ""
		}
		fileList += fmt.Sprintf(`<div class="file-item">
			<div class="file-icon">📁</div>
			<div class="file-info">
				<div class="file-name"><a href="/files?path=%s">..</a></div>
				<div class="file-meta">返回上一级</div>
			</div>
		</div>`, url.QueryEscape(parentPath))
	}

	// 添加文件和目录
	for _, file := range files {
		name := file.Name()
		filePath := filepath.Join(currentPath, name)
		fileURL := url.QueryEscape(filePath)

		// 获取文件信息
		info, err := file.Info()
		if err != nil {
			continue
		}

		// 生成文件图标
		var icon string
		if file.IsDir() {
			icon = "📁"
		} else {
			icon = "📄"
		}

		// 生成文件元信息
		var meta string
		if file.IsDir() {
			meta = "目录 • " + info.ModTime().Format("2006-01-02 15:04:05")
		} else {
			meta = fmt.Sprintf("文件 • %s • %s", utils.FormatFileSize(info.Size()), info.ModTime().Format("2006-01-02 15:04:05"))
		}

		// 为管理员添加复选框
		var checkbox string
		if sess != nil && sess.Role == constants.RoleAdmin {
			checkbox = fmt.Sprintf(`<input type="checkbox" name="selected-files" value="%s" style="margin-right: 15px; transform: scale(1.2);">`, fileURL)
		}

		// 生成文件项
		var item string
		if file.IsDir() {
			item = fmt.Sprintf(`<div class="file-item">
				%s
				<div class="file-icon">%s</div>
				<div class="file-info">
					<div class="file-name"><a href="/files?path=%s">%s</a></div>
					<div class="file-meta">%s</div>
				</div>
				<div class="file-actions">
					%s
				</div>
			</div>`, checkbox, icon, fileURL, name, meta, utils.GetAdminActions(r, filePath))
		} else {
			// 检查文件是否在待审核目录中
			pendingFilePath := filepath.Join(currentPath, name)
			pendingFullPath := filepath.Join(config.AppConfig.Server.PendingDir, pendingFilePath)
			_, pendingErr := os.Stat(pendingFullPath)
			isPending := pendingErr == nil

			// 如果是待审核文件，添加待审核状态
			if isPending {
				meta += " • <span style=\"color: orange;\">待审核</span>"
			}

			item = fmt.Sprintf(`<div class="file-item">
				%s
				<div class="file-icon">%s</div>
				<div class="file-info">
					<div class="file-name">%s</div>
					<div class="file-meta">%s</div>
				</div>
				<div class="file-actions">
					<a href="/download?path=%s" class="btn btn-secondary">下载</a>
					%s
				</div>
			</div>`, checkbox, icon, name, meta, fileURL, utils.GetAdminActions(r, filePath))
		}

		fileList += item
	}

	// 如果不是管理员，添加当前用户的待审核文件列表
	if sess != nil && sess.Role != constants.RoleAdmin {
		// 获取待审核目录的根路径
		pendingRoot := config.AppConfig.Server.PendingDir
		log.Printf("DEBUG: 待审核根目录: %s", pendingRoot)

		// 构建当前用户的待审核目录路径
		userPendingDir := filepath.Join(pendingRoot, sess.Username)
		log.Printf("DEBUG: 用户待审核目录: %s", userPendingDir)

		// 确保用户待审核目录存在
		os.MkdirAll(userPendingDir, 0755)

		// 检查用户待审核目录是否存在
		if _, err := os.Stat(userPendingDir); err == nil {
			// 生成CSS样式（只添加一次）
			cssStyleAdded := false
			var cssStyle string

			// 递归遍历用户待审核目录中的所有文件
			// 包括根目录和子目录中的待审核文件
			var allPendingFiles []string

			walkErr := filepath.Walk(userPendingDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !info.IsDir() {
					// 获取相对路径
					relPath, err := filepath.Rel(userPendingDir, filepath.Dir(path))
					if err != nil {
						return err
					}
					log.Printf("DEBUG: 待审核文件路径: %s, 相对目录: %s", path, relPath)

					// 只有当文件所在的相对目录与当前浏览的目录匹配时，才添加到列表
					if relPath == currentPath {
						allPendingFiles = append(allPendingFiles, path)
					}
				}
				return nil
			})

			if walkErr != nil {
				log.Printf("DEBUG: 遍历待审核文件失败: %v", walkErr)
			} else {
				log.Printf("DEBUG: 匹配的待审核文件数量: %d", len(allPendingFiles))

				// 遍历匹配的待审核文件
				for _, filePath := range allPendingFiles {
					// 获取文件名
					filename := filepath.Base(filePath)
					log.Printf("DEBUG: 待审核文件: %s", filename)

					// 获取文件信息
					fileInfo, err := os.Stat(filePath)
					if err != nil {
						log.Printf("DEBUG: 获取文件信息失败: %v", err)
						continue
					}

					// 生成文件图标
					icon := "📄"

					// 生成文件元信息
					meta := fmt.Sprintf("文件 • %s • %s", utils.FormatFileSize(fileInfo.Size()), fileInfo.ModTime().Format("2006-01-02 15:04:05"))

					// 生成CSS样式（只添加一次）
					if !cssStyleAdded {
						cssStyle = `<style>
								.pending-file-item {
									border-left: 4px solid orange;
									background-color: #fff8e1;
									transition: all 0.3s ease;
								}
								.pending-file-item:hover {
									background-color: #ffeeba;
								}
								.status-badge {
									display: inline-block;
									padding: 4px 8px;
									border-radius: 12px;
									font-size: 12px;
									font-weight: bold;
									text-align: center;
									min-width: 60px;
									text-decoration: none;
								}
								.status-badge.pending {
									background-color: #ffc107;
									color: #856404;
								}
							</style>`
						cssStyleAdded = true
					}

					// 生成文件项
					item := fmt.Sprintf(`<div class="file-item pending-file-item">
							<div class="file-icon">%s</div>
							<div class="file-info">
								<div class="file-name">%s</div>
								<div class="file-meta">%s</div>
							</div>
							<div class="file-actions">
								<span class="status-badge pending">待审核</span>
							</div>
						</div>`+cssStyle, icon, filename, meta)

					fileList += item
					log.Printf("DEBUG: 添加待审核文件到列表: %s", filename)
				}
			}
		}
	}

	// 如果文件列表为空，返回空消息
	if fileList == "" {
		return utils.GetEmptyMessage()
	}

	// 添加批量操作内容到文件列表
	return batchActions + fileList + batchScript
}

// 批量删除处理函数
func BatchDeleteHandler(w http.ResponseWriter, r *http.Request) {
	// 检查请求方法
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil || sess.Role != constants.RoleAdmin {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	// 解析表单数据
	r.ParseForm()
	files := r.Form["files"]

	if len(files) == 0 {
		http.Redirect(w, r, "/files?msg=请选择要删除的文件&type=error", http.StatusFound)
		return
	}

	// 记录日志
	utils.LogUserAction(r, "batch_delete", fmt.Sprintf("批量删除文件: %v", files))

	// 批量删除文件
	var deletedCount int
	var failedCount int

	for _, filePath := range files {
		// URL解码路径
		decodedPath, err := url.QueryUnescape(filePath)
		if err != nil {
			failedCount++
			continue
		}

		// 构建完整路径
		fullPath := filepath.Join(config.AppConfig.Server.DownloadDir, decodedPath)

		// 删除文件或目录
		err = os.RemoveAll(fullPath)
		if err != nil {
			failedCount++
			continue
		}

		deletedCount++
	}

	// 构建成功消息
	msg := fmt.Sprintf("成功删除 %d 个文件，失败 %d 个", deletedCount, failedCount)
	http.Redirect(w, r, "/files?msg="+url.QueryEscape(msg), http.StatusFound)
}

// 批量移动处理函数
func BatchMoveHandler(w http.ResponseWriter, r *http.Request) {
	// 检查请求方法
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil || sess.Role != constants.RoleAdmin {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	// 解析表单数据
	r.ParseForm()
	files := r.Form["files"]
	targetPath := r.FormValue("target_path")

	if len(files) == 0 {
		http.Redirect(w, r, "/files?msg=请选择要移动的文件&type=error", http.StatusFound)
		return
	}

	if targetPath == "" {
		http.Redirect(w, r, "/files?msg=请输入目标路径&type=error", http.StatusFound)
		return
	}

	// 清理目标路径
	targetPath = filepath.Clean(targetPath)
	if strings.HasPrefix(targetPath, "..") {
		http.Redirect(w, r, "/files?msg=无效的目标路径&type=error", http.StatusFound)
		return
	}

	// 构建完整的目标路径
	targetFullPath := filepath.Join(config.AppConfig.Server.DownloadDir, targetPath)

	// 确保目标目录存在
	os.MkdirAll(targetFullPath, 0755)

	// 记录日志
	utils.LogUserAction(r, "batch_move", fmt.Sprintf("批量移动文件: %v 到 %s", files, targetPath))

	// 批量移动文件
	var movedCount int
	var failedCount int

	for _, filePath := range files {
		// URL解码路径
		decodedPath, err := url.QueryUnescape(filePath)
		if err != nil {
			failedCount++
			continue
		}

		// 构建源文件完整路径
		sourceFullPath := filepath.Join(config.AppConfig.Server.DownloadDir, decodedPath)

		// 获取文件名
		filename := filepath.Base(decodedPath)

		// 构建目标文件完整路径
		targetFilePath := filepath.Join(targetFullPath, filename)

		// 检查目标文件是否已存在
		if _, err := os.Stat(targetFilePath); err == nil {
			// 文件已存在，生成新文件名
			ext := filepath.Ext(filename)
			nameWithoutExt := filename[:len(filename)-len(ext)]
			count := 1
			newFilename := fmt.Sprintf("%s_%d%s", nameWithoutExt, count, ext)
			targetFilePath = filepath.Join(targetFullPath, newFilename)

			// 检查新文件名是否已存在
			for _, err := os.Stat(targetFilePath); err == nil; _, err = os.Stat(targetFilePath) {
				count++
				newFilename := fmt.Sprintf("%s_%d%s", nameWithoutExt, count, ext)
				targetFilePath = filepath.Join(targetFullPath, newFilename)
			}
		}

		// 移动文件
		err = os.Rename(sourceFullPath, targetFilePath)
		if err != nil {
			failedCount++
			continue
		}

		movedCount++
	}

	// 构建成功消息
	msg := fmt.Sprintf("成功移动 %d 个文件，失败 %d 个", movedCount, failedCount)
	http.Redirect(w, r, "/files?msg="+url.QueryEscape(msg), http.StatusFound)
}

// 批量复制处理函数
func BatchCopyHandler(w http.ResponseWriter, r *http.Request) {
	// 检查请求方法
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil || sess.Role != constants.RoleAdmin {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	// 解析表单数据
	r.ParseForm()
	files := r.Form["files"]
	targetPath := r.FormValue("target_path")

	if len(files) == 0 {
		http.Redirect(w, r, "/files?msg=请选择要复制的文件&type=error", http.StatusFound)
		return
	}

	if targetPath == "" {
		http.Redirect(w, r, "/files?msg=请输入目标路径&type=error", http.StatusFound)
		return
	}

	// 清理目标路径
	targetPath = filepath.Clean(targetPath)
	if strings.HasPrefix(targetPath, "..") {
		http.Redirect(w, r, "/files?msg=无效的目标路径&type=error", http.StatusFound)
		return
	}

	// 构建完整的目标路径
	targetFullPath := filepath.Join(config.AppConfig.Server.DownloadDir, targetPath)

	// 确保目标目录存在
	os.MkdirAll(targetFullPath, 0755)

	// 记录日志
	utils.LogUserAction(r, "batch_copy", fmt.Sprintf("批量复制文件: %v 到 %s", files, targetPath))

	// 批量复制文件
	var copiedCount int
	var failedCount int

	for _, filePath := range files {
		// URL解码路径
		decodedPath, err := url.QueryUnescape(filePath)
		if err != nil {
			failedCount++
			continue
		}

		// 构建源文件完整路径
		sourceFullPath := filepath.Join(config.AppConfig.Server.DownloadDir, decodedPath)

		// 获取文件信息
		sourceInfo, err := os.Stat(sourceFullPath)
		if err != nil {
			failedCount++
			continue
		}

		// 获取文件名
		filename := filepath.Base(decodedPath)

		// 构建目标文件完整路径
		targetFilePath := filepath.Join(targetFullPath, filename)

		// 检查目标文件是否已存在
		if _, err := os.Stat(targetFilePath); err == nil {
			// 文件已存在，生成新文件名
			ext := filepath.Ext(filename)
			nameWithoutExt := filename[:len(filename)-len(ext)]
			count := 1
			newFilename := fmt.Sprintf("%s_%d%s", nameWithoutExt, count, ext)
			targetFilePath = filepath.Join(targetFullPath, newFilename)

			// 检查新文件名是否已存在
			for _, err := os.Stat(targetFilePath); err == nil; _, err = os.Stat(targetFilePath) {
				count++
				newFilename := fmt.Sprintf("%s_%d%s", nameWithoutExt, count, ext)
				targetFilePath = filepath.Join(targetFullPath, newFilename)
			}
		}

		// 复制文件或目录
		if sourceInfo.IsDir() {
			// 复制目录
			err = copyDir(sourceFullPath, targetFilePath)
		} else {
			// 复制文件
			err = copyFile(sourceFullPath, targetFilePath)
		}

		if err != nil {
			failedCount++
			continue
		}

		copiedCount++
	}

	// 构建成功消息
	msg := fmt.Sprintf("成功复制 %d 个文件，失败 %d 个", copiedCount, failedCount)
	http.Redirect(w, r, "/files?msg="+url.QueryEscape(msg), http.StatusFound)
}

// 辅助函数：复制文件
func copyFile(src, dst string) error {
	// 打开源文件
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// 创建目标文件
	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// 复制文件内容
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return err
	}

	// 复制文件权限
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	return os.Chmod(dst, srcInfo.Mode())
}

// 辅助函数：复制目录
func copyDir(src, dst string) error {
	// 创建目标目录
	os.MkdirAll(dst, 0755)

	// 读取源目录内容
	dirEntries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range dirEntries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			// 递归复制子目录
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			// 复制文件
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}
