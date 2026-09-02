package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/session"
)

// 暴力破解防护常量
const (
	// MaxLoginAttempts 最大失败尝试次数
	MaxLoginAttempts = 5
	// BanDuration 封禁时间
	BanDuration = 5 * time.Minute
	// CleanupInterval 清理过期记录的间隔
	CleanupInterval = 10 * time.Minute
)

// FailedLoginAttempt 记录失败登录尝试的结构体
type FailedLoginAttempt struct {
	Count       int       // 失败尝试次数
	LastAttempt time.Time // 最后一次尝试时间
	BannedUntil time.Time // 封禁截止时间
}

// failedLoginMap 存储IP的失败登录尝试记录
var (
	failedLoginMap   = make(map[string]*FailedLoginAttempt)
	failedLoginMutex sync.Mutex
)

func init() {
	// 启动定期清理过期记录的goroutine
	go cleanupFailedLoginRecords()
}

// cleanupFailedLoginRecords 定期清理过期的失败登录记录
func cleanupFailedLoginRecords() {
	for {
		time.Sleep(CleanupInterval)

		failedLoginMutex.Lock()
		now := time.Now()
		for ip, attempt := range failedLoginMap {
			// 如果最后一次尝试时间超过封禁时间+清理间隔，则删除记录
			if now.Sub(attempt.LastAttempt) > BanDuration+CleanupInterval {
				delete(failedLoginMap, ip)
			}
		}
		failedLoginMutex.Unlock()
	}
}

// IsIPBanned 检查IP是否被封禁
func IsIPBanned(ip string) bool {
	failedLoginMutex.Lock()
	defer failedLoginMutex.Unlock()

	attempt, exists := failedLoginMap[ip]
	if !exists {
		return false
	}

	// 如果还在封禁期内，返回true
	if time.Now().Before(attempt.BannedUntil) {
		return true
	}

	// 封禁期已过，但不要删除记录，只重置封禁时间
	// 这样可以保留失败尝试次数，防止IP在封禁期过后立即进行新的暴力破解
	attempt.BannedUntil = time.Time{}
	return false
}

// RecordFailedLogin 记录一次失败的登录尝试
func RecordFailedLogin(ip string) {
	failedLoginMutex.Lock()
	defer failedLoginMutex.Unlock()

	now := time.Now()
	Log(LogLevelDebug, "anonymous", "guest", "login_attempt_debug", fmt.Sprintf("处理IP: %s，当前映射大小: %d，映射地址: %p", ip, len(failedLoginMap), &failedLoginMap))

	// 打印当前映射中的所有IP
	for storedIP, storedAttempt := range failedLoginMap {
		Log(LogLevelDebug, "anonymous", "guest", "login_attempt_debug", fmt.Sprintf("映射中存在IP: %s，失败次数: %d", storedIP, storedAttempt.Count))
	}

	// 直接通过map访问和修改值，确保更新正确
	if _, exists := failedLoginMap[ip]; !exists {
		// 第一次失败，创建新记录
		Log(LogLevelDebug, "anonymous", "guest", "login_attempt_debug", fmt.Sprintf("IP %s 不存在，创建新记录", ip))
		failedLoginMap[ip] = &FailedLoginAttempt{
			Count:       1,
			LastAttempt: now,
			BannedUntil: time.Time{},
		}
		Log(LogLevelDebug, "anonymous", "guest", "login_attempt_failed", fmt.Sprintf("IP %s 第1次失败登录尝试", ip))
		// 再次打印映射大小，确认记录已创建
		Log(LogLevelDebug, "anonymous", "guest", "login_attempt_debug", fmt.Sprintf("创建记录后，映射大小: %d", len(failedLoginMap)))
		return
	}

	// 更新失败记录 - 直接通过map访问确保更新正确的对象
	failedLoginMap[ip].Count++
	failedLoginMap[ip].LastAttempt = now
	Log(LogLevelDebug, "anonymous", "guest", "login_attempt_failed", fmt.Sprintf("IP %s 第%d次失败登录尝试", ip, failedLoginMap[ip].Count))

	// 如果失败次数超过阈值，设置封禁时间
	if failedLoginMap[ip].Count >= MaxLoginAttempts {
		failedLoginMap[ip].BannedUntil = now.Add(BanDuration)
		Log(LogLevelSecurity, "anonymous", "guest", "login_attempt_banned", fmt.Sprintf("IP %s 因多次失败登录尝试被封禁，封禁时间: %v，失败次数: %d", ip, BanDuration, failedLoginMap[ip].Count))
	}
}

// ClearFailedLogin 清除IP的失败登录记录（登录成功时调用）
func ClearFailedLogin(ip string) {
	failedLoginMutex.Lock()
	defer failedLoginMutex.Unlock()

	delete(failedLoginMap, ip)
}

