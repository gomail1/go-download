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

// returnError 根据请求类型返回错误响应
func returnError(w http.ResponseWriter, r *http.Request, message string) {
	// 检查是否是AJAX请求
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		// AJAX请求，返回JSON响应
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf(`{"success": false, "message": "%s"}`, message)))
	} else {
		// 普通请求，重定向回上传页面并显示错误消息
		http.Redirect(w, r, fmt.Sprintf("/upload?msg=%s&type=error", url.QueryEscape(message)), http.StatusFound)
	}
}

// 上传文件处理函数
func UploadHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	// 检查协议同意状态
	if !sess.AgreedToTerms {
		http.Redirect(w, r, "/terms", http.StatusFound)
		return
	}

	// 计算今日剩余上传空间
	var remainingUpload string
	if sess.Username == "admin" {
		remainingUpload = "无限制"
	} else {
		remainingUpload = utils.FormatFileSize(GetRemainingUpload(sess.Username))
	}

	var err error

	// GET请求：显示上传表单
	if r.Method == "GET" {
		// 获取CSRF令牌隐藏字段
		sessionID := utils.GetSessionIDFromRequest(r)
		csrfTokenField := utils.GenerateCSRFTokenField(sessionID)

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

		// 构建目录选择树形视图
		// 1. 创建目录树结构
		type DirNode struct {
			Name     string
			Path     string
			Children []*DirNode
		}

		root := &DirNode{Name: "根目录", Path: ".", Children: []*DirNode{}}
		dirMap := map[string]*DirNode{".": root}

		// 按路径长度排序，确保父目录先于子目录处理
		sort.Slice(dirList, func(i, j int) bool {
			return len(dirList[i]) < len(dirList[j])
		})

		// 构建目录树
		for _, dir := range dirList {
			if dir == "." {
				continue // 跳过根目录
			}

			// 解析父目录路径
			parentPath := filepath.Dir(dir)
			if parentPath == "" {
				parentPath = "."
			}

			// 获取或创建父节点
			parentNode, exists := dirMap[parentPath]
			if !exists {
				parentNode = &DirNode{Name: filepath.Base(parentPath), Path: parentPath, Children: []*DirNode{}}
				dirMap[parentPath] = parentNode
				// 如果父节点还没有父节点，将其添加到根节点
				if parentPath != "." {
					root.Children = append(root.Children, parentNode)
				}
			}

			// 创建当前节点
			nodeName := filepath.Base(dir)
			currentNode := &DirNode{Name: nodeName, Path: dir, Children: []*DirNode{}}
			dirMap[dir] = currentNode

			// 将当前节点添加到父节点的子节点列表
			parentNode.Children = append(parentNode.Children, currentNode)
		}

		// 2. 生成树形视图HTML
		var generateTreeHTML func(node *DirNode, level int) string
		generateTreeHTML = func(node *DirNode, level int) string {
			if node == nil {
				return ""
			}

			// 检查当前节点是否被选中
			selected := ""
			if node.Path == path {
				selected = " checked=\"checked\""
			}

			// 缩进
			indent := strings.Repeat("  ", level)

			// 节点HTML
			html := fmt.Sprintf(`%s<li class="tree-node">
			%s  <div class="tree-item">
			%s    <input type="radio" id="dir-%s" name="directory" value="%s"%s style="margin-right: 8px;">
			%s    <label for="dir-%s" class="tree-label">%s</label>
			%s    <span class="tree-toggle">▼</span>
			%s  </div>`,
				indent, indent, indent,
				url.QueryEscape(node.Path), url.QueryEscape(node.Path), selected,
				indent, url.QueryEscape(node.Path), node.Name,
				indent, indent)

			// 添加子节点
			if len(node.Children) > 0 {
				html += fmt.Sprintf(`
			%s  <ul class="tree-children">`, indent)

				for _, child := range node.Children {
					html += generateTreeHTML(child, level+1)
				}

				html += fmt.Sprintf(`
			%s  </ul>`, indent)
			}

			html += fmt.Sprintf(`
			%s</li>`, indent)

			return html
		}

		dirSelectHTML := `<div id="directory-tree" class="directory-tree">
			<ul class="tree-root">` + generateTreeHTML(root, 0) + `
			</ul>
		</div>`

		// 构建HTML页面
		html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=5.0, minimum-scale=1.0">
	<title>上传文件 - ` + config.AppConfig.Server.ServerName + `</title>
	` + utils.GenerateCSRFTokenMeta(sessionID) + `
	<script src="/static/js/csrf.js"></script>
	<link rel="icon" href="data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 100 100%22><text y=%22.9em%22 font-size=%2290%22>📦</text></svg>">
	<link rel="stylesheet" href="/static/styles.css">

</head>
<body class="v2 admin-layout">
		<div class="admin-layout-wrapper">
			` + utils.GetAdminSidebar(r, config.AppConfig.Server.ServerName) + `
			<main class="admin-main">
				<div class="admin-page-header">
					<h1 class="admin-page-title">上传文件</h1>
					<p class="admin-page-desc">选择目标目录，拖拽文件即可开始上传</p>
				</div>
			<div class="upload-page-v2">
			<!-- 显示消息 -->
			<div class="upload-message-v2">
				` + utils.GetMessage(r) + `
			</div>

			<!-- 上传表单 -->
			<form method="POST" enctype="multipart/form-data" class="upload-form-v2">
				` + csrfTokenField + `
				<div class="upload-grid-v2">
					<!-- 左侧：目录选择 -->
					<div class="upload-card-v2">
						<div class="upload-card-title-v2">
							<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
							选择目录
						</div>
						<div class="form-group-v2">
							` + dirSelectHTML + `
						</div>
					</div>

					<!-- 右侧：上传区域 -->
					<div class="upload-card-v2">
						<div class="upload-card-title-v2">
							<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
							上传文件
						</div>

						<!-- 拖拽上传区域 -->
						<div id="drop-area" class="drop-area-v2">
							<div class="drop-content-v2">
								<div class="drop-icon-v2">📁</div>
								<h3>拖拽文件或文件夹到此处</h3>
								<label class="file-label-v2">
									<input type="file" id="file" name="file" multiple required>
									<span class="btn-v2 btn-primary-v2">选择文件</span>
								</label>
								<p class="drop-hint-v2">支持选择多个文件或整个文件夹</p>
							</div>
						</div>

						<div class="max-size-info-v2">
							<div class="limit-item-v2"><span>最大文件大小</span><span>` + utils.GetMaxFileSizeText(sess) + `</span></div>
							<div class="limit-item-v2"><span>今日已上传</span><span>` + utils.FormatFileSize(GetDailyUpload(sess.Username)) + `</span></div>
							<div class="limit-item-v2"><span>今日剩余</span><span class="limit-highlight-v2">` + remainingUpload + `</span></div>
						</div>

						<!-- 已选择文件列表 -->
						<div id="file-list" class="file-list-container-v2" style="display: none;">
							<div class="file-list-header-v2">
								<h3>已选择的文件 (<span id="file-count">0</span>)</h3>
								<button type="button" id="clear-files" class="btn-v2 btn-secondary-v2 btn-sm-v2">清空列表</button>
							</div>
							<div id="selected-files" class="selected-files-v2"></div>
						</div>

						<!-- 上传进度条 -->
						<div id="upload-progress" class="upload-progress-v2" style="display: none;">
							<div class="progress-label-v2">上传进度:</div>
							<div class="progress-bar-container-v2">
								<div id="progress-bar" class="progress-bar-v2" style="width: 0%;"></div>
							</div>
							<div id="progress-text" class="progress-text-v2">0%</div>
						</div>

						<div class="form-actions-v2">
							<button type="button" id="upload-btn" class="btn-v2 btn-primary-v2">开始上传</button>
							<a href="/files?path=` + path + `" class="btn-v2 btn-secondary-v2">返回文件列表</a>
						</div>
					</div>
				</div>
			</form>
		</div>
	</main>
		</div>

	<script>
		// 上传进度监控
			document.addEventListener('DOMContentLoaded', function() {
				const fileInput = document.getElementById('file');
				const dropArea = document.getElementById('drop-area');
				const progressContainer = document.getElementById('upload-progress');
				const progressBar = document.getElementById('progress-bar');
				const progressText = document.getElementById('progress-text');
				const uploadBtn = document.getElementById('upload-btn');
				const fileListContainer = document.getElementById('file-list');
				const selectedFilesContainer = document.getElementById('selected-files');
				const fileCountElement = document.getElementById('file-count');
				const clearFilesBtn = document.getElementById('clear-files');
				
				// 树形视图功能
				const treeItems = document.querySelectorAll('.tree-item');
				treeItems.forEach(item => {
					const toggle = item.querySelector('.tree-toggle');
					const children = item.parentElement.querySelector('.tree-children');
					
					if (children) {
						// 如果有子节点，添加点击事件
						item.addEventListener('click', function(e) {
							// 只有点击toggle或item本身（非radio和label）时才切换展开/折叠
							if (e.target === toggle || e.target === item) {
								e.preventDefault();
								toggle.classList.toggle('collapsed');
								children.classList.toggle('collapsed');
							}
						});
					} else {
						// 如果没有子节点，隐藏toggle
						if (toggle) {
							toggle.style.display = 'none';
						}
					}
				});
				
				// 默认只展开第一级目录
				const firstLevelChildren = document.querySelectorAll('.tree-root > .tree-node > .tree-children');
				firstLevelChildren.forEach(children => {
					children.classList.add('collapsed');
					const toggle = children.parentElement.querySelector('.tree-toggle');
					if (toggle) {
						toggle.classList.add('collapsed');
					}
				});

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
					// 对于带有相对路径的文件，使用相对路径和大小进行比较
					if (file.webkitRelativePath && existingFile.webkitRelativePath) {
						if (existingFile.webkitRelativePath === file.webkitRelativePath && existingFile.size === file.size) {
							return true;
						}
					} else {
						// 对于普通文件，使用文件名和大小进行比较
						if (existingFile.name === file.name && existingFile.size === file.size) {
							return true;
						}
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

			// 递归读取文件系统句柄（文件或文件夹）
			async function readFileSystemHandle(handle, path = '') {
				if (handle.kind === 'file') {
					// 读取文件
					const file = await handle.getFile();
					// 添加相对路径信息
					file.webkitRelativePath = path + file.name;
					// 检查文件是否已存在，避免重复添加
					if (!isFileExists(file)) {
						selectedFiles.push(file);
					}
				} else if (handle.kind === 'directory') {
					// 读取文件夹
					const entries = await handle.values();
					for await (const entry of entries) {
						await readFileSystemHandle(entry, path + handle.name + '/');
					}
				}
			}

			// 处理DataTransferItemList中的所有项
			async function processDataTransferItems(items) {
				for (let i = 0; i < items.length; i++) {
					const item = items[i];
					if (item.kind === 'file') {
						// 首先检查是否为文件夹
						const entry = item.webkitGetAsEntry();
						if (entry && entry.isDirectory) {
							// 如果是文件夹，递归读取内容
							await readDirectoryEntry(entry, '');
						} else if (entry && entry.isFile) {
							// 如果是文件，读取文件内容
							entry.file(function(file) {
								// 添加相对路径信息
								file.webkitRelativePath = entry.fullPath.substring(1); // 移除开头的斜杠
								// 检查文件是否已存在，避免重复添加
								if (!isFileExists(file)) {
									selectedFiles.push(file);
								}
							});
						} else {
							// 对于不支持webkitGetAsEntry的浏览器，使用getAsFile()
							const file = item.getAsFile();
							if (file) {
								// 检查文件大小是否大于0，避免添加文件夹本身
								if (file.size > 0) {
									// 检查文件是否已存在，避免重复添加
									if (!isFileExists(file)) {
										selectedFiles.push(file);
									}
								} else {
									// 尝试使用FileSystemHandle API读取文件夹内容
									try {
										const handle = await item.getAsFileSystemHandle();
										if (handle) {
											await readFileSystemHandle(handle, '');
										}
									} catch (err) {
										console.error('Error reading file system handle:', err);
									}
								}
							}
						}
					}
				}
				// 显示更新后的文件列表
				displayFileList();
			}

			// 使用FileEntry API递归读取文件夹内容（兼容更多浏览器）
			async function readDirectoryEntry(entry, path = '') {
				return new Promise((resolve) => {
					if (entry.isFile) {
						// 读取文件
						entry.file(function(file) {
							// 添加相对路径信息
							file.webkitRelativePath = path + entry.name;
							// 检查文件是否已存在，避免重复添加
							if (!isFileExists(file)) {
								selectedFiles.push(file);
							}
							resolve();
						});
					} else if (entry.isDirectory) {
						// 读取文件夹
						const reader = entry.createReader();
						reader.readEntries(async function(entries) {
							// 创建一个Promise数组，等待所有子项处理完成
							const promises = [];
							for (let i = 0; i < entries.length; i++) {
								promises.push(readDirectoryEntry(entries[i], path + entry.name + '/'));
							}
							// 等待所有子项处理完成
							await Promise.all(promises);
							resolve();
						});
					} else {
						resolve();
					}
				});
			}

			// 处理文件放置
			dropArea.addEventListener('drop', async function(e) {
				const items = e.dataTransfer.items;
				await processDataTransferItems(items);
			}, false);

			// 处理文件选择
			fileInput.addEventListener('change', function() {
				// 遍历新选择的文件，追加到现有列表中
				for (let i = 0; i < this.files.length; i++) {
					const file = this.files[i];
					// 检查文件是否已存在，避免重复添加
					if (!isFileExists(file)) {
						selectedFiles.push(file);
					}
				}
				// 显示更新后的文件列表
				displayFileList();
			});

			// 显示动态消息
			const showDynamicMessage = function(message, type) {
				const messageDiv = document.createElement('div');
				messageDiv.className = 'message message-' + type;
				messageDiv.textContent = message;
				messageDiv.style.marginBottom = '20px';
				messageDiv.style.padding = '10px';
				messageDiv.style.borderRadius = '3px';
				messageDiv.style.transition = 'all 0.3s ease';
				messageDiv.style.opacity = '0';
				messageDiv.style.transform = 'translateY(-10px)';

				// 插入到表单上方
				const form = document.querySelector('.upload-form');
				const header = form.querySelector('h2');
				form.insertBefore(messageDiv, header.nextSibling);

				// 显示动画
				setTimeout(() => {
					messageDiv.style.opacity = '1';
					messageDiv.style.transform = 'translateY(0)';
				}, 100);

				// 3秒后自动消失
				setTimeout(() => {
					messageDiv.style.opacity = '0';
					messageDiv.style.transform = 'translateY(-10px)';
					setTimeout(() => {
						messageDiv.remove();
					}, 300);
				}, 3000);
			};

			// 上传文件
			uploadBtn.addEventListener('click', function() {
				if (selectedFiles.length === 0) {
					alert('请选择要上传的文件');
					return;
				}

				// 获取选中的目录（从radio按钮中）
				const selectedRadio = document.querySelector('input[name="directory"]:checked');
				const directory = selectedRadio ? selectedRadio.value : '.';
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
						// 检查响应内容
						let response = null;
						let isSuccess = true;
						let errorMessage = '';
						
						try {
							// 尝试解析JSON响应
							response = JSON.parse(this.responseText);
							if (!response.success) {
								isSuccess = false;
								errorMessage = response.message || '上传失败';
							}
						} catch (e) {
							// 如果不是JSON响应，检查是否是HTML响应
							if (this.responseText.includes('今日上传量已超过限制')) {
								isSuccess = false;
								errorMessage = '今日上传量已超过限制';
							}
						}
						
						if (isSuccess) {
							uploadedFiles++;
							uploadedSize += file.size;

							// 更新进度
							const percentComplete = Math.round((uploadedSize / totalSize) * 100);
							progressBar.style.width = percentComplete + '%';
							progressText.textContent = percentComplete + '% (' + uploadedFiles + '/' + totalFiles + ')';

							// 所有文件上传完成
							if (uploadedFiles === totalFiles) {
								// 提示上传成功
								window.location.href = '/files?path=' + encodeURIComponent(targetDir) + '&msg=' + encodeURIComponent('文件上传成功');
							}
						} else {
							// 显示错误信息（使用页面内消息，不使用弹窗）
							showDynamicMessage(errorMessage, 'error');
							// 重置进度条
							progressContainer.style.display = 'none';
							progressBar.style.width = '0%';
							progressText.textContent = '0%';
							uploadBtn.disabled = false;
							uploadBtn.textContent = '开始上传';
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
					// 设置AJAX请求标识
					xhr.setRequestHeader('X-Requested-With', 'XMLHttpRequest');
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

	if r.Method == "POST" {
		// 验证CSRF令牌
		sessionID := utils.GetSessionIDFromRequest(r)
		csrfToken := r.FormValue("csrf_token")
		if !utils.ValidateCSRFToken(sessionID, csrfToken) {
			http.Error(w, "CSRF令牌验证失败", http.StatusForbidden)
			return
		}

		// 解析表单
		err = r.ParseMultipartForm(10 * 1024 * 1024) // 限制表单大小为10MB
		if err != nil {
			returnError(w, r, "表单解析失败")
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
			returnError(w, r, "目录名解析失败")
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
			returnError(w, r, "文件获取失败")
			return
		}
		defer file.Close()

		// 文件类型安全检查
		uploadFilename := handler.Filename
		// 检查是否为危险的WebShell文件类型
		dangerousWebExts := []string{
			".php", ".php3", ".php4", ".php5", ".phtml",
			".asp", ".aspx", ".ascx", ".ashx", ".asmx",
			".jsp", ".jspx", ".jsf", ".jws",
			".pl", ".pm", ".py", ".pyc", ".pyo", ".rb", ".rbw",
			".sh", ".bash", ".zsh", ".ksh", ".csh", ".fish",
			".cgi", ".fcgi", ".scgi",
		}
		fileExt := strings.ToLower(filepath.Ext(uploadFilename))
		for _, dangerousExt := range dangerousWebExts {
			if fileExt == dangerousExt {
				utils.Log(utils.LogLevelSecurity, sess.Username, "user", "upload_blocked",
					fmt.Sprintf("IP: %s 上传危险文件类型被阻止，filename=%s, ext=%s", utils.GetClientIP(r), uploadFilename, fileExt))
				returnError(w, r, fmt.Sprintf("不允许上传该类型文件 (%s)", fileExt))
				return
			}
		}

		// 检查文件大小
		if sess.MaxFileSize > 0 && handler.Size > sess.MaxFileSize {
			returnError(w, r, fmt.Sprintf("文件大小超过限制 (%s)", utils.FormatFileSize(sess.MaxFileSize)))
			return
		}

		// 检查每日上传限制 - 只有admin用户不受限制，其他所有登录用户都受限制
		if sess.Username != "admin" {
			// 获取今日已上传大小
			uploaded := GetDailyUpload(sess.Username)
			// 检查是否超过限制
			log.Printf("检查每日上传限制：用户=%s，已上传=%d B，本次上传=%d B，限制=%d B", sess.Username, uploaded, handler.Size, constants.DailyUploadLimit)
			if uploaded+handler.Size > constants.DailyUploadLimit {
				log.Printf("上传限制检查失败：用户=%s，已上传=%d B，本次上传=%d B，限制=%d B", sess.Username, uploaded, handler.Size, constants.DailyUploadLimit)

				// 检查是否是AJAX请求
				if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
					// AJAX请求，返回JSON响应
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte(`{"success": false, "message": "今日上传量已超过限制"}`))
				} else {
					// 普通请求，返回HTML响应
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte(`<html><body><script>alert('今日上传量已超过限制'); window.location.href='/upload';</script></body></html>`))
				}
				return
			}
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

		if sess.Role == constants.RoleAdmin || sess.Role == constants.RoleSubAdmin {
			// 管理员和二级管理员直接保存到下载目录
			targetDir = config.AppConfig.Server.DownloadDir
			successMsg = fmt.Sprintf("文件 '%s' 上传成功", filename)
		} else {
			// 普通用户保存到待审核目录的用户子目录
			targetDir = filepath.Join(config.AppConfig.Server.PendingDir, sess.Username)
			successMsg = fmt.Sprintf("文件 '%s' 上传成功，等待管理员审核", filename)
		}

		// 构建保存路径
		savePath := filepath.Join(targetDir, fullPath, filepath.Base(filename))

		// 创建目标目录（递归创建所有必要的父目录）
		err = os.MkdirAll(filepath.Dir(savePath), 0755)
		if err != nil {
			returnError(w, r, "创建目录失败")
			return
		}

		// 创建目标文件
		dst, err := os.Create(savePath)
		if err != nil {
			returnError(w, r, "创建文件失败")
			return
		}
		defer dst.Close()

		// 复制文件内容
		_, err = io.Copy(dst, file)
		if err != nil {
			returnError(w, r, "文件写入失败")
			return
		}

		// 更新上传统计
		AddDailyUpload(sess.Username, handler.Size)

		// 使相关缓存失效
		if sess.Role == constants.RoleAdmin || sess.Role == constants.RoleSubAdmin {
			// 管理员和二级管理员直接保存到下载目录，使下载目录中的对应路径缓存失效
			targetPath := filepath.Join(config.AppConfig.Server.DownloadDir, path)
			invalidateCache(targetPath)
		}

		// 记录日志
		utils.LogUserAction(r, "upload_file", fmt.Sprintf("上传文件: %s", savePath))

		// 检查是否是AJAX请求
		if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
			// AJAX请求，返回JSON响应
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success": true, "message": "上传成功"}`))
		} else {
			// 普通请求，重定向到文件列表页面
			http.Redirect(w, r, fmt.Sprintf("/files?path=%s&msg=%s", url.QueryEscape(path), url.QueryEscape(successMsg)), http.StatusFound)
		}
	}
}
