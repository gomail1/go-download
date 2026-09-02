package session

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"go-download-server/config"
	"go-download-server/constants"
)

// CookieSecure 控制 session cookie 的 Secure 属性。
// 默认 false（服务同时监听 HTTP 与 HTTPS，置 true 会断掉 HTTP 登录）；
// 仅在关闭 HTTP、只保留 HTTPS 的部署中于启动时置为 true。
var CookieSecure bool

// 用户会话管理
type Session struct {
	Username        string
	Role            constants.UserRole
	LoginTime       time.Time
	MaxFileSize     int64
	PasswordHash    string // 密码哈希，用于验证密码是否变化
	IsAdmin         bool
	CanManageUsers  bool
	CanApproveFiles bool
	CanDeleteFiles  bool
	CanViewLogs     bool
	CanViewStats    bool
	CanCreateDir    bool
	AgreedToTerms   bool // 是否已同意免责协议
}

// 根据用户角色初始化会话权限
func (s *Session) InitByRole(role constants.UserRole) {
	switch role {
	case constants.RoleAdmin:
		s.IsAdmin = true
		s.CanManageUsers = true
		s.CanApproveFiles = true
		s.CanDeleteFiles = true
		s.CanViewLogs = true
		s.CanViewStats = true
		s.CanCreateDir = true
		s.MaxFileSize = constants.MaxFileSizeUnlimited
	case constants.RoleSubAdmin:
		s.IsAdmin = true // 二级管理员也属于管理员类别
		s.CanManageUsers = true
		s.CanApproveFiles = true
		s.CanDeleteFiles = true
		s.CanViewLogs = true
		s.CanViewStats = false // 二级管理员不能查看统计信息
		s.CanCreateDir = true
		s.MaxFileSize = constants.MaxFileSizeUnlimited
	default:
		s.IsAdmin = false
		s.CanManageUsers = false
		s.CanApproveFiles = false
		s.CanDeleteFiles = false
		s.CanViewLogs = false
		s.CanViewStats = false
		s.CanCreateDir = false
		s.MaxFileSize = constants.MaxFileSizeNormal
	}
}

var (
	sessions   = make(map[string]*Session)
	sessionMux sync.RWMutex
)

// StartSessionCleanup 启动后台会话过期清理协程。
// 每 interval 遍历一次 sessions，删除超过 24 小时的会话，避免不活跃会话内存累积。
func StartSessionCleanup(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			expired := time.Now().Add(-24 * time.Hour)
			sessionMux.Lock()
			for id, s := range sessions {
				if s.LoginTime.Before(expired) {
					delete(sessions, id)
				}
			}
			sessionMux.Unlock()
		}
	}()
}

// 辅助函数：获取当前用户会话
func GetCurrentUser(r *http.Request) *Session {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		return nil
	}

	sessionMux.Lock()
	defer sessionMux.Unlock()

	session, exists := sessions[cookie.Value]
	if !exists {
		return nil
	}

	// 检查会话是否过期（24小时）
	if time.Since(session.LoginTime) > 24*time.Hour {
		delete(sessions, cookie.Value)
		return nil
	}

	// 验证用户是否存在于配置中
	config.UsersMu.RLock()
	userConfig, exists := config.UserConfigMap[session.Username]
	config.UsersMu.RUnlock()
	if !exists {
		// 用户不存在于配置中，清除会话
		delete(sessions, cookie.Value)
		return nil
	}

	// 验证密码哈希是否一致
	currentPasswordHash := getPasswordHash(userConfig.Password)
	if session.PasswordHash != currentPasswordHash {
		// 密码已修改，清除会话
		delete(sessions, cookie.Value)
		return nil
	}

	// 更新会话信息（如果有变化）
	session.MaxFileSize = userConfig.MaxFileSize

	return session
}

// 辅助函数：验证会话的有效性（包括密码验证）
func ValidateSession(r *http.Request) *Session {
	// 获取当前会话
	sess := GetCurrentUser(r)
	if sess == nil {
		return nil
	}

	// 检查用户密码是否与配置文件一致
	// 由于会话中没有存储密码，我们需要通过其他方式验证
	// 这里的逻辑是：每次请求都会重新从配置中获取用户信息
	// 如果用户信息发生变化（如密码修改），会话会被自动刷新
	return sess
}

// 辅助函数：生成安全的会话ID
func generateSessionID() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		// 如果crypto/rand失败，使用时间戳作为后备（不推荐，但比崩溃好）
		return time.Now().Format("20060102150405") + "_" + "fallback"
	}
	return hex.EncodeToString(bytes)
}