// 获取客户端真实IP地址
func GetClientIP(r *http.Request) string {
	// 检查X-Forwarded-For头（反向代理场景）
	xForwardedFor := r.Header.Get("X-Forwarded-For")
	if xForwardedFor != "" {
		// X-Forwarded-For头可能包含多个IP地址，第一个是真实IP
		parts := strings.Split(xForwardedFor, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return normalizeIP(ip)
			}
		}
	}

	// 检查X-Real-IP头
	xRealIP := r.Header.Get("X-Real-IP")
	if xRealIP != "" {
		return normalizeIP(strings.TrimSpace(xRealIP))
	}

	// 如果以上都没有，从远程地址解析IP
	remoteAddr := r.RemoteAddr

	// 处理IPv6地址格式：[IP]:端口
	if strings.HasPrefix(remoteAddr, "[") {
		if idx := strings.Index(remoteAddr, "]"); idx != -1 {
			return normalizeIP(remoteAddr[1:idx])
		}
	}

	// 处理IPv4地址格式：IP:端口
	if idx := strings.LastIndex(remoteAddr, ":"); idx != -1 {
		return normalizeIP(remoteAddr[:idx])
	}

	return normalizeIP(remoteAddr)
}

// normalizeIP 标准化IP地址，将IPv6回环地址转换为IPv4格式
func normalizeIP(ip string) string {
	// 将IPv6回环地址 ::1 转换为 IPv4 回环地址 127.0.0.1
	if ip == "::1" || ip == "[::1]" {
		return "127.0.0.1"
	}
	// 去除IPv6地址的方括号
	ip = strings.TrimPrefix(ip, "[")
	ip = strings.TrimSuffix(ip, "]")
	return ip
}

