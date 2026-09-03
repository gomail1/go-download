package handlers

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/session"
	"go-download-server/utils"
)

// 辅助函数：用户认证
func authenticateUser(username, password string) (constants.UserRole, bool) {
	// 检查配置文件中的用户
	config.UsersMu.RLock()
	userConfig, exists := config.UserConfigMap[username]
	config.UsersMu.RUnlock()
	if exists {
		// 使用兼容明文和bcrypt的密码验证
		if session.VerifyPassword(password, userConfig.Password) {
			// 根据角色返回对应的UserRole
			switch userConfig.Role {
			case "admin":
				return constants.RoleAdmin, true
			case "subadmin":
				return constants.RoleSubAdmin, true
			case "normal":
				return constants.RoleNormal, true
			default:
				return constants.RoleNormal, true
			}
		}
		return constants.RoleNormal, false
	}
	return constants.RoleNormal, false
}

// 登录处理函数
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// GET请求：显示登录表单
	if r.Method == "GET" {
		// 使用客户端IP作为临时sessionID生成CSRF令牌（登录前无正式session）
		clientIP := utils.GetClientIP(r)
		tempSessionID := "login_" + clientIP
		csrfTokenField := utils.GenerateCSRFTokenField(tempSessionID)

		html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
	<link rel="stylesheet" href="/static/styles.css">
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=5.0, minimum-scale=1.0">
    <title>登录 - ` + config.AppConfig.Server.ServerName + `</title>
    <link rel="icon" href="data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 100 100%22><text y=%22.9em%22 font-size=%2290%22>📦</text></svg>">

</head>
<body class="v2 login-page-v2">
    <div class="login-wrapper-v2">
        <div class="login-card-v2">
            <div class="login-logo-v2">
                <div class="logo-mark">📦</div>
            </div>
            <h1 class="login-title-v2">登录到 ` + config.AppConfig.Server.ServerName + `</h1>
            <p class="login-subtitle-v2">欢迎回来，请登录您的账户</p>

            <!-- 显示错误消息 -->
            <div class="login-message-v2">` + utils.GetMessage(r) + `</div>

            <!-- 登录表单 -->
            <form method="POST" class="login-form-v2">
                ` + csrfTokenField + `
                <div class="form-group-v2">
                    <label for="username">用户名</label>
                    <input type="text" id="username" name="username" placeholder="请输入用户名" required>
                </div>

                <div class="form-group-v2">
                    <label for="password">密码</label>
                    <input type="password" id="password" name="password" placeholder="请输入密码" required>
                </div>

                <button type="submit" class="login-btn-v2">登录</button>
            </form>

            <!-- 版本信息 -->
            <div class="login-footer-v2">
                <p>版本: ` + constants.Version + ` | 开发者: ` + constants.Developer + ` | <a href="` + constants.RepoURL + `" target="_blank" title="GitHub仓库">GitHub</a></p>
            </div>
        </div>
    </div>
</body>
</html>`
		w.Write([]byte(html))
		return
	}

	// POST请求：处理登录逻辑
	if r.Method == "POST" {
		// 验证CSRF令牌（使用客户端IP作为临时sessionID）
		clientIP := utils.GetClientIP(r)
		tempSessionID := "login_" + clientIP
		csrfToken := r.FormValue("csrf_token")
		if !utils.ValidateCSRFToken(tempSessionID, csrfToken) {
			utils.Log(utils.LogLevelSecurity, "anonymous", "guest", "login_attempt", fmt.Sprintf("IP: %s CSRF令牌验证失败", clientIP))
			http.Redirect(w, r, fmt.Sprintf("/login?msg=%s&type=error", url.QueryEscape("请求已过期，请刷新页面后重新登录")), http.StatusFound)
			return
		}

		// 解析表单
		r.ParseForm()
		username := r.FormValue("username")
		password := r.FormValue("password")

		// 严格验证用户名和密码
		// 1. 长度限制
		if len(username) < 3 || len(username) > 20 {
			utils.Log(utils.LogLevelSecurity, "anonymous", "guest", "login_attempt", fmt.Sprintf("IP: %s 用户名长度不符合要求，用户名: %s", clientIP, username))
			http.Redirect(w, r, fmt.Sprintf("/login?msg=%s&type=error", url.QueryEscape("用户名长度必须在3-20个字符之间")), http.StatusFound)
			return
		}
		if len(password) < 6 || len(password) > 30 {
			utils.Log(utils.LogLevelSecurity, "anonymous", "guest", "login_attempt", fmt.Sprintf("IP: %s 密码长度不符合要求，用户名: %s", clientIP, username))
			http.Redirect(w, r, fmt.Sprintf("/login?msg=%s&type=error", url.QueryEscape("密码长度必须在6-30个字符之间")), http.StatusFound)
			return
		}

		// 2. 正则表达式过滤 - 用户名只允许字母、数字、下划线
		validPattern := regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
		if !validPattern.MatchString(username) {
			utils.Log(utils.LogLevelSecurity, "anonymous", "guest", "login_attempt", fmt.Sprintf("IP: %s 用户名包含非法字符，用户名: %s", clientIP, username))
			http.Redirect(w, r, fmt.Sprintf("/login?msg=%s&type=error", url.QueryEscape("用户名只能包含字母、数字和下划线")), http.StatusFound)
			return
		}
		// 密码支持所有字符，只检查长度

		// 3. 检查IP是否被封禁
		if utils.IsIPBanned(clientIP) {
			utils.Log(utils.LogLevelSecurity, "anonymous", "guest", "login_attempt", fmt.Sprintf("IP: %s 已被封禁，尝试登录，用户名: %s", clientIP, username))
			http.Redirect(w, r, fmt.Sprintf("/login?msg=%s&type=error", url.QueryEscape("登录失败次数过多，请稍后再试")), http.StatusFound)
			return
		}

		// 验证用户
		role, ok := authenticateUser(username, password)
		if !ok {
			// 记录失败尝试
			utils.RecordFailedLogin(clientIP)
			utils.Log(utils.LogLevelSecurity, "anonymous", "guest", "login_attempt", fmt.Sprintf("IP: %s 用户名或密码错误，用户名: %s", clientIP, username))
			http.Redirect(w, r, fmt.Sprintf("/login?msg=%s&type=error", url.QueryEscape("用户名或密码错误")), http.StatusFound)
			return
		}

		// 登录成功，清除失败记录
		utils.ClearFailedLogin(clientIP)

		// 自动升级明文密码为bcrypt哈希（平滑迁移）
		// 如果用户密码还是明文存储，登录成功后自动升级为bcrypt
		go func(username string) {
			if err := session.UpgradePasswordToBcrypt(username); err != nil {
				log.Printf("密码自动升级失败（用户: %s）: %v", username, err)
			}
		}(username)

		// 设置会话
		sessionID := session.SetSession(w, username, role)

		// 生成CSRF令牌
		_, err := utils.SetCSRFToken(sessionID)
		if err != nil {
			log.Printf("生成CSRF令牌失败: %v", err)
		}

		// 记录日志
		var roleStr string
		switch role {
		case constants.RoleAdmin:
			roleStr = "admin"
		case constants.RoleSubAdmin:
			roleStr = "subadmin"
		case constants.RoleNormal:
			roleStr = "normal"
		default:
			roleStr = "unknown"
		}
		utils.Log(utils.LogLevelSuccess, username, roleStr, "login", fmt.Sprintf("IP: %s 登录成功", clientIP))

		// 按角色重定向：管理员 / 二级管理员进入管理后台，其余进入主页
		if role == constants.RoleAdmin || role == constants.RoleSubAdmin {
			http.Redirect(w, r, "/admin", http.StatusFound)
		} else {
			http.Redirect(w, r, "/", http.StatusFound)
		}
	}
}

// 密码变更提示页面处理函数
func PasswordChangedHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=5.0, minimum-scale=1.0">
    <title>密码已变更 - ` + config.AppConfig.Server.ServerName + `</title>
    <link rel="icon" href="data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 100 100%22><text y=%22.9em%22 font-size=%2290%22>📦</text></svg>">

    <script>
        // 5秒后自动跳转到首页
        window.onload = function() {
            let countdown = 5;
            const countdownElement = document.getElementById('countdown');
            
            const timer = setInterval(() => {
                countdown--;
                if (countdownElement) {
                    countdownElement.textContent = countdown;
                }
                
                if (countdown <= 0) {
                    clearInterval(timer);
                    window.location.href = '/';
                }
            }, 1000);
        };
    </script>
</head>
<body class="v2 login-page-v2">
    <div class="login-wrapper-v2">
        <div class="login-card-v2">
            <div class="login-logo-v2">
                <div class="logo-mark">🔐</div>
            </div>
            <h1 class="login-title-v2">密码已变更</h1>
            <p class="login-subtitle-v2">您的密码已成功修改，请重新登录以继续使用系统。</p>
            <a href="/" class="btn-v2 btn-primary-v2" style="width: 100%; margin-bottom: 16px;">返回首页</a>
            <div class="countdown-v2">
                <p style="font-size: 13px; color: var(--v2-text-muted);">将在 <span id="countdown" style="color: var(--v2-primary); font-weight: 600;">5</span> 秒后自动返回首页</p>
            </div>
            <div class="login-footer-v2">
                <p>版本: ` + constants.Version + ` | 开发者: ` + constants.Developer + `</p>
            </div>
        </div>
    </div>
</body>
</html>`
	w.Write([]byte(html))
}

// 登出处理函数
func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	// 清除会话
	session.ClearSession(w, r)

	// 记录日志
	sess := session.GetCurrentUser(r)
	if sess != nil {
		var roleStr string
		switch sess.Role {
		case constants.RoleAdmin:
			roleStr = "admin"
		case constants.RoleNormal:
			roleStr = "normal"

		default:
			roleStr = "unknown"
		}
		utils.Log(utils.LogLevelInfo, sess.Username, roleStr, "logout", "退出登录")
	} else {
		utils.Log(utils.LogLevelInfo, "anonymous", "guest", "logout", "匿名用户退出登录")
	}

	// 确保没有缓存
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	// 重定向到首页
	http.Redirect(w, r, "/", http.StatusFound)
}
