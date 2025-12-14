package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	port        = 9980
	downloadDir = "./downloads"
	pendingDir  = "./pending"
	logDir      = "./logs"
	logFile     = "server.log"
	// 飞牛系统部署路径配置
	serverName = "Go 下载站"
	// 版本信息
	version   = "v0.0.1"
	developer = "gomail1"
)

// 用户角色类型
type UserRole int

const (
	RoleTest UserRole = iota
	RoleNormal
	RoleAdmin
)

// 权限常量
const (
	MaxFileSizeTest      int64 = 1024 * 1024 * 1024  // 1024MB
	MaxFileSizeNormal    int64 = 10240 * 1024 * 1024 // 10240MB
	MaxFileSizeUnlimited int64 = 0                   // 无限制
)

// 配置文件结构体
type UserConfig struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	Role        string `json:"role"`
	MaxFileSize int64  `json:"max_file_size"`
}

type ServerConfig struct {
	Port        int    `json:"port"`
	DownloadDir string `json:"download_dir"`
	PendingDir  string `json:"pending_dir"`
	LogDir      string `json:"log_dir"`
	LogFile     string `json:"log_file"`
}

type Config struct {
	Users  []UserConfig `json:"users"`
	Server ServerConfig `json:"server"`
}

// 全局配置实例
var config Config

// 用户配置映射
var userConfigMap map[string]UserConfig

// 加载配置文件
func loadConfig() error {
	// 首先尝试从当前工作目录加载配置文件
	currentDir, err := os.Getwd()
	if err == nil {
		configPath := filepath.Join(currentDir, "config.json")
		file, err := os.Open(configPath)
		if err == nil {
			defer file.Close()
			// 解析配置文件
			if err := json.NewDecoder(file).Decode(&config); err == nil {
				// 初始化用户配置映射
				userConfigMap = make(map[string]UserConfig)
				for _, user := range config.Users {
					userConfigMap[user.Username] = user
				}
				return nil
			}
		}
	}

	// 如果当前工作目录没有配置文件，再尝试从执行目录加载
	configPath := filepath.Join(getExecDir(), "config.json")
	file, err := os.Open(configPath)
	if err != nil {
		return fmt.Errorf("无法打开配置文件: %w", err)
	}
	defer file.Close()

	// 解析配置文件
	if err := json.NewDecoder(file).Decode(&config); err != nil {
		return fmt.Errorf("无法解析配置文件: %w", err)
	}

	// 初始化用户配置映射
	userConfigMap = make(map[string]UserConfig)
	for _, user := range config.Users {
		userConfigMap[user.Username] = user
	}

	return nil
}

// 保存配置文件
func saveConfig() error {
	// 首先尝试保存到当前工作目录
	currentDir, err := os.Getwd()
	if err == nil {
		configPath := filepath.Join(currentDir, "config.json")
		file, err := os.Create(configPath)
		if err == nil {
			defer file.Close()
			// 将配置序列化为JSON格式并写入文件
			encoder := json.NewEncoder(file)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(&config); err == nil {
				return nil
			}
		}
	}

	// 如果当前工作目录保存失败，尝试保存到执行目录
	configPath := filepath.Join(getExecDir(), "config.json")
	file, err := os.Create(configPath)
	if err != nil {
		return fmt.Errorf("无法创建配置文件: %w", err)
	}
	defer file.Close()

	// 将配置序列化为JSON格式并写入文件
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(&config); err != nil {
		return fmt.Errorf("无法写入配置文件: %w", err)
	}

	return nil
}

// 飞牛系统环境检测
func isFeiniuSystem() bool {
	// 检测是否是飞牛系统环境
	hostname, err := os.Hostname()
	if err == nil && strings.Contains(strings.ToLower(hostname), "feiniu") {
		return true
	}

	// 检查特定路径或环境变量
	if _, err := os.Stat("/feiniu"); err == nil {
		return true
	}

	return false
}

// 获取可执行文件目录
func getExecDir() string {
	if isFeiniuSystem() {
		// 飞牛系统路径处理
		return "/opt/feiniu/go-download-server"
	}

	// 其他系统使用当前工作目录
	execPath, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(execPath)
}

var (
	startTime time.Time
)

// 用户会话管理
type Session struct {
	Username    string
	Role        UserRole
	LoginTime   time.Time
	MaxFileSize int64
}

var (
	sessions   = make(map[string]*Session)
	sessionMux sync.Mutex
)

// 辅助函数：用户认证
func authenticateUser(username, password string) (UserRole, bool) {
	// 检查配置文件中的用户
	if userConfig, exists := userConfigMap[username]; exists {
		if userConfig.Password == password {
			// 根据角色返回对应的UserRole
			switch userConfig.Role {
			case "admin":
				return RoleAdmin, true
			case "normal":
				return RoleNormal, true
			case "test":
				return RoleTest, true
			default:
				return RoleTest, true
			}
		}
		return RoleTest, false
	}
	return RoleTest, false
}

// 辅助函数：获取当前用户会话
func getCurrentUser(r *http.Request) *Session {
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
	return fmt.Sprintf("%d_%s", time.Now().UnixNano(), strconv.Itoa(rand.Intn(10000)))
}