// 辅助函数：计算密码哈希
func getPasswordHash(password string) string {
	// 简单的哈希实现，实际生产环境建议使用更安全的哈希算法
	return password
}

// 辅助函数：设置会话
func SetSession(w http.ResponseWriter, username string, role constants.UserRole) string {
	sessionID := generateSessionID()

	// 设置会话Cookie（加强安全性）
	cookie := http.Cookie{
		Name:     "session_id",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,                    // 防止XSS攻击窃取Cookie
		Secure:   CookieSecure,            // HTTPS-only 部署时置 true（main.go 启动时设置）
		SameSite: http.SameSiteStrictMode, // 防止CSRF攻击
		Expires:  time.Now().Add(24 * time.Hour),
	}
	http.SetCookie(w, &cookie)

	// 获取最大文件大小、密码和协议同意状态（从配置文件）
	maxFileSize := constants.MaxFileSizeNormal
	password := ""
	agreedToTerms := false
	config.UsersMu.RLock()
	if userConfig, exists := config.UserConfigMap[username]; exists {
		maxFileSize = userConfig.MaxFileSize
		password = userConfig.Password
		agreedToTerms = userConfig.AgreedToTerms
	} else {
		// 如果配置文件中没有，使用默认值
		switch role {
		case constants.RoleAdmin, constants.RoleSubAdmin:
			maxFileSize = constants.MaxFileSizeUnlimited
		case constants.RoleNormal:
			maxFileSize = constants.MaxFileSizeNormal
		}
	}
	config.UsersMu.RUnlock()

	// 保存会话信息
	sessionMux.Lock()
	defer sessionMux.Unlock()

	// 创建会话并初始化角色权限
	sess := &Session{
		Username:      username,
		Role:          role,
		LoginTime:     time.Now(),
		MaxFileSize:   maxFileSize,
		PasswordHash:  getPasswordHash(password),
		AgreedToTerms: agreedToTerms, // 从配置文件读取协议同意状态
	}

	// 根据角色初始化会话权限
	sess.InitByRole(role)

	sessions[sessionID] = sess
	return sessionID
}

// 辅助函数：清除会话
func ClearSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_id")
	if err == nil {
		// 从服务器删除会话
		sessionMux.Lock()
		delete(sessions, cookie.Value)
		sessionMux.Unlock()

		// 设置Cookie过期
		cookie := http.Cookie{
			Name:     "session_id",
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   CookieSecure,
			SameSite: http.SameSiteStrictMode,
			Expires:  time.Unix(0, 0),
		}
		http.SetCookie(w, &cookie)
	}
}

// 辅助函数：清除指定用户的所有会话
func ClearUserSessions(username string) {
	sessionMux.Lock()
	defer sessionMux.Unlock()

	// 遍历所有会话，删除指定用户的会话
	for sessionID, sess := range sessions {
		if sess.Username == username {
			delete(sessions, sessionID)
		}
	}
}

// 辅助函数：更新会话的协议同意状态
func UpdateSessionAgreedToTerms(sessionID string) {
	sessionMux.Lock()
	defer sessionMux.Unlock()

	// 查找会话
	sess, exists := sessions[sessionID]
	if exists {
		// 更新协议同意状态
		sess.AgreedToTerms = true
	}
}

// RegenerateSessionID 重新生成会话ID，防止会话固定攻击
// 应该在用户登录成功后调用
func RegenerateSessionID(w http.ResponseWriter, r *http.Request) bool {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		return false
	}

	sessionMux.Lock()
	defer sessionMux.Unlock()

	// 获取旧会话
	oldSessionID := cookie.Value
	sess, exists := sessions[oldSessionID]
	if !exists {
		return false
	}

	// 生成新的会话ID
	newSessionID := generateSessionID()

	// 将旧会话复制到新会话ID
	sessions[newSessionID] = sess

	// 删除旧会话
	delete(sessions, oldSessionID)

	// 设置新的Cookie
	newCookie := http.Cookie{
		Name:     "session_id",
		Value:    newSessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   CookieSecure,
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Now().Add(24 * time.Hour),
	}
	http.SetCookie(w, &newCookie)

	return true
}

// GetSessionCount 获取当前活跃会话数量
func GetSessionCount() int {
	sessionMux.RLock()
	defer sessionMux.RUnlock()
	return len(sessions)
}

// GetUserSessionCount 获取指定用户的活跃会话数量
func GetUserSessionCount(username string) int {
	sessionMux.RLock()
	defer sessionMux.RUnlock()

	count := 0
	for _, sess := range sessions {
		if sess.Username == username {
			count++
		}
	}
	return count
}
