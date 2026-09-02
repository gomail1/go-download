package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-download-server/internal/config"
	"go-download-server/internal/core"
	"go-download-server/internal/logger"
	"go-download-server/utils"

	"github.com/gin-gonic/gin"
)

// Server represents the API server
type Server struct {
	engine     *gin.Engine
	httpServer *http.Server
	coreEngine core.Engine
}

// NewServer creates a new API server
func NewServer(coreEngine core.Engine) *Server {
	// Set Gin mode based on log level
	if config.Get().Core.LogLevel == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	// 添加Recovery中间件，保留错误恢复功能但移除日志
	engine.Use(gin.Recovery())

	// Create server instance
	server := &Server{
		engine:     engine,
		coreEngine: coreEngine,
	}

	// Setup routes
	server.setupRoutes()

	return server
}

// API请求日志中间件
func (s *Server) apiLogMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 记录API请求日志
		action := "api_request"
		details := fmt.Sprintf("方法: %s, 路径: %s", c.Request.Method, c.Request.URL.Path)
		utils.LogRequest(c.Request, action, details)
		c.Next()
	}
}

// Setup routes
func (s *Server) setupRoutes() {
	// 添加API日志中间件
	s.engine.Use(s.apiLogMiddleware())

	// 需要登录 + CSRF防护的路由组（管理页面AJAX调用）
	authed := s.engine.Group("/", s.sessionAuthMiddleware(), s.csrfWriteMiddleware())
	{
		// Tasks endpoints
		authed.GET("/tasks", s.GetTasks)
		authed.POST("/tasks", s.CreateTask)
		authed.POST("/tasks/upload", s.UploadTorrentFile)
		authed.GET("/tasks/:id", s.GetTask)
		authed.PUT("/tasks/:id/pause", s.PauseTask)
		authed.PUT("/tasks/:id/resume", s.ResumeTask)
		authed.DELETE("/tasks/:id", s.DeleteTask)

		// Statistics endpoint
		authed.GET("/stats", s.GetStatistics)
	}

	// WebSocket endpoint（握手为GET，仅要求登录，无法自定义CSRF请求头）
	s.engine.GET("/ws/events", s.sessionAuthMiddleware(), s.WebSocketHandler)
}

// Start starts the API server
func (s *Server) Start() error {
	cfg := config.Get()
	addr := fmt.Sprintf(":%d", cfg.UI.WebPort)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.engine,
	}

	// Start server in a goroutine
	go func() {
		logger.Infof("WEB服务器启动，监听端口: %d", cfg.UI.WebPort)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("WEB服务器启动失败: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("正在关闭WEB服务器...")

	// Create context with timeout for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		logger.Fatalf("WEB服务器关闭失败: %v", err)
	}

	logger.Info("WEB服务器已关闭")
	return nil
}

// GetHandler returns the HTTP handler for the API server
func (s *Server) GetHandler() http.Handler {
	return s.engine
}