// 辅助函数：设置会话
func setSession(w http.ResponseWriter, username string, role UserRole) {
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
	maxFileSize := MaxFileSizeTest
	if userConfig, exists := userConfigMap[username]; exists {
		maxFileSize = userConfig.MaxFileSize
	} else {
		// 如果配置文件中没有，使用默认值
		switch role {
		case RoleAdmin:
			maxFileSize = MaxFileSizeUnlimited
		case RoleNormal:
			maxFileSize = MaxFileSizeNormal
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
func clearSession(w http.ResponseWriter, r *http.Request) {
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

// 辅助函数：格式化文件大小
func formatFileSize(size int64) string {
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
func getEmptyMessage() string {
	return `<div class="empty-message">
		<div class="empty-icon">📁</div>
		<p>该目录为空</p>
		<p>点击"上传文件"添加内容</p>
	</div>`
}

// 辅助函数：清理文件名
func sanitizeFilename(filename string) string {
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

// 主页处理函数
func indexHandler(w http.ResponseWriter, r *http.Request) {
	// 重定向到文件列表页面
	http.Redirect(w, r, "/files", http.StatusFound)
}

// 文件列表处理函数
func filesHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 获取当前路径
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "."
	}

	// URL解码路径
	var err error
	path, err = url.QueryUnescape(path)
	if err != nil {
		path = "."
	}

	// 安全检查：防止路径遍历
	path = filepath.Clean(path)
	if strings.HasPrefix(path, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// 构建完整路径
	fullPath := filepath.Join(downloadDir, path)

	// 获取文件列表
	files, err := os.ReadDir(fullPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("无法读取目录: %v", err), http.StatusInternalServerError)
		return
	}

	// 构建HTML页面
	html := `<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>文件列表 - ` + serverName + `</title>
	<style>
		body {
			font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
			background-color: #f5f5f5;
			margin: 0;
			padding: 0;
		}
		.container {
			max-width: 1200px;
			margin: 0 auto;
			padding: 20px;
		}
		header {
			background-color: #4CAF50;
			color: white;
			padding: 15px;
			border-radius: 5px;
			margin-bottom: 20px;
		}
		.header-content {
			display: flex;
			justify-content: space-between;
			align-items: center;
		}
		nav {
			background-color: white;
			padding: 15px;
			border-radius: 5px;
			margin-bottom: 20px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.nav-links {
			display: flex;
			gap: 15px;
		}
		.nav-links a {
			text-decoration: none;
			color: #333;
			padding: 8px 12px;
			border-radius: 3px;
			transition: background-color 0.3s;
		}
		.nav-links a:hover {
			background-color: #e0e0e0;
		}
		.file-list {
			background-color: white;
			padding: 20px;
			border-radius: 5px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.file-item {
			display: flex;
			align-items: center;
			padding: 10px;
			border-bottom: 1px solid #eee;
			transition: background-color 0.3s;
		}
		.file-item:hover {
			background-color: #f9f9f9;
		}
		.file-icon {
			font-size: 24px;
			margin-right: 15px;
			width: 30px;
			text-align: center;
		}
		.file-info {
			flex-grow: 1;
		}
		.file-name {
			font-weight: bold;
			margin-bottom: 3px;
		}
		.file-meta {
			font-size: 12px;
			color: #666;
		}
		.file-actions {
			display: flex;
			gap: 10px;
		}
		.btn {
			padding: 5px 10px;
			border: none;
			border-radius: 3px;
			cursor: pointer;
			text-decoration: none;
			font-size: 14px;
			transition: background-color 0.3s;
		}
		.btn-primary {
			background-color: #4CAF50;
			color: white;
		}
		.btn-primary:hover {
			background-color: #45a049;
		}
		.btn-secondary {
			background-color: #2196F3;
			color: white;
		}
		.btn-secondary:hover {
			background-color: #0b7dda;
		}
		.btn-danger {
			background-color: #f44336;
			color: white;
		}
		.btn-danger:hover {
			background-color: #da190b;
		}
		.empty-message {
			text-align: center;
			padding: 60px 20px;
			color: #666;
		}
		.empty-icon {
			font-size: 64px;
			margin-bottom: 20px;
		}
		.path-bar {
			background-color: #f5f5f5;
			padding: 10px;
			border-radius: 3px;
			margin-bottom: 20px;
			font-size: 14px;
		}
		.path-item {
			display: inline-block;
			margin-right: 5px;
		}
		.path-separator {
			color: #999;
			margin-right: 5px;
		}
		.pagination {
			margin-top: 20px;
			text-align: center;
		}
		.page-link {
			display: inline-block;
			padding: 5px 10px;
			margin: 0 2px;
			border: 1px solid #ddd;
			border-radius: 3px;
			text-decoration: none;
			color: #333;
			transition: background-color 0.3s;
		}
		.page-link:hover {
			background-color: #e0e0e0;
		}
		.page-link.active {
			background-color: #4CAF50;
			color: white;
			border-color: #4CAF50;
		}
		footer {
			margin-top: 20px;
			text-align: center;
			color: #666;
			font-size: 12px;
			padding: 10px;
			border-top: 1px solid #eee;
		}
	</style>
</head>
<body>
	<div class="container">
		<header>
			<div class="header-content">
				<h1>📦 ` + serverName + `</h1>
				<div>
					` + getCurrentUserInfo(r) + `
				</div>
			</div>
		</header>

		<nav>
			<div class="nav-links">
				<a href="/files">文件列表</a>
				<a href="/upload">上传文件</a>
				` + getAdminLinks(r) + `
			</div>
		</nav>

		<!-- 显示消息 -->
		` + getMessage(r) + `

		<div class="file-list">
			<!-- 路径导航 -->
			<div class="path-bar">
				<div class="path-item">
					<a href="/files?path=." class="path-link">📁 根目录</a>
				</div>
				` + generatePathNavigation(path) + `
			</div>

			<!-- 文件列表 -->
			` + generateFileList(r, files, path) + `
		</div>
	
		<footer>
			<p>版本: ` + version + ` | 开发者: ` + developer + `</p>
		</footer>
	</div>
</body>
</html>`

	w.Write([]byte(html))
}

// 辅助函数：获取目录列表
func getDirectoryList(baseDir string) []string {
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
		log.Printf("获取目录列表失败: %v", err)
		return []string{}
	}

	// 添加根目录选项
	directories = append([]string{"."}, directories...)
	return directories
}

// 辅助函数：生成路径导航
func generatePathNavigation(path string) string {
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
		navigation += fmt.Sprintf(`<span class="path-separator">›</span>
				<div class="path-item">
					<a href="/files?path=%s" class="path-link">%s</a>
				</div>`, url.QueryEscape(currentPath), part)
	}

	return navigation
}

// 辅助函数：生成文件列表
func generateFileList(r *http.Request, files []os.DirEntry, currentPath string) string {
	if len(files) == 0 {
		return getEmptyMessage()
	}

	var fileList string

	// 获取当前用户
	session := getCurrentUser(r)

	// 先添加返回上一级目录的选项（如果不是根目录）
	if currentPath != "." {
		parentPath := filepath.Dir(currentPath)
		if parentPath == "." {
			parentPath = ""
		}
		fileList += fmt.Sprintf(`<div class="file-item">
			<div class="file-icon">📁</div>
			<div class="file-info">
				<div class="file-name"><a href="/files?path=%s">..</a></div>
				<div class="file-meta">返回上一级</div>
			</div>
		</div>`, url.QueryEscape(parentPath))
	}

	// 添加文件和目录
	for _, file := range files {
		name := file.Name()
		filePath := filepath.Join(currentPath, name)
		fileURL := url.QueryEscape(filePath)

		// 获取文件信息
		info, err := file.Info()
		if err != nil {
			continue
		}

		// 生成文件图标
		var icon string
		if file.IsDir() {
			icon = "📁"
		} else {
			icon = "📄"
		}

		// 生成文件元信息
		var meta string
		if file.IsDir() {
			meta = "目录 • " + info.ModTime().Format("2006-01-02 15:04:05")
		} else {
			meta = fmt.Sprintf("文件 • %s • %s", formatFileSize(info.Size()), info.ModTime().Format("2006-01-02 15:04:05"))
		}

		// 生成文件项
		var item string
		if file.IsDir() {
			item = fmt.Sprintf(`<div class="file-item">
				<div class="file-icon">%s</div>
				<div class="file-info">
					<div class="file-name"><a href="/files?path=%s">%s</a></div>
					<div class="file-meta">%s</div>
				</div>
				<div class="file-actions">
					%s
				</div>
			</div>`, icon, fileURL, name, meta, getAdminActions(r, filePath))
		} else {
			// 检查文件是否在待审核目录中
			pendingFilePath := filepath.Join(currentPath, name)
			pendingFullPath := filepath.Join(pendingDir, pendingFilePath)
			_, pendingErr := os.Stat(pendingFullPath)
			isPending := pendingErr == nil

			// 如果是待审核文件，添加待审核状态
			if isPending {
				meta += " • <span style=\"color: orange;\">待审核</span>"
			}

			item = fmt.Sprintf(`<div class="file-item">
				<div class="file-icon">%s</div>
				<div class="file-info">
					<div class="file-name">%s</div>
					<div class="file-meta">%s</div>
				</div>
				<div class="file-actions">
					<a href="/download?path=%s" class="btn btn-secondary">下载</a>
					%s
				</div>
			</div>`, icon, name, meta, fileURL, getAdminActions(r, filePath))
		}

		fileList += item
	}

	// 如果不是管理员，添加待审核文件列表
	if session != nil && session.Role != RoleAdmin {
		pendingFullPath := filepath.Join(pendingDir, currentPath)
		log.Printf("DEBUG: 检查待审核文件路径: %s", pendingFullPath)
		pendingFiles, err := os.ReadDir(pendingFullPath)
		if err != nil {
			log.Printf("DEBUG: 读取待审核目录失败: %v", err)
		} else if len(pendingFiles) > 0 {
			log.Printf("DEBUG: 找到待审核文件数量: %d", len(pendingFiles))
			for _, file := range pendingFiles {
				log.Printf("DEBUG: 待审核文件/目录: %s, 是否为目录: %t", file.Name(), file.IsDir())
				// 只处理待审核文件，不处理待审核目录
				if file.IsDir() {
					continue
				}

				name := file.Name()

				// 获取文件信息
				info, err := file.Info()
				if err != nil {
					log.Printf("DEBUG: 获取文件信息失败: %v", err)
					continue
				}

				// 生成文件图标
				icon := "📄"

				// 生成文件元信息
				meta := fmt.Sprintf("文件 • %s • %s • <span style=\"color: orange;\">待审核</span>", formatFileSize(info.Size()), info.ModTime().Format("2006-01-02 15:04:05"))

				// 生成文件项
				item := fmt.Sprintf(`<div class="file-item">
					<div class="file-icon">%s</div>
					<div class="file-info">
						<div class="file-name">%s</div>
						<div class="file-meta">%s</div>
					</div>
					<div class="file-actions">
						<span class="btn btn-secondary" disabled>待审核</span>
					</div>
				</div>`, icon, name, meta)

				fileList += item
				log.Printf("DEBUG: 添加待审核文件到列表: %s", name)
			}
		} else {
			log.Printf("DEBUG: 待审核目录为空")
		}
	}

	return fileList
}

// 辅助函数：获取当前用户信息
func getCurrentUserInfo(r *http.Request) string {
	session := getCurrentUser(r)
	if session != nil {
		return fmt.Sprintf(`
					<span class="user-info">
						欢迎, %s (角色: %s) • 
						<a href="/logout" class="btn btn-secondary">退出登录</a>
					</span>`, session.Username, getRoleName(session.Role))
	} else {
		return `<a href="/login" class="btn btn-primary">登录</a>`
	}
}

// 辅助函数：获取角色名称
func getRoleName(role UserRole) string {
	switch role {
	case RoleAdmin:
		return "管理员"
	case RoleNormal:
		return "普通用户"
	case RoleTest:
		return "测试用户"
	default:
		return "未知角色"
	}
}

// 辅助函数：根据字符串获取角色名称
func getRoleNameByString(roleStr string) string {
	var role UserRole
	switch roleStr {
	case "test":
		role = RoleTest
	case "normal":
		role = RoleNormal
	case "admin":
		role = RoleAdmin
	default:
		role = RoleTest
	}
	return getRoleName(role)
}

// 辅助函数：获取管理员链接
func getAdminLinks(r *http.Request) string {
	session := getCurrentUser(r)
	if session != nil && session.Role == RoleAdmin {
		return `<a href="/admin">管理员</a>`
	}
	return ""
}

// 辅助函数：获取管理员操作按钮
func getAdminActions(r *http.Request, path string) string {
	session := getCurrentUser(r)
	if session != nil && session.Role == RoleAdmin {
		return fmt.Sprintf(`<a href="/delete?path=%s" class="btn btn-danger" onclick="return confirm('确定要删除吗？')">删除</a>`, url.QueryEscape(path))
	}
	return ""
}

// 上传文件处理函数
func uploadHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 检查用户权限
	session := getCurrentUser(r)
	if session == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	var err error

	// GET请求：显示上传表单
	if r.Method == "GET" {
		// 获取上传路径
		path := r.URL.Query().Get("path")
		if path == "" {
			path = "."
		}

		// URL解码路径
		path, err = url.QueryUnescape(path)
		if err != nil {
			path = "."
		}

		// 安全检查
		path = filepath.Clean(path)
		if strings.HasPrefix(path, "..") {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}

		// 获取目录列表
		var dirList []string
		// 所有用户都应该看到下载目录的结构
		dirList = getDirectoryList(downloadDir)

		// 构建目录选择下拉框
		dirSelectHTML := `<select id="directory" name="directory" class="form-control">`
		for _, dir := range dirList {
			selected := ""
			if dir == path {
				selected = " selected"
			}
			dirSelectHTML += fmt.Sprintf(`<option value="%s"%s>%s</option>`, url.QueryEscape(dir), selected, dir)
		}
		dirSelectHTML += `</select>`

		// 构建HTML页面
		html := `<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>上传文件 - ` + serverName + `</title>
	<style>
		body {
			font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
			background-color: #f5f5f5;
			margin: 0;
			padding: 0;
		}
		.container {
			max-width: 1200px;
			margin: 0 auto;
			padding: 20px;
		}
		header {
			background-color: #4CAF50;
			color: white;
			padding: 15px;
			border-radius: 5px;
			margin-bottom: 20px;
		}
		.header-content {
			display: flex;
			justify-content: space-between;
			align-items: center;
		}
		nav {
			background-color: white;
			padding: 15px;
			border-radius: 5px;
			margin-bottom: 20px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.nav-links {
			display: flex;
			gap: 15px;
		}
		.nav-links a {
			text-decoration: none;
			color: #333;
			padding: 8px 12px;
			border-radius: 3px;
			transition: background-color 0.3s;
		}
		.nav-links a:hover {
			background-color: #e0e0e0;
		}
		.upload-form {
			background-color: white;
			padding: 30px;
			border-radius: 5px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.form-group {
			margin-bottom: 20px;
		}
		label {
			display: block;
			margin-bottom: 5px;
			font-weight: bold;
		}
		input[type="file"] {
			display: block;
			margin-bottom: 10px;
			padding: 10px;
			border: 2px dashed #ddd;
			border-radius: 5px;
			width: 100%;
			background-color: #f9f9f9;
		}
		input[type="file"]:hover {
			border-color: #4CAF50;
		}
		select.form-control {
			display: block;
			width: 100%;
			padding: 10px;
			border: 1px solid #ddd;
			border-radius: 5px;
			font-size: 16px;
		}
		.btn {
			padding: 10px 20px;
			border: none;
			border-radius: 3px;
			cursor: pointer;
			text-decoration: none;
			font-size: 16px;
			transition: background-color 0.3s;
		}
		.btn-primary {
			background-color: #4CAF50;
			color: white;
		}
		.btn-primary:hover {
			background-color: #45a049;
		}
		.message {
			padding: 10px;
			border-radius: 3px;
			margin-bottom: 20px;
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
		.user-info {
			font-size: 14px;
			color: #666;
		}
		.max-size-info {
			font-size: 14px;
			color: #666;
			margin-top: 10px;
		}
		footer {
			margin-top: 20px;
			text-align: center;
			color: #666;
			font-size: 12px;
			padding: 10px;
			border-top: 1px solid #eee;
		}
	</style>
</head>
<body>
	<div class="container">
		<header>
			<div class="header-content">
				<h1>📦 ` + serverName + `</h1>
				<div>
					` + getCurrentUserInfo(r) + `
				</div>
			</div>
		</header>

		<nav>
			<div class="nav-links">
				<a href="/files">文件列表</a>
				<a href="/upload">上传文件</a>
				` + getAdminLinks(r) + `
			</div>
		</nav>

		<div class="upload-form">
			<h2>上传文件</h2>

			<!-- 显示消息 -->
			` + getMessage(r) + `

			<!-- 上传表单 -->
			<form method="POST" enctype="multipart/form-data">
				<div class="form-group">
					<label for="directory">选择目录</label>
					` + dirSelectHTML + `
				</div>

				<div class="form-group">
					<label for="file">选择文件</label>
					<input type="file" id="file" name="file" required>
					<div class="max-size-info">
						最大文件大小: ` + getMaxFileSizeText(session) + `
					</div>
				</div>

				<div class="form-group">
					<button type="submit" class="btn btn-primary">开始上传</button>
					<a href="/files?path=` + path + `" class="btn btn-secondary">返回</a>
				</div>
			</form>
		</div>
	
		<footer>
			<p>版本: ` + version + ` | 开发者: ` + developer + `</p>
		</footer>
	</div>
</body>
</html>`

		w.Write([]byte(html))
		return
	}

	// POST请求：处理文件上传
	if r.Method == "POST" {
		// 解析表单
		err = r.ParseMultipartForm(10 * 1024 * 1024) // 限制表单大小为10MB
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/upload?msg=%s&type=error", "表单解析失败"), http.StatusFound)
			return
		}

		// 获取选择的目录
		path := r.FormValue("directory")
		if path == "" {
			path = "."
		}

		// URL解码目录名（因为下拉框中的值是URL编码的）
		path, err = url.QueryUnescape(path)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/upload?msg=%s&type=error", "目录名解析失败"), http.StatusFound)
			return
		}

		// 安全检查
		path = filepath.Clean(path)
		if strings.HasPrefix(path, "..") {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}

		// 获取文件
		file, handler, err := r.FormFile("file")
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/upload?msg=%s&type=error", "文件获取失败"), http.StatusFound)
			return
		}
		defer file.Close()

		// 检查文件大小
		if session.MaxFileSize > 0 && handler.Size > session.MaxFileSize {
			http.Redirect(w, r, fmt.Sprintf("/upload?msg=%s&type=error", fmt.Sprintf("文件大小超过限制 (%s)", formatFileSize(session.MaxFileSize))), http.StatusFound)
			return
		}

		// 清理文件名
		filename := sanitizeFilename(handler.Filename)

		// 根据用户角色决定保存目录
		var targetDir string
		var successMsg string

		if session.Role == RoleAdmin {
			// 管理员直接保存到下载目录
			targetDir = downloadDir
			successMsg = fmt.Sprintf("文件 '%s' 上传成功", filename)
		} else {
			// 测试用户和普通用户保存到待审核目录
			targetDir = pendingDir
			successMsg = fmt.Sprintf("文件 '%s' 上传成功，等待管理员审核", filename)
		}

		// 构建保存路径
		savePath := filepath.Join(targetDir, path, filename)

		// 创建目标目录
		err = os.MkdirAll(filepath.Dir(savePath), 0755)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/upload?msg=%s&type=error", "创建目录失败"), http.StatusFound)
			return
		}

		// 创建目标文件
		dst, err := os.Create(savePath)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/upload?msg=%s&type=error", "创建文件失败"), http.StatusFound)
			return
		}
		defer dst.Close()

		// 复制文件内容
		_, err = io.Copy(dst, file)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/upload?msg=%s&type=error", "文件保存失败"), http.StatusFound)
			return
		}

		// 记录日志
		log.Printf("%s 上传了文件: %s，状态: %s", session.Username, savePath, successMsg)

		// 重定向回文件列表页面并显示成功消息
		http.Redirect(w, r, fmt.Sprintf("/files?path=%s&msg=%s&type=success", url.QueryEscape(path), url.QueryEscape(successMsg)), http.StatusFound)
	}
}

