package utils

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
)

// CSRFToken CSRF令牌管理
var (
	csrfTokens   = make(map[string]string) // sessionID -> csrfToken
	csrfTokensMu sync.RWMutex
)

// GenerateCSRFToken 生成一个新的CSRF令牌
func GenerateCSRFToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// SetCSRFToken 为指定会话设置CSRF令牌
func SetCSRFToken(sessionID string) (string, error) {
	token, err := GenerateCSRFToken()
	if err != nil {
		return "", err
	}

	csrfTokensMu.Lock()
	defer csrfTokensMu.Unlock()
	csrfTokens[sessionID] = token
	return token, nil
}

// GetCSRFToken 获取指定会话的CSRF令牌
func GetCSRFToken(sessionID string) string {
	csrfTokensMu.RLock()
	defer csrfTokensMu.RUnlock()
	return csrfTokens[sessionID]
}

// ValidateCSRFToken 验证CSRF令牌是否有效
func ValidateCSRFToken(sessionID, token string) bool {
	if sessionID == "" || token == "" {
		return false
	}

	csrfTokensMu.RLock()
	defer csrfTokensMu.RUnlock()

	expectedToken, exists := csrfTokens[sessionID]
	if !exists {
		return false
	}

	// 使用常量时间比较，防止时序攻击
	return secureCompare(expectedToken, token)
}

// secureCompare 常量时间字符串比较
func secureCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}

	result := 0
	for i := 0; i < len(a); i++ {
		result |= int(a[i]) ^ int(b[i])
	}
	return result == 0
}

// ClearCSRFToken 清除指定会话的CSRF令牌
func ClearCSRFToken(sessionID string) {
	csrfTokensMu.Lock()
	defer csrfTokensMu.Unlock()
	delete(csrfTokens, sessionID)
}

// GetSessionIDFromRequest 从请求中获取会话ID
// 注意：这需要根据项目的会话管理实现来调整
func GetSessionIDFromRequest(r *http.Request) string {
	// 尝试从Cookie中获取会话ID
	cookie, err := r.Cookie("session_id")
	if err != nil {
		// 如果没有session_id cookie，尝试从其他地方获取
		// 这里可以根据项目的实际会话管理实现来调整
		return ""
	}
	return cookie.Value
}

// CSRFMiddleware CSRF防护中间件
// 注意：这是一个基础实现，需要根据项目的实际情况进行调整
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 只对POST、PUT、DELETE等修改请求进行CSRF验证
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "DELETE" || r.Method == "PATCH" {
			// 获取会话ID
			sessionID := GetSessionIDFromRequest(r)

			// 如果没有会话ID，可能是匿名用户，跳过CSRF验证
			// 注意：这需要根据项目的安全策略来调整
			if sessionID == "" {
				next.ServeHTTP(w, r)
				return
			}

			// 从表单或请求头中获取CSRF令牌
			csrfToken := r.FormValue("csrf_token")
			if csrfToken == "" {
				csrfToken = r.Header.Get("X-CSRF-Token")
			}

			// 验证CSRF令牌
			if !ValidateCSRFToken(sessionID, csrfToken) {
				http.Error(w, "CSRF token validation failed", http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// GenerateCSRFTokenField 生成包含CSRF令牌的隐藏表单字段HTML
func GenerateCSRFTokenField(sessionID string) string {
	token := GetCSRFToken(sessionID)
	if token == "" {
		// 如果没有令牌，生成一个新的
		var err error
		token, err = SetCSRFToken(sessionID)
		if err != nil {
			return ""
		}
	}
	return `<input type="hidden" name="csrf_token" value="` + token + `">`
}

// EnsureCSRFToken 返回指定会话的CSRF令牌，不存在时自动生成
func EnsureCSRFToken(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	if token := GetCSRFToken(sessionID); token != "" {
		return token
	}
	token, err := SetCSRFToken(sessionID)
	if err != nil {
		return ""
	}
	return token
}

// GenerateCSRFTokenMeta 生成CSRF令牌的meta标签，供前端AJAX读取
// 页面需同时引入 /static/js/csrf.js，该脚本会自动为 fetch/XHR 请求附加令牌
func GenerateCSRFTokenMeta(sessionID string) string {
	token := EnsureCSRFToken(sessionID)
	if token == "" {
		return ""
	}
	return `<meta name="csrf-token" content="` + token + `">`
}

// ValidateCSRFTokenFromRequest 从请求中校验CSRF令牌
// 优先读取 X-CSRF-Token 请求头（AJAX），其次读取表单字段 csrf_token（普通表单）
func ValidateCSRFTokenFromRequest(r *http.Request) bool {
	sessionID := GetSessionIDFromRequest(r)
	if sessionID == "" {
		return false
	}
	// 先读请求头：避免对 JSON body 的请求触发表单解析
	token := r.Header.Get("X-CSRF-Token")
	if token == "" {
		token = r.FormValue("csrf_token")
	}
	return ValidateCSRFToken(sessionID, token)
}
