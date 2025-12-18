package handlers

import (
	"fmt"
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

		// 生成文件项
		var item string
		if file.IsDir() {
			item = fmt.Sprintf(`<div class="file-item">
				<div class="file-icon">%s</div>
				<div class="file-info">
					<div class="file-name"><a href="/files?path=%s">%s</a></div>
					<div class="file-meta">%s</div>
				</div>
				<div class="file-actions">
					%s
				</div>
			</div>`, icon, fileURL, name, meta, utils.GetAdminActions(r, filePath))
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
				<div class="file-icon">%s</div>
				<div class="file-info">
					<div class="file-name">%s</div>
					<div class="file-meta">%s</div>
				</div>
				<div class="file-actions">
					<a href="/download?path=%s" class="btn btn-secondary">下载</a>
					%s
				</div>
			</div>`, icon, name, meta, fileURL, utils.GetAdminActions(r, filePath))
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

	return fileList
}