// 辅助函数：获取最大文件大小文本
func getMaxFileSizeText(session *Session) string {
	if session.MaxFileSize == MaxFileSizeUnlimited {
		return "无限制"
	}
	return formatFileSize(session.MaxFileSize)
}

// 辅助函数：获取消息
func getMessage(r *http.Request) string {
	msg := r.URL.Query().Get("msg")
	msgType := r.URL.Query().Get("type")

	if msg == "" {
		return ""
	}

	class := "message-success"
	if msgType == "error" {
		class = "message-error"
	}

	return fmt.Sprintf(`<div class="message %s">%s</div>`, class, msg)
}

// 下载文件处理函数
func downloadHandler(w http.ResponseWriter, r *http.Request) {
	// 获取文件路径
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "Missing path parameter", http.StatusBadRequest)
		return
	}

	// 安全检查：防止路径遍历
	path = filepath.Clean(path)
	if strings.HasPrefix(path, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// 构建完整路径
	fullPath := filepath.Join(downloadDir, path)

	// 检查文件是否存在
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("文件不存在: %v", err), http.StatusNotFound)
		return
	}

	// 检查是否是目录
	if fileInfo.IsDir() {
		http.Error(w, "Cannot download directory", http.StatusBadRequest)
		return
	}

	// 打开文件
	file, err := os.Open(fullPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("无法打开文件: %v", err), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// 设置响应头
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", url.QueryEscape(filepath.Base(fullPath))))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(fileInfo.Size(), 10))

	// 发送文件
	io.Copy(w, file)

	// 记录日志
	log.Printf("下载文件: %s", fullPath)
}

// 删除文件处理函数
func deleteHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	session := getCurrentUser(r)
	if session == nil || session.Role != RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 获取文件路径
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "Missing path parameter", http.StatusBadRequest)
		return
	}

	// 安全检查：防止路径遍历
	path = filepath.Clean(path)
	if strings.HasPrefix(path, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// 构建完整路径
	fullPath := filepath.Join(downloadDir, path)

	// 检查文件是否存在
	_, err := os.Stat(fullPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("文件不存在: %v", err), http.StatusNotFound)
		return
	}

	// 删除文件或目录
	err = os.RemoveAll(fullPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("删除失败: %v", err), http.StatusInternalServerError)
		return
	}

	// 记录日志
	log.Printf("管理员删除了: %s", fullPath)

	// 重定向回文件列表页面
	parentPath := filepath.Dir(path)
	if parentPath == "." {
		parentPath = ""
	}
	http.Redirect(w, r, fmt.Sprintf("/files?path=%s&msg=%s&type=success", url.QueryEscape(parentPath), url.QueryEscape("删除成功")), http.StatusFound)
}

