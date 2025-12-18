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

// 目录创建处理函数
func MkdirHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil || sess.Role != constants.RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// GET请求：显示创建目录表单
	if r.Method == "GET" {
		// 获取当前路径
		path := r.URL.Query().Get("path")
		if path == "" {
			path = "."
		}

		// URL解码路径
		path, err := url.QueryUnescape(path)
		if err != nil {
			path = "."
		}

		// 安全检查
		path = filepath.Clean(path)
		if strings.HasPrefix(path, "..") {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}

		// 构建HTML页面
		html := `<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>创建目录 - ` + constants.ServerName + `</title>
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
		.form-container {
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
		input[type="text"] {
			width: 100%;
			padding: 10px;
			border: 1px solid #ddd;
			border-radius: 3px;
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
			background-color: #6c757d;
			color: white;
		}
		.btn-secondary:hover {
			background-color: #5a6268;
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

		<div class="form-container">
			<h2>创建目录</h2>

			<!-- 显示消息 -->
			` + utils.GetMessage(r) + `

			<!-- 创建目录表单 -->
			<form method="POST">
				<div class="form-group">
					<label for="parent_dir">父目录</label>
					<input type="text" id="parent_dir_display" value="` + (func() string {
			if path == "." {
				return "根目录"
			} else {
				return path
			}
		}()) + `" readonly>
					<input type="hidden" id="parent_dir" name="parent_dir" value="` + path + `">
				</div>

				<div class="form-group">
					<label for="dir_name">目录名称</label>
					<input type="text" id="dir_name" name="dir_name" placeholder="请输入目录名称" required>
				</div>

				<div class="form-group">
					<button type="submit" class="btn btn-primary">创建目录</button>
					<a href="/files?path=` + path + `" class="btn btn-secondary">返回</a>
				</div>
			</form>
		</div>
	</div>
</body>
</html>`

		w.Write([]byte(html))
		return
	}

	// POST请求：处理目录创建
	if r.Method == "POST" {
		// 解析表单
		r.ParseForm()
		parentDir := r.FormValue("parent_dir")
		dirName := r.FormValue("dir_name")

		// 检查目录名称
		if dirName == "" {
			http.Redirect(w, r, fmt.Sprintf("/mkdir?path=%s&msg=%s&type=error", url.QueryEscape(parentDir), url.QueryEscape("目录名称不能为空")), http.StatusFound)
			return
		}

		// 清理目录名称
		dirName = utils.SanitizeFilename(dirName)

		// 安全检查
		parentDir = filepath.Clean(parentDir)
		if strings.HasPrefix(parentDir, "..") {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}

		// 构建完整路径
		fullPath := filepath.Join(config.AppConfig.Server.DownloadDir, parentDir, dirName)

		// 检查目录是否已存在
		if _, err := os.Stat(fullPath); err == nil {
			http.Redirect(w, r, fmt.Sprintf("/mkdir?path=%s&msg=%s&type=error", url.QueryEscape(parentDir), url.QueryEscape("目录已存在")), http.StatusFound)
			return
		}

		// 创建目录
		err := os.MkdirAll(fullPath, 0755)
		if err != nil {
			log.Printf("创建目录失败: %v", err)
			http.Redirect(w, r, fmt.Sprintf("/mkdir?path=%s&msg=%s&type=error", url.QueryEscape(parentDir), url.QueryEscape(fmt.Sprintf("目录创建失败: %v", err))), http.StatusFound)
			return
		}

		// 记录日志
		log.Printf("管理员 %s 创建了目录: %s", sess.Username, fullPath)

		// 重定向回文件列表页面并显示成功消息
		successMsg := fmt.Sprintf("目录 '%s' 创建成功", dirName)
		http.Redirect(w, r, fmt.Sprintf("/files?path=%s&msg=%s&type=success", url.QueryEscape(filepath.Join(parentDir, dirName)), url.QueryEscape(successMsg)), http.StatusFound)
	}
}
