package handlers

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/session"
	"go-download-server/utils"
)

// 用户管理处理函数
func UserManagementHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil || sess.Role != constants.RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 构建用户列表HTML
	userListHTML := ""
	for _, user := range config.AppConfig.Users {
		userListHTML += fmt.Sprintf(`<tr>
			<td>%s</td>
			<td>%s</td>
			<td>%s</td>
			<td style="white-space: nowrap;">
				<form method="POST" action="/change-password" style="display: inline;">
					<input type="hidden" name="change_pwd" value="%s">
					<input type="password" name="new_pwd_%s" placeholder="新密码" style="width: 120px; margin-right: 5px;">
					<input type="password" name="confirm_pwd_%s" placeholder="确认密码" style="width: 120px; margin-right: 5px;">
					<button type="submit" class="btn btn-secondary btn-sm">修改</button>
				</form>
			</td>
			<td>
				%s
			</td>
		</tr>`,
			user.Username,
			utils.GetRoleNameByString(user.Role),
			utils.FormatFileSize(user.MaxFileSize),
			user.Username,
			user.Username,
			user.Username,
			getDeleteButton(user.Username),
		)
	}

	// 构建HTML页面
	html := `<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>用户管理 - ` + constants.ServerName + `</title>
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
			border-radius: 5px;
			margin-bottom: 20px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
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
			transition: all 0.3s ease;
		}
		.nav-links a:hover {
			background-color: #e0e0e0;
			color: #4CAF50;
		}
		.user-management {
			background-color: white;
			padding: 30px;
			border-radius: 8px;
			box-shadow: 0 2px 10px rgba(0,0,0,0.05);
			border: 1px solid #e9ecef;
		}
		h2 {
			color: #4CAF50;
			margin-bottom: 25px;
			font-size: 24px;
			border-bottom: 2px solid #e9ecef;
			padding-bottom: 10px;
		}
		h3 {
			color: #495057;
			margin-top: 25px;
			margin-bottom: 15px;
			font-size: 18px;
		}
		table {
			width: 100%;
			border-collapse: collapse;
			margin-top: 20px;
			background-color: white;
			border-radius: 8px;
			overflow: hidden;
			box-shadow: 0 2px 4px rgba(0,0,0,0.05);
		}
		th, td {
			padding: 15px;
			text-align: left;
			border-bottom: 1px solid #e9ecef;
		}
		th {
			background-color: #f8f9fa;
			font-weight: 600;
			color: #495057;
			text-transform: uppercase;
			font-size: 12px;
			letter-spacing: 0.5px;
		}
		tr {
			transition: all 0.3s ease;
		}
		tr:hover {
			background-color: #f8f9fa;
			transform: translateY(-1px);
			box-shadow: 0 2px 8px rgba(0,0,0,0.05);
		}
		.form-group {
			margin-bottom: 15px;
		}
		label {
			display: block;
			margin-bottom: 8px;
			font-weight: 500;
			color: #495057;
			font-size: 14px;
		}
		input[type="text"], input[type="password"], select {
			width: 100%;
			padding: 12px;
			border: 1px solid #ced4da;
			border-radius: 6px;
			font-size: 16px;
			transition: all 0.3s ease;
			background-color: white;
		}
		input[type="text"]:focus, input[type="password"]:focus, select:focus {
			outline: none;
			border-color: #4CAF50;
			box-shadow: 0 0 0 3px rgba(76, 175, 80, 0.1);
		}
		.btn {
			padding: 10px 20px;
			border: none;
			border-radius: 6px;
			cursor: pointer;
			text-decoration: none;
			font-size: 14px;
			font-weight: 500;
			transition: all 0.3s ease;
			text-align: center;
			display: inline-block;
		}
		.btn-primary {
			background-color: #4CAF50;
			color: white;
			box-shadow: 0 2px 4px rgba(76, 175, 80, 0.2);
		}
		.btn-primary:hover {
			background-color: #45a049;
			transform: translateY(-1px);
			box-shadow: 0 4px 8px rgba(76, 175, 80, 0.3);
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
		.btn-danger {
			background-color: #dc3545;
			color: white;
			box-shadow: 0 2px 4px rgba(220, 53, 69, 0.2);
		}
		.btn-danger:hover {
			background-color: #c82333;
			transform: translateY(-1px);
			box-shadow: 0 4px 8px rgba(220, 53, 69, 0.3);
		}
		.btn-sm {
			padding: 6px 12px;
			font-size: 12px;
		}
		.message {
			padding: 15px;
			border-radius: 6px;
			margin-bottom: 20px;
			border: 1px solid transparent;
			font-weight: 500;
		}
		.message-success {
			background-color: #d4edda;
			color: #155724;
			border-color: #c3e6cb;
			box-shadow: 0 2px 4px rgba(21, 87, 36, 0.1);
		}
		.message-error {
			background-color: #f8d7da;
			color: #721c24;
			border-color: #f5c6cb;
			box-shadow: 0 2px 4px rgba(114, 28, 36, 0.1);
		}
		/* 添加用户表单样式 */
		.add-user-form {
			background-color: #f8f9fa;
			padding: 20px;
			border-radius: 8px;
			border: 1px solid #e9ecef;
			margin-bottom: 30px;
		}
		/* 用户列表样式 */
		.user-list {
			margin-top: 30px;
		}
		/* 表格响应式 */
		@media (max-width: 768px) {
			table {
				font-size: 14px;
			}
			th, td {
				padding: 10px;
			}
		}
		/* 输入框组样式 */
		.input-group {
			display: flex;
			gap: 10px;
			align-items: flex-end;
		}
		/* 操作列样式 */
		.action-column {
			text-align: center;
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

		<div class="user-management">
			<h2>用户管理</h2>

			<!-- 显示消息 -->
			` + utils.GetMessage(r) + `

			<!-- 添加用户表单 -->
			<h3>添加用户</h3>
			<form method="POST" action="/add-user">
				<div style="display: grid; grid-template-columns: 1fr 1fr 1fr 1fr 1fr; gap: 15px;">
					<div class="form-group">
						<label for="new_username">用户名</label>
						<input type="text" id="new_username" name="new_username" placeholder="用户名" required>
					</div>
					<div class="form-group">
						<label for="new_user_pwd">密码</label>
						<input type="password" id="new_user_pwd" name="new_user_pwd" placeholder="密码" required>
					</div>
					<div class="form-group">
						<label for="new_user_role">角色</label>
						<select id="new_user_role" name="new_user_role">
							<option value="normal">普通用户</option>
							<option value="test">测试用户</option>
						</select>
					</div>
					<div class="form-group" style="display: none;">
						<label for="new_user_size">最大文件大小 (MB)</label>
						<input type="text" id="new_user_size" name="new_user_size" placeholder="1024" value="1024">
					</div>
					<div class="form-group">
						<label>最大文件大小</label>
						<div id="max_file_size_display">1024 MB</div>
					</div>
					<div class="form-group" style="display: flex; align-items: flex-end;">
						<button type="submit" class="btn btn-primary">添加用户</button>
					</div>
				</div>
			</form>

			<script>
			// 根据角色自动设置最大文件大小
			const roleSelect = document.getElementById('new_user_role');
			const sizeInput = document.getElementById('new_user_size');
			const sizeDisplay = document.getElementById('max_file_size_display');

			roleSelect.addEventListener('change', function() {
				let size;
				switch(this.value) {
					case 'normal':
						size = 10240; // 10GB
						sizeDisplay.textContent = '10 GB';
						break;
					case 'test':
						size = 1024; // 1GB
						sizeDisplay.textContent = '1 GB';
						break;
					default:
						size = 1024;
						sizeDisplay.textContent = '1 GB';
				}
				sizeInput.value = size;
			});

			// 初始化显示
			roleSelect.dispatchEvent(new Event('change'));
			</script>
			
			<!-- 用户列表 -->
			<h3>用户列表</h3>
			<table>
				<tr>
					<th>用户名</th>
					<th>角色</th>
					<th>最大文件大小</th>
					<th>修改密码</th>
					<th>操作</th>
				</tr>
				` + userListHTML + `
			</table>
		</div>
	</div>
</body>
</html>`

	w.Write([]byte(html))
}

