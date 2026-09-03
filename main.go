package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/handlers"
	"go-download-server/internal/api"
	"go-download-server/internal/core"
	"go-download-server/internal/event"
	"go-download-server/internal/logger"
	"go-download-server/protocols/bt"
	"go-download-server/protocols/ftp"
	"go-download-server/protocols/httpx"
	"go-download-server/session"
	"go-download-server/utils"
)

var (
	startTime time.Time
)

func main() {
	// 初始化日志系统（必须放在最前面）
	logger.Init("info")

	// 解析命令行参数
	port := flag.Int("port", 0, "HTTP端口")
	httpsPort := flag.Int("https-port", 0, "HTTPS端口")
	certFile := flag.String("cert-file", "", "SSL证书文件路径")
	keyFile := flag.String("key-file", "", "SSL密钥文件路径")
	flag.Parse()

	// 初始化随机数种子
	rand.Seed(time.Now().UnixNano())

	// 记录启动时间
	startTime = time.Now()
	handlers.StartTime = startTime

	// 启动监控器
	utils.GetMonitor().Start()

	// 加载配置文件
	if err := config.LoadConfig(); err != nil {
		logger.Warnf("警告: 无法加载配置文件，将使用默认配置: %v", err)
		// 使用默认配置
		config.AppConfig = config.Config{
			Users: []config.UserConfig{
				{
					Username:    "admin",
					Password:    "admin123",
					Role:        "admin",
					MaxFileSize: constants.MaxFileSizeUnlimited,
				},
				{
					Username:    "user",
					Password:    "user123",
					Role:        "normal",
					MaxFileSize: constants.MaxFileSizeNormal,
				},
				{
					Username:    "download-user",
					Password:    "",
					Role:        "download",
					MaxFileSize: constants.MaxFileSizeUnlimited,
				},
			},
			Server: config.ServerConfig{
				Port:        constants.Port,
				HttpsPort:   constants.HttpsPort,
				CertFile:    constants.DefaultCertFile,
				KeyFile:     constants.DefaultKeyFile,
				DownloadDir: constants.DownloadDir,
				PendingDir:  constants.PendingDir,
				LogDir:      constants.LogDir,
				LogFile:     constants.LogFile,
				ServerName:  constants.ServerName,
			},
		}
		config.SyncUserConfigMap()
		// 保存默认配置到文件
		if err := config.SaveConfig(); err != nil {
			logger.Warnf("警告: 无法保存默认配置: %v", err)
		}
	}

	// 首次启动时自动创建API密钥（如果未配置）
	if config.AppConfig.Server.APIKey == "" {
		apiKey, err := utils.GenerateAPIKey()
		if err != nil {
			logger.Warnf("警告: 自动生成API密钥失败: %v", err)
		} else {
			// 保存前先备份已有配置文件（防止意外覆盖）
			configPath := filepath.Join(".", "config", "config.json")
			backupPath := filepath.Join(".", "config", "config.json.bak")
			if _, err := os.Stat(configPath); err == nil {
				// 配置文件存在，先备份
				if input, err := os.ReadFile(configPath); err == nil {
					os.WriteFile(backupPath, input, 0644)
					logger.Infof("配置文件已备份: %s", backupPath)
				}
			}

			config.AppConfig.Server.APIKey = apiKey
			if err := config.SaveConfig(); err != nil {
				logger.Warnf("警告: 保存API密钥到配置文件失败: %v", err)
			} else {
				logger.Infof("========================================")
				logger.Infof("  首次启动，已自动生成API密钥")
				logger.Infof("  API Key: %s", apiKey)
				logger.Infof("  仅添加 api_key 字段，其他配置保持不变")
				logger.Infof("  请妥善保管，用于调用 /api/stats 等接口")
				logger.Infof("  配置文件: config/config.json")
				logger.Infof("========================================")
			}
		}
	}

	// 初始化事件系统
	event.Init()

	// 初始化协议管理器
	protocolMgr := core.NewSimpleProtocolManager()

	// 注册协议
	protocolMgr.RegisterProtocol("http", func() core.Protocol { return httpx.NewHTTPProtocol() })
	protocolMgr.RegisterProtocol("https", func() core.Protocol { return httpx.NewHTTPProtocol() })
	// BT（磁力/种子）下载：基于 anacrolix/torrent，真实可用
	protocolMgr.RegisterProtocol("bt", func() core.Protocol { return bt.NewBTProtocol() })
	protocolMgr.RegisterProtocol("magnet", func() core.Protocol { return bt.NewBTProtocol() })
	// FTP 下载：PASV + RETR 真实拉取（仅明文 ftp://，未实现 FTPS/TLS）
	protocolMgr.RegisterProtocol("ftp", func() core.Protocol { return ftp.NewFTPProtocol() })

	// 初始化QuadEngine
	quadEngine := core.NewQuadEngine(protocolMgr)

	// 初始化API服务器
	apiServer := api.NewServer(quadEngine)

	// 初始化WebSocket hub（赋值给 GlobalWebSocketHub，/ws/events 握手与事件广播依赖它）
	api.InitWebSocketHub()

	// 将QuadFetch API路由集成到现有的HTTP服务器中
	http.Handle("/api/", http.StripPrefix("/api", apiServer.GetHandler()))
	http.Handle("/ws/events", apiServer.GetHandler())

	// 注册下载管理相关的路由
	handlers.QuadEngine = quadEngine
	handlers.RegisterDownloadManagementRoutes()

	// 将WebSocket hub暴露给全局变量，便于handlers使用
	// handlers.WSHub = wsHub

	// 应用命令行参数
	if *port > 0 {
		config.AppConfig.Server.Port = *port
	}
	if *httpsPort > 0 {
		config.AppConfig.Server.HttpsPort = *httpsPort
	}
	if *certFile != "" {
		config.AppConfig.Server.CertFile = *certFile
	}
	if *keyFile != "" {
		config.AppConfig.Server.KeyFile = *keyFile
	}

	// 检查环境变量
	if envPort := os.Getenv("PORT"); envPort != "" {
		var port int
		if _, err := fmt.Sscanf(envPort, "%d", &port); err == nil && port > 0 {
			config.AppConfig.Server.Port = port
		}
	}
	if envHttpsPort := os.Getenv("HTTPS_PORT"); envHttpsPort != "" {
		var port int
		if _, err := fmt.Sscanf(envHttpsPort, "%d", &port); err == nil && port > 0 {
			config.AppConfig.Server.HttpsPort = port
		}
	}
	if envCertFile := os.Getenv("SSL_CERT_FILE"); envCertFile != "" {
		config.AppConfig.Server.CertFile = envCertFile
	}
	if envKeyFile := os.Getenv("SSL_KEY_FILE"); envKeyFile != "" {
		config.AppConfig.Server.KeyFile = envKeyFile
	}

	// 确保必要的目录存在
	var err error

	err = os.MkdirAll(config.AppConfig.Server.DownloadDir, 0755)
	if err != nil {
		logger.Fatalf("无法创建下载目录: %v", err)
	}

	err = os.MkdirAll(config.AppConfig.Server.PendingDir, 0755)
	if err != nil {
		logger.Fatalf("无法创建待审核目录: %v", err)
	}

	err = os.MkdirAll(config.AppConfig.Server.LogDir, 0755)
	if err != nil {
		logger.Fatalf("无法创建日志目录: %v", err)
	}

	// 确保SSL证书目录存在
	err = os.MkdirAll("ssl", 0755)
	if err != nil {
		logger.Warnf("无法创建SSL证书目录: %v", err)
	}

	// 初始化每日上传数据
	handlers.InitDailyUpload()

	// 初始化统计数据
	handlers.InitStats()

	// 初始化IP下载统计
	handlers.InitIPStats()

	// 初始化图标缓存（使用配置目录下的路径，升级时无需新增路径映射）
	iconCacheDir := config.AppConfig.Server.IconCacheDir
	if iconCacheDir == "" {
		iconCacheDir = "./config/icons/cache"
	}
	if err := utils.InitIconCache(iconCacheDir); err != nil {
		logger.Warnf("初始化图标缓存失败: %v", err)
	}

	// 启动文件缓存定期清理任务
	handlers.StartCacheCleanupTask()

	// 启动会话过期清理（每10分钟清理一次）
	session.StartSessionCleanup(10 * time.Minute)

	// 注册HTTP处理函数
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.HandleFunc("/", handlers.IndexHandler)
	http.HandleFunc("/files", handlers.FilesHandler)
	http.HandleFunc("/upload", handlers.UploadHandler)
	http.HandleFunc("/download", handlers.DownloadHandler)
	http.HandleFunc("/delete", handlers.DeleteHandler)
	http.HandleFunc("/batch-delete", handlers.BatchDeleteHandler)
	http.HandleFunc("/batch-move", handlers.BatchMoveHandler)
	http.HandleFunc("/batch-copy", handlers.BatchCopyHandler)
	http.HandleFunc("/login", handlers.LoginHandler)
	http.HandleFunc("/logout", handlers.LogoutHandler)
	http.HandleFunc("/admin", handlers.AdminHandler)
	http.HandleFunc("/user-management", handlers.UserManagementHandler)
	http.HandleFunc("/change-password", handlers.ChangePasswordHandler)
	http.HandleFunc("/add-user", handlers.AddUserHandler)
	http.HandleFunc("/delete-user", handlers.DeleteUserHandler)
	http.HandleFunc("/mkdir", handlers.MkdirHandler)
	http.HandleFunc("/review", handlers.ReviewHandler)
	http.HandleFunc("/approve", handlers.ApproveHandler)
	http.HandleFunc("/reject", handlers.RejectHandler)
	http.HandleFunc("/logs", handlers.LogsHandler)
	http.HandleFunc("/info", handlers.InfoHandler)
	// IP管理路由
	http.HandleFunc("/ip-management", handlers.IPAdminHandler)
	http.HandleFunc("/api/ip/stats", handlers.APIListIPStats)
	http.HandleFunc("/api/ip/block", handlers.APIBlockIP)
	http.HandleFunc("/api/ip/unblock", handlers.APIUnblockIP)
	http.HandleFunc("/api/ip/limit-config", handlers.APIGetIPLimitConfig)
	http.HandleFunc("/api/ip/limit-config/update", handlers.APIUpdateIPLimitConfig)
	http.HandleFunc("/password-changed", handlers.PasswordChangedHandler)
	http.HandleFunc("/api/increment-share", handlers.IncrementShareHandler)
	http.HandleFunc("/api/stats", handlers.StatsHandler)
	http.HandleFunc("/api/save-custom-sort", handlers.SaveCustomSortHandler)
	http.HandleFunc("/api/get-custom-sort", handlers.GetCustomSortHandler)
	http.HandleFunc("/api/generate-short-url", handlers.GenerateShortURLHandler)
	http.HandleFunc("/api/delete-short-url", handlers.DeleteShortURLHandler)
	// 分类管理API
	http.HandleFunc("/api/categories", handlers.CategoriesHandler)
	http.HandleFunc("/api/categories/", handlers.CategoryDetailHandler)
	http.HandleFunc("/api/file-category", handlers.FileCategoryHandler)
	// http.HandleFunc("/api/daily-traffic", handlers.GetDailyTrafficStatsHandler) // 移除每日流量统计API路由
	http.HandleFunc("/heatmap", handlers.HeatmapHandler)
	// 短链重定向路由
	http.HandleFunc("/s/", handlers.ShortURLHandler)
	// 注册协议相关的路由
	http.HandleFunc("/terms", handlers.TermsHandler)
	http.HandleFunc("/agree-terms", handlers.AgreeTermsHandler)
	// 图标提取路由
	http.HandleFunc("/icon", handlers.IconHandler)

	// 创建带中间件的Handler
	var handler http.Handler = http.DefaultServeMux
	handler = SecurityHeadersMiddleware(handler)
	handler = CacheMiddleware(handler)
	handler = GzipMiddleware(handler)
	handler = LoggingMiddleware(handler)

	// 启动HTTP服务器
	httpAddr := fmt.Sprintf("0.0.0.0:%d", config.AppConfig.Server.Port) // 明确指定监听所有IPv4接口
	logger.Infof("HTTP服务器启动成功，监听地址: %s", httpAddr)
	logger.Infof("HTTP访问地址: http://localhost:%d", config.AppConfig.Server.Port)
	logger.Infof("HTTP IPv4访问地址: http://127.0.0.1:%d", config.AppConfig.Server.Port)

	// 显式创建 http.Server 以支持优雅关闭
	httpServer := &http.Server{Addr: httpAddr, Handler: handler}
	// 启动HTTP服务器
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("HTTP服务器启动失败: %v", err)
		}
	}()

	// 检查证书文件是否存在
	_, certErr := os.Stat(config.AppConfig.Server.CertFile)
	_, keyErr := os.Stat(config.AppConfig.Server.KeyFile)

	// 启动HTTPS服务器（在goroutine中，不阻塞HTTP服务器）
	httpsServer := &http.Server{Addr: fmt.Sprintf(":%d", config.AppConfig.Server.HttpsPort), Handler: handler}
	go func() {
		httpsAddr := httpsServer.Addr

		if os.IsNotExist(certErr) || os.IsNotExist(keyErr) {
			logger.Warnf("警告: SSL证书文件不存在 (%s, %s)，HTTPS服务将无法启动",
				config.AppConfig.Server.CertFile, config.AppConfig.Server.KeyFile)
			logger.Info("请使用以下命令生成自签名证书:")
			logger.Infof("openssl req -x509 -newkey rsa:4096 -nodes -out %s -keyout %s -days 365",
				config.AppConfig.Server.CertFile, config.AppConfig.Server.KeyFile)
			logger.Info("HTTP服务仍将继续运行...")
			return
		}

		logger.Infof("HTTPS服务器启动成功，监听地址: %s", httpsAddr)
		logger.Infof("HTTPS访问地址: https://localhost%s", httpsAddr)

		// 启动HTTPS服务器
		if err := httpsServer.ListenAndServeTLS(config.AppConfig.Server.CertFile, config.AppConfig.Server.KeyFile); err != nil && err != http.ErrServerClosed {
			logger.Warnf("警告: HTTPS服务器启动失败: %v", err)
			return
		}
	}()

	// 等待信号以优雅关闭服务器
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 等待信号
	sig := <-sigChan
	logger.Infof("收到信号 %v，正在优雅关闭...", sig)

	// 停止接收新连接，等待在途请求完成
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Warnf("HTTP服务器关闭超时: %v", err)
	}
	if err := httpsServer.Shutdown(shutdownCtx); err != nil {
		logger.Warnf("HTTPS服务器关闭超时: %v", err)
	}

	// 关闭下载引擎（取消任务上下文，停止常驻协程）
	if err := quadEngine.Close(); err != nil {
		logger.Warnf("关闭下载引擎失败: %v", err)
	}

	// 保存统计数据
	logger.Infof("保存统计数据...")
	// 直接调用saveStatsData()，确保数据保存
	handlers.SaveStatsData()

	logger.Infof("服务器已关闭")
}