// GetRequestScheme 获取请求的协议（http/https）
// 优先检查 X-Forwarded-Proto 头（反向代理场景），其次检查 r.TLS
func GetRequestScheme(r *http.Request) string {
	// 优先检查反向代理的 X-Forwarded-Proto 头
	if forwardedProto := r.Header.Get("X-Forwarded-Proto"); forwardedProto != "" {
		return forwardedProto
	}
	// 检查是否为HTTPS连接
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// GenerateAPIKey 生成一个安全的随机API密钥（32字节，64个十六进制字符）
func GenerateAPIKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// 辅助函数：格式化文件大小
func FormatFileSize(size int64) string {
	// 如果文件大小为-1，表示无限制
	if size == constants.MaxFileSizeUnlimited {
		return "无限制"
	}

	const (
		_          = iota
		KB float64 = 1 << (10 * iota)
		MB
		GB
		TB
	)

	var unit string
	var sizeFloat float64

	switch {
	case size >= int64(TB):
		sizeFloat = float64(size) / TB
		unit = "TB"
	case size >= int64(GB):
		sizeFloat = float64(size) / GB
		unit = "GB"
	case size >= int64(MB):
		sizeFloat = float64(size) / MB
		unit = "MB"
	case size >= int64(KB):
		sizeFloat = float64(size) / KB
		unit = "KB"
	default:
		sizeFloat = float64(size)
		unit = "B"
	}

	return fmt.Sprintf("%.2f %s", sizeFloat, unit)
}

// 辅助函数：获取空目录消息
func GetEmptyMessage() string {
	return `<div class="empty-message">
		<div class="empty-icon">📁</div>
		<p>该目录为空</p>
		<p>点击"上传文件"添加内容</p>
	</div>`
}

// 辅助函数：清理文件名
func SanitizeFilename(filename string) string {
	// 移除路径信息，只保留文件名
	filename = filepath.Base(filename)

	// 替换无效字符
	invalidChars := `<>:"/\|?*`
	for _, char := range invalidChars {
		filename = strings.ReplaceAll(filename, string(char), "_")
	}

	// 移除前后空白字符
	filename = strings.TrimSpace(filename)

	// 如果文件名是空的，设置默认名
	if filename == "" {
		filename = fmt.Sprintf("file_%d", time.Now().Unix())
	}

	return filename
}

// 辅助函数：获取当前用户信息
func GetCurrentUserInfo(r *http.Request) string {
	session := session.GetCurrentUser(r)
	if session != nil {
		return fmt.Sprintf(`
										<span class="user-info" style="color: white; font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; font-size: 14px;">
												欢迎, %s (角色: %s) • 
												<a href="/logout" style="color: white; text-decoration: none; font-weight: bold; margin-left: 10px; font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; font-size: 14px;">退出登录</a>
										</span>`, session.Username, GetRoleName(session.Role))
	} else {
		return `<a href="/login" style="color: white; text-decoration: none; font-weight: bold; font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; font-size: 14px;">登录</a>`
	}
}

// 辅助函数：获取角色名称
func GetRoleName(role constants.UserRole) string {
	switch role {
	case constants.RoleAdmin:
		return "管理员"
	case constants.RoleSubAdmin:
		return "二级管理员"
	case constants.RoleNormal:
		return "普通用户"

	default:
		return "未知角色"
	}
}

// 辅助函数：根据字符串获取角色名称
func GetRoleNameByString(roleStr string) string {
	var role constants.UserRole
	switch roleStr {
	case "normal":
		role = constants.RoleNormal
	case "admin":
		role = constants.RoleAdmin
	case "subadmin":
		role = constants.RoleSubAdmin
	default:
		role = constants.RoleNormal
	}
	return GetRoleName(role)
}

// 统计待审核文件数量
func CountPendingFiles() int {
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
				// 过滤掉BT客户端的临时文件和未完成的下载文件
				fileName := info.Name()
				if fileName == ".torrent.bolt.db" {
					// 跳过BT临时数据库文件
					return nil
				}
				if strings.HasSuffix(fileName, ".part") {
					// 跳过未完成的下载文件
					return nil
				}
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

// 辅助函数：获取管理员链接
func GetAdminLinks(r *http.Request) string {
	sess := session.GetCurrentUser(r)
	if sess != nil && (sess.Role == constants.RoleAdmin || sess.Role == constants.RoleSubAdmin) {
		pendingCount := CountPendingFiles()
		// 根据当前路径设置active类
		adminActive := ""

		if strings.HasPrefix(r.URL.Path, "/admin") || strings.HasPrefix(r.URL.Path, "/review") || strings.HasPrefix(r.URL.Path, "/user-management") {
			adminActive = " class=\"active admin-link\""
		} else {
			adminActive = " class=\"admin-link\""
		}

		// 下载管理菜单
		downloadActive := ""
		if strings.HasPrefix(r.URL.Path, "/downloads") || strings.HasPrefix(r.URL.Path, "/new-download") || strings.HasPrefix(r.URL.Path, "/download-tasks") {
			downloadActive = " class=\"active admin-link\""
		} else {
			downloadActive = " class=\"admin-link\""
		}

		// 管理员可以看到全部链接
		heatmapActive := ""
		if strings.HasPrefix(r.URL.Path, "/heatmap") {
			heatmapActive = " class=\"active admin-link\""
		} else {
			heatmapActive = " class=\"admin-link\""
		}

		// 二级管理员也可以看到所有链接，包括热力图
		return fmt.Sprintf(`<a href="/admin"%s>管理员<span class="pending-badge-v2">%d</span></a><a href="/downloads"%s>下载管理</a><a href="/heatmap"%s>热力图</a>`, adminActive, pendingCount, downloadActive, heatmapActive)
	}
	return ""
}

// 获取管理员侧边栏 HTML（侧边栏布局）
func GetAdminSidebar(r *http.Request, serverName string) string {
	sess := session.GetCurrentUser(r)
	if sess == nil {
		return ""
	}

	pendingCount := CountPendingFiles()
	currentPath := r.URL.Path

	// 判断当前页面是否激活
	isActive := func(path string) string {
		if strings.HasPrefix(currentPath, path) {
			return "active"
		}
		return ""
	}

	// 角色显示名
	roleName := "普通用户"
	if sess.Role == constants.RoleAdmin {
		roleName = "管理员"
	} else if sess.Role == constants.RoleSubAdmin {
		roleName = "二级管理员"
	}

	// 用户头像首字母
	avatar := "👤"
	if sess.Username != "" {
		avatar = strings.ToUpper(sess.Username[:1])
	}

	sidebar := fmt.Sprintf(`<aside class="admin-sidebar">
		<div class="sidebar-logo">
			<div class="sidebar-logo-mark">📦</div>
			<div class="sidebar-logo-text">%s</div>
		</div>

		<div class="sidebar-section">
			<div class="sidebar-section-title">概览</div>
			<nav class="sidebar-nav">
				<a href="/admin" class="%s">
					<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/></svg>
					仪表盘
				</a>
			</nav>
		</div>

		<div class="sidebar-section">
			<div class="sidebar-section-title">内容管理</div>
			<nav class="sidebar-nav">
				<a href="/files" class="%s">
					<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
					文件列表
				</a>
				<a href="/upload" class="%s">
					<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
					上传文件
				</a>
				<a href="/review" class="%s">
					<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 11l3 3L22 4"/><path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11"/></svg>
					文件审核
					<span class="sidebar-badge">%d</span>
				</a>
				<a href="/downloads" class="%s">
					<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
					下载管理
				</a>
			</nav>
		</div>

		<div class="sidebar-section">
			<div class="sidebar-section-title">系统管理</div>
			<nav class="sidebar-nav">
				<a href="/user-management" class="%s">
					<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>
					用户管理
				</a>
				<a href="/logs" class="%s">
					<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/></svg>
					操作日志
				</a>
				<a href="/heatmap" class="%s">
					<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18"/><path d="M9 21V9"/></svg>
					热力图
				</a>
				<a href="/ip-management" class="%s">
					<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>
					IP管理
				</a>
				<a href="/info" class="%s">
					<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>
					服务器信息
				</a>
			</nav>
		</div>

		<div class="sidebar-bottom">
			<div class="sidebar-user">
				<div class="sidebar-avatar">%s</div>
				<div class="sidebar-user-info">
					<div class="sidebar-user-name">%s</div>
					<div class="sidebar-user-role">%s</div>
				</div>
			</div>
			<a href="/logout" class="sidebar-logout">
				<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></svg>
				退出登录
			</a>
		</div>
	</aside>`,
		serverName,
		isActive("/admin"),
		isActive("/files"),
		isActive("/upload"),
		isActive("/review"),
		pendingCount,
		isActive("/downloads"),
		isActive("/user-management"),
		isActive("/logs"),
		isActive("/heatmap"),
		isActive("/ip-management"),
		isActive("/info"),
		avatar,
		sess.Username,
		roleName,
	)

	return sidebar
}

// 辅助函数：获取管理员操作按钮（POST 表单，携带 CSRF 令牌）
func GetAdminActions(r *http.Request, path string) string {
	session := session.GetCurrentUser(r)
	if session != nil && session.Role == constants.RoleAdmin {
		csrfField := GenerateCSRFTokenField(GetSessionIDFromRequest(r))
		return fmt.Sprintf(`<form method="POST" action="/delete" style="display:inline;" onsubmit="return confirm('确定要删除吗？')"><input type="hidden" name="path" value="%s">%s<button type="submit" class="btn btn-danger">删除</button></form>`, html.EscapeString(path), csrfField)
	}
	return ""
}

// 辅助函数：获取最大文件大小文本
func GetMaxFileSizeText(session *session.Session) string {
	if session.MaxFileSize == constants.MaxFileSizeUnlimited {
		return "无限制"
	}
	return FormatFileSize(session.MaxFileSize)
}

// 辅助函数：获取消息
func GetMessage(r *http.Request) string {
	msg := html.EscapeString(r.URL.Query().Get("msg"))
	msgType := r.URL.Query().Get("type")

	if msg == "" {
		return ""
	}

	class := "message-success"
	if msgType == "error" {
		class = "message-error"
	}

	return fmt.Sprintf(`<div class="message %s show-message">%s</div>
	<script>
		setTimeout(function() {
			var message = document.querySelector('.show-message');
			if (message) {
				message.style.transition = 'opacity 0.5s ease, transform 0.5s ease';
				message.style.opacity = '0';
				message.style.transform = 'translateY(-10px)';
				setTimeout(function() {
					if (message.parentNode) {
						message.parentNode.removeChild(message);
					}
				}, 500);
			}
		}, 5000);
	</script>
	%s`, class, msg, GetMessageStyle())
}

// 辅助函数：获取消息样式
func GetMessageStyle() string {
	return `<style>
		.message {
			padding: 12px 20px;
			border-radius: 5px;
			margin-bottom: 20px;
			font-weight: bold;
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
		.show-message {
			opacity: 1;
			transform: translateY(0);
		}
	</style>`
}

// 辅助函数：获取动态消息显示的JavaScript代码
func GetDynamicMessageScript() string {
	return `		// 显示动态消息
		const showDynamicMessage = function(message, type) {
			const messageDiv = document.createElement('div');
			messageDiv.className = 'message message-' + type;
			messageDiv.textContent = message;
			messageDiv.style.marginBottom = '20px';
			messageDiv.style.padding = '10px';
			messageDiv.style.borderRadius = '3px';
			messageDiv.style.transition = 'all 0.3s ease';
			messageDiv.style.opacity = '0';
			messageDiv.style.transform = 'translateY(-10px)';

			// 插入到表单上方
			const form = document.querySelector('.upload-form');
			const header = form.querySelector('h2');
			form.insertBefore(messageDiv, header.nextSibling);

			// 显示动画
			setTimeout(() => {
				messageDiv.style.opacity = '1';
				messageDiv.style.transform = 'translateY(0)';
			}, 100);

			// 3秒后自动消失
			setTimeout(() => {
				messageDiv.style.opacity = '0';
				messageDiv.style.transform = 'translateY(-10px)';
				setTimeout(() => {
					messageDiv.remove();
				}, 300);
			}, 3000);
		};
	`
}

// 辅助函数：格式化时间间隔
func FormatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

// 辅助函数：获取目录列表
func GetDirectoryList(baseDir string) []string {
	var directories []string

	// 遍历基础目录
	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 如果是目录且不是基础目录本身，添加到列表
		if info.IsDir() && path != baseDir {
			// 获取相对路径
			relPath, err := filepath.Rel(baseDir, path)
			if err != nil {
				return err
			}
			directories = append(directories, relPath)
		}
		return nil
	})

	if err != nil {
		return []string{}
	}

	// 添加根目录选项
	directories = append([]string{"."}, directories...)
	return directories
}

