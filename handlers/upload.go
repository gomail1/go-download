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

// 上传文件处理函数
func UploadHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	var err error

	// GET请求：显示上传表单
	if r.Method == "GET" {
		// 获取上传路径
		path := r.URL.Query().Get("path")
		if path == "" {
			path = "."
		}

		// URL解码路径
		path, err = url.QueryUnescape(path)
		if err != nil {
			path = "."
		}

		// 安全检查
		path = filepath.Clean(path)
		if strings.HasPrefix(path, "..") {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}

		// 获取目录列表
		var dirList []string
		// 所有用户都应该看到下载目录的结构
		dirList = utils.GetDirectoryList(config.AppConfig.Server.DownloadDir)

		// 构建目录选择下拉框
		dirSelectHTML := `<select id="directory" name="directory" class="form-control">`
		for _, dir := range dirList {
			selected := ""
			if dir == path {
				selected = " selected"
			}
			// 将根目录显示为"根目录"而不是"."
			displayName := dir
			if dir == "." {
				displayName = "根目录"
			}
			dirSelectHTML += fmt.Sprintf(`<option value="%s"%s>%s</option>`, url.QueryEscape(dir), selected, displayName)
		}
		dirSelectHTML += `</select>`

		// 构建HTML页面
		html := `<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
			<meta name="viewport" content="width=device-width, initial-scale=1.0">
			<title>上传文件 - ` + constants.ServerName + `</title>
			
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
		.upload-form {
			background-color: white;
			padding: 20px;
			border-radius: 5px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
			margin-bottom: 20px;
		}
		.upload-form h2 {
			margin-top: 0;
			margin-bottom: 20px;
			color: #333;
			font-size: 24px;
		}
		.form-group {
			margin-bottom: 20px;
		}
		label {
			display: block;
			margin-bottom: 5px;
			font-weight: bold;
		}
		input[type="file"] {
			display: block;
			margin-bottom: 10px;
			padding: 10px;
			border: 2px dashed #ddd;
			border-radius: 5px;
			width: 100%;
			background-color: #f9f9f9;
		}
		input[type="file"]:hover {
			border-color: #4CAF50;
		}
		select {
			display: block;
			width: 100%;
			padding: 10px;
			border: 1px solid #ddd;
			border-radius: 5px;
			font-size: 16px;
		}
		.btn {
			padding: 10px 20px;
			border: none;
			border-radius: 3px;
			cursor: pointer;
			text-decoration: none;
			font-size: 16px;
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
		.btn-success {
			background-color: #4CAF50;
			color: white;
		}
		.btn-success:hover {
			background-color: #45a049;
		}
		.message {
			padding: 10px;
			border-radius: 3px;
			margin-bottom: 20px;
		}
		.message-success {
			background-color: #d4edda;
			color: #155724;
			border: 1px solid #c3e6cb;
		}
		.message-error {
			background-color: #f8d7da;
			color: #721c24;
			border: 1px solid #f5c6cb;
		}
		.user-info {
			font-size: 14px;
			color: #666;
		}
		.max-size-info {
			font-size: 14px;
			color: #666;
			margin-top: 10px;
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
		footer {
			margin-top: 20px;
			text-align: center;
			color: #666;
			font-size: 12px;
			padding: 10px;
			border-top: 1px solid #eee;
		}
		/* 拖拽上传区域样式 */
		.drop-area {
			border: 2px dashed #ddd;
			border-radius: 10px;
			padding: 40px;
			text-align: center;
			background-color: #f9f9f9;
			transition: all 0.3s ease;
			margin: 20px 0;
			cursor: pointer;
		}
		.drop-area:hover {
			border-color: #4CAF50;
			background-color: #e8f5e9;
		}
		.drop-area.drag-over {
			border-color: #4CAF50;
			background-color: #e8f5e9;
			transform: scale(1.02);
			box-shadow: 0 8px 15px rgba(0,0,0,0.1);
		}
		.drop-content {
			display: flex;
			flex-direction: column;
			align-items: center;
			justify-content: center;
		}
		.drop-icon {
			font-size: 48px;
			margin-bottom: 15px;
			color: #4CAF50;
		}
		.drop-content h3 {
			margin: 0 0 10px 0;
			color: #333;
			font-size: 20px;
		}
		.drop-content p {
			margin: 5px 0;
			color: #666;
		}
		.drop-hint {
			font-size: 12px;
			color: #888;
			margin-top: 15px;
		}
		.file-label {
			display: inline-block;
			margin: 10px 0;
		}
		.file-label input[type="file"] {
			display: none;
		}

		/* 文件列表样式 */
		.file-list-container {
			background-color: #f9f9f9;
			border: 1px solid #eee;
			border-radius: 5px;
			padding: 15px;
			margin: 20px 0;
		}
		.file-list-header {
			display: flex;
			justify-content: space-between;
			align-items: center;
			margin-bottom: 15px;
			padding-bottom: 10px;
			border-bottom: 1px solid #eee;
		}
		.file-list-header h3 {
			margin: 0;
			color: #333;
			font-size: 18px;
		}
		.btn-sm {
			padding: 5px 10px;
			font-size: 12px;
		}
		.selected-files {
			max-height: 300px;
			overflow-y: auto;
		}
		.file-item {
			display: flex;
			justify-content: space-between;
			align-items: center;
			padding: 10px;
			margin-bottom: 8px;
			background-color: white;
			border: 1px solid #eee;
			border-radius: 4px;
			transition: all 0.2s ease;
		}
		.file-item:hover {
			background-color: #e8f5e9;
			border-color: #4CAF50;
			transform: translateX(5px);
		}
		.file-info {
			display: flex;
			align-items: center;
			flex: 1;
		}
		.file-icon-small {
			font-size: 20px;
			margin-right: 10px;
			color: #4CAF50;
		}
		.file-name {
			font-weight: 500;
			color: #333;
			overflow: hidden;
			text-overflow: ellipsis;
			white-space: nowrap;
		}
		.file-size {
			font-size: 12px;
			color: #666;
			margin-left: 10px;
		}
		.remove-file {
			background: none;
			border: none;
			color: #dc3545;
			cursor: pointer;
			font-size: 16px;
			padding: 5px;
			transition: color 0.2s ease;
		}
		.remove-file:hover {
			color: #c82333;
		}

		/* 上传进度条样式 */
		.upload-progress {
			background-color: #f9f9f9;
			border: 1px solid #eee;
			border-radius: 5px;
			padding: 15px;
			margin: 20px 0;
		}
		.progress-label {
			font-weight: bold;
			margin-bottom: 10px;
			color: #333;
		}
		.progress-bar-container {
			width: 100%;
			height: 20px;
			background-color: #eee;
			border-radius: 10px;
			overflow: hidden;
			margin-bottom: 8px;
		}
		.progress-bar {
			height: 100%;
			background-color: #4CAF50;
			border-radius: 10px;
			width: 0%;
			transition: width 0.3s ease;
		}
		.progress-text {
			text-align: center;
			font-weight: bold;
			color: #333;
			font-size: 14px;
		}
		.form-actions {
			display: flex;
			gap: 10px;
			margin-top: 20px;
		}
		.form-actions .btn {
			flex: 1;
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

		<div class="upload-form">
			<h2>上传文件</h2>

			<!-- 显示消息 -->
			` + utils.GetMessage(r) + `

			<!-- 上传表单 -->
			<form method="POST" enctype="multipart/form-data">
				<div class="form-group">
					<label for="directory">选择目录</label>
					` + dirSelectHTML + `
				</div>

				<!-- 拖拽上传区域 -->
				<div id="drop-area" class="drop-area">
					<div class="drop-content">
						<div class="drop-icon">📁</div>
						<h3>拖拽文件或文件夹到此处</h3>
						<p>或</p>
						<label class="file-label">
							<input type="file" id="file" name="file" multiple required>
							<span class="btn btn-primary">选择文件/文件夹</span>
						</label>
						<p class="drop-hint">支持选择多个文件或整个文件夹</p>
					</div>
				</div>

				<div class="max-size-info">
					最大文件大小: ` + utils.GetMaxFileSizeText(sess) + `
				</div>

				<!-- 已选择文件列表 -->
				<div id="file-list" class="file-list-container" style="display: none;">
					<div class="file-list-header">
						<h3>已选择的文件 (<span id="file-count">0</span>)</h3>
						<button type="button" id="clear-files" class="btn btn-secondary btn-sm">清空列表</button>
					</div>
					<div id="selected-files" class="selected-files"></div>
				</div>

				<!-- 上传进度条 -->
				<div id="upload-progress" class="upload-progress" style="display: none;">
					<div class="progress-label">上传进度:</div>
					<div class="progress-bar-container">
						<div id="progress-bar" class="progress-bar" style="width: 0%;"></div>
					</div>
					<div id="progress-text" class="progress-text">0%</div>
				</div>

				<div class="form-actions">
					<button type="button" id="upload-btn" class="btn btn-primary">开始上传</button>
					<a href="/files?path=` + path + `" class="btn btn-secondary">返回</a>
				</div>
			</form>
		</div>

		<footer>
			<p>版本: ` + constants.Version + ` | 开发者: ` + constants.Developer + `</p>
		</footer>
	</div>

	<script>
		// 上传进度监控
		document.addEventListener('DOMContentLoaded', function() {
			const fileInput = document.getElementById('file');
			const directorySelect = document.getElementById('directory');
			const dropArea = document.getElementById('drop-area');
			const progressContainer = document.getElementById('upload-progress');
			const progressBar = document.getElementById('progress-bar');
			const progressText = document.getElementById('progress-text');
			const uploadBtn = document.getElementById('upload-btn');
			const fileListContainer = document.getElementById('file-list');
			const selectedFilesContainer = document.getElementById('selected-files');
			const fileCountElement = document.getElementById('file-count');
			const clearFilesBtn = document.getElementById('clear-files');

			let selectedFiles = [];

			// 拖拽事件处理
			['dragenter', 'dragover', 'dragleave', 'drop'].forEach(eventName => {
				dropArea.addEventListener(eventName, preventDefaults, false);
			});

			function preventDefaults(e) {
				e.preventDefault();
				e.stopPropagation();
			}

			// 拖拽进入和悬停时的样式
			['dragenter', 'dragover'].forEach(eventName => {
				dropArea.addEventListener(eventName, highlight, false);
			});

			// 拖拽离开和放置时的样式
			['dragleave', 'drop'].forEach(eventName => {
				dropArea.addEventListener(eventName, unhighlight, false);
			});

			function highlight() {
				dropArea.classList.add('drag-over');
			}

			function unhighlight() {
				dropArea.classList.remove('drag-over');
			}

			// 处理文件放置
			dropArea.addEventListener('drop', handleDrop, false);

			function handleDrop(e) {
				const dt = e.dataTransfer;
				const files = dt.files;
				handleFiles(files);
			}

			// 处理文件选择
			fileInput.addEventListener('change', function() {
				handleFiles(this.files);
			});

			// 格式化文件大小
			function formatFileSize(bytes) {
				if (bytes === 0) return '0 Bytes';
				const k = 1024;
				const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
				const i = Math.floor(Math.log(bytes) / Math.log(k));
				return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
			}

			// 检查文件是否已存在
			function isFileExists(file) {
				for (let i = 0; i < selectedFiles.length; i++) {
					const existingFile = selectedFiles[i];
					// 检查文件名和大小是否相同
					if (existingFile.name === file.name && existingFile.size === file.size) {
						return true;
					}
				}
				return false;
			}

			// 显示文件列表
			function displayFileList() {
				selectedFilesContainer.innerHTML = '';
				fileCountElement.textContent = selectedFiles.length;

				if (selectedFiles.length === 0) {
					fileListContainer.style.display = 'none';
					return;
				}

				fileListContainer.style.display = 'block';

				selectedFiles.forEach((file, index) => {
					const fileItem = document.createElement('div');
					fileItem.className = 'file-item';
					fileItem.dataset.index = index;

					// 确定显示的文件名
					const displayName = file.webkitRelativePath ? file.webkitRelativePath : file.name;

					// 使用普通字符串拼接，避免模板字符串问题
					fileItem.innerHTML = 
						'<div class="file-info">' +
						'  <div class="file-icon-small">📄</div>' +
						'  <div class="file-name">' + displayName + '</div>' +
						'  <div class="file-size">' + formatFileSize(file.size) + '</div>' +
						'</div>' +
						'<button type="button" class="remove-file" title="删除文件">✕</button>';

					selectedFilesContainer.appendChild(fileItem);
				});

				// 添加删除文件事件监听器
				const removeButtons = document.querySelectorAll('.remove-file');
				removeButtons.forEach(btn => {
					btn.addEventListener('click', function() {
						const index = parseInt(this.parentElement.dataset.index);
						removeFile(index);
					});
				});
			}

			// 删除单个文件
			function removeFile(index) {
				selectedFiles.splice(index, 1);
				displayFileList();
				// 更新fileInput的files属性
				updateFileInput();
			}

			// 清空文件列表
			clearFilesBtn.addEventListener('click', function() {
				selectedFiles = [];
				displayFileList();
				// 更新fileInput的files属性
				updateFileInput();
			});

			// 更新fileInput的files属性
			function updateFileInput() {
				// 创建一个新的DataTransfer对象
				const dataTransfer = new DataTransfer();
				// 将selectedFiles中的文件添加到DataTransfer
				selectedFiles.forEach(file => {
					dataTransfer.items.add(file);
				});
				// 更新fileInput的files属性
				fileInput.files = dataTransfer.files;
			}

			// 递归处理文件和文件夹（追加模式）
			async function handleFiles(files) {
				// 遍历新选择的文件，追加到现有列表中
				for (let i = 0; i < files.length; i++) {
					const file = files[i];
					// 检查文件是否已存在，避免重复添加
					if (!isFileExists(file)) {
						selectedFiles.push(file);
					}
				}
				// 显示更新后的文件列表
				displayFileList();
			}

			// 上传文件
			uploadBtn.addEventListener('click', function() {
				if (selectedFiles.length === 0) {
					alert('请选择要上传的文件');
					return;
				}

				const directory = directorySelect.value;
				uploadFiles(selectedFiles, directory);
			});

			// 上传多个文件
			function uploadFiles(files, targetDir) {
				let totalFiles = files.length;
				let uploadedFiles = 0;
				let totalSize = 0;
				let uploadedSize = 0;

				// 计算总大小
				for (let file of files) {
					totalSize += file.size;
				}

				// 显示进度条
				progressContainer.style.display = 'block';
				uploadBtn.disabled = true;

				// 逐个上传文件
				files.forEach((file, index) => {
					const formData = new FormData();
					formData.append('file', file);
					formData.append('directory', targetDir);
					// 传递相对路径，用于保留文件夹结构
					if (file.webkitRelativePath) {
						formData.append('relativePath', file.webkitRelativePath);
					}

					const xhr = new XMLHttpRequest();

					// 监听上传进度
					xhr.upload.addEventListener('progress', function(e) {
						if (e.lengthComputable) {
							// 更新已上传大小
							const fileUploaded = uploadedSize + e.loaded;
							const percentComplete = Math.round((fileUploaded / totalSize) * 100);
							progressBar.style.width = percentComplete + '%';
							progressText.textContent = percentComplete + '% (' + (uploadedFiles + 1) + '/' + totalFiles + ')';
						}
					});

					// 上传完成处理
					xhr.addEventListener('load', function() {
						uploadedFiles++;
						uploadedSize += file.size;

						// 更新进度
						const percentComplete = Math.round((uploadedSize / totalSize) * 100);
						progressBar.style.width = percentComplete + '%';
						progressText.textContent = percentComplete + '% (' + uploadedFiles + '/' + totalFiles + ')';

						// 所有文件上传完成
						if (uploadedFiles === totalFiles) {
							window.location.href = '/files?path=' + encodeURIComponent(targetDir) + '&msg=' + encodeURIComponent('文件上传成功');
						}
					});

					// 上传错误处理
					xhr.addEventListener('error', function() {
						alert('文件上传失败，请重试');
						// 重置进度条
						progressContainer.style.display = 'none';
						progressBar.style.width = '0%';
						progressText.textContent = '0%';
						uploadBtn.disabled = false;
					});

					// 发送请求
					xhr.open('POST', '/upload', true);
					xhr.send(formData);
				});
			}
		});
	</script>
</body>
</html>`

		w.Write([]byte(html))
		return
	}

	// POST请求：处理文件上传
	if r.Method == "POST" {
		// 解析表单
		err = r.ParseMultipartForm(10 * 1024 * 1024) // 限制表单大小为10MB
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/upload?msg=%s&type=error", url.QueryEscape("表单解析失败")), http.StatusFound)
			return
		}

		// 获取选择的目录
		path := r.FormValue("directory")
		if path == "" {
			path = "."
		}

		// URL解码目录名（因为下拉框中的值是URL编码的）
		path, err = url.QueryUnescape(path)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/upload?msg=%s&type=error", url.QueryEscape("目录名解析失败")), http.StatusFound)
			return
		}

		// 安全检查
		path = filepath.Clean(path)
		if strings.HasPrefix(path, "..") {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}

		// 获取文件
		file, handler, err := r.FormFile("file")
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/upload?msg=%s&type=error", url.QueryEscape("文件获取失败")), http.StatusFound)
			return
		}
		defer file.Close()

		// 检查文件大小
		if sess.MaxFileSize > 0 && handler.Size > sess.MaxFileSize {
			http.Redirect(w, r, fmt.Sprintf("/upload?msg=%s&type=error", url.QueryEscape(fmt.Sprintf("文件大小超过限制 (%s)", utils.FormatFileSize(sess.MaxFileSize)))), http.StatusFound)
			return
		}

		// 获取相对路径（用于文件夹上传，保留目录结构）
		relativePath := r.FormValue("relativePath")
		var filename string
		var fullPath string

		// 根据是否有相对路径决定文件名和保存路径
		if relativePath != "" {
			// 文件夹上传，使用相对路径保留目录结构
			filename = utils.SanitizeFilename(relativePath)
			// 提取路径部分
			pathOnly := filepath.Dir(filename)
			// 构建完整路径
			fullPath = filepath.Join(path, pathOnly)
		} else {
			// 单个文件上传
			filename = utils.SanitizeFilename(handler.Filename)
			fullPath = path
		}

		// 根据用户角色决定保存目录
		var targetDir string
		var successMsg string

		if sess.Role == constants.RoleAdmin {
			// 管理员直接保存到下载目录
			targetDir = config.AppConfig.Server.DownloadDir
			successMsg = fmt.Sprintf("文件 '%s' 上传成功", filename)
		} else {
			// 测试用户和普通用户保存到待审核目录的用户子目录
			targetDir = filepath.Join(config.AppConfig.Server.PendingDir, sess.Username)
			successMsg = fmt.Sprintf("文件 '%s' 上传成功，等待管理员审核", filename)
		}

		// 构建保存路径
		savePath := filepath.Join(targetDir, fullPath, filepath.Base(filename))

		// 创建目标目录（递归创建所有必要的父目录）
		err = os.MkdirAll(filepath.Dir(savePath), 0755)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/upload?msg=%s&type=error", url.QueryEscape("创建目录失败")), http.StatusFound)
			return
		}

		// 创建目标文件
		dst, err := os.Create(savePath)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/upload?msg=%s&type=error", url.QueryEscape("创建文件失败")), http.StatusFound)
			return
		}
		defer dst.Close()

		// 复制文件内容
		_, err = io.Copy(dst, file)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/upload?msg=%s&type=error", url.QueryEscape("文件保存失败")), http.StatusFound)
			return
		}

		// 记录日志
		var roleStr string
		switch sess.Role {
		case constants.RoleAdmin:
			roleStr = "admin"
		case constants.RoleNormal:
			roleStr = "normal"
		case constants.RoleTest:
			roleStr = "test"
		default:
			roleStr = "unknown"
		}
		utils.Log(utils.LogLevelSuccess, sess.Username, roleStr, "upload_file", fmt.Sprintf("文件: %s，状态: %s，路径: %s", filename, successMsg, path))

		// 重定向回文件列表页面并显示成功消息
		http.Redirect(w, r, fmt.Sprintf("/files?path=%s&msg=%s&type=success", url.QueryEscape(path), url.QueryEscape(successMsg)), http.StatusFound)
	}
}