// 添加用户处理函数
func AddUserHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil || sess.Role != constants.RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 解析表单
	r.ParseForm()
	username := r.FormValue("new_username")
	password := r.FormValue("new_user_pwd")
	role := r.FormValue("new_user_role")
	sizeStr := r.FormValue("new_user_size")

	// 验证输入
	if username == "" || password == "" {
		http.Redirect(w, r, fmt.Sprintf("/user-management?msg=%s&type=error", url.QueryEscape("用户名或密码不能为空")), http.StatusFound)
		return
	}

	// 检查用户名是否已存在
	for _, user := range config.AppConfig.Users {
		if user.Username == username {
			http.Redirect(w, r, fmt.Sprintf("/user-management?msg=%s&type=error", url.QueryEscape("用户名已存在")), http.StatusFound)
			return
		}
	}

	// 解析文件大小
	size, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		size = 1024 // 默认1GB
	} else {
		size = size * 1024 * 1024 // 转换为字节
	}

	// 添加新用户
	newUser := config.UserConfig{
		Username:    username,
		Password:    password,
		Role:        role,
		MaxFileSize: size,
	}
	config.AppConfig.Users = append(config.AppConfig.Users, newUser)

	// 更新用户配置映射
	config.UserConfigMap[username] = newUser

	// 保存配置
	if err := config.SaveConfig(); err != nil {
		log.Printf("保存配置失败: %v", err)
		http.Redirect(w, r, fmt.Sprintf("/user-management?msg=%s&type=error", url.QueryEscape("保存配置失败")), http.StatusFound)
		return
	}

	// 记录日志
	utils.Log(utils.LogLevelSuccess, sess.Username, "admin", "add_user", fmt.Sprintf("添加了新用户: %s，角色: %s", username, role))

	// 重定向回用户管理页面
	http.Redirect(w, r, fmt.Sprintf("/user-management?msg=%s&type=success", url.QueryEscape("用户添加成功")), http.StatusFound)
}