// 辅助函数：生成路径导航
func GeneratePathNavigation(path string) string {
	if path == "." {
		return ""
	}

	var navigation string
	var currentPath string

	parts := strings.Split(path, string(os.PathSeparator))
	for _, part := range parts {
		if part == "." {
			continue
		}

		currentPath = filepath.Join(currentPath, part)
		navigation += fmt.Sprintf(`<span class="path-separator">/</span>
								<div class="path-item">
									<a href="/files?path=%s" class="path-link">%s</a>
								</div>`, url.QueryEscape(currentPath), part)
	}

	return navigation
}

// 日志级别类型
type LogLevel string

const (
	LogLevelInfo     LogLevel = "info"
	LogLevelSuccess  LogLevel = "success"
	LogLevelWarning  LogLevel = "warning"
	LogLevelError    LogLevel = "error"
	LogLevelDebug    LogLevel = "debug"
	LogLevelSecurity LogLevel = "security"
)

// 日志文件句柄缓存：按天复用，避免每条日志都 open+fsync+close 的高频 I/O 开销
var (
	logMu       sync.Mutex
	logFilePool = make(map[string]*os.File)
)

// 辅助函数：记录日志
func Log(level LogLevel, username, role, action, details string) {
	// 确保日志目录存在
	if err := os.MkdirAll(config.AppConfig.Server.LogDir, 0755); err != nil {
		// 如果无法创建日志目录，输出到标准错误
		log.Printf("创建日志目录失败: %v\n", err)
		log.Printf("[%s] [%s] [%s] [%s] %s %s\n",
			time.Now().Format("2006-01-02 15:04:05"),
			string(level),
			username,
			role,
			action,
			details)
		return
	}

	// 日志轮转：每天生成一个新的日志文件
	logFilename := fmt.Sprintf("%s_%s.log", strings.TrimSuffix(config.AppConfig.Server.LogFile, ".log"), time.Now().Format("20060102"))
	logFilePath := filepath.Join(config.AppConfig.Server.LogDir, logFilename)

	// 从缓存获取当日文件句柄（避免每条日志 open+fsync，高并发 I/O 热点）
	logMu.Lock()
	logFile := logFilePool[logFilePath]
	if logFile == nil {
		f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			logMu.Unlock()
			// 如果无法打开日志文件，输出到标准错误
			log.Printf("无法打开日志文件: %v\n", err)
			log.Printf("[%s] [%s] [%s] [%s] %s %s\n",
				time.Now().Format("2006-01-02 15:04:05"),
				string(level),
				username,
				role,
				action,
				details)
			return
		}
		logFile = f
		logFilePool[logFilePath] = f
	}
	logMu.Unlock()

	// 直接写入日志，避免fmt.Sprintf处理包含%字符的字符串
	logEntry := time.Now().Format("2006-01-02 15:04:05") + " [" + string(level) + "] [" + username + "] [" + role + "] " + action + " " + details + "\n"

	// 写入日志（不再逐条 Sync，避免磁盘 I/O 热点；进程退出时由 OS 刷盘）
	if _, err := logFile.WriteString(logEntry); err != nil {
		log.Printf("写入日志失败: %v\n", err)
	}
}

