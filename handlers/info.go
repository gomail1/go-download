package handlers

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/session"
	"go-download-server/utils"
)

// 服务器信息处理函数
func InfoHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil || sess.Role != constants.RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 记录日志
	utils.LogUserAction(r, "view_server_info", "查看服务器信息")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 构建HTML页面
	html := `<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>服务器信息 - ` + constants.ServerName + `</title>
	<style>
		body {
			font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
			background-color: #f8f9fa;
			margin: 0;
			padding: 0;
			color: #333;
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
			border-radius: 8px;
			margin-bottom: 20px;
			box-shadow: 0 2px 10px rgba(0,0,0,0.1);
		}
		.header-content {
			display: flex;
			justify-content: space-between;
			align-items: center;
		}
		nav {
			background-color: white;
			padding: 15px;
			border-radius: 8px;
			margin-bottom: 20px;
			box-shadow: 0 2px 10px rgba(0,0,0,0.05);
			border: 1px solid #e9ecef;
		}
		.nav-links {
			display: flex;
			gap: 15px;
		}
		.nav-links a {
			text-decoration: none;
			color: #333;
			padding: 8px 16px;
			border-radius: 6px;
			transition: all 0.3s ease;
			font-weight: 500;
		}
		.nav-links a:hover {
			background-color: #e0e0e0;
			color: #4CAF50;
			transform: translateY(-1px);
		}
		.info-panel {
			background-color: white;
			padding: 30px;
			border-radius: 8px;
			box-shadow: 0 2px 10px rgba(0,0,0,0.05);
			border: 1px solid #e9ecef;
		}
		.info-panel h2 {
			color: #4CAF50;
			margin-bottom: 25px;
			font-size: 24px;
			border-bottom: 2px solid #e9ecef;
			padding-bottom: 10px;
		}
		.info-grid {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
			gap: 20px;
			margin-bottom: 30px;
		}
		.info-card {
			background-color: #f8f9fa;
			padding: 20px;
			border-radius: 8px;
			border: 1px solid #e9ecef;
			transition: all 0.3s ease;
			box-shadow: 0 2px 4px rgba(0,0,0,0.05);
		}
		.info-card:hover {
			transform: translateY(-5px);
			box-shadow: 0 8px 15px rgba(0,0,0,0.1);
			border-color: #4CAF50;
		}
		.info-card h3 {
			color: #4CAF50;
			margin-bottom: 15px;
			font-size: 18px;
			border-bottom: 1px solid #e9ecef;
			padding-bottom: 8px;
		}
		.info-item {
			display: flex;
			justify-content: space-between;
			margin-bottom: 12px;
			padding: 8px 0;
			border-bottom: 1px dashed #e9ecef;
		}
		.info-item:last-child {
			margin-bottom: 0;
			border-bottom: none;
		}
		.info-label {
			font-weight: 500;
			color: #555;
			min-width: 120px;
		}
		.info-value {
			color: #333;
			font-weight: 600;
		}
		.info-section {
			margin-bottom: 30px;
			padding: 20px;
			background-color: #f8f9fa;
			border-radius: 8px;
			border: 1px solid #e9ecef;
		}
		.info-section h3 {
			color: #4CAF50;
			margin-bottom: 15px;
			font-size: 18px;
			border-bottom: 2px solid #e9ecef;
			padding-bottom: 10px;
		}
		.stats-grid {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
			gap: 20px;
			margin-bottom: 30px;
		}
		.stat-card {
			background: linear-gradient(135deg, #4CAF50 0%, #45a049 100%);
			color: white;
			padding: 25px;
			border-radius: 8px;
			text-align: center;
			box-shadow: 0 4px 8px rgba(76, 175, 80, 0.3);
			transition: all 0.3s ease;
		}
		.stat-card:hover {
			transform: translateY(-5px);
			box-shadow: 0 8px 15px rgba(76, 175, 80, 0.4);
		}
		.stat-number {
			font-size: 36px;
			font-weight: bold;
			margin-bottom: 10px;
		}
		.stat-label {
			font-size: 14px;
			opacity: 0.9;
		}
		.btn {
			padding: 10px 20px;
			border: none;
			border-radius: 8px;
			cursor: pointer;
			text-decoration: none;
			font-size: 14px;
			font-weight: 500;
			transition: all 0.3s ease;
			text-align: center;
			display: inline-block;
			margin-right: 10px;
		}
		.btn-secondary {
			background-color: #6c757d;
			color: white;
			box-shadow: 0 2px 4px rgba(108, 117, 125, 0.2);
		}
		.btn-secondary:hover {
			background-color: #5a6268;
			transform: translateY(-1px);
			box-shadow: 0 4px 8px rgba(108, 117, 125, 0.3);
		}
		.footer {
			margin-top: 20px;
			text-align: center;
			color: #666;
			font-size: 14px;
			padding: 15px;
			border-top: 1px solid #e9ecef;
		}
		.permission-table {
			width: 100%;
			border-collapse: collapse;
			margin-top: 15px;
		}
		.permission-table th,
		.permission-table td {
			padding: 12px;
			text-align: left;
			border-bottom: 1px solid #e9ecef;
		}
		.permission-table th {
			background-color: #f8f9fa;
			font-weight: 600;
			color: #555;
		}
		.permission-table tr:hover {
			background-color: #f8f9fa;
		}
		/* 使用说明样式 */
		.instructions {
			margin-top: 15px;
			padding: 15px;
			background-color: #e8f5e9;
			border-left: 4px solid #4CAF50;
			border-radius: 0 8px 8px 0;
		}
		.instructions ul {
			margin: 0;
			padding-left: 20px;
		}
		.instructions li {
			margin-bottom: 8px;
			color: #333;
		}
		.instructions li:last-child {
			margin-bottom: 0;
		}
		/* 响应式设计 */
		@media (max-width: 768px) {
			.info-grid {
				grid-template-columns: 1fr;
			}
			.stats-grid {
				grid-template-columns: repeat(2, 1fr);
			}
		}
		@media (max-width: 480px) {
			.stats-grid {
				grid-template-columns: 1fr;
			}
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

		<div class="info-panel">
			<h2>服务器信息</h2>
			
			<!-- 统计卡片 -->
			<div class="stats-grid">
				<div class="stat-card">
					<div class="stat-number">` + fmt.Sprintf("%d", len(config.AppConfig.Users)) + `</div>
					<div class="stat-label">总用户数</div>
				</div>
				<div class="stat-card">
					<div class="stat-number">` + time.Now().Format("15:04:05") + `</div>
					<div class="stat-label">当前时间</div>
				</div>
				<div class="stat-card">
					<div class="stat-number">` + time.Now().Format("2006-01-02") + `</div>
					<div class="stat-label">当前日期</div>
				</div>
				<div class="stat-card">
					<div class="stat-number">` + utils.FormatDuration(time.Since(StartTime)) + `</div>
					<div class="stat-label">运行时间</div>
				</div>
			</div>

			<!-- 服务器基本信息 -->
			<div class="info-section">
				<h3>服务器基本信息</h3>
				<div class="info-grid">
					<div class="info-card">
						<h3>系统信息</h3>
						<div class="info-item">
							<div class="info-label">操作系统</div>
							<div class="info-value">` + runtime.GOOS + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">架构</div>
							<div class="info-value">` + runtime.GOARCH + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">Go版本</div>
							<div class="info-value">` + runtime.Version() + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">CPU核心数</div>
							<div class="info-value">` + fmt.Sprintf("%d", runtime.NumCPU()) + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">当前进程ID</div>
							<div class="info-value">` + fmt.Sprintf("%d", os.Getpid()) + `</div>
						</div>
					</div>

					<div class="info-card">
						<h3>服务器配置</h3>
						<div class="info-item">
							<div class="info-label">HTTP端口</div>
							<div class="info-value">` + fmt.Sprintf("%d", config.AppConfig.Server.Port) + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">HTTPS端口</div>
							<div class="info-value">` + fmt.Sprintf("%d", config.AppConfig.Server.HttpsPort) + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">下载目录</div>
							<div class="info-value">` + config.AppConfig.Server.DownloadDir + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">待审核目录</div>
							<div class="info-value">` + config.AppConfig.Server.PendingDir + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">日志目录</div>
							<div class="info-value">` + config.AppConfig.Server.LogDir + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">日志文件</div>
							<div class="info-value">` + config.AppConfig.Server.LogFile + `</div>
						</div>
					</div>

					<div class="info-card">
						<h3>项目信息</h3>
						<div class="info-item">
							<div class="info-label">项目名称</div>
							<div class="info-value">` + constants.ServerName + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">版本</div>
							<div class="info-value">` + constants.Version + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">开发者</div>
							<div class="info-value">` + constants.Developer + `</div>
						</div>
						<div class="info-item">
							<div class="info-label">启动时间</div>
							<div class="info-value">` + StartTime.Format("2006-01-02 15:04:05") + `</div>
						</div>
					</div>


				</div>
			</div>

			<!-- 用户角色权限表 -->
			<div class="info-section">
				<h3>用户角色权限</h3>
				<table class="permission-table">
					<tr>
						<th>权限</th>
						<th>管理员</th>
						<th>普通用户</th>
						<th>测试用户</th>
					</tr>
					<tr>
						<td>查看文件列表</td>
						<td>✓</td>
						<td>✓</td>
						<td>✓</td>
					</tr>
					<tr>
						<td>上传文件</td>
						<td>✓</td>
						<td>✓</td>
						<td>✓</td>
					</tr>
					<tr>
						<td>下载文件</td>
						<td>✓</td>
						<td>✓</td>
						<td>✓</td>
					</tr>
					<tr>
						<td>删除文件</td>
						<td>✓</td>
						<td>✓</td>
						<td>✓</td>
					</tr>
					<tr>
						<td>创建目录</td>
						<td>✓</td>
						<td>✓</td>
						<td>✓</td>
					</tr>
					<tr>
						<td>审核文件</td>
						<td>✓</td>
						<td>✗</td>
						<td>✗</td>
					</tr>
					<tr>
						<td>用户管理</td>
						<td>✓</td>
						<td>✗</td>
						<td>✗</td>
					</tr>
					<tr>
						<td>查看日志</td>
						<td>✓</td>
						<td>✗</td>
						<td>✗</td>
					</tr>
					<tr>
						<td>查看服务器信息</td>
						<td>✓</td>
						<td>✗</td>
						<td>✗</td>
					</tr>
				</table>
			</div>

			<!-- 使用说明 -->
			<div class="info-section">
				<h3>使用说明</h3>
				<div class="instructions">
					<ul>
						<li>管理员可以创建、修改和删除用户账号</li>
						<li>普通用户和测试用户可以上传文件，但需要管理员审核才能发布</li>
						<li>管理员可以查看所有用户的待审核文件并进行审批或拒绝</li>
						<li>用户可以查看自己的待审核文件状态</li>
						<li>所有操作都会被记录到服务器日志中</li>
						<li>服务器日志可以通过"查看日志"功能进行搜索和筛选</li>
						<li>管理员可以创建目录，方便文件分类管理</li>
						<li>用户可以修改自己的密码，但不能修改角色</li>
					</ul>
				</div>
			</div>

			<div class="footer">
				<p>版本: ` + constants.Version + ` | 开发者: ` + constants.Developer + `</p>
			</div>
		</div>
	</div>
</body>
</html>`

	w.Write([]byte(html))
}