// 修改密码处理函数
func ChangePasswordHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil || sess.Role != constants.RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 解析表单
	r.ParseForm()

	// 处理修改密码
	for key, values := range r.Form {
		if key == "change_pwd" && len(values) > 0 {
			username := values[0]
			newPwd := r.FormValue(fmt.Sprintf("new_pwd_%s", username))
			confirmPwd := r.FormValue(fmt.Sprintf("confirm_pwd_%s", username))

			// 验证密码
			if newPwd == "" || newPwd != confirmPwd {
				http.Redirect(w, r, fmt.Sprintf("/user-management?msg=%s&type=error", url.QueryEscape("密码不能为空或两次输入不一致")), http.StatusFound)
				return
			}

			// 更新密码
			for i, user := range config.AppConfig.Users {
				if user.Username == username {
					config.AppConfig.Users[i].Password = newPwd
					// 更新map中的用户信息
					updatedUser := user
					updatedUser.Password = newPwd
					config.UserConfigMap[username] = updatedUser
					break
				}
			}

			// 保存配置
			if err := config.SaveConfig(); err != nil {
				log.Printf("保存配置失败: %v", err)
				http.Redirect(w, r, fmt.Sprintf("/user-management?msg=%s&type=error", url.QueryEscape("保存配置失败")), http.StatusFound)
				return
			}

			// 记录日志
			utils.Log(utils.LogLevelSuccess, sess.Username, "admin", "change_password", fmt.Sprintf("修改了用户 %s 的密码", username))

			// 重定向回用户管理页面
			http.Redirect(w, r, fmt.Sprintf("/user-management?msg=%s&type=success", url.QueryEscape("密码修改成功")), http.StatusFound)
			return
		}
	}

	// 默认重定向
	http.Redirect(w, r, "/user-management", http.StatusFound)
}

// 删除用户处理函数
func DeleteUserHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil || sess.Role != constants.RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 解析表单
	r.ParseForm()
	username := r.FormValue("delete_user")

	// 检查是否为管理员用户
	if username == "admin" {
		http.Redirect(w, r, fmt.Sprintf("/user-management?msg=%s&type=error", url.QueryEscape("管理员用户不可删除")), http.StatusFound)
		return
	}

	// 删除用户
	newUsers := []config.UserConfig{}
	for _, user := range config.AppConfig.Users {
		if user.Username != username {
			newUsers = append(newUsers, user)
		}
	}
	config.AppConfig.Users = newUsers

	// 更新用户配置映射
	delete(config.UserConfigMap, username)

	// 保存配置
	if err := config.SaveConfig(); err != nil {
		log.Printf("保存配置失败: %v", err)
		http.Redirect(w, r, fmt.Sprintf("/user-management?msg=%s&type=error", url.QueryEscape("保存配置失败")), http.StatusFound)
		return
	}

	// 记录日志
	utils.Log(utils.LogLevelSuccess, sess.Username, "admin", "delete_user", fmt.Sprintf("删除了用户: %s", username))

	// 重定向回用户管理页面
	http.Redirect(w, r, fmt.Sprintf("/user-management?msg=%s&type=success", url.QueryEscape("用户删除成功")), http.StatusFound)
}

// 辅助函数：获取删除按钮
func getDeleteButton(username string) string {
	if username == "admin" {
		return "" // 管理员账号不显示删除按钮
	}
	return fmt.Sprintf(`<form method="POST" action="/delete-user" style="display: inline;">
		<input type="hidden" name="delete_user" value="%s">
		<button type="submit" class="btn btn-danger btn-sm" onclick="return confirm('确定要删除用户 %s 吗？')">删除</button>
	</form>`, username, username)
}