// GetRecentLogs 获取最近的操作日志并格式化成HTML
func GetRecentLogs(count int) string {
	// 构建今天的日志文件路径
	logFilename := fmt.Sprintf("%s_%s.log", strings.TrimSuffix(config.AppConfig.Server.LogFile, ".log"), time.Now().Format("20060102"))
	logFilePath := filepath.Join(config.AppConfig.Server.LogDir, logFilename)

	// 读取日志文件
	data, err := os.ReadFile(logFilePath)
	if err != nil {
		return `<div style="text-align: center; padding: 40px; color: #9ca3af; font-size: 14px;">暂无操作日志</div>`
	}

	// 按行分割，取最后N条
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}

	// 反转顺序（最新的在前面）
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}

	// 格式化成HTML
	var html strings.Builder
	html.WriteString(`<div style="display: flex; flex-direction: column; gap: 0;">`)

	for _, line := range lines {
		if line == "" {
			continue
		}

		// 解析日志行：时间 [级别] [用户名] [角色] 操作 详情
		parts := strings.SplitN(line, " ", 6)
		if len(parts) < 6 {
			continue
		}

		timestamp := parts[0] + " " + parts[1]
		level := strings.Trim(parts[2], "[]")
		username := strings.Trim(parts[3], "[]")
		role := strings.Trim(parts[4], "[]")
		action := parts[5]

		// 级别颜色
		levelColor := "#6b7280"
		levelBg := "#f3f4f6"
		switch level {
		case "INFO":
			levelColor = "#2563eb"
			levelBg = "#dbeafe"
		case "SUCCESS":
			levelColor = "#16a34a"
			levelBg = "#dcfce7"
		case "WARN":
			levelColor = "#d97706"
			levelBg = "#fef3c7"
		case "ERROR":
			levelColor = "#dc2626"
			levelBg = "#fee2e2"
		}

		// 角色显示
		roleText := role
		switch role {
		case "admin":
			roleText = "管理员"
		case "subadmin":
			roleText = "二级管理员"
		case "normal":
			roleText = "普通用户"
		case "guest":
			roleText = "访客"
		}

		// XSS防护：对用户可控的输出进行HTML编码
		safeUsername := EscapeHTML(username)
		safeAction := EscapeHTML(action)

		html.WriteString(fmt.Sprintf(`
			<div style="display: flex; align-items: flex-start; padding: 12px 0; border-bottom: 1px solid #f3f4f6; gap: 12px;">
				<div style="flex-shrink: 0; width: 8px; height: 8px; border-radius: 50%%; background: %s; margin-top: 6px;"></div>
				<div style="flex: 1; min-width: 0;">
					<div style="display: flex; align-items: center; gap: 8px; margin-bottom: 4px;">
						<span style="font-size: 12px; color: #9ca3af;">%s</span>
						<span style="font-size: 11px; padding: 2px 6px; border-radius: 4px; background: %s; color: %s; font-weight: 500;">%s</span>
						<span style="font-size: 12px; color: #6b7280;">%s</span>
						<span style="font-size: 11px; color: #9ca3af;">(%s)</span>
					</div>
					<div style="font-size: 13px; color: #374151; word-break: break-all;">%s</div>
				</div>
			</div>
		`, levelColor, timestamp, levelBg, levelColor, level, safeUsername, roleText, safeAction))
	}

	html.WriteString(`</div>`)

	if len(lines) == 0 {
		return `<div style="text-align: center; padding: 40px; color: #9ca3af; font-size: 14px;">暂无操作日志</div>`
	}

	return html.String()
}

