package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/session"
	"go-download-server/utils"
)

// 用户管理处理函数
func UserManagementHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	sess := session.GetCurrentUser(r)
	if sess == nil {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}
	// 管理员和二级管理员都可以访问用户管理
	if sess.Role != constants.RoleAdmin && sess.Role != constants.RoleSubAdmin {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.Redirect(w, r, "/?msg=您没有权限访问该页面", http.StatusFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 获取CSRF令牌隐藏字段
	sessionID := utils.GetSessionIDFromRequest(r)
	csrfTokenField := utils.GenerateCSRFTokenField(sessionID)

	// 生成角色选择下拉框HTML
	roleSelectHTML := "<option value=\"normal\">普通用户</option>"
	if sess.Role == constants.RoleAdmin {
		roleSelectHTML += "<option value=\"subadmin\">二级管理员</option>"
	}

	// 构建用户列表HTML
	userListHTML := ""
	config.UsersMu.RLock()
	defer config.UsersMu.RUnlock()
	for _, user := range config.AppConfig.Users {
		// 二级管理员只能看到普通用户，不能看到管理员和其他二级管理员账号
		if sess.Role == constants.RoleSubAdmin {
			// 跳过管理员和二级管理员账号
			if strings.ToLower(user.Role) == "admin" || strings.ToLower(user.Role) == "subadmin" {
				continue
			}
		}

		// 根据用户角色和MaxFileSize决定显示文本
		var fileSizeText string
		if strings.ToLower(user.Role) == "admin" || user.MaxFileSize == constants.MaxFileSizeUnlimited {
			fileSizeText = "无限制"
		} else {
			fileSizeText = utils.FormatFileSize(user.MaxFileSize)
		}

		// 密码修改表单和按钮
		changePasswordForm := ""
		// 管理员可以修改所有用户密码，二级管理员只能修改普通用户密码
		if sess.Role == constants.RoleAdmin {
			changePasswordForm = fmt.Sprintf(`<form method="POST" action="/change-password" style="display: flex; gap: 8px; align-items: center; flex-wrap: wrap;">
				` + csrfTokenField + `
				<input type="hidden" name="change_pwd" value="%s">
				<input type="password" name="new_pwd_%s" placeholder="新密码" style="padding: 8px 12px; border: 1px solid var(--v2-border); border-radius: 8px; font-size: 13px; color: var(--v2-text); background: var(--v2-bg-elev); outline: none; width: 110px;">
				<input type="password" name="confirm_pwd_%s" placeholder="确认密码" style="padding: 8px 12px; border: 1px solid var(--v2-border); border-radius: 8px; font-size: 13px; color: var(--v2-text); background: var(--v2-bg-elev); outline: none; width: 110px;">
				<button type="submit" class="btn-v2 btn-secondary-v2 btn-sm-v2">修改</button>
			</form>`, utils.EscapeHTML(user.Username), utils.EscapeHTML(user.Username), utils.EscapeHTML(user.Username))
		} else if sess.Role == constants.RoleSubAdmin {
			// 二级管理员只能修改普通用户密码
			if strings.ToLower(user.Role) == "normal" {
				changePasswordForm = fmt.Sprintf(`<form method="POST" action="/change-password" style="display: flex; gap: 8px; align-items: center; flex-wrap: wrap;">
					` + csrfTokenField + `
					<input type="hidden" name="change_pwd" value="%s">
					<input type="password" name="new_pwd_%s" placeholder="新密码" style="padding: 8px 12px; border: 1px solid var(--v2-border); border-radius: 8px; font-size: 13px; color: var(--v2-text); background: var(--v2-bg-elev); outline: none; width: 110px;">
					<input type="password" name="confirm_pwd_%s" placeholder="确认密码" style="padding: 8px 12px; border: 1px solid var(--v2-border); border-radius: 8px; font-size: 13px; color: var(--v2-text); background: var(--v2-bg-elev); outline: none; width: 110px;">
					<button type="submit" class="btn-v2 btn-secondary-v2 btn-sm-v2">修改</button>
				</form>`, utils.EscapeHTML(user.Username), utils.EscapeHTML(user.Username), utils.EscapeHTML(user.Username))
			}
		}

		// XSS防护：对用户名进行HTML编码
		safeUsername := utils.EscapeHTML(user.Username)
		userListHTML += fmt.Sprintf(`<tr>
			<td data-label="用户名">%s</td>
			<td data-label="角色">%s</td>
			<td data-label="最大文件大小">%s</td>
			<td data-label="修改密码" style="white-space: nowrap;">
				%s
			</td>
			<td data-label="操作">
				%s
			</td>
		</tr>`,
			safeUsername,
			utils.GetRoleNameByString(user.Role),
			fileSizeText,
			changePasswordForm,
			getDeleteButton(user.Username, csrfTokenField),
		)
	}

	// 构建HTML页面
	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=5.0, minimum-scale=1.0">
	<title>用户管理 - ` + config.AppConfig.Server.ServerName + `</title>
	<link rel="icon" href="data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 100 100%22><text y=%22.9em%22 font-size=%2290%22>📦</text></svg>">
	<link rel="stylesheet" href="/static/styles.css">

</head>
<body class="v2 admin-layout">
		<div class="admin-layout-wrapper">
			` + utils.GetAdminSidebar(r, config.AppConfig.Server.ServerName) + `
			<main class="admin-main">
				<div class="admin-page-header">
					<h1 class="admin-page-title">用户管理</h1>
					<p class="admin-page-desc">管理系统用户账号、角色和权限</p>
				</div>

		<!-- 显示消息 -->
		<div class="upload-message-v2">
			` + utils.GetMessage(r) + `
		</div>

		<!-- 添加用户表单 -->
		<div class="upload-card-v2" style="margin-bottom: 24px;">
			<div class="upload-card-title-v2">
				<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M16 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="8.5" cy="7" r="4"/><line x1="20" y1="8" x2="20" y2="14"/><line x1="23" y1="11" x2="17" y2="11"/></svg>
				添加用户
			</div>
			<form method="POST" action="/add-user" class="add-user-form-v2">
				` + csrfTokenField + `
				<div style="display: grid; grid-template-columns: repeat(3, 1fr); gap: 16px; margin-bottom: 16px;">
					<div class="form-group-v2">
						<label for="new_username">用户名</label>
						<input type="text" id="new_username" name="new_username" placeholder="请输入用户名" required>
					</div>
					<div class="form-group-v2">
						<label for="new_user_pwd">密码</label>
						<input type="password" id="new_user_pwd" name="new_user_pwd" placeholder="请输入密码" required>
					</div>
					<div class="form-group-v2">
						<label for="new_user_role">角色</label>
						<select id="new_user_role" name="new_user_role" style="padding: 12px 16px; border: 1px solid var(--v2-border); border-radius: 10px; font-size: 14px; color: var(--v2-text); background: var(--v2-bg-elev); outline: none; width: 100%;">` + roleSelectHTML + `</select>
					</div>
				</div>
				<div class="form-group-v2" style="display: none;">
					<label for="new_user_size">最大文件大小 (MB)</label>
					<input type="text" id="new_user_size" name="new_user_size" placeholder="1024" value="1024">
				</div>
				<div style="display: flex; justify-content: space-between; align-items: flex-end; gap: 16px;">
					<div class="form-group-v2" style="flex: 0 0 calc(33.333% - 8px);">
						<label>最大文件大小</label>
						<div id="max_file_size_display" style="padding: 12px 16px; background: var(--v2-bg); border: 1px solid var(--v2-border); border-radius: 10px; font-size: 14px; color: var(--v2-text-secondary);">20 GB</div>
					</div>
					<button type="submit" class="btn-v2 btn-primary-v2" style="flex: 0 0 auto; min-width: 140px;">添加用户</button>
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
						size = 20480; // 20GB
						sizeDisplay.textContent = '20 GB';
						break;
					case 'subadmin':
						size = -1; // -1表示无限制
						sizeDisplay.textContent = '无限制';
						break;
					default:
						size = 20480; // 20GB
						sizeDisplay.textContent = '20 GB'
			}
			sizeInput.value = size;
			});

			// 初始化显示
			roleSelect.dispatchEvent(new Event('change'));
		</script>
		</div>

		<!-- 用户列表 -->
		<div class="upload-card-v2">
			<div class="upload-card-title-v2">
				<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>
				用户列表
			</div>
			<table class="data-table-v2">
				<thead>
					<tr>
						<th>用户名</th>
						<th>角色</th>
						<th>最大文件大小</th>
						<th>修改密码</th>
						<th>操作</th>
					</tr>
				</thead>
				<tbody>
					` + userListHTML + `
				</tbody>
			</table>
		</div>
	</main>
		</div>
	</body>
