package handlers

import (
	"fmt"
	"log"
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

// 文件审核页面处理函数
func ReviewHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil || sess.Role != constants.RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 获取当前路径
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "."
	}

	// 声明错误变量
	var err error
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

	// 构建目录列表
	var dirList []string
	dirList = utils.GetDirectoryList(config.AppConfig.Server.DownloadDir)

	// 获取所有用户子目录
	pendingRootDir := config.AppConfig.Server.PendingDir
	userDirs, err := os.ReadDir(pendingRootDir)
	if err != nil {
		http.Error(w, fmt.Sprintf("无法读取待审核根目录: %v", err), http.StatusInternalServerError)
		return
	}

	// 构建待审核文件列表HTML
	pendingFilesHTML := ""
	totalFiles := 0

	// 遍历所有用户子目录
	for _, userDir := range userDirs {
		if !userDir.IsDir() {
			continue
		}
		username := userDir.Name()

		// 递归查找当前用户所有待审核文件
		userPendingRoot := filepath.Join(pendingRootDir, username)
		log.Printf("DEBUG: 检查用户待审核根目录: %s", userPendingRoot)

		var findAllFiles func(string)
		findAllFiles = func(dirPath string) {
			userFiles, err := os.ReadDir(dirPath)
			if err != nil {
				// 如果目录不存在，跳过
				return
			}

			// 遍历当前用户的待审核文件
			for _, file := range userFiles {
				fullFilePath := filepath.Join(dirPath, file.Name())
				if file.IsDir() {
					// 递归处理子目录
					findAllFiles(fullFilePath)
				} else {
					totalFiles++

					// 获取文件信息
					fileInfo, err := file.Info()
					if err != nil {
						log.Printf("获取文件信息失败: %v", err)
						continue
					}

					// 计算相对路径
					relPath, err := filepath.Rel(userPendingRoot, dirPath)
					if err != nil {
						relPath = "."
					}

					// 为当前文件单独构建目录选择下拉框HTML
					// 根据文件的上传路径（relPath）设置默认选中值
					fileDirSelectHTML := `<select name="target_dir" class="form-control">`
					for _, dir := range dirList {
						selected := ""
						// 默认选中文件的上传路径
						if dir == relPath {
							selected = " selected"
						}
						// 将根目录显示为"根目录"而不是"."
						displayName := dir
						if dir == "." {
							displayName = "根目录"
						}
						fileDirSelectHTML += fmt.Sprintf(`<option value="%s"%s>%s</option>`, url.QueryEscape(dir), selected, displayName)
					}
					fileDirSelectHTML += `</select>`

					pendingFilesHTML += fmt.Sprintf(`<div class="pending-file">
						<div class="file-info">
							<div class="file-name">%s</div>
							<div class="file-meta">
								%s • %s • <span style=\"color: blue;\">用户: %s</span> • <span style=\"color: gray;\">路径: %s</span>
							</div>
						</div>
						<div class="file-actions">
						<form method="POST" action="/approve">
							<input type="hidden" name="file" value="%s">
							<input type="hidden" name="current_path" value="%s">
							<input type="hidden" name="username" value="%s">
							<div class="form-group">
								<label for="target_dir">目标目录:</label>
								%s
								<button type="submit" class="btn btn-success">通过</button>
							</div>
						</form>
						<form method="POST" action="/reject">
							<input type="hidden" name="file" value="%s">
							<input type="hidden" name="current_path" value="%s">
							<input type="hidden" name="username" value="%s">
							<button type="submit" class="btn btn-danger">拒绝</button>
						</form>
					</div>
					</div>`,
						file.Name(),
						utils.FormatFileSize(fileInfo.Size()),
						fileInfo.ModTime().Format("2006-01-02 15:04:05"),
						username,
						relPath,
						url.QueryEscape(file.Name()),
						url.QueryEscape(relPath),
						username,
						fileDirSelectHTML,
						url.QueryEscape(file.Name()),
						url.QueryEscape(relPath),
						username,
					)
				}
			}
		}

		// 开始递归查找
		findAllFiles(filepath.Join(userPendingRoot, path))
	}

	// 如果没有待审核文件
	if totalFiles == 0 {
		pendingFilesHTML = `<div class="empty-message">
			<div class="empty-icon">📭</div>
			<h3>暂无待审核文件</h3>
			<p>所有文件已审核完成</p>
		</div>`
	}

	// 构建HTML页面
	html := `<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>文件审核 - ` + constants.ServerName + `</title>
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
		.review-panel {
			background-color: white;
			padding: 30px;
			border-radius: 5px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
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
		.pending-files {
			margin-top: 20px;
		}
		.pending-file {
			display: flex;
			flex-direction: column;
			padding: 15px;
			border: 1px solid #eee;
			border-radius: 5px;
			margin-bottom: 15px;
			background-color: #f9f9f9;
			transition: background-color 0.3s;
		}
		.pending-file:hover {
			background-color: #f0f0f0;
		}
		.file-info {
			margin-bottom: 15px;
		}
		.file-name {
			font-weight: bold;
			margin-bottom: 5px;
			font-size: 16px;
		}
		.file-meta {
			font-size: 14px;
			color: #666;
		}
		.file-actions {
			display: flex;
			gap: 20px;
			align-items: center;
			border-top: 1px solid #eee;
			padding-top: 15px;
		}
		.file-actions form {
			margin: 0;
		}
		.form-group {
			display: flex;
			align-items: center;
			gap: 10px;
		}
		.form-group label {
			margin: 0;
			font-weight: bold;
			color: #555;
		}
		.form-group .form-control {
			width: auto;
			margin-bottom: 0;
			min-width: 150px;
		}
		.action-buttons {
			display: flex;
			gap: 10px;
		}
		.btn {
			padding: 8px 16px;
			border: none;
			border-radius: 3px;
			cursor: pointer;
			text-decoration: none;
			font-size: 14px;
			transition: background-color 0.3s;
		}
		.btn-success {
			background-color: #28a745;
			color: white;
		}
		.btn-success:hover {
			background-color: #218838;
		}
		.btn-danger {
			background-color: #dc3545;
			color: white;
		}
		.btn-danger:hover {
			background-color: #c82333;
		}
		.form-control {
			display: block;
			width: 100%;
			padding: 10px;
			border: 1px solid #ddd;
			border-radius: 3px;
			font-size: 16px;
			margin-bottom: 10px;
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
				<a href="/admin">管理员</a>
			</div>
		</nav>

		<!-- 显示消息 -->
		` + utils.GetMessage(r) + `

		<div class="review-panel">
			<h2>文件审核</h2>

			<!-- 路径导航 -->
			<div class="path-bar">
				<div class="path-item">
					<a href="/review?path=./" class="path-link">📁 根目录</a>
				</div>
				` + utils.GeneratePathNavigation(path) + `
			</div>

			<!-- 待审核文件列表 -->
			<div class="pending-files">
				` + pendingFilesHTML + `
			</div>
		</div>
	</div>
</body>
</html>`

	w.Write([]byte(html))
}