// 辅助函数：记录HTTP请求日志
func LogRequest(r *http.Request, action, details string) {
	// 获取当前用户信息
	username := "anonymous"
	role := "guest"

	sess := session.GetCurrentUser(r)
	if sess != nil {
		username = sess.Username
		// 正确转换角色类型为字符串
		switch sess.Role {
		case constants.RoleAdmin:
			role = "admin"
		case constants.RoleSubAdmin:
			role = "subadmin"
		case constants.RoleNormal:
			role = "normal"
		default:
			role = "unknown"
		}
	}

	// 获取客户端IP地址
	clientIP := GetClientIP(r)

	// 获取HTTP/HTTPS协议和端口信息
	protocol := "http"
	port := config.AppConfig.Server.Port

	// 检查是否为HTTPS连接
	if r.TLS != nil {
		protocol = "https"
		port = config.AppConfig.Server.HttpsPort
	}

	// 检查X-Forwarded-Proto头信息（如果有反向代理的话）
	if forwardedProto := r.Header.Get("X-Forwarded-Proto"); forwardedProto != "" {
		protocol = forwardedProto
	}

	// 检查X-Forwarded-Port头信息（如果有反向代理的话）
	if forwardedPort := r.Header.Get("X-Forwarded-Port"); forwardedPort != "" {
		// 尝试解析端口号
		if p, err := strconv.Atoi(forwardedPort); err == nil {
			port = p
		}
	}

	// 检查请求路径是否为API
	isAPI := strings.HasPrefix(r.URL.Path, "/api/")
	apiInfo := ""
	if isAPI {
		apiInfo = fmt.Sprintf(" [API: %s]", r.URL.Path)
	}

	// 根据请求内容自动选择日志级别
	var level LogLevel
	switch {
	// 错误类操作
	case strings.Contains(details, "失败"):
		level = LogLevelError
	case strings.Contains(details, "错误"):
		level = LogLevelError
	case strings.Contains(details, "异常"):
		level = LogLevelError
	case strings.Contains(details, "不存在"):
		level = LogLevelError
	case strings.Contains(details, "拒绝"):
		level = LogLevelError
	// 成功类操作
	case strings.Contains(action, "success"):
		level = LogLevelSuccess
	// 调试类操作
	case strings.Contains(action, "debug"):
		level = LogLevelDebug
	// 警告类操作
	case strings.Contains(action, "warning"):
		level = LogLevelWarning
	// 其他通用请求 - 默认使用info级别
	default:
		level = LogLevelInfo
	}

	// 添加HTTP/HTTPS、端口和IP地址信息到详情中
	enhancedDetails := fmt.Sprintf("%s [协议: %s, 端口: %d, IP: %s]%s", details, protocol, port, clientIP, apiInfo)

	// 记录日志
	Log(level, username, role, action, enhancedDetails)
}

