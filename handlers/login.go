package handlers

import (
	"fmt"
	"net/http"
	"net/url"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/session"
	"go-download-server/utils"
)

// 辅助函数：用户认证
func authenticateUser(username, password string) (constants.UserRole, bool) {
	// 检查配置文件中的用户
	if userConfig, exists := config.UserConfigMap[username]; exists {
		if userConfig.Password == password {
			// 根据角色返回对应的UserRole
			switch userConfig.Role {
			case "admin":
				return constants.RoleAdmin, true
			case "normal":
				return constants.RoleNormal, true
			case "test":
				return constants.RoleTest, true
			default:
				return constants.RoleTest, true
			}
		}
		return constants.RoleTest, false
	}
	return constants.RoleTest, false
}

// 登录处理函数
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// GET请求：显示登录表单
	if r.Method == "GET" {
		html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>登录 - ` + constants.ServerName + `</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        
        body {
            font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            display: flex;
            justify-content: center;
            align-items: center;
        }
        
        .login-container {
            background-color: white;
            border-radius: 10px;
            box-shadow: 0 10px 30px rgba(0, 0, 0, 0.3);
            padding: 40px;
            width: 100%;
            max-width: 400px;
        }
        
        h1 {
            text-align: center;
            color: #333;
            margin-bottom: 30px;
            font-size: 24px;
        }
        
        .logo {
            font-size: 48px;
            text-align: center;
            margin-bottom: 20px;
        }
        
        .form-group {
            margin-bottom: 20px;
        }
        
        label {
            display: block;
            margin-bottom: 5px;
            color: #555;
            font-weight: 500;
        }
        
        input[type="text"],
        input[type="password"] {
            width: 100%;
            padding: 12px;
            border: 1px solid #ddd;
            border-radius: 5px;
            font-size: 16px;
            transition: border-color 0.3s;
        }
        
        input[type="text"]:focus,
        input[type="password"]:focus {
            border-color: #667eea;
            outline: none;
            box-shadow: 0 0 0 2px rgba(102, 126, 234, 0.1);
        }
        
        .btn {
            width: 100%;
            padding: 12px;
            background-color: #667eea;
            color: white;
            border: none;
            border-radius: 5px;
            font-size: 16px;
            cursor: pointer;
            transition: background-color 0.3s;
        }
        
        .btn:hover {
            background-color: #5568d3;
        }
        
        .message {
            padding: 12px;
            border-radius: 5px;
            margin-bottom: 20px;
            text-align: center;
        }
        
        .message-error {
            background-color: #f8d7da;
            color: #721c24;
            border: 1px solid #f5c6cb;
        }
        
        .version-info {
            margin-top: 20px;
            text-align: center;
            color: #666;
            font-size: 12px;
            padding-top: 20px;
            border-top: 1px solid #eee;
        }

    </style>
</head>
<body>
    <div class="login-container">
        <div class="logo">📦</div>
        <h1>登录到 ` + constants.ServerName + `</h1>
        
        <!-- 显示错误消息 -->
        ` + utils.GetMessage(r) + `
        
        <!-- 登录表单 -->
        <form method="POST">
            <div class="form-group">
                <label for="username">用户名</label>
                <input type="text" id="username" name="username" placeholder="请输入用户名" required>
            </div>
            
            <div class="form-group">
                <label for="password">密码</label>
                <input type="password" id="password" name="password" placeholder="请输入密码" required>
            </div>
            
            <div class="form-group">
                <button type="submit" class="btn">登录</button>
            </div>
        </form>
        
        <!-- 版本信息 -->
        <div class="version-info">
            <p>版本: ` + constants.Version + ` | 开发者: ` + constants.Developer + `</p>
        </div>

    </div>
</body>
</html>`
		w.Write([]byte(html))
		return
	}

	// POST请求：处理登录逻辑
	if r.Method == "POST" {
		// 解析表单
		r.ParseForm()
		username := r.FormValue("username")
		password := r.FormValue("password")

		// 验证用户
		role, ok := authenticateUser(username, password)
		if !ok {
			http.Redirect(w, r, fmt.Sprintf("/login?msg=%s&type=error", url.QueryEscape("用户名或密码错误")), http.StatusFound)
			return
		}

		// 设置会话
		session.SetSession(w, username, role)

		// 记录日志
		var roleStr string
		switch role {
		case constants.RoleAdmin:
			roleStr = "admin"
		case constants.RoleNormal:
			roleStr = "normal"
		case constants.RoleTest:
			roleStr = "test"
		default:
			roleStr = "unknown"
		}
		utils.Log(utils.LogLevelSuccess, username, roleStr, "login", "登录成功")

		// 重定向到主页
		http.Redirect(w, r, "/", http.StatusFound)
	}
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
		case constants.RoleTest:
			roleStr = "test"
		default:
			roleStr = "unknown"
		}
		utils.Log(utils.LogLevelInfo, sess.Username, roleStr, "logout", "退出登录")
	} else {
		utils.Log(utils.LogLevelInfo, "anonymous", "guest", "logout", "匿名用户退出登录")
	}

	// 重定向到登录页面
	http.Redirect(w, r, "/login", http.StatusFound)
}
