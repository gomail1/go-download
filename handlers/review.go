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
	if sess == nil {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}
	// 管理员和二级管理员都可以审核文件
	if sess.Role != constants.RoleAdmin && sess.Role != constants.RoleSubAdmin {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/?msg=您没有权限访问该页面", http.StatusFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 生成CSRF令牌字段，供审核通过/拒绝表单使用
	sessionID := utils.GetSessionIDFromRequest(r)
	csrfTokenField := utils.GenerateCSRFTokenField(sessionID)

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
					// 过滤掉BT客户端的临时文件和未完成的下载文件
					fileName := file.Name()
					if fileName == ".torrent.bolt.db" {
						// 跳过BT临时数据库文件
						continue
					}
					if strings.HasSuffix(fileName, ".part") {
						// 跳过未完成的下载文件
						continue
					}
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
					fileDirSelectHTML := `<select name="target_dir" class="form-control" style="width: 100px; margin-right: 5px;">`
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

					pendingFilesHTML += fmt.Sprintf(`<div class="file-item">
							<div class="file-item-content">
								<div class="file-icon">📄</div>
								<div class="file-info">
									<div class="file-name">%s</div>
									<div class="file-meta">
										<span class="file-size">%s</span>
										<span class="file-date">%s</span>
										<span class="file-user" style="color: blue;">用户: %s</span>
										<span class="file-path" style="color: gray;">路径: %s</span>
									</div>
								</div>
							</div>
							<div class="file-actions-container">
								<div class="file-actions">
									<form method="POST" action="/approve" style="display: inline;">
						<input type="hidden" name="file" value="%s">
						<input type="hidden" name="current_path" value="%s">
						<input type="hidden" name="username" value="%s">
						%s
						%s
						<button type="submit" class="btn btn-success">通过</button>
				</form>
				<form method="POST" action="/reject" style="display: inline;">
						<input type="hidden" name="file" value="%s">
						<input type="hidden" name="current_path" value="%s">
						<input type="hidden" name="username" value="%s">
						%s
						<button type="submit" class="btn btn-danger">拒绝</button>
				</form>
								</div>
							</div>
						</div>`,
						utils.EscapeHTML(file.Name()),
						utils.FormatFileSize(fileInfo.Size()),
						fileInfo.ModTime().Format("2006-01-02 15:04:05"),
						username,
						relPath,
						url.QueryEscape(file.Name()),
						url.QueryEscape(relPath),
						username,
						fileDirSelectHTML,
						csrfTokenField,
						url.QueryEscape(file.Name()),
						url.QueryEscape(relPath),
						username,
						csrfTokenField,
					)
				}
			}
		}

		// 开始递归查找，从用户待审核根目录开始，显示所有待审核文件
		findAllFiles(userPendingRoot)
	}

	// 如果没有待审核文件
	if totalFiles == 0 {
		pendingFilesHTML = `<div class="file-item">
			<div style="font-size: 64px; margin-bottom: 20px;">📭</div>
			<h3>暂无待审核文件</h3>
			<p>所有文件已审核完成</p>
		</div>`
	}

	// 构建HTML页面
	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
	` + utils.GenerateCSRFTokenMeta(sessionID) + `
	<script src="/static/js/csrf.js"></script>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=5.0, minimum-scale=1.0">
	<title>文件审核 - ` + config.AppConfig.Server.ServerName + `</title>
	<link rel="icon" href="data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 100 100%22><text y=%22.9em%22 font-size=%2290%22>📦</text></svg>">
	<link rel="stylesheet" href="/static/styles.css">

</head>
<body class="v2 admin-layout">
		<div class="admin-layout-wrapper">
			` + utils.GetAdminSidebar(r, config.AppConfig.Server.ServerName) + `
			<main class="admin-main">
				<div class="admin-page-header">
					<h1 class="admin-page-title">文件审核</h1>
					<p class="admin-page-desc">审核用户上传的待审核文件</p>
				</div>

		<!-- 显示消息 -->
		<div class="upload-message-v2">
			` + utils.GetMessage(r) + `
		</div>

		<!-- 路径导航 -->
		<nav class="breadcrumb-v2" aria-label="面包屑导航">
			` + func() string {
				if path == "." || path == "./" {
					// 当前在根目录，高亮显示
					return `<span class="breadcrumb-current breadcrumb-home-active">
						<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"></path><polyline points="9 22 9 12 15 12 15 22"></polyline></svg>
						根目录
					</span>`
				}
				// 不在根目录，可点击返回根目录
				return `<a href="/review?path=./" class="breadcrumb-home" title="根目录">
					<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"></path><polyline points="9 22 9 12 15 12 15 22"></polyline></svg>
					根目录
				</a>`
			}() + `
			` + utils.GeneratePathNavigationWithBase(path, "/review") + `
		</nav>

		<!-- 待审核文件列表 -->
		<div class="pending-files-v2">
			` + pendingFilesHTML + `
		</div>
	</main>
		</div>
	</body>
</html>`

	w.Write([]byte(html))
}