// 辅助函数：记录用户操作日志，根据操作类型自动选择合适的日志级别
func LogUserAction(r *http.Request, action, details string) {
	// 直接调用LogRequest函数，这样可以自动获得HTTP/HTTPS端口和API信息
	LogRequest(r, action, details)
}

// 辅助函数：清理空目录
func CleanupEmptyDirectories(pendingDir, username, currentPath string) {
	// 构建完整的目录路径
	fullPath := filepath.Join(pendingDir, username, currentPath)

	// 检查目录是否存在
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return
	}

	// 检查目录是否为空
	files, err := os.ReadDir(fullPath)
	if err != nil {
		return
	}

	// 如果目录为空，删除它
	if len(files) == 0 {
		// 尝试删除目录，添加重试机制
		const maxRetries = 3
		const retryDelay = 500 * time.Millisecond
		var deleteErr error

		for i := 0; i < maxRetries; i++ {
			deleteErr = os.Remove(fullPath)
			if deleteErr == nil {
				Log(LogLevelDebug, "system", "admin", "cleanup_directories", fmt.Sprintf("删除空目录: %s", fullPath))
				break
			}

			// 检查是否是目录被占用错误
			if i < maxRetries-1 {
				Log(LogLevelDebug, "system", "admin", "cleanup_directories", fmt.Sprintf("删除空目录失败，正在重试 (%d/%d): %v, 目录路径: %s", i+1, maxRetries, deleteErr, fullPath))
				time.Sleep(retryDelay)
			}
		}

		if deleteErr == nil {
			// 递归检查父目录
			parentPath := filepath.Dir(currentPath)
			// 停止条件：当父目录为 "." 或空字符串时，表示已经到达用户目录
			if parentPath != "." && parentPath != "" {
				CleanupEmptyDirectories(pendingDir, username, parentPath)
			}
		}
	}
}

// EncodePath 编码文件路径用于URL查询参数
// 注意：路径是作为查询参数传递的，所以需要编码所有在查询参数中有特殊含义的字符
// 特别是 + 字符，因为在查询参数中 + 表示空格
func EncodePath(path string) string {
	if path == "." || path == "" {
		return ""
	}

	// 先规范化路径，统一使用正斜杠
	normalizedPath := NormalizePath(path)

	// 分割路径组件（使用正斜杠分割，因为已经规范化）
	parts := strings.Split(normalizedPath, "/")

	// 对每个部分进行URL查询编码
	// 使用QueryEscape会把空格编码为+，但我们需要%20，所以先编码再替换
	encodedParts := make([]string, len(parts))
	for i, part := range parts {
		// 使用QueryEscape编码，然后把+替换为%20
		// 这样可以确保 + 字符被正确编码为 %2B
		encoded := url.QueryEscape(part)
		encoded = strings.ReplaceAll(encoded, "+", "%20")
		encodedParts[i] = encoded
	}

	return strings.Join(encodedParts, "/")
}

// DecodePath 解码URL中的文件路径
func DecodePath(encodedPath string) (string, error) {
	if encodedPath == "" {
		return ".", nil
	}

	// 首先尝试直接使用PathUnescape解码原始路径
	decodedPath, err := url.PathUnescape(encodedPath)
	if err == nil && !strings.Contains(decodedPath, "%!") {
		// 处理路径分隔符
		decodedPath = strings.ReplaceAll(decodedPath, "/", string(filepath.Separator))
		return decodedPath, nil
	}

	// 处理 + 号（URL编码中的空格）
	encodedPath = strings.ReplaceAll(encodedPath, "+", " ")

	// 处理错误的日志格式编码，如 %!E(MISSING)
	// 这种格式常见于Go语言的fmt.Sprintf中使用%v等占位符但参数不匹配的情况
	// 先替换所有的 %!(...) 格式序列
	cleanPath := encodedPath

	// 专门处理 %!(MISSING) 这种格式
	reMissing := regexp.MustCompile(`%![A-Z]\(MISSING\)`)
	cleanPath = reMissing.ReplaceAllString(cleanPath, "_")

	// 处理其他可能的格式错误
	reInvalid := regexp.MustCompile(`%![^\s]*`)
	cleanPath = reInvalid.ReplaceAllString(cleanPath, "_")

	// 处理独立的百分号
	cleanPath = strings.ReplaceAll(cleanPath, "%", "_")

	// 处理路径分隔符
	cleanPath = strings.ReplaceAll(cleanPath, "/", string(filepath.Separator))

	// 移除重复的下划线
	reDuplicateUnderscore := regexp.MustCompile(`_+`)
	cleanPath = reDuplicateUnderscore.ReplaceAllString(cleanPath, "_")

	// 移除开头和结尾的下划线
	cleanPath = strings.Trim(cleanPath, "_")

	// 确保路径不为空
	if cleanPath == "" {
		cleanPath = "decoded_dir"
	}

	// 清理后的路径可能已经是安全的，可以直接返回
	return cleanPath, nil
}