// 登录处理函数
func loginHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// GET请求：显示登录表单
	if r.Method == "GET" {
		html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>登录 - ` + serverName + `</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        
        body {
            font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            display: flex;
            justify-content: center;
            align-items: center;
        }
        
        .login-container {
            background-color: white;
            border-radius: 10px;
            box-shadow: 0 10px 30px rgba(0, 0, 0, 0.3);
            padding: 40px;
            width: 100%;
            max-width: 400px;
        }
        
        h1 {
            text-align: center;
            color: #333;
            margin-bottom: 30px;
            font-size: 24px;
        }
        
        .logo {
            font-size: 48px;
            text-align: center;
            margin-bottom: 20px;
        }
        
        .form-group {
            margin-bottom: 20px;
        }
        
        label {
            display: block;
            margin-bottom: 5px;
            color: #555;
            font-weight: 500;
        }
        
        input[type="text"],
        input[type="password"] {
            width: 100%;
            padding: 12px;
            border: 1px solid #ddd;
            border-radius: 5px;
            font-size: 16px;
            transition: border-color 0.3s;
        }
        
        input[type="text"]:focus,
        input[type="password"]:focus {
            border-color: #667eea;
            outline: none;
            box-shadow: 0 0 0 2px rgba(102, 126, 234, 0.1);
        }
        
        .btn {
            width: 100%;
            padding: 12px;
            background-color: #667eea;
            color: white;
            border: none;
            border-radius: 5px;
            font-size: 16px;
            cursor: pointer;
            transition: background-color 0.3s;
        }
        
        .btn:hover {
            background-color: #5568d3;
        }
        
        .message {
            padding: 12px;
            border-radius: 5px;
            margin-bottom: 20px;
            text-align: center;
        }
        
        .message-error {
            background-color: #f8d7da;
            color: #721c24;
            border: 1px solid #f5c6cb;
        }
        
        .version-info {
            margin-top: 20px;
            text-align: center;
            color: #666;
            font-size: 12px;
            padding-top: 20px;
            border-top: 1px solid #eee;
        }

    </style>
</head>
<body>
    <div class="login-container">
        <div class="logo">📦</div>
        <h1>登录到 ` + serverName + `</h1>
        
        <!-- 显示错误消息 -->
        ` + getMessage(r) + `
        
        <!-- 登录表单 -->
        <form method="POST">
            <div class="form-group">
                <label for="username">用户名</label>
                <input type="text" id="username" name="username" placeholder="请输入用户名" required>
            </div>
            
            <div class="form-group">
                <label for="password">密码</label>
                <input type="password" id="password" name="password" placeholder="请输入密码" required>
            </div>
            
            <div class="form-group">
                <button type="submit" class="btn">登录</button>
            </div>
        </form>
        
        <!-- 版本信息 -->
        <div class="version-info">
            <p>版本: ` + version + ` | 开发者: ` + developer + `</p>
        </div>

    </div>
</body>
</html>`

		w.Write([]byte(html))
		return
	}

	// POST请求：处理登录逻辑
	if r.Method == "POST" {
		// 解析表单
		err := r.ParseForm()
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/login?msg=%s&type=error", "表单解析失败"), http.StatusFound)
			return
		}

		// 获取用户名和密码
		username := r.FormValue("username")
		password := r.FormValue("password")

		// 验证用户
		role, ok := authenticateUser(username, password)
		if !ok {
			http.Redirect(w, r, fmt.Sprintf("/login?msg=%s&type=error", "用户名或密码错误"), http.StatusFound)
			return
		}

		// 设置会话
		setSession(w, username, role)

		// 记录日志
		log.Printf("用户 %s 登录成功", username)

		// 重定向到主页
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

// 登出处理函数
func logoutHandler(w http.ResponseWriter, r *http.Request) {
	// 清除会话
	clearSession(w, r)

	// 记录日志
	log.Printf("用户退出登录")

	// 重定向到登录页面
	http.Redirect(w, r, "/login", http.StatusFound)
}

// 管理员页面处理函数
func adminHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	session := getCurrentUser(r)
	if session == nil || session.Role != RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 构建HTML页面
	html := `<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>管理员 - ` + serverName + `</title>
	<style>
		body {
			font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
			background-color: #f5f5f5;
			margin: 0;
			padding: 0;
		}
		.container {
			max-width: 1200px;
			margin: 0 auto;
			padding: 20px;
		}
		header {
			background-color: #4CAF50;
			color: white;
			padding: 15px;
			border-radius: 5px;
			margin-bottom: 20px;
		}
		.header-content {
			display: flex;
			justify-content: space-between;
			align-items: center;
		}
		nav {
			background-color: white;
			padding: 15px;
			border-radius: 5px;
			margin-bottom: 20px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.nav-links {
			display: flex;
			gap: 15px;
		}
		.nav-links a {
			text-decoration: none;
			color: #333;
			padding: 8px 12px;
			border-radius: 3px;
			transition: background-color 0.3s;
		}
		.nav-links a:hover {
			background-color: #e0e0e0;
		}
		.admin-panel {
			background-color: white;
			padding: 30px;
			border-radius: 5px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.admin-options {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
			gap: 20px;
			margin-top: 20px;
		}
		.admin-option {
			padding: 20px;
			background-color: #f9f9f9;
			border-radius: 5px;
			text-align: center;
			border: 1px solid #eee;
			transition: transform 0.3s, box-shadow 0.3s;
		}
		.admin-option:hover {
			transform: translateY(-5px);
			box-shadow: 0 5px 15px rgba(0,0,0,0.1);
		}
		.admin-option-icon {
			font-size: 48px;
			margin-bottom: 10px;
		}
		.admin-option-title {
			font-size: 18px;
			font-weight: bold;
			margin-bottom: 5px;
		}
		.admin-option-description {
			font-size: 14px;
			color: #666;
			margin-bottom: 15px;
		}
		.btn {
			padding: 8px 16px;
			border: none;
			border-radius: 3px;
			cursor: pointer;
			text-decoration: none;
			font-size: 14px;
			transition: background-color 0.3s;
		}
		.btn-primary {
			background-color: #4CAF50;
			color: white;
		}
		.btn-primary:hover {
			background-color: #45a049;
		}
		.stats {
			background-color: #f9f9f9;
			padding: 20px;
			border-radius: 5px;
			margin-bottom: 20px;
		}
		.stat-item {
			display: inline-block;
			margin-right: 30px;
			margin-bottom: 10px;
		}
		.stat-label {
			font-size: 14px;
			color: #666;
		}
		.stat-value {
			font-size: 24px;
			font-weight: bold;
			color: #333;
		}
	</style>
</head>
<body>
	<div class="container">
		<header>
			<div class="header-content">
				<h1>📦 ` + serverName + `</h1>
				<div>
					` + getCurrentUserInfo(r) + `
				</div>
			</div>
		</header>

		<nav>
			<div class="nav-links">
				<a href="/files">文件列表</a>
				<a href="/upload">上传文件</a>
				<a href="/admin">管理员</a>
			</div>
		</nav>

		<div class="admin-panel">
			<h2>管理员控制面板</h2>
			
			<!-- 服务器统计信息 -->
			<div class="stats">
				<h3>服务器统计</h3>
				<div class="stat-item">
					<div class="stat-label">当前时间</div>
					<div class="stat-value">` + time.Now().Format("2006-01-02 15:04:05") + `</div>
				</div>
				<div class="stat-item">
					<div class="stat-label">运行时间</div>
					<div class="stat-value">` + formatDuration(time.Since(startTime)) + `</div>
				</div>
			</div>

			<!-- 管理员选项 -->
			<div class="admin-options">
				<!-- 文件审核 -->
				<div class="admin-option">
					<div class="admin-option-icon">✅</div>
					<div class="admin-option-title">文件审核</div>
					<div class="admin-option-description">审核用户上传的文件</div>
					<a href="/review" class="btn btn-primary">审核文件</a>
				</div>

				<!-- 创建目录 -->
				<div class="admin-option">
					<div class="admin-option-icon">📁</div>
					<div class="admin-option-title">创建目录</div>
					<div class="admin-option-description">在服务器上创建新目录</div>
					<a href="/mkdir" class="btn btn-primary">创建目录</a>
				</div>

				<!-- 用户管理 -->
				<div class="admin-option">
					<div class="admin-option-icon">👤</div>
					<div class="admin-option-title">用户管理</div>
					<div class="admin-option-description">管理用户账号和密码</div>
					<a href="/user-management" class="btn btn-primary">用户管理</a>
				</div>

				<!-- 查看日志 -->
				<div class="admin-option">
					<div class="admin-option-icon">📝</div>
					<div class="admin-option-title">查看日志</div>
					<div class="admin-option-description">查看服务器日志</div>
					<a href="/logs" class="btn btn-primary">查看日志</a>
				</div>

				<!-- 服务器信息 -->
				<div class="admin-option">
					<div class="admin-option-icon">ℹ️</div>
					<div class="admin-option-title">服务器信息</div>
					<div class="admin-option-description">查看服务器详细信息</div>
					<a href="/info" class="btn btn-primary">查看信息</a>
				</div>
			</div>
		</div>
	</div>
</body>
</html>`

	w.Write([]byte(html))
}

// 用户管理处理函数
func userManagementHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	session := getCurrentUser(r)
	if session == nil || session.Role != RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 构建用户列表表格HTML
	usersHTML := `<table class="user-table">
		<thead>
			<tr>
				<th>用户名</th>
				<th>角色</th>
				<th>最大文件大小</th>
				<th>操作</th>
			</tr>
		</thead>
		<tbody>`
	for _, user := range config.Users {
		deleteButton := ""
		if user.Username != "admin" {
			deleteButton = `<form action="/delete-user" method="POST" style="display: inline;">
							<input type="hidden" name="username" value="` + user.Username + `">
							<button type="submit" class="btn btn-danger" onclick="return confirm('确定要删除用户 ` + user.Username + ` 吗？')">删除</button>
						</form>`
		}

		usersHTML += `<tr>
			<td>` + user.Username + `</td>
			<td>` + getRoleNameByString(user.Role) + `</td>
			<td>` + formatFileSize(user.MaxFileSize) + `</td>
			<td>
				<!-- 修改密码和删除按钮 -->
				<div class="form-row password-row">
					<form action="/change-password" method="POST" style="display: inline;">
						<input type="hidden" name="username" value="` + user.Username + `">
						<input type="password" name="new_password" placeholder="新密码" required style="margin-right: 5px;">
						<input type="password" name="confirm_password" placeholder="确认密码" required style="margin-right: 5px;">
						<button type="submit" class="btn btn-primary">修改</button>
					</form>
					` + deleteButton + `
				</div>
			</td>
		</tr>`
	}
	usersHTML += `</tbody>
	</table>`

	// 构建添加用户表单HTML
	addUserHTML := `<div class="add-user-form">
		<h3>添加新用户</h3>
		<form action="/add-user" method="POST">
			<div class="form-row">
				<div class="form-group">
					<label>用户名:</label>
					<input type="text" name="username" required>
				</div>
				<div class="form-group">
					<label>密码:</label>
					<input type="password" name="password" required>
				</div>
				<div class="form-group">
					<label>角色:</label>
					<select name="role">
						<option value="normal">普通用户</option>
						<option value="test">测试用户</option>
					</select>
				</div>
				<div class="form-group">
					<label>最大文件大小 (GB):</label>
					<input type="number" name="max_file_size" min="1" max="100" value="10" required>
				</div>
				<div class="form-group submit-group">
					<button type="submit" class="btn btn-primary">添加用户</button>
				</div>
			</div>
		</form>
	</div>`

	// 构建HTML页面
	html := `<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>用户管理 - ` + serverName + `</title>
	<style>
		body {
			font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
			background-color: #f5f5f5;
			margin: 0;
			padding: 0;
		}
		.container {
			max-width: 1200px;
			margin: 0 auto;
			padding: 20px;
		}
		header {
			background-color: #4CAF50;
			color: white;
			padding: 15px;
			border-radius: 5px;
			margin-bottom: 20px;
		}
		.header-content {
			display: flex;
			justify-content: space-between;
			align-items: center;
		}
		nav {
			background-color: white;
			padding: 15px;
			border-radius: 5px;
			margin-bottom: 20px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.nav-links {
			display: flex;
			gap: 15px;
		}
		.nav-links a {
			text-decoration: none;
			color: #333;
			padding: 8px 12px;
			border-radius: 3px;
			transition: background-color 0.3s;
		}
		.nav-links a:hover {
			background-color: #e0e0e0;
		}
		.admin-panel {
			background-color: white;
			padding: 30px;
			border-radius: 5px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.user-table {
			width: 100%;
			border-collapse: collapse;
			margin: 20px 0;
			background-color: white;
			border: 1px solid #ddd;
			border-radius: 5px;
			overflow: hidden;
		}
		.user-table th, .user-table td {
			padding: 12px 15px;
			text-align: left;
			border-bottom: 1px solid #eee;
		}
		.user-table th {
			background-color: #f8f9fa;
			font-weight: bold;
			color: #333;
		}
		.user-table tr:hover {
			background-color: #f5f5f5;
		}
		.user-table tr:last-child td {
			border-bottom: none;
		}
		.form-group {
			margin-bottom: 15px;
			margin-right: 15px;
		}
		.form-group.submit-group {
			display: flex;
			align-items: flex-end;
			justify-content: center;
		}
		.form-group label {
			display: block;
			margin-bottom: 5px;
			font-weight: bold;
			font-size: 12px;
		}
		.form-group input, .form-group select {
			padding: 8px;
			border: 1px solid #ddd;
			border-radius: 3px;
			width: 150px;
		}
		.form-row {
			display: flex;
			align-items: center;
			gap: 10px;
		}
		.password-form {
			margin-top: 10px;
			margin-bottom: 0;
			display: block;
			clear: both;
		}
		.password-row {
			align-items: center;
			gap: 5px;
		}
		.password-row input {
			width: 100px;
		}
		.btn {
			padding: 8px 16px;
			border: none;
			border-radius: 3px;
			cursor: pointer;
			text-decoration: none;
			font-size: 14px;
			transition: background-color 0.3s;
		}
		.add-user-form button {
			height: 34px;
			vertical-align: middle;
		}
		.btn-primary {
			background-color: #4CAF50;
			color: white;
		}
		.btn-primary:hover {
			background-color: #45a049;
		}
		.btn-danger {
			background-color: #f44336;
			color: white;
		}
		.btn-danger:hover {
			background-color: #da190b;
		}
		.back-link {
			display: inline-block;
			margin-bottom: 20px;
			padding: 8px 16px;
			background-color: #e0e0e0;
			color: #333;
			border-radius: 3px;
			text-decoration: none;
		}
		.back-link:hover {
			background-color: #d0d0d0;
		}
		.add-user-form {
			background-color: #f8f9fa;
			padding: 20px;
			border-radius: 5px;
			margin-bottom: 20px;
			border: 1px solid #eee;
		}
		.add-user-form h3 {
			margin-top: 0;
			margin-bottom: 15px;
		}
	</style>
</head>
<body>
	<div class="container">
		<header>
			<div class="header-content">
				<h1>📦 ` + serverName + `</h1>
				<div>
					` + getCurrentUserInfo(r) + `
				</div>
			</div>
		</header>

		<nav>
			<div class="nav-links">
				<a href="/files">文件列表</a>
				<a href="/upload">上传文件</a>
				<a href="/admin">管理员</a>
			</div>
		</nav>

		<div class="admin-panel">
			<a href="/admin" class="back-link">⬅️ 返回管理员面板</a>
			<h2>用户管理</h2>
			
			<!-- 添加用户表单 -->
			` + addUserHTML + `
			
			<!-- 用户列表 -->
			<h3>用户列表</h3>
			` + usersHTML + `
		</div>
	</div>
</body>
</html>`

	w.Write([]byte(html))
}

// 修改密码处理函数
func changePasswordHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	session := getCurrentUser(r)
	if session == nil || session.Role != RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 处理POST请求
	if r.Method == "POST" {
		// 解析表单数据
		r.ParseForm()
		username := r.FormValue("username")
		newPassword := r.FormValue("new_password")
		confirmPassword := r.FormValue("confirm_password")

		// 验证新密码和确认密码是否一致
		if newPassword != confirmPassword {
			http.Error(w, "新密码和确认密码不一致", http.StatusBadRequest)
			return
		}

		// 查找用户并更新密码
		userFound := false
		for i, user := range config.Users {
			if user.Username == username {
				// 更新密码
				config.Users[i].Password = newPassword
				// 更新用户配置映射
				userConfigMap[username] = config.Users[i]
				userFound = true
				break
			}
		}

		if !userFound {
			http.Error(w, "用户不存在", http.StatusBadRequest)
			return
		}

		// 保存配置文件
		if err := saveConfig(); err != nil {
			http.Error(w, "保存配置文件失败: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// 重定向回用户管理页面
		http.Redirect(w, r, "/user-management", http.StatusFound)
		return
	}

	// 如果不是POST请求，重定向到用户管理页面
	http.Redirect(w, r, "/user-management", http.StatusFound)
}

// 添加用户处理函数
func addUserHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	session := getCurrentUser(r)
	if session == nil || session.Role != RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 处理POST请求
	if r.Method == "POST" {
		// 解析表单数据
		r.ParseForm()
		username := r.FormValue("username")
		password := r.FormValue("password")
		roleStr := r.FormValue("role")
		maxFileSizeStr := r.FormValue("max_file_size")

		// 验证表单数据
		if username == "" || password == "" || roleStr == "" || maxFileSizeStr == "" {
			http.Error(w, "请填写所有必填字段", http.StatusBadRequest)
			return
		}

		// 验证用户名是否已存在
		for _, user := range config.Users {
			if user.Username == username {
				http.Error(w, "用户名已存在", http.StatusBadRequest)
				return
			}
		}

		// 解析最大文件大小
		maxFileSize, err := strconv.Atoi(maxFileSizeStr)
		if err != nil || maxFileSize < 1 || maxFileSize > 100 {
			http.Error(w, "最大文件大小必须是1-100之间的整数", http.StatusBadRequest)
			return
		}

		// 验证角色
		switch roleStr {
		case "normal", "test":
			// 角色有效
		default:
			http.Error(w, "无效的角色", http.StatusBadRequest)
			return
		}

		// 创建新用户
		newUser := UserConfig{
			Username:    username,
			Password:    password,
			Role:        roleStr,
			MaxFileSize: int64(maxFileSize) * 1024 * 1024 * 1024, // 转换为字节
		}

		// 添加到配置
		config.Users = append(config.Users, newUser)
		userConfigMap[username] = newUser

		// 保存配置文件
		if err := saveConfig(); err != nil {
			http.Error(w, "保存配置文件失败: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// 重定向回用户管理页面
		http.Redirect(w, r, "/user-management", http.StatusFound)
		return
	}

	// 如果不是POST请求，重定向到用户管理页面
	http.Redirect(w, r, "/user-management", http.StatusFound)
}

// 删除用户处理函数
func deleteUserHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	session := getCurrentUser(r)
	if session == nil || session.Role != RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 处理POST请求
	if r.Method == "POST" {
		// 解析表单数据
		r.ParseForm()
		username := r.FormValue("username")

		// 验证用户名
		if username == "" {
			http.Error(w, "用户名不能为空", http.StatusBadRequest)
			return
		}

		// 不能删除管理员账户
		if username == "admin" {
			http.Error(w, "不能删除管理员账户", http.StatusBadRequest)
			return
		}

		// 查找并删除用户
		userFound := false
		var newUsers []UserConfig
		for _, user := range config.Users {
			if user.Username == username {
				userFound = true
				// 从用户配置映射中删除
				delete(userConfigMap, username)
			} else {
				newUsers = append(newUsers, user)
			}
		}

		if !userFound {
			http.Error(w, "用户不存在", http.StatusBadRequest)
			return
		}

		// 更新配置
		config.Users = newUsers

		// 保存配置文件
		if err := saveConfig(); err != nil {
			http.Error(w, "保存配置文件失败: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// 重定向回用户管理页面
		http.Redirect(w, r, "/user-management", http.StatusFound)
		return
	}

	// 如果不是POST请求，重定向到用户管理页面
	http.Redirect(w, r, "/user-management", http.StatusFound)
}

// 辅助函数：格式化时间间隔
func formatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if days > 0 {
		return fmt.Sprintf("%d天 %d小时 %d分钟 %d秒", days, hours, minutes, seconds)
	} else if hours > 0 {
		return fmt.Sprintf("%d小时 %d分钟 %d秒", hours, minutes, seconds)
	} else if minutes > 0 {
		return fmt.Sprintf("%d分钟 %d秒", minutes, seconds)
	} else {
		return fmt.Sprintf("%d秒", seconds)
	}
}

// 创建目录处理函数
func mkdirHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	session := getCurrentUser(r)
	if session == nil || session.Role != RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// GET请求：显示创建目录表单
	if r.Method == "GET" {
		// 获取当前路径
		path := r.URL.Query().Get("path")
		if path == "" {
			path = "."
		}

		// 安全检查
		path = filepath.Clean(path)
		if strings.HasPrefix(path, "..") {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}

		// 构建HTML页面
		html := `<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>创建目录 - ` + serverName + `</title>
	<style>
		body {
			font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
			background-color: #f5f5f5;
			margin: 0;
			padding: 0;
		}
		.container {
			max-width: 1200px;
			margin: 0 auto;
			padding: 20px;
		}
		header {
			background-color: #4CAF50;
			color: white;
			padding: 15px;
			border-radius: 5px;
			margin-bottom: 20px;
		}
		.header-content {
			display: flex;
			justify-content: space-between;
			align-items: center;
		}
		nav {
			background-color: white;
			padding: 15px;
			border-radius: 5px;
			margin-bottom: 20px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.nav-links {
			display: flex;
			gap: 15px;
		}
		.nav-links a {
			text-decoration: none;
			color: #333;
			padding: 8px 12px;
			border-radius: 3px;
			transition: background-color 0.3s;
		}
		.nav-links a:hover {
			background-color: #e0e0e0;
		}
		.form-container {
			background-color: white;
			padding: 30px;
			border-radius: 5px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.form-group {
			margin-bottom: 20px;
		}
		label {
			display: block;
			margin-bottom: 5px;
			font-weight: bold;
		}
		input[type="text"],
		select {
			width: 100%;
			padding: 10px;
			border: 1px solid #ddd;
			border-radius: 3px;
			font-size: 16px;
			transition: border-color 0.3s;
		}
		input[type="text"]:focus,
		select:focus {
			border-color: #4CAF50;
			outline: none;
		}
		.btn {
			padding: 10px 20px;
			border: none;
			border-radius: 3px;
			cursor: pointer;
			text-decoration: none;
			font-size: 16px;
			transition: background-color 0.3s;
		}
		.btn-primary {
			background-color: #4CAF50;
			color: white;
		}
		.btn-primary:hover {
			background-color: #45a049;
		}
		.btn-secondary {
			background-color: #f5f5f5;
			color: #333;
			border: 1px solid #ddd;
		}
		.btn-secondary:hover {
			background-color: #e0e0e0;
		}
		.message {
			padding: 10px;
			border-radius: 3px;
			margin-bottom: 20px;
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
	</style>
</head>
<body>
	<div class="container">
		<header>
			<div class="header-content">
				<h1>📦 ` + serverName + `</h1>
				<div>
					` + getCurrentUserInfo(r) + `
				</div>
			</div>
		</header>

		<nav>
			<div class="nav-links">
				<a href="/files">文件列表</a>
				<a href="/upload">上传文件</a>
				<a href="/admin">管理员</a>
			</div>
		</nav>

		<div class="form-container">
			<h2>创建目录</h2>

			<!-- 显示消息 -->
			` + getMessage(r) + `

			<!-- 创建目录表单 -->
			<form method="POST">
				<div class="form-group">
					<label for="dirName">目录名称</label>
					<input type="text" id="dirName" name="dirName" placeholder="请输入目录名称" required>
				</div>

				<div class="form-group">
					<label for="parentDir">父目录</label>
					<select id="parentDir" name="parentDir">
						<option value=".">根目录</option>
						` + generateDirectoryOptions(downloadDir, ".") + `
					</select>
				</div>

				<div class="form-group">
					<button type="submit" class="btn btn-primary">创建目录</button>
					<a href="/files?path=` + path + `" class="btn btn-secondary">返回</a>
				</div>
			</form>
		</div>
	</div>
</body>
</html>`

		w.Write([]byte(html))
		return
	}

	// POST请求：处理目录创建
	if r.Method == "POST" {
		// 解析表单
		err := r.ParseForm()
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/mkdir?msg=%s&type=error", "表单解析失败"), http.StatusFound)
			return
		}

		// 获取表单数据
		dirName := r.FormValue("dirName")
		parentDir := r.FormValue("parentDir")

		// 验证目录名称
		if dirName == "" {
			http.Redirect(w, r, fmt.Sprintf("/mkdir?msg=%s&type=error", "目录名称不能为空"), http.StatusFound)
			return
		}

		// 清理目录名称
		dirName = sanitizeFilename(dirName)

		// 构建完整路径
		var fullPath string
		if parentDir == "." {
			// 创建在根目录
			fullPath = filepath.Join(downloadDir, dirName)
		} else {
			// 创建在指定父目录
			parentDir = filepath.Clean(parentDir)
			if strings.Contains(parentDir, "..") {
				http.Redirect(w, r, fmt.Sprintf("/mkdir?msg=%s&type=error", "无效的父目录"), http.StatusFound)
				return
			}
			fullPath = filepath.Join(downloadDir, parentDir, dirName)
		}

		// 创建目录
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			http.Redirect(w, r, fmt.Sprintf("/mkdir?msg=%s&type=error", fmt.Sprintf("创建目录失败: %v", err)), http.StatusFound)
			return
		}

		// 记录日志
		log.Printf("管理员创建目录: %s", fullPath)

		// 重定向回创建目录页面并显示成功消息
		http.Redirect(w, r, fmt.Sprintf("/mkdir?msg=%s&type=success", fmt.Sprintf("目录 %s 创建成功", dirName)), http.StatusFound)
	}
}

// 辅助函数：生成目录选项
func generateDirectoryOptions(rootDir, currentPath string) string {
	var options string

	// 遍历目录
	filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// 只处理目录
		if !info.IsDir() {
			return nil
		}

		// 跳过根目录
		if path == rootDir {
			return nil
		}

		// 计算相对路径
		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return nil
		}

		// 安全检查：防止路径遍历
		if strings.Contains(relPath, "..") {
			return nil
		}

		// 生成选项
		selected := ""
		if relPath == currentPath {
			selected = "selected"
		}
		options += fmt.Sprintf(`<option value="%s" %s>%s</option>`, url.QueryEscape(relPath), selected, relPath)

		return nil
	})

	return options
}

// 定义一个结构体来保存文件信息和完整路径
type FileWithPath struct {
	Entry    os.DirEntry
	FullPath string
}

// 文件审核处理函数
func reviewHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	session := getCurrentUser(r)
	if session == nil || session.Role != RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 获取当前路径
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "."
	}

	// 安全检查
	path = filepath.Clean(path)
	if strings.HasPrefix(path, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// 构建完整路径
	fullPath := filepath.Join(pendingDir, path)

	// 如果是根目录，递归获取所有待审核文件
	if path == "." {
		// 递归遍历所有目录并收集文件
		var allFilesWithPath []FileWithPath

		// 检查pendingDir是否存在
		if _, err := os.Stat(fullPath); !os.IsNotExist(err) {
			err := filepath.Walk(fullPath, func(walkPath string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}

				// 如果是文件且不是目录，添加到列表
				if !info.IsDir() {
					// 获取相对路径（相对于pendingDir）
					relPath, err := filepath.Rel(pendingDir, walkPath)
					if err != nil {
						return err
					}

					// 创建一个临时的DirEntry对象
					dirEntry, err := os.ReadDir(filepath.Dir(walkPath))
					if err != nil {
						return err
					}

					// 找到对应的文件
					for _, entry := range dirEntry {
						if entry.Name() == info.Name() {
							allFilesWithPath = append(allFilesWithPath, FileWithPath{
								Entry:    entry,
								FullPath: relPath,
							})
							break
						}
					}
				}
				return nil
			})

			if err != nil {
				http.Error(w, fmt.Sprintf("无法读取目录: %v", err), http.StatusInternalServerError)
				return
			}
		}
		// 如果pendingDir不存在，allFilesWithPath将为空，自然显示"没有待审核的文件"

		// 生成HTML页面
		html := `<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>文件审核 - ` + serverName + `</title>
	<style>
		body {
			font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
			background-color: #f5f5f5;
			margin: 0;
			padding: 0;
		}
		.container {
			max-width: 1200px;
			margin: 0 auto;
			padding: 20px;
		}
		header {
			background-color: #4CAF50;
			color: white;
			padding: 15px;
			border-radius: 5px;
			margin-bottom: 20px;
		}
		.header-content {
			display: flex;
			justify-content: space-between;
			align-items: center;
		}
		nav {
			background-color: white;
			padding: 15px;
			border-radius: 5px;
			margin-bottom: 20px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.nav-links {
			display: flex;
			gap: 15px;
		}
		.nav-links a {
			text-decoration: none;
			color: #333;
			padding: 8px 12px;
			border-radius: 3px;
			transition: background-color 0.3s;
		}
		.nav-links a:hover {
			background-color: #e0e0e0;
		}
		.review-list {
			background-color: white;
			padding: 20px;
			border-radius: 5px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.file-item {
			display: flex;
			align-items: center;
			padding: 15px;
			border-bottom: 1px solid #eee;
			transition: background-color 0.3s;
		}
		.file-item:hover {
			background-color: #f9f9f9;
		}
		.file-icon {
			font-size: 24px;
			margin-right: 15px;
			width: 30px;
			text-align: center;
		}
		.file-info {
			flex-grow: 1;
		}
		.file-name {
			font-weight: bold;
			margin-bottom: 3px;
		}
		.file-meta {
			font-size: 12px;
			color: #666;
		}
		.file-actions {
			display: flex;
			gap: 10px;
		}
		.btn {
			padding: 8px 16px;
			border: none;
			border-radius: 3px;
			cursor: pointer;
			text-decoration: none;
			font-size: 14px;
			transition: background-color 0.3s;
		}
		.btn-primary {
			background-color: #4CAF50;
			color: white;
		}
		.btn-primary:hover {
			background-color: #45a049;
		}
		.btn-danger {
			background-color: #f44336;
			color: white;
		}
		.btn-danger:hover {
			background-color: #da190b;
		}
		.empty-message {
			text-align: center;
			padding: 60px 20px;
			color: #666;
		}
		.empty-icon {
			font-size: 64px;
			margin-bottom: 20px;
		}
		.path-bar {
			background-color: #f5f5f5;
			padding: 10px;
			border-radius: 3px;
			margin-bottom: 20px;
			font-size: 14px;
		}
		.path-item {
			display: inline-block;
			margin-right: 5px;
		}
		.path-separator {
			color: #999;
			margin-right: 5px;
		}
		.message {
			padding: 10px;
			border-radius: 3px;
			margin-bottom: 20px;
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
	</style>
</head>
<body>
	<div class="container">
		<header>
			<div class="header-content">
				<h1>📦 ` + serverName + `</h1>
				<div>
					` + getCurrentUserInfo(r) + `
				</div>
			</div>
		</header>

		<nav>
			<div class="nav-links">
				<a href="/files">文件列表</a>
				<a href="/upload">上传文件</a>
				<a href="/admin">管理员</a>
				<a href="/review">文件审核</a>
			</div>
		</nav>

		<!-- 显示消息 -->
		` + getMessage(r) + `

		<div class="review-list">
			<!-- 路径导航 -->
			<div class="path-bar">
				<div class="path-item">
					<a href="/review?path=." class="path-link">📁 待审核文件</a>
				</div>
			</div>

			<!-- 文件列表 -->
			` + generatePendingFileListWithPath(allFilesWithPath) + `
		</div>
	</div>
</body>
</html>`

		w.Write([]byte(html))
		return
	}

	// 普通目录读取
	files, err := os.ReadDir(fullPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("无法读取目录: %v", err), http.StatusInternalServerError)
		return
	}

	// 构建HTML页面
	html := `<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>文件审核 - ` + serverName + `</title>
	<style>
		body {
			font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
			background-color: #f5f5f5;
			margin: 0;
			padding: 0;
		}
		.container {
			max-width: 1200px;
			margin: 0 auto;
			padding: 20px;
		}
		header {
			background-color: #4CAF50;
			color: white;
			padding: 15px;
			border-radius: 5px;
			margin-bottom: 20px;
		}
		.header-content {
			display: flex;
			justify-content: space-between;
			align-items: center;
		}
		nav {
			background-color: white;
			padding: 15px;
			border-radius: 5px;
			margin-bottom: 20px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.nav-links {
			display: flex;
			gap: 15px;
		}
		.nav-links a {
			text-decoration: none;
			color: #333;
			padding: 8px 12px;
			border-radius: 3px;
			transition: background-color 0.3s;
		}
		.nav-links a:hover {
			background-color: #e0e0e0;
		}
		.review-list {
			background-color: white;
			padding: 20px;
			border-radius: 5px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.file-item {
			display: flex;
			align-items: center;
			padding: 15px;
			border-bottom: 1px solid #eee;
			transition: background-color 0.3s;
		}
		.file-item:hover {
			background-color: #f9f9f9;
		}
		.file-icon {
			font-size: 24px;
			margin-right: 15px;
			width: 30px;
			text-align: center;
		}
		.file-info {
			flex-grow: 1;
		}
		.file-name {
			font-weight: bold;
			margin-bottom: 3px;
		}
		.file-meta {
			font-size: 12px;
			color: #666;
		}
		.file-actions {
			display: flex;
			gap: 10px;
		}
		.btn {
			padding: 8px 16px;
			border: none;
			border-radius: 3px;
			cursor: pointer;
			text-decoration: none;
			font-size: 14px;
			transition: background-color 0.3s;
		}
		.btn-primary {
			background-color: #4CAF50;
			color: white;
		}
		.btn-primary:hover {
			background-color: #45a049;
		}
		.btn-danger {
			background-color: #f44336;
			color: white;
		}
		.btn-danger:hover {
			background-color: #da190b;
		}
		.empty-message {
			text-align: center;
			padding: 60px 20px;
			color: #666;
		}
		.empty-icon {
			font-size: 64px;
			margin-bottom: 20px;
		}
		.path-bar {
			background-color: #f5f5f5;
			padding: 10px;
			border-radius: 3px;
			margin-bottom: 20px;
			font-size: 14px;
		}
		.path-item {
			display: inline-block;
			margin-right: 5px;
		}
		.path-separator {
			color: #999;
			margin-right: 5px;
		}
		.message {
			padding: 10px;
			border-radius: 3px;
			margin-bottom: 20px;
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
	</style>
</head>
<body>
	<div class="container">
		<header>
			<div class="header-content">
				<h1>📦 ` + serverName + `</h1>
				<div>
					` + getCurrentUserInfo(r) + `
				</div>
			</div>
		</header>

		<nav>
			<div class="nav-links">
				<a href="/files">文件列表</a>
				<a href="/upload">上传文件</a>
				<a href="/admin">管理员</a>
				<a href="/review">文件审核</a>
			</div>
		</nav>

		<!-- 显示消息 -->
		` + getMessage(r) + `

		<div class="review-list">
			<!-- 路径导航 -->
			<div class="path-bar">
				<div class="path-item">
					<a href="/review?path=." class="path-link">📁 待审核文件</a>
				</div>
				` + generatePathNavigation(path) + `
			</div>

			<!-- 文件列表 -->
			` + generatePendingFileList(files, path) + `
		</div>
	</div>
</body>
</html>`

	w.Write([]byte(html))
}

// 辅助函数：生成待审核文件列表
func generatePendingFileList(files []os.DirEntry, currentPath string) string {
	if len(files) == 0 {
		return `<div class="empty-message">
			<div class="empty-icon">📁</div>
			<p>没有待审核的文件</p>
		</div>`
	}

	var fileList string

	// 先添加返回上一级目录的选项（如果不是根目录）
	if currentPath != "." {
		parentPath := filepath.Dir(currentPath)
		if parentPath == "." {
			parentPath = ""
		}
		fileList += fmt.Sprintf(`<div class="file-item">
			<div class="file-icon">📁</div>
			<div class="file-info">
				<div class="file-name"><a href="/review?path=%s">..</a></div>
				<div class="file-meta">返回上一级</div>
			</div>
		</div>`, url.QueryEscape(parentPath))
	}

	// 获取下载目录的所有子目录
	downloadDirs := getDirectoryList(downloadDir)
	// 过滤掉根目录选项（因为已经在HTML中添加了）
	var subDirsHTML string
	for _, dir := range downloadDirs {
		if dir != "." {
			subDirsHTML += fmt.Sprintf(`<option value="%s">%s</option>`, url.QueryEscape(dir), dir)
		}
	}

	// 添加文件和目录
	for _, file := range files {
		name := file.Name()
		filePath := filepath.Join(currentPath, name)
		fileURL := url.QueryEscape(filePath)

		// 获取文件信息
		info, err := file.Info()
		if err != nil {
			continue
		}

		// 生成文件图标
		var icon string
		if file.IsDir() {
			icon = "📁"
		} else {
			icon = "📄"
		}

		// 生成文件元信息
		var meta string
		if file.IsDir() {
			meta = "目录 • " + info.ModTime().Format("2006-01-02 15:04:05")
		} else {
			meta = fmt.Sprintf("文件 • %s • %s", formatFileSize(info.Size()), info.ModTime().Format("2006-01-02 15:04:05"))
		}

		// 生成文件项
		var item string
		if file.IsDir() {
			item = fmt.Sprintf(`<div class="file-item">
				<div class="file-icon">%s</div>
				<div class="file-info">
					<div class="file-name"><a href="/review?path=%s">%s</a></div>
					<div class="file-meta">%s</div>
				</div>
				<div class="file-actions">
					<form method="POST" action="/reject" style="display: inline;">
						<input type="hidden" name="path" value="%s">
						<button type="submit" class="btn btn-danger btn-sm" onclick="return confirm('确定要拒绝这个目录吗？');">拒绝</button>
					</form>
				</div>
			</div>`, icon, fileURL, name, meta, fileURL)
		} else {
			item = fmt.Sprintf(`<div class="file-item">
				<div class="file-icon">%s</div>
				<div class="file-info">
					<div class="file-name">%s</div>
					<div class="file-meta">%s</div>
				</div>
				<div class="file-actions">
					<form method="POST" action="/approve" style="display: inline; margin-right: 10px;">
						<input type="hidden" name="path" value="%s">
						<label for="target_dir_%s" style="display: block; margin-bottom: 5px; font-size: 12px;">目标目录:</label>
						<select id="target_dir_%s" name="target_dir" style="margin-bottom: 10px; padding: 5px; border: 1px solid #ddd; border-radius: 3px; font-size: 12px;">
							<option value=".">根目录</option>
							%s
						</select>
						<button type="submit" class="btn btn-primary btn-sm">通过</button>
					</form>
					<form method="POST" action="/reject" style="display: inline;">
						<input type="hidden" name="path" value="%s">
						<button type="submit" class="btn btn-danger btn-sm">拒绝</button>
					</form>
				</div>
			</div>`, icon, name, meta, fileURL, fileURL, fileURL, subDirsHTML, fileURL)
		}

		fileList += item
	}

	return fileList
}

// 辅助函数：生成带路径的待审核文件列表
func generatePendingFileListWithPath(files []FileWithPath) string {
	if len(files) == 0 {
		return `<div class="empty-message">
			<div class="empty-icon">📁</div>
			<p>没有待审核的文件</p>
		</div>`
	}

	var fileList string

	// 获取下载目录的所有子目录
	downloadDirs := getDirectoryList(downloadDir)
	// 过滤掉根目录选项（因为已经在HTML中添加了）
	var subDirsHTML string
	for _, dir := range downloadDirs {
		if dir != "." {
			subDirsHTML += fmt.Sprintf(`<option value="%s">%s</option>`, url.QueryEscape(dir), dir)
		}
	}

	// 添加文件和目录
	for _, file := range files {
		name := file.Entry.Name()
		filePath := file.FullPath
		fileURL := url.QueryEscape(filePath)

		// 获取文件信息
		info, err := file.Entry.Info()
		if err != nil {
			continue
		}

		// 生成文件图标
		var icon string
		if file.Entry.IsDir() {
			icon = "📁"
		} else {
			icon = "📄"
		}

		// 生成文件元信息
		var meta string
		if file.Entry.IsDir() {
			meta = "目录 • " + info.ModTime().Format("2006-01-02 15:04:05")
		} else {
			meta = fmt.Sprintf("文件 • %s • %s", formatFileSize(info.Size()), info.ModTime().Format("2006-01-02 15:04:05"))
		}

		// 生成文件项
		var item string
		if file.Entry.IsDir() {
			item = fmt.Sprintf(`<div class="file-item">
				<div class="file-icon">%s</div>
				<div class="file-info">
					<div class="file-name"><a href="/review?path=%s">%s</a></div>
					<div class="file-meta">%s</div>
				</div>
				<div class="file-actions">
					<form method="POST" action="/reject" style="display: inline;">
						<input type="hidden" name="path" value="%s">
						<button type="submit" class="btn btn-danger btn-sm" onclick="return confirm('确定要拒绝这个目录吗？');">拒绝</button>
					</form>
				</div>
			</div>`, icon, fileURL, name, meta, fileURL)
		} else {
			// 显示完整路径在文件名旁边
			var displayPath string
			if filepath.Dir(filePath) != "." {
				displayPath = fmt.Sprintf(" (%s)", filepath.Dir(filePath))
			}
			item = fmt.Sprintf(`<div class="file-item">
				<div class="file-icon">%s</div>
				<div class="file-info">
					<div class="file-name">%s%s</div>
					<div class="file-meta">%s</div>
				</div>
				<div class="file-actions">
					<form method="POST" action="/approve" style="display: inline; margin-right: 10px;">
						<input type="hidden" name="path" value="%s">
						<label for="target_dir_%s" style="display: block; margin-bottom: 5px; font-size: 12px;">目标目录:</label>
						<select id="target_dir_%s" name="target_dir" style="margin-bottom: 10px; padding: 5px; border: 1px solid #ddd; border-radius: 3px; font-size: 12px;">
							<option value=".">根目录</option>
							%s
						</select>
						<button type="submit" class="btn btn-primary btn-sm">通过</button>
					</form>
					<form method="POST" action="/reject" style="display: inline;">
						<input type="hidden" name="path" value="%s">
						<button type="submit" class="btn btn-danger btn-sm">拒绝</button>
					</form>
				</div>
			</div>`, icon, name, displayPath, meta, fileURL, fileURL, fileURL, subDirsHTML, fileURL)
		}

		fileList += item
	}

	return fileList
}

// 审核通过处理函数
func approveHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	session := getCurrentUser(r)
	if session == nil || session.Role != RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// POST请求：处理审核通过
	if r.Method == "POST" {
		// 解析表单
		err := r.ParseForm()
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/review?msg=%s&type=error", "表单解析失败"), http.StatusFound)
			return
		}

		// 获取文件路径
		path := r.FormValue("path")
		if path == "" {
			http.Redirect(w, r, fmt.Sprintf("/review?msg=%s&type=error", "文件路径不能为空"), http.StatusFound)
			return
		}

		// 解码URL编码的路径
		path, err = url.QueryUnescape(path)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/review?msg=%s&type=error", "路径解码失败"), http.StatusFound)
			return
		}

		// 安全检查
		path = filepath.Clean(path)
		if strings.HasPrefix(path, "..") {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}

		// 获取目标目录
		targetDir := r.FormValue("target_dir")
		if targetDir == "" {
			targetDir = "."
		}

		// 解码URL编码的目标目录
		targetDir, err = url.QueryUnescape(targetDir)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/review?msg=%s&type=error", "目标目录解码失败"), http.StatusFound)
			return
		}

		// 安全检查
		targetDir = filepath.Clean(targetDir)
		if strings.HasPrefix(targetDir, "..") {
			http.Error(w, "Invalid target directory", http.StatusBadRequest)
			return
		}

		// 构建源路径和目标路径
		sourcePath := filepath.Join(pendingDir, path)

		// 获取文件名
		filename := filepath.Base(path)

		// 构建目标路径：目标目录 + 文件名
		targetPath := filepath.Join(downloadDir, targetDir, filename)

		// 调试日志
		log.Printf("审核通过文件: %s", path)
		log.Printf("源路径: %s", sourcePath)
		log.Printf("目标路径: %s", targetPath)

		// 检查源文件是否存在
		if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
			http.Redirect(w, r, fmt.Sprintf("/review?msg=%s&type=error", fmt.Sprintf("源文件不存在: %s", sourcePath)), http.StatusFound)
			return
		}

		// 创建目标目录
		err = os.MkdirAll(filepath.Dir(targetPath), 0755)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/review?msg=%s&type=error", fmt.Sprintf("创建目录失败: %v", err)), http.StatusFound)
			return
		}

		// 移动文件
		err = os.Rename(sourcePath, targetPath)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/review?msg=%s&type=error", fmt.Sprintf("移动文件失败: %v", err)), http.StatusFound)
			return
		}

		// 检查并删除待审核目录中可能存在的空目录
		sourceDir := filepath.Dir(sourcePath)
		if sourceDir != pendingDir {
			// 检查目录是否为空
			files, err := os.ReadDir(sourceDir)
			if err == nil && len(files) == 0 {
				// 删除空目录
				os.Remove(sourceDir)
				log.Printf("删除空目录: %s", sourceDir)

				// 检查父目录是否也为空，如果是则继续删除
				parentDir := filepath.Dir(sourceDir)
				if parentDir != pendingDir {
					parentFiles, err := os.ReadDir(parentDir)
					if err == nil && len(parentFiles) == 0 {
						os.Remove(parentDir)
						log.Printf("删除空父目录: %s", parentDir)
					}
				}
			}
		}

		// 记录日志
		log.Printf("管理员 %s 审核通过了文件: %s", session.Username, path)

		// 重定向回审核页面
		http.Redirect(w, r, fmt.Sprintf("/review?msg=%s&type=success", "文件审核通过"), http.StatusFound)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// 审核拒绝处理函数
func rejectHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	session := getCurrentUser(r)
	if session == nil || session.Role != RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// POST请求：处理审核拒绝
	if r.Method == "POST" {
		// 解析表单
		err := r.ParseForm()
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/review?msg=%s&type=error", "表单解析失败"), http.StatusFound)
			return
		}

		// 获取文件路径
		path := r.FormValue("path")
		if path == "" {
			http.Redirect(w, r, fmt.Sprintf("/review?msg=%s&type=error", "文件路径不能为空"), http.StatusFound)
			return
		}

		// 解码URL编码的路径
		path, err = url.QueryUnescape(path)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/review?msg=%s&type=error", "路径解码失败"), http.StatusFound)
			return
		}

		// 安全检查
		path = filepath.Clean(path)
		if strings.HasPrefix(path, "..") {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}

		// 构建完整路径
		fullPath := filepath.Join(pendingDir, path)

		// 删除文件
		err = os.RemoveAll(fullPath)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("/review?msg=%s&type=error", fmt.Sprintf("删除文件失败: %v", err)), http.StatusFound)
			return
		}

		// 记录日志
		log.Printf("管理员 %s 拒绝了文件: %s", session.Username, path)

		// 重定向回审核页面
		http.Redirect(w, r, fmt.Sprintf("/review?msg=%s&type=success", "文件审核拒绝"), http.StatusFound)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// 服务器日志处理函数
func logHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	session := getCurrentUser(r)
	if session == nil || session.Role != RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 读取日志文件
	logFilePath := filepath.Join(logDir, logFile)
	logContent, err := os.ReadFile(logFilePath)
	if err != nil {
		logContent = []byte("无法读取日志文件: " + err.Error())
	}

	// 构建HTML页面
	html := `<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>服务器日志 - ` + serverName + `</title>
	<style>
		body {
			font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
			background-color: #f5f5f5;
			margin: 0;
			padding: 0;
		}
		.container {
			max-width: 1200px;
			margin: 0 auto;
			padding: 20px;
		}
		header {
			background-color: #4CAF50;
			color: white;
			padding: 15px;
			border-radius: 5px;
			margin-bottom: 20px;
		}
		.header-content {
			display: flex;
			justify-content: space-between;
			align-items: center;
		}
		nav {
			background-color: white;
			padding: 15px;
			border-radius: 5px;
			margin-bottom: 20px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.nav-links {
			display: flex;
			gap: 15px;
		}
		.nav-links a {
			text-decoration: none;
			color: #333;
			padding: 8px 12px;
			border-radius: 3px;
			transition: background-color 0.3s;
		}
		.nav-links a:hover {
			background-color: #e0e0e0;
		}
		.content {
			background-color: white;
			padding: 30px;
			border-radius: 5px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		pre {
			background-color: #f8f9fa;
			padding: 20px;
			border-radius: 5px;
			overflow-x: auto;
			white-space: pre-wrap;
			word-wrap: break-word;
			border: 1px solid #dee2e6;
			max-height: 600px;
			overflow-y: auto;
		}
		.btn {
			padding: 10px 20px;
			border: none;
			border-radius: 3px;
			cursor: pointer;
			text-decoration: none;
			font-size: 16px;
			transition: background-color 0.3s;
		}
		.btn-primary {
			background-color: #4CAF50;
			color: white;
		}
		.btn-primary:hover {
			background-color: #45a049;
		}
	</style>
</head>
<body>
	<div class="container">
		<header>
			<div class="header-content">
				<h1>服务器日志 - ` + serverName + `</h1>
				<div>
					<span>欢迎, ` + session.Username + ` (管理员)</span>
					<a href="/logout" class="btn btn-primary" style="margin-left: 15px;">退出登录</a>
				</div>
			</div>
		</header>
		
		<nav>
			<div class="nav-links">
				<a href="/">首页</a>
				<a href="/files">文件列表</a>
				<a href="/upload">上传文件</a>
				<a href="/admin">管理中心</a>
			</div>
		</nav>
		
		<div class="content">
			<h2>服务器日志</h2>
			<p>以下是服务器的运行日志记录：</p>
			<pre>` + strings.ReplaceAll(string(logContent), "<", "&lt;") + `</pre>
			<div style="margin-top: 20px;">
				<a href="/admin" class="btn btn-primary">返回管理中心</a>
			</div>
		</div>
	</div>
</body>
</html>`

	fmt.Fprint(w, html)
}

// 服务器信息处理函数
func infoHandler(w http.ResponseWriter, r *http.Request) {
	// 检查用户权限
	session := getCurrentUser(r)
	if session == nil || session.Role != RoleAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 获取服务器信息
	osInfo := fmt.Sprintf("操作系统: %s", runtime.GOOS)
	archInfo := fmt.Sprintf("架构: %s", runtime.GOARCH)
	goVersion := fmt.Sprintf("Go版本: %s", runtime.Version())
	uptime := fmt.Sprintf("运行时间: %v", time.Since(startTime))

	// 统计信息
	var totalFiles int
	var totalSize int64
	err := filepath.Walk(downloadDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalFiles++
			totalSize += info.Size()
		}
		return nil
	})
	if err != nil {
		totalFiles = 0
		totalSize = 0
	}

	var pendingFiles int
	err = filepath.Walk(pendingDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			pendingFiles++
		}
		return nil
	})
	if err != nil {
		pendingFiles = 0
	}

	// 构建HTML页面
	html := `<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>服务器信息 - ` + serverName + `</title>
	<style>
		body {
			font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
			background-color: #f5f5f5;
			margin: 0;
			padding: 0;
		}
		.container {
			max-width: 1200px;
			margin: 0 auto;
			padding: 20px;
		}
		header {
			background-color: #4CAF50;
			color: white;
			padding: 15px;
			border-radius: 5px;
			margin-bottom: 20px;
		}
		.header-content {
			display: flex;
			justify-content: space-between;
			align-items: center;
		}
		nav {
			background-color: white;
			padding: 15px;
			border-radius: 5px;
			margin-bottom: 20px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.nav-links {
			display: flex;
			gap: 15px;
		}
		.nav-links a {
			text-decoration: none;
			color: #333;
			padding: 8px 12px;
			border-radius: 3px;
			transition: background-color 0.3s;
		}
		.nav-links a:hover {
			background-color: #e0e0e0;
		}
		.content {
			background-color: white;
			padding: 30px;
			border-radius: 5px;
			box-shadow: 0 2px 4px rgba(0,0,0,0.1);
		}
		.info-grid {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
			gap: 20px;
			margin-top: 20px;
		}
		.info-card {
			background-color: #f8f9fa;
			padding: 20px;
			border-radius: 5px;
			border: 1px solid #dee2e6;
		}
		.info-card h3 {
			margin-top: 0;
			color: #4CAF50;
		}
		.info-item {
			margin-bottom: 10px;
		}
		.btn {
			padding: 10px 20px;
			border: none;
			border-radius: 3px;
			cursor: pointer;
			text-decoration: none;
			font-size: 16px;
			transition: background-color 0.3s;
		}
		.btn-primary {
			background-color: #4CAF50;
			color: white;
		}
		.btn-primary:hover {
			background-color: #45a049;
		}
	</style>
</head>
<body>
	<div class="container">
		<header>
			<div class="header-content">
				<h1>服务器信息 - ` + serverName + `</h1>
				<div>
					<span>欢迎, ` + session.Username + ` (管理员)</span>
					<a href="/logout" class="btn btn-primary" style="margin-left: 15px;">退出登录</a>
				</div>
			</div>
		</header>
		
		<nav>
			<div class="nav-links">
				<a href="/">首页</a>
				<a href="/files">文件列表</a>
				<a href="/upload">上传文件</a>
				<a href="/admin">管理中心</a>
			</div>
		</nav>
		
		<div class="content">
			<h2>服务器信息</h2>
			<div class="info-grid">
				<div class="info-card">
					<h3>系统信息</h3>
					<div class="info-item">` + osInfo + `</div>
					<div class="info-item">` + archInfo + `</div>
					<div class="info-item">` + goVersion + `</div>
					<div class="info-item">` + uptime + `</div>
				</div>
				
				<div class="info-card">
					<h3>存储信息</h3>
					<div class="info-item">下载目录: ` + downloadDir + `</div>
					<div class="info-item">待审核目录: ` + pendingDir + `</div>
				</div>
				
				<div class="info-card">
					<h3>文件统计</h3>
					<div class="info-item">已发布文件: ` + strconv.Itoa(totalFiles) + ` 个</div>
					<div class="info-item">总大小: ` + humanReadableSize(totalSize) + `</div>
					<div class="info-item">待审核文件: ` + strconv.Itoa(pendingFiles) + ` 个</div>
				</div>
				
				<div class="info-card">
					<h3>服务器配置</h3>
					<div class="info-item">端口: ` + strconv.Itoa(port) + `</div>
					<div class="info-item">访问地址: http://localhost:` + strconv.Itoa(port) + `</div>
				</div>
			</div>
			<div style="margin-top: 20px;">
				<a href="/admin" class="btn btn-primary">返回管理中心</a>
			</div>
		</div>
	</div>
</body>
</html>`

	fmt.Fprint(w, html)
}

// 格式化文件大小为人类可读格式
func humanReadableSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	} else if size < 1024*1024 {
		return fmt.Sprintf("%.2f KB", float64(size)/1024)
	} else if size < 1024*1024*1024 {
		return fmt.Sprintf("%.2f MB", float64(size)/(1024*1024))
	} else {
		return fmt.Sprintf("%.2f GB", float64(size)/(1024*1024*1024))
	}
}

// 主函数
func main() {
	// 设置开始时间
	startTime = time.Now()

	// 加载配置文件
	if err := loadConfig(); err != nil {
		log.Fatalf("加载配置文件失败: %v", err)
	}

	// 创建下载目录
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		log.Fatalf("无法创建下载目录: %v", err)
	}

	// 创建待审核目录
	if err := os.MkdirAll(pendingDir, 0755); err != nil {
		log.Fatalf("无法创建待审核目录: %v", err)
	}

	// 创建日志目录
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Fatalf("无法创建日志目录: %v", err)
	}

	// 打开日志文件
	logFilePath := filepath.Join(logDir, logFile)
	logFileHandle, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("无法打开日志文件: %v", err)
	}
	defer logFileHandle.Close()

	// 设置日志输出到文件和控制台
	log.SetOutput(io.MultiWriter(os.Stdout, logFileHandle))
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	// 注册路由
	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/files", filesHandler)
	http.HandleFunc("/upload", uploadHandler)
	http.HandleFunc("/download", downloadHandler)
	http.HandleFunc("/delete", deleteHandler)
	http.HandleFunc("/login", loginHandler)
	http.HandleFunc("/logout", logoutHandler)
	http.HandleFunc("/admin", adminHandler)
	http.HandleFunc("/mkdir", mkdirHandler)
	http.HandleFunc("/review", reviewHandler)
	http.HandleFunc("/approve", approveHandler)
	http.HandleFunc("/reject", rejectHandler)
	http.HandleFunc("/logs", logHandler)
	http.HandleFunc("/info", infoHandler)
	http.HandleFunc("/user-management", userManagementHandler)
	http.HandleFunc("/change-password", changePasswordHandler)
	http.HandleFunc("/add-user", addUserHandler)
	http.HandleFunc("/delete-user", deleteUserHandler)

	// 启动服务器
	addr := fmt.Sprintf(":%d", port)
	log.Printf("服务器已启动，监听端口 %d\n", port)
	log.Printf("访问地址: http://localhost%s\n", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
