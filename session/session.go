package session

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

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

	// 大部分请求只需读锁即可完成校验，避免每个请求在会话读取处被串行化（原实现使用写锁）
	sessionMux.RLock()
	session, exists := sessions[cookie.Value]
	var (
		loginTime time.Time
		username  string
		passHash  string
	)
	if exists {
		loginTime = session.LoginTime
		username = session.Username
		passHash = session.PasswordHash
	}
	sessionMux.RUnlock()

	if !exists {
		return nil
	}

	// 检查会话是否过期（24小时）
	if time.Since(loginTime) > 24*time.Hour {
		invalidateSession(cookie.Value)
		return nil
	}

	// 验证用户是否存在于配置中
	config.UsersMu.RLock()
	userConfig, exists := config.UserConfigMap[username]
	config.UsersMu.RUnlock()
	if !exists {
		// 用户不存在于配置中，清除会话
		invalidateSession(cookie.Value)
		return nil
	}

	// 验证密码哈希是否一致
	currentPasswordHash := getPasswordHash(userConfig.Password)
	if passHash != currentPasswordHash {
		// 密码已修改，清除会话
		invalidateSession(cookie.Value)
		return nil
	}

	// 更新会话信息（如果有变化）—— 仅此处需要写锁
	sessionMux.Lock()
	session.MaxFileSize = userConfig.MaxFileSize
	sessionMux.Unlock()

	return session
}

// invalidateSession 在独立的写锁中删除指定会话，避免在读锁区间内尝试升级为写锁
func invalidateSession(id string) {
	sessionMux.Lock()
	delete(sessions, id)
	sessionMux.Unlock()
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

// 辅助函数：获取密码哈希（用于会话比较，直接返回存储的密码字符串）
// 注意：这里不重新哈希，因为bcrypt每次哈希结果不同（随机salt）
// 会话比较直接比较配置文件中存储的原始密码字符串
func getPasswordHash(password string) string {
	return password
}

// HashPassword 使用bcrypt哈希密码
// 新用户添加和密码修改时调用
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// IsBcryptHash 判断密码是否是bcrypt哈希
// bcrypt哈希格式：$2a$10$... 或 $2b$10$...
func IsBcryptHash(password string) bool {
	return strings.HasPrefix(password, "$2a$") || strings.HasPrefix(password, "$2b$") || strings.HasPrefix(password, "$2y$")
}

// VerifyPassword 验证密码，兼容明文和bcrypt哈希
// storedPassword 是配置文件中存储的密码（可能是明文或bcrypt哈希）
// inputPassword 是用户输入的明文密码
func VerifyPassword(inputPassword, storedPassword string) bool {
	// 如果是bcrypt哈希，使用bcrypt验证
	if IsBcryptHash(storedPassword) {
		err := bcrypt.CompareHashAndPassword([]byte(storedPassword), []byte(inputPassword))
		return err == nil
	}
	// 否则是明文密码，恒定时间比较，避免时序侧信道泄露密码长度/内容
	return subtle.ConstantTimeCompare([]byte(inputPassword), []byte(storedPassword)) == 1
}

// UpgradePasswordToBcrypt 将用户的明文密码升级为bcrypt哈希
// 登录时如果检测到明文密码，验证成功后自动调用此函数升级
func UpgradePasswordToBcrypt(username string) error {
	// 第一步：在持有锁的情况下完成数据修改
	config.UsersMu.Lock()

	// 查找用户在 AppConfig.Users 切片中的索引
	userIndex := -1
	for i, u := range config.AppConfig.Users {
		if u.Username == username {
			userIndex = i
			break
		}
	}

	if userIndex == -1 {
		config.UsersMu.Unlock()
		return fmt.Errorf("用户不存在: %s", username)
	}

	userConfig := config.AppConfig.Users[userIndex]

	// 如果已经是bcrypt哈希，不需要升级
	if IsBcryptHash(userConfig.Password) {
		config.UsersMu.Unlock()
		return nil
	}

	// 使用bcrypt哈希密码
	hashedPassword, err := HashPassword(userConfig.Password)
	if err != nil {
		config.UsersMu.Unlock()
		return fmt.Errorf("密码哈希失败: %w", err)
	}

	// 更新 AppConfig.Users 切片中的密码
	config.AppConfig.Users[userIndex].Password = hashedPassword

	// 重新同步 UserConfigMap
	config.SyncUserConfigMapLocked()

	// 释放锁（SaveConfig内部会获取读锁，不能在持有写锁时调用）
	config.UsersMu.Unlock()

	// 第二步：释放锁后保存配置文件
	if err := config.SaveConfig(); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	return nil
}

// UpgradeAllPlaintextPasswords 启动时批量将明文密码升级为 bcrypt 哈希。
// 空密码用户（如无登录权限的 download 用户）跳过，避免改变"空密码"语义。
// 返回实际升级的用户数；无明文密码时返回 0。
func UpgradeAllPlaintextPasswords() (int, error) {
	// 第一步：持锁快照明文密码用户
	type plainUser struct{ username, plain string }
	var targets []plainUser
	config.UsersMu.RLock()
	for _, u := range config.AppConfig.Users {
		if u.Password != "" && !IsBcryptHash(u.Password) {
			targets = append(targets, plainUser{u.Username, u.Password})
		}
	}
	config.UsersMu.RUnlock()

	if len(targets) == 0 {
		return 0, nil
	}

	// 第二步：锁外逐个哈希（bcrypt cost 10 较耗时，避免长时间持锁）
	hashed := make(map[string]string, len(targets))
	for _, t := range targets {
		h, err := HashPassword(t.plain)
		if err != nil {
			return 0, fmt.Errorf("哈希用户 %s 密码失败: %w", t.username, err)
		}
		hashed[t.username] = h
	}

	// 第三步：写回（按 username 定位，期间若已被并发改为 bcrypt 则跳过）
	upgraded := 0
	config.UsersMu.Lock()
	for i := range config.AppConfig.Users {
		u := &config.AppConfig.Users[i]
		if h, ok := hashed[u.Username]; ok && !IsBcryptHash(u.Password) {
			u.Password = h
			upgraded++
		}
	}
	if upgraded > 0 {
		config.SyncUserConfigMapLocked()
	}
	config.UsersMu.Unlock()

	// 第四步：释放锁后保存配置
	if upgraded > 0 {
		if err := config.SaveConfig(); err != nil {
			return upgraded, fmt.Errorf("保存升级后配置失败: %w", err)
		}
	}
	return upgraded, nil
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