// SafeJoin 安全地拼接路径
func SafeJoin(base, relative string) string {
	// 清理路径
	base = filepath.Clean(base)
	relative = filepath.Clean(relative)

	// 拼接路径
	result := filepath.Join(base, relative)

	// 安全检查，防止路径遍历
	if !strings.HasPrefix(result, base) {
		return base
	}

	return result
}

// CheckAPIAuthentication 检查API认证
// 对于网页界面访问，通过session cookie验证
// 对于其他渠道访问，通过API密钥（当前使用密码）验证
func CheckAPIAuthentication(r *http.Request) bool {
	// 获取客户端IP
	clientIP := GetClientIP(r)

	// 1. 检查是否是网页界面访问（通过User-Agent和session cookie）
	userAgent := r.Header.Get("User-Agent")
	isBrowser := strings.Contains(userAgent, "Mozilla/") || strings.Contains(userAgent, "Chrome/") || strings.Contains(userAgent, "Safari/")

	if isBrowser {
		// 网页界面访问，检查session
		sess := session.GetCurrentUser(r)
		return sess != nil && (sess.Role == constants.RoleAdmin || sess.Role == constants.RoleSubAdmin)
	}

	// 2. 非网页界面访问，需要API密钥认证
	// 检查IP是否被封禁
	if IsIPBanned(clientIP) {
		return false
	}

	// 从Authorization头获取API密钥
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		// 记录失败尝试
		RecordFailedLogin(clientIP)
		return false
	}

	// 解析API密钥（格式：Authorization: ApiKey <key> 或直接使用 <key>）
	apiKey := strings.TrimPrefix(authHeader, "ApiKey ")
	if apiKey == authHeader {
		// 如果没有ApiKey前缀，直接使用authHeader作为密钥
		apiKey = authHeader
	}

	// 优先使用独立 APIKey 认证（不暴露管理员密码）
	if config.AppConfig.Server.APIKey != "" {
		if secureCompare(apiKey, config.AppConfig.Server.APIKey) {
			// 认证成功，清除失败记录
			ClearFailedLogin(clientIP)
			return true
		}
		// 认证失败，记录失败尝试
		RecordFailedLogin(clientIP)
		return false
	}

	// 兼容旧行为：检查是否有管理员用户使用该密钥（常数时间比较）
	config.UsersMu.RLock()
	defer config.UsersMu.RUnlock()
	for _, user := range config.AppConfig.Users {
		if (user.Role == "admin" || user.Role == "subadmin") && secureCompare(user.Password, apiKey) {
			// 认证成功，清除失败记录
			ClearFailedLogin(clientIP)
			return true
		}
	}

	// 认证失败，记录失败尝试
	RecordFailedLogin(clientIP)
	return false
}

// FixPathEncoding 修复路径编码问题
func FixPathEncoding(path string) string {
	// 尝试多次解码，直到没有变化
	for {
		decoded, err := url.QueryUnescape(path)
		if err != nil || decoded == path {
			break
		}
		path = decoded
	}
	return path
}

// NormalizePath 标准化路径分隔符，统一使用正斜杠
func NormalizePath(path string) string {
	if path == "" {
		return ""
	}

	// 将所有反斜杠替换为正斜杠
	normalized := strings.ReplaceAll(path, "\\", "/")

	// 清理路径，移除多余的分隔符，但要注意在Windows上filepath.Clean可能会把正斜杠转回反斜杠
	normalized = filepath.ToSlash(filepath.Clean(normalized))

	// 特殊处理Windows根目录
	if strings.HasPrefix(normalized, "/") && len(normalized) > 1 && normalized[1] == ':' {
		// Windows路径如 /C:/Users -> C:/Users
		normalized = normalized[1:]
	}

	return normalized
}
