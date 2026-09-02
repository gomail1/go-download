package api

import (
	"net/http"

	"go-download-server/session"
	"go-download-server/utils"

	"github.com/gin-gonic/gin"
)

// sessionAuthMiddleware 要求已登录用户（管理员后台页面与AJAX共用）
func (s *Server) sessionAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		sess := session.GetCurrentUser(c.Request)
		if sess == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "未登录或会话已过期，请重新登录",
			})
			return
		}
		c.Next()
	}
}

// csrfWriteMiddleware 对写请求（POST/PUT/DELETE/PATCH）校验CSRF令牌
// 前端页面已注入 meta + csrf.js，fetch/XHR 会自动携带 X-CSRF-Token 头
func (s *Server) csrfWriteMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
			if !utils.ValidateCSRFTokenFromRequest(c.Request) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "CSRF令牌验证失败",
				})
				return
			}
		}
		c.Next()
	}
}

// sessionAuthOnlyMiddleware 仅要求登录（用于WebSocket等无法自定义请求头的场景）
func (s *Server) sessionAuthOnlyMiddleware() gin.HandlerFunc {
	return s.sessionAuthMiddleware()
}
