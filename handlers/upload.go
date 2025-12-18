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
			padding: 30px;
			border-radius: 5px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
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
		select.form-control {
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

				<div class="form-group">
					<label for="file">选择文件</label>
					<input type="file" id="file" name="file" required>
					<div class="max-size-info">
						最大文件大小: ` + utils.GetMaxFileSizeText(sess) + `
					</div>
				</div>

				<div class="form-group">
					<button type="submit" class="btn btn-primary">开始上传</button>
					<a href="/files?path=` + path + `" class="btn btn-secondary">返回</a>
				</div>
			</form>
		</div>
	
		<footer>
			<p>版本: ` + constants.Version + ` | 开发者: ` + constants.Developer + `</p>
		</footer>
	</div>
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

		// 清理文件名
		filename := utils.SanitizeFilename(handler.Filename)

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
		savePath := filepath.Join(targetDir, path, filename)

		// 创建目标目录
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