</html>`
	w.Write([]byte(html))
}

// 获取删除用户按钮
func getDeleteButton(username string, csrfTokenField string) string {
	if username == "admin" {
		return "<span style=\"color: var(--v2-text-muted); font-size: 13px;\">不可删除</span>"
	}
	// XSS防护：对用户名进行HTML编码
	safeUsername := utils.EscapeHTML(username)
	// 对JavaScript中的用户名进行额外编码
	safeJSUsername := strings.ReplaceAll(safeUsername, "'", "\\'")
	return fmt.Sprintf(`<form method="POST" action="/delete-user" style="display: inline;" onsubmit="return confirm('确定要删除用户 %s 吗？')">
		` + csrfTokenField + `
		<input type="hidden" name="delete_user" value="%s">
		<button type="submit" class="btn-v2 btn-sm-v2" style="background: #fef2f2; color: #dc2626; border: 1px solid #fecaca;">删除</button>
	</form>`, safeJSUsername, safeUsername)
}

// 修改用户密码处理函数
func ChangePasswordHandler(w http.ResponseWriter, r *http.Request) {
	sess := session.GetCurrentUser(r)
	if sess == nil {
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}
	if sess.Role != constants.RoleAdmin && sess.Role != constants.RoleSubAdmin {
		http.Redirect(w, r, "/?msg=您没有权限", http.StatusFound)
		return
	}

	// 验证CSRF令牌
	sessionID := utils.GetSessionIDFromRequest(r)
	csrfToken := r.FormValue("csrf_token")
	if !utils.ValidateCSRFToken(sessionID, csrfToken) {
		http.Error(w, "CSRF令牌验证失败", http.StatusForbidden)
		return
	}

	username := r.FormValue("change_pwd")
	newPwd := r.FormValue("new_pwd_" + username)
	confirmPwd := r.FormValue("confirm_pwd_" + username)

	if username == "" || newPwd == "" {
		http.Redirect(w, r, "/user-management?msg=用户名和密码不能为空", http.StatusFound)
		return
	}
	if newPwd != confirmPwd {
		http.Redirect(w, r, "/user-management?msg=两次密码不一致", http.StatusFound)
		return
	}

	// 二级管理员不能修改管理员密码
	if sess.Role == constants.RoleSubAdmin {
		config.UsersMu.RLock()
		for _, u := range config.AppConfig.Users {
			if u.Username == username && (u.Role == "admin" || u.Role == "subadmin") {
				config.UsersMu.RUnlock()
				http.Redirect(w, r, "/user-management?msg=您没有权限修改该用户密码", http.StatusFound)
				return
			}
		}
		config.UsersMu.RUnlock()
	}

	// 使用bcrypt哈希新密码
	hashedNewPwd, err := session.HashPassword(newPwd)
	if err != nil {
		http.Redirect(w, r, "/user-management?msg=密码加密失败", http.StatusFound)
		return
	}

	// 修改密码并同步 UserConfigMap（登录鉴权与 session 校验使用该映射）
	config.UsersMu.Lock()
	for i := range config.AppConfig.Users {
		if config.AppConfig.Users[i].Username == username {
			config.AppConfig.Users[i].Password = hashedNewPwd
			break
		}
	}
	config.SyncUserConfigMapLocked()
	config.UsersMu.Unlock()

	if err := config.SaveConfig(); err != nil {
		http.Redirect(w, r, "/user-management?msg=保存配置失败", http.StatusFound)
		return
	}

	http.Redirect(w, r, "/user-management?msg=密码修改成功", http.StatusFound)
}

// 添加用户处理函数
func AddUserHandler(w http.ResponseWriter, r *http.Request) {
	sess := session.GetCurrentUser(r)
	if sess == nil {
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}
	if sess.Role != constants.RoleAdmin && sess.Role != constants.RoleSubAdmin {
		http.Redirect(w, r, "/?msg=您没有权限", http.StatusFound)
		return
	}

	// 验证CSRF令牌
	sessionID := utils.GetSessionIDFromRequest(r)
	csrfToken := r.FormValue("csrf_token")
	if !utils.ValidateCSRFToken(sessionID, csrfToken) {
		http.Error(w, "CSRF令牌验证失败", http.StatusForbidden)
		return
	}

	username := r.FormValue("new_username")
	password := r.FormValue("new_user_pwd")
	role := r.FormValue("new_user_role")
	sizeStr := r.FormValue("new_user_size")

	if username == "" || password == "" {
		http.Redirect(w, r, "/user-management?msg=用户名和密码不能为空", http.StatusFound)
		return
	}

	// 检查用户名是否已存在
	config.UsersMu.RLock()
	for _, u := range config.AppConfig.Users {
		if u.Username == username {
			config.UsersMu.RUnlock()
			http.Redirect(w, r, "/user-management?msg=用户名已存在", http.StatusFound)
			return
		}
	}
	config.UsersMu.RUnlock()

	// 二级管理员只能添加普通用户
	if sess.Role == constants.RoleSubAdmin {
		role = "normal"
	}

	maxSize := int64(1024 * 1024 * 1024) // 默认 1GB
	if sizeStr != "" {
		if size, err := strconv.ParseInt(sizeStr, 10, 64); err == nil {
			maxSize = size * 1024 * 1024
		}
	}

	// 使用bcrypt哈希密码
	hashedPassword, err := session.HashPassword(password)
	if err != nil {
		http.Redirect(w, r, "/user-management?msg=密码加密失败", http.StatusFound)
		return
	}

	newUser := config.UserConfig{
		Username:    username,
		Password:    hashedPassword,
		Role:        role,
		MaxFileSize: maxSize,
	}

	config.UsersMu.Lock()
	config.AppConfig.Users = append(config.AppConfig.Users, newUser)
	config.SyncUserConfigMapLocked()
	config.UsersMu.Unlock()

	if err := config.SaveConfig(); err != nil {
		http.Redirect(w, r, "/user-management?msg=保存配置失败", http.StatusFound)
		return
	}

	http.Redirect(w, r, "/user-management?msg=用户添加成功", http.StatusFound)
}

// 删除用户处理函数
func DeleteUserHandler(w http.ResponseWriter, r *http.Request) {
	sess := session.GetCurrentUser(r)
	if sess == nil {
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}
	if sess.Role != constants.RoleAdmin {
		http.Redirect(w, r, "/?msg=您没有权限", http.StatusFound)
		return
	}

	// 验证CSRF令牌
	sessionID := utils.GetSessionIDFromRequest(r)
	csrfToken := r.FormValue("csrf_token")
	if !utils.ValidateCSRFToken(sessionID, csrfToken) {
		http.Error(w, "CSRF令牌验证失败", http.StatusForbidden)
		return
	}

	username := r.FormValue("delete_user")
	if username == "" || username == "admin" {
		http.Redirect(w, r, "/user-management?msg=无法删除该用户", http.StatusFound)
		return
	}

	config.UsersMu.Lock()
	newUsers := []config.UserConfig{}
	for _, u := range config.AppConfig.Users {
		if u.Username != username {
			newUsers = append(newUsers, u)
		}
	}
	config.AppConfig.Users = newUsers
	config.SyncUserConfigMapLocked()
	config.UsersMu.Unlock()

	if err := config.SaveConfig(); err != nil {
		http.Redirect(w, r, "/user-management?msg=保存配置失败", http.StatusFound)
		return
	}

	http.Redirect(w, r, "/user-management?msg=用户删除成功", http.StatusFound)
}