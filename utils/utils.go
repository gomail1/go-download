package utils

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/session"
)

// 辅助函数：格式化文件大小
func FormatFileSize(size int64) string {
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
					<span class="user-info" style="color: white;">
						欢迎, %s (角色: %s) • 
						<a href="/logout" style="color: white; text-decoration: none; font-weight: bold; margin-left: 10px;">退出登录</a>
					</span>`, session.Username, GetRoleName(session.Role))
	} else {
		return `<a href="/login" style="color: white; text-decoration: none; font-weight: bold;">登录</a>`
	}
}

// 辅助函数：获取角色名称
func GetRoleName(role constants.UserRole) string {
	switch role {
	case constants.RoleAdmin:
		return "管理员"
	case constants.RoleNormal:
		return "普通用户"
	case constants.RoleTest:
		return "测试用户"
	default:
		return "未知角色"
	}
}

// 辅助函数：根据字符串获取角色名称
func GetRoleNameByString(roleStr string) string {
	var role constants.UserRole
	switch roleStr {
	case "test":
		role = constants.RoleTest
	case "normal":
		role = constants.RoleNormal
	case "admin":
		role = constants.RoleAdmin
	default:
		role = constants.RoleTest
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
	session := session.GetCurrentUser(r)
	if session != nil && session.Role == constants.RoleAdmin {
		pendingCount := CountPendingFiles()
		return fmt.Sprintf(`<a href="/admin" class="admin-link">管理员<span class="pending-count">%d</span></a>`, pendingCount)
	}
	return ""
}

// 辅助函数：获取管理员操作按钮
func GetAdminActions(r *http.Request, path string) string {
	session := session.GetCurrentUser(r)
	if session != nil && session.Role == constants.RoleAdmin {
		return fmt.Sprintf(`<a href="/delete?path=%s" class="btn btn-danger" onclick="return confirm('确定要删除吗？')">删除</a>`, url.QueryEscape(path))
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
	msg := r.URL.Query().Get("msg")
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
				message.classList.add('fade-out');
				setTimeout(function() {
					message.remove();
				}, 500);
			}
		}, 5000);
	</script>
	<style>
		.message {
			padding: 12px 20px;
			border-radius: 5px;
			margin-bottom: 20px;
			transition: opacity 0.5s ease, transform 0.5s ease;
			opacity: 1;
			transform: translateY(0);
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
		.fade-out {
			opacity: 0;
			transform: translateY(-10px);
		}
	</style>`, class, msg)
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
		navigation += fmt.Sprintf(`<span class="path-separator">›</span>
						<div class="path-item">
							<a href="/files?path=%s" class="path-link">%s</a>
						</div>`, url.QueryEscape(currentPath), part)
	}

	return navigation
}

// 日志级别类型
type LogLevel string

const (
	LogLevelInfo    LogLevel = "info"
	LogLevelSuccess LogLevel = "success"
	LogLevelWarning LogLevel = "warning"
	LogLevelError   LogLevel = "error"
	LogLevelDebug   LogLevel = "debug"
)

// 辅助函数：记录日志
func Log(level LogLevel, username, role, action, details string) {
	// 格式化日志条目
	logEntry := fmt.Sprintf("[%s] [%s] [%s] [%s] %s %s\n",
		time.Now().Format("2006-01-02 15:04:05"),
		string(level),
		username,
		role,
		action,
		details)

	// 确保日志目录存在
	os.MkdirAll(config.AppConfig.Server.LogDir, 0755)

	// 打开日志文件（追加模式）
	logFilePath := filepath.Join(config.AppConfig.Server.LogDir, config.AppConfig.Server.LogFile)
	logFile, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		// 如果无法打开日志文件，输出到标准错误
		log.Printf("无法打开日志文件: %v\n", err)
		log.Printf("日志内容: %s\n", logEntry)
		return
	}
	defer logFile.Close()

	// 写入日志
	_, err = logFile.WriteString(logEntry)
	if err != nil {
		log.Printf("写入日志失败: %v\n", err)
		return
	}

	// 同步到磁盘
	logFile.Sync()
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
		case constants.RoleNormal:
			role = "normal"
		case constants.RoleTest:
			role = "test"
		default:
			role = "unknown"
		}
	}

	// 记录日志
	Log(LogLevelInfo, username, role, action, details)
}

// 辅助函数：记录用户操作日志
func LogUserAction(r *http.Request, action, details string) {
	LogRequest(r, action, details)
}
