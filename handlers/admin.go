package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/session"
	"go-download-server/utils"
)

// 服务器启动时间
var StartTime time.Time

// 统计待审核文件数量
func countPendingFiles() int {
	pendingRootDir := config.AppConfig.Server.PendingDir
	count := 0

	// 遍历所有用户子目录
	userDirs, err := os.ReadDir(pendingRootDir)
	if err != nil {
		return count
	}

	// 遍历每个用户目录
	for _, userDir := range userDirs {
		if !userDir.IsDir() {
			continue
		}

		userPendingDir := filepath.Join(pendingRootDir, userDir.Name())

		// 递归统计当前用户目录下的所有文件
		err := filepath.Walk(userPendingDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				count++
			}
			return nil
		})
		if err != nil {
			continue
		}
	}

	return count
}

// 管理员页面处理函数
func AdminHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil || sess.Role != constants.RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 构建HTML页面
	html := `<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>管理员 - ` + constants.ServerName + `</title>
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
		/* 导航栏管理员链接徽章样式 */
		.admin-link {
			position: relative;
			padding-right: 20px;
		}
		.nav-links .admin-link .pending-count {
			top: -8px;
			right: -8px;
			font-size: 12px;
			width: 20px;
			height: 20px;
		}
		.admin-panel {
			background-color: white;
			padding: 30px;
			border-radius: 5px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.admin-options {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
			gap: 20px;
			margin-top: 20px;
		}
		.admin-option {
			padding: 20px;
			background-color: #f9f9f9;
			border-radius: 5px;
			text-align: center;
			border: 1px solid #eee;
			transition: transform 0.3s, box-shadow 0.3s;
			position: relative;
		}
		.admin-option:hover {
			transform: translateY(-5px);
			box-shadow: 0 5px 15px rgba(0,0,0,0.1);
		}
		.admin-option-icon {
			font-size: 48px;
			margin-bottom: 10px;
		}
		.admin-option-title {
			font-size: 18px;
			font-weight: bold;
			margin-bottom: 5px;
		}
		.admin-option-description {
			font-size: 14px;
			color: #666;
			margin-bottom: 15px;
		}
		.pending-count {
			position: absolute;
			top: -10px;
			right: -10px;
			background-color: #dc3545;
			color: white;
			font-size: 16px;
			font-weight: bold;
			width: 30px;
			height: 30px;
			border-radius: 50%;
			display: flex;
			align-items: center;
			justify-content: center;
			box-shadow: 0 2px 4px rgba(0,0,0,0.2);
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
		.btn-primary {
			background-color: #4CAF50;
			color: white;
		}
		.btn-primary:hover {
			background-color: #45a049;
		}
		.stats {
			background-color: #f9f9f9;
			padding: 20px;
			border-radius: 5px;
			margin-bottom: 20px;
		}
		.stat-item {
			display: inline-block;
			margin-right: 30px;
			margin-bottom: 10px;
		}
		.stat-label {
			font-size: 14px;
			color: #666;
		}
		.stat-value {
			font-size: 24px;
			font-weight: bold;
			color: #333;
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

		<div class="admin-panel">
			<h2>管理员控制面板</h2>
			
			<!-- 服务器统计信息 -->
			<div class="stats">
				<h3>服务器统计</h3>
				<div class="stat-item">
					<div class="stat-label">当前时间</div>
					<div class="stat-value">` + time.Now().Format("2006-01-02 15:04:05") + `</div>
				</div>
				<div class="stat-item">
					<div class="stat-label">运行时间</div>
					<div class="stat-value">` + utils.FormatDuration(time.Since(StartTime)) + `</div>
				</div>
			</div>

			<!-- 管理员选项 -->
			<div class="admin-options">
				<!-- 创建目录 -->
				<div class="admin-option">
					<div class="admin-option-icon">📁</div>
					<div class="admin-option-title">创建目录</div>
					<div class="admin-option-description">在服务器上创建新目录</div>
					<a href="/mkdir" class="btn btn-primary">创建目录</a>
				</div>

				<!-- 文件审核 -->
				<div class="admin-option">
					<div class="pending-count">` + fmt.Sprintf("%d", countPendingFiles()) + `</div>
					<div class="admin-option-icon">✅</div>
					<div class="admin-option-title">文件审核</div>
					<div class="admin-option-description">审核用户上传的文件</div>
					<a href="/review" class="btn btn-primary">审核文件</a>
				</div>

				<!-- 用户管理 -->
				<div class="admin-option">
					<div class="admin-option-icon">👤</div>
					<div class="admin-option-title">用户管理</div>
					<div class="admin-option-description">管理用户账号和密码</div>
					<a href="/user-management" class="btn btn-primary">用户管理</a>
				</div>

				<!-- 查看日志 -->
				<div class="admin-option">
					<div class="admin-option-icon">📝</div>
					<div class="admin-option-title">查看日志</div>
					<div class="admin-option-description">查看服务器日志</div>
					<a href="/logs" class="btn btn-primary">查看日志</a>
				</div>

				<!-- 服务器信息 -->
				<div class="admin-option">
					<div class="admin-option-icon">ℹ️</div>
					<div class="admin-option-title">服务器信息</div>
					<div class="admin-option-description">查看服务器详细信息</div>
					<a href="/info" class="btn btn-primary">查看信息</a>
				</div>
			</div>
		</div>
	</div>
</body>
</html>`

	w.Write([]byte(html))
}
