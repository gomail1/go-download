package session

import (
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"

	"go-download-server/config"
	"go-download-server/constants"
)

// 用户会话管理
type Session struct {
	Username    string
	Role        constants.UserRole
	LoginTime   time.Time
	MaxFileSize int64
}

var (
	sessions   = make(map[string]*Session)
	sessionMux sync.Mutex
)

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

	return session
}

// 辅助函数：生成会话ID
func generateSessionID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10) + "_" + strconv.Itoa(rand.Intn(10000))
}

// 辅助函数：设置会话
func SetSession(w http.ResponseWriter, username string, role constants.UserRole) {
	sessionID := generateSessionID()

	// 设置会话Cookie
	cookie := http.Cookie{
		Name:     "session_id",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // 开发环境使用false，生产环境建议使用true
		Expires:  time.Now().Add(24 * time.Hour),
	}
	http.SetCookie(w, &cookie)

	// 获取最大文件大小（从配置文件）
	maxFileSize := constants.MaxFileSizeTest
	if userConfig, exists := config.UserConfigMap[username]; exists {
		maxFileSize = userConfig.MaxFileSize
	} else {
		// 如果配置文件中没有，使用默认值
		switch role {
		case constants.RoleAdmin:
			maxFileSize = constants.MaxFileSizeUnlimited
		case constants.RoleNormal:
			maxFileSize = constants.MaxFileSizeNormal
		}
	}

	// 保存会话信息
	sessionMux.Lock()
	defer sessionMux.Unlock()

	sessions[sessionID] = &Session{
		Username:    username,
		Role:        role,
		LoginTime:   time.Now(),
		MaxFileSize: maxFileSize,
	}
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
			Secure:   false,
			Expires:  time.Unix(0, 0),
		}
		http.SetCookie(w, &cookie)
	}
}
