package handlers

import (
	"net/http"
	"time"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/internal/logger"
	"go-download-server/session"
	"go-download-server/utils"
)

// 免责协议页面处理函数
func TermsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 检查用户是否已登录
	sess := session.GetCurrentUser(r)
	if sess == nil {
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}

	// 生成HTML
	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
	<link rel="stylesheet" href="/static/styles.css">
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=5.0, minimum-scale=1.0">
    <title>免责协议 - ` + config.AppConfig.Server.ServerName + `</title>
    <link rel="icon" href="data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 100 100%22><text y=%22.9em%22 font-size=%2290%22>📦</text></svg>">

</head>
<body class="v2">
	<!-- 顶部导航栏 -->
	<header class="topbar">
		<div class="topbar-left">
			<a href="/files" class="logo">
				<div class="logo-mark">📦</div>
				` + config.AppConfig.Server.ServerName + `
			</a>
			<nav class="topbar-nav">
				<a href="/files">文件列表</a>
				<a href="/upload">上传文件</a>
			</nav>
		</div>
		<div class="topbar-right">
			<div class="user-info-v2">
				` + utils.GetCurrentUserInfo(r) + `
			</div>
		</div>
	</header>

	<!-- 主内容区 -->
	<main class="page-main">
		<div class="upload-card-v2" style="max-width: 900px; margin: 0 auto;">
			<div class="upload-card-title-v2" style="font-size: 20px;">
				<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/><polyline points="10 9 9 9 8 9"/></svg>
				免责协议
			</div>

			<!-- 显示消息 -->
			<div class="upload-message-v2">
				` + utils.GetMessage(r) + `
			</div>

			<div class="terms-content-v2" style="line-height: 1.8; color: var(--v2-text-secondary); font-size: 14px;">
				<h2 style="font-size: 16px; font-weight: 700; color: var(--v2-text); margin: 20px 0 10px;">1. 服务条款</h2>
				<p style="margin-bottom: 12px;">欢迎使用 ` + constants.ServerName + ` 服务。本协议是您与本服务提供者之间关于使用本服务的法律协议，涵盖了文件管理、多协议下载等所有功能。</p>
				<p style="margin-bottom: 12px;">通过点击"同意"按钮，您表示您已阅读、理解并同意受本协议的约束。如果您不同意本协议的任何条款，您应立即停止使用本服务。</p>

				<h2 style="font-size: 16px; font-weight: 700; color: var(--v2-text); margin: 20px 0 10px;">2. 服务内容</h2>
				<p style="margin-bottom: 12px;">` + constants.ServerName + ` 是一个集成了多协议下载功能的文件管理服务，包括以下核心功能：</p>
				<ul style="margin-bottom: 12px; padding-left: 20px;">
					<li>文件上传、下载、分享和管理</li>
					<li>多协议下载支持（HTTP/HTTPS、FTP/SFTP、BitTorrent等）</li>
					<li>文件审核和管理</li>
					<li>用户权限管理</li>
				</ul>

				<h2 style="font-size: 16px; font-weight: 700; color: var(--v2-text); margin: 20px 0 10px;">3. 用户责任</h2>
				<ul style="margin-bottom: 12px; padding-left: 20px;">
					<li>您必须遵守中华人民共和国的相关法律法规，不得利用本服务进行任何违法活动。</li>
					<li>您不得使用本服务上传、下载、存储、分享任何侵犯他人知识产权的内容。</li>
					<li>您不得使用本服务上传、下载、存储、分享任何色情、暴力、恐怖、反动等违法内容。</li>
					<li>您必须对自己使用本服务的行为承担全部法律责任。</li>
					<li>您不得利用本服务进行任何形式的网络攻击、DDoS攻击或其他恶意行为。</li>
					<li>您不得使用本服务下载、存储、分享任何病毒、木马或其他恶意软件。</li>
				</ul>

				<h2 style="font-size: 16px; font-weight: 700; color: var(--v2-text); margin: 20px 0 10px;">4. 文件分享免责声明</h2>
				<ul style="margin-bottom: 12px; padding-left: 20px;">
					<li>本服务仅提供文件存储和分享的技术支持，不对用户分享的文件内容承担任何法律责任。</li>
					<li>本服务不保证所有分享链接都能正常访问，不对链接失效承担责任。</li>
					<li>本服务有权删除任何违反法律法规或服务条款的分享内容，无需提前通知。</li>
					<li>用户应对自己分享的文件内容负责，确保其合法性和安全性。</li>
				</ul>

				<h2 style="font-size: 16px; font-weight: 700; color: var(--v2-text); margin: 20px 0 10px;">5. 下载功能免责声明</h2>
				<ul style="margin-bottom: 12px; padding-left: 20px;">
					<li>本服务仅提供多协议下载的技术支持，不对用户下载的内容承担任何法律责任。</li>
					<li>本服务不保证所有下载链接都能正常使用，不对下载失败承担责任。</li>
					<li>本服务不保证下载内容的安全性，用户应自行承担下载内容的风险。</li>
					<li>本服务有权随时终止或修改下载服务内容，无需提前通知用户。</li>
					<li>用户应自行判断下载内容的合法性，不得下载任何违法内容。</li>
				</ul>

				<h2 style="font-size: 16px; font-weight: 700; color: var(--v2-text); margin: 20px 0 10px;">6. 知识产权</h2>
				<p style="margin-bottom: 12px;">本服务的所有内容，包括但不限于代码、文档、图像、音频、视频等，均受中华人民共和国及国际知识产权法律保护。</p>
				<p style="margin-bottom: 12px;">未经授权，您不得复制、修改、分发或销售本服务的任何内容。</p>

				<h2 style="font-size: 16px; font-weight: 700; color: var(--v2-text); margin: 20px 0 10px;">7. 协议修改</h2>
				<p style="margin-bottom: 12px;">本服务有权随时修改本协议，修改后的协议将在本页面公布，不再另行通知。</p>
				<p style="margin-bottom: 12px;">您继续使用本服务，即表示您同意修改后的协议。</p>

				<h2 style="font-size: 16px; font-weight: 700; color: var(--v2-text); margin: 20px 0 10px;">8. 服务变更与终止</h2>
				<ul style="margin-bottom: 12px; padding-left: 20px;">
					<li>本服务有权随时变更或终止部分或全部服务内容，无需提前通知。</li>
					<li>如您违反本协议条款，本服务有权终止您的账号和服务使用权限。</li>
					<li>服务终止后，您应立即停止使用本服务，并删除所有相关数据。</li>
				</ul>

				<h2 style="font-size: 16px; font-weight: 700; color: var(--v2-text); margin: 20px 0 10px;">9. 数据安全</h2>
				<ul style="margin-bottom: 12px; padding-left: 20px;">
					<li>本服务将尽力保护用户数据的安全，但不保证数据的绝对安全。</li>
					<li>本服务不承担因数据丢失、泄露或损坏而导致的任何责任。</li>
					<li>用户应自行备份重要数据，避免数据丢失。</li>
				</ul>

				<h2 style="font-size: 16px; font-weight: 700; color: var(--v2-text); margin: 20px 0 10px;">10. 适用法律</h2>
				<p style="margin-bottom: 12px;">本协议的订立、执行、解释及争议的解决均适用中华人民共和国法律。</p>
				<p style="margin-bottom: 12px;">如双方发生争议，应首先通过友好协商解决；协商不成的，任何一方均可向有管辖权的人民法院提起诉讼。</p>
			</div>

		<div style="display: flex; gap: 12px; margin-top: 24px; padding-top: 20px; border-top: 1px solid var(--v2-border-light);">
			<form method="POST" action="/agree-terms" style="flex: 1;">
				` + utils.GenerateCSRFTokenField(utils.GetSessionIDFromRequest(r)) + `
				<button type="submit" name="action" value="agree" class="btn-v2 btn-primary-v2" style="width: 100%;">同意并继续使用</button>
			</form>
			<form method="POST" action="/agree-terms" style="flex: 1;">
				` + utils.GenerateCSRFTokenField(utils.GetSessionIDFromRequest(r)) + `
				<button type="submit" name="action" value="disagree" class="btn-v2" style="width: 100%; background: #FEF3F2; color: #B42318;">不同意，退出登录</button>
			</form>
		</div>
		</div>
	</main>

	<!-- 页脚 -->
	<footer class="footer-v2">
		<div class="footer-content-v2">
			<div>版本: ` + constants.Version + ` | 开发者: ` + constants.Developer + `</div>
		</div>
	</footer>
