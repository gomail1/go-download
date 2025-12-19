package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/handlers"
)

var (
	startTime time.Time
)

func main() {
	// 解析命令行参数
	httpsPort := flag.Int("https-port", 0, "HTTPS端口")
	certFile := flag.String("cert-file", "", "SSL证书文件路径")
	keyFile := flag.String("key-file", "", "SSL密钥文件路径")
	flag.Parse()

	// 初始化随机数种子
	rand.Seed(time.Now().UnixNano())

	// 记录启动时间
	startTime = time.Now()
	handlers.StartTime = startTime

	// 加载配置文件
	if err := config.LoadConfig(); err != nil {
		log.Printf("警告: 无法加载配置文件，将使用默认配置: %v", err)
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
					Username:    "test",
					Password:    "test123",
					Role:        "test",
					MaxFileSize: constants.MaxFileSizeTest,
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
			},
		}
		config.UserConfigMap = make(map[string]config.UserConfig)
		for _, user := range config.AppConfig.Users {
			config.UserConfigMap[user.Username] = user
		}
		// 保存默认配置到文件
		if err := config.SaveConfig(); err != nil {
			log.Printf("警告: 无法保存默认配置: %v", err)
		}
	}

	// 应用命令行参数
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
		log.Fatalf("无法创建下载目录: %v", err)
	}

	err = os.MkdirAll(config.AppConfig.Server.PendingDir, 0755)
	if err != nil {
		log.Fatalf("无法创建待审核目录: %v", err)
	}

	err = os.MkdirAll(config.AppConfig.Server.LogDir, 0755)
	if err != nil {
		log.Fatalf("无法创建日志目录: %v", err)
	}

	// 确保SSL证书目录存在
	err = os.MkdirAll("ssl", 0755)
	if err != nil {
		log.Printf("无法创建SSL证书目录: %v", err)
	}

	// 注册HTTP处理函数
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

	// 启动HTTP服务器
	httpAddr := fmt.Sprintf(":%d", config.AppConfig.Server.Port)
	log.Printf("HTTP服务器启动成功，监听地址: %s", httpAddr)
	log.Printf("HTTP访问地址: http://localhost%s", httpAddr)

	// 启动HTTP服务器
	go func() {
		if err := http.ListenAndServe(httpAddr, nil); err != nil {
			log.Fatalf("HTTP服务器启动失败: %v", err)
		}
	}()

	// 启动HTTPS服务器
	httpsAddr := fmt.Sprintf(":%d", config.AppConfig.Server.HttpsPort)
	log.Printf("HTTPS服务器启动成功，监听地址: %s", httpsAddr)
	log.Printf("HTTPS访问地址: https://localhost%s", httpsAddr)

	// 检查证书文件是否存在
	_, certErr := os.Stat(config.AppConfig.Server.CertFile)
	_, keyErr := os.Stat(config.AppConfig.Server.KeyFile)

	// 启动HTTPS服务器（在goroutine中，不阻塞HTTP服务器）
	go func() {
		if os.IsNotExist(certErr) || os.IsNotExist(keyErr) {
			log.Printf("警告: SSL证书文件不存在 (%s, %s)，HTTPS服务将无法启动",
				config.AppConfig.Server.CertFile, config.AppConfig.Server.KeyFile)
			log.Printf("请使用以下命令生成自签名证书:")
			log.Printf("openssl req -x509 -newkey rsa:4096 -nodes -out %s -keyout %s -days 365",
				config.AppConfig.Server.CertFile, config.AppConfig.Server.KeyFile)
			return
		}

		// 启动HTTPS服务器
		if err := http.ListenAndServeTLS(httpsAddr, config.AppConfig.Server.CertFile, config.AppConfig.Server.KeyFile, nil); err != nil {
			log.Printf("警告: HTTPS服务器启动失败: %v", err)
			return
		}
	}()

	// 等待HTTP服务器退出
	select {}
}