</body>
</html>`

	w.Write([]byte(html))
}

// 协议同意处理函数
func AgreeTermsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 检查用户是否已登录
	sess := session.GetCurrentUser(r)
	if sess == nil {
		http.Redirect(w, r, "/login?msg=请先登录", http.StatusFound)
		return
	}

	// 验证CSRF令牌
	if !utils.ValidateCSRFTokenFromRequest(r) {
		http.Error(w, "CSRF令牌验证失败", http.StatusForbidden)
		return
	}

	// 解析表单
	r.ParseForm()
	action := r.FormValue("action")

	if action == "agree" {
		// 用户同意协议，更新配置文件和会话状态
		// 更新配置文件
		config.UsersMu.Lock()
		for i, user := range config.AppConfig.Users {
			if user.Username == sess.Username {
				// 更新用户的协议同意状态
				config.AppConfig.Users[i].AgreedToTerms = true
				config.AppConfig.Users[i].AgreedTermsVersion = config.AppConfig.Legal.TermsVersion
				config.AppConfig.Users[i].AgreedTermsTime = time.Now().Format("2006-01-02 15:04:05")
				// 更新用户配置映射
				config.UserConfigMap[sess.Username] = config.AppConfig.Users[i]
				break
			}
		}
		config.UsersMu.Unlock()

		// 保存配置文件
		if err := config.SaveConfig(); err != nil {
			logger.Errorf("保存配置文件失败: %v", err)
		}

		// 获取会话ID
		cookie, err := r.Cookie("session_id")
		if err == nil {
			// 更新会话中的AgreedToTerms字段
			session.UpdateSessionAgreedToTerms(cookie.Value)
		}

		// 跳转到首页
		http.Redirect(w, r, "/", http.StatusFound)
	} else {
		// 用户不同意协议，退出登录
		session.ClearSession(w, r)
		http.Redirect(w, r, "/login?msg=您必须同意免责协议才能使用本服务", http.StatusFound)
	}
}
