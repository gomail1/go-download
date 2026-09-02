package utils

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"go-download-server/config"
)

// 日志管理增强工具

const (
	// MaxLogFileSize 单个日志文件最大大小（10MB）
	MaxLogFileSize = 10 * 1024 * 1024
	// MaxLogFiles 保留的最大日志文件数量
	MaxLogFiles = 30
	// MaxLogFileAge 日志文件最大保留天数
	MaxLogFileAge = 30
)

// SanitizeLogContent 对日志内容进行敏感信息脱敏
func SanitizeLogContent(content string) string {
	if content == "" {
		return content
	}

	// 脱敏IP地址（保留前两段，隐藏后两段）
	ipRegex := regexp.MustCompile(`\b(\d{1,3}\.\d{1,3})\.\d{1,3}\.\d{1,3}\b`)
	content = ipRegex.ReplaceAllString(content, "$1.***.***")

	// 脱敏密码（password=xxx 或 password: xxx）
	passwordRegex := regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[=:]\s*\S+`)
	content = passwordRegex.ReplaceAllString(content, "$1=***")

	// 脱敏令牌（token=xxx 或 token: xxx）
	tokenRegex := regexp.MustCompile(`(?i)(token|apikey|api_key|secret)\s*[=:]\s*\S+`)
	content = tokenRegex.ReplaceAllString(content, "$1=***")

	// 脱敏会话ID（session_id=xxx）
	sessionRegex := regexp.MustCompile(`(?i)(session_id|sessionid)\s*[=:]\s*\S+`)
	content = sessionRegex.ReplaceAllString(content, "$1=***")

	// 脱敏邮箱地址（保留首字母和域名）
	emailRegex := regexp.MustCompile(`\b([a-zA-Z0-9._%+-])[a-zA-Z0-9._%+-]*@([a-zA-Z0-9.-]+\.[a-zA-Z]{2,})\b`)
	content = emailRegex.ReplaceAllString(content, "$1***@$2")

	// 脱敏手机号（保留前3位和后4位）
	phoneRegex := regexp.MustCompile(`\b(1[3-9]\d)\d{4}(\d{4})\b`)
	content = phoneRegex.ReplaceAllString(content, "$1****$2")

	// 脱敏身份证号（保留前6位和后4位）
	idCardRegex := regexp.MustCompile(`\b(\d{6})\d{8}(\d{4}|\d{3}[Xx])\b`)
	content = idCardRegex.ReplaceAllString(content, "$1********$2")

	return content
}

// RotateLogFileIfNeeded 检查日志文件大小，如果超过限制则进行轮转
func RotateLogFileIfNeeded(logFilePath string) error {
	// 检查文件是否存在
	fileInfo, err := os.Stat(logFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 文件不存在，不需要轮转
		}
		return err
	}

	// 检查文件大小
	if fileInfo.Size() < MaxLogFileSize {
		return nil // 文件大小未超过限制，不需要轮转
	}

	// 生成轮转后的文件名
	dir := filepath.Dir(logFilePath)
	base := filepath.Base(logFilePath)
	ext := filepath.Ext(base)
	nameWithoutExt := strings.TrimSuffix(base, ext)
	timestamp := time.Now().Format("20060102_150405")
	rotatedPath := filepath.Join(dir, fmt.Sprintf("%s_%s%s", nameWithoutExt, timestamp, ext))

	// 重命名当前日志文件
	if err := os.Rename(logFilePath, rotatedPath); err != nil {
		return fmt.Errorf("重命名日志文件失败: %v", err)
	}

	// 压缩轮转后的日志文件
	go func() {
		if err := compressLogFile(rotatedPath); err != nil {
			// 压缩失败不影响主流程，只记录错误
			fmt.Printf("压缩日志文件失败: %v\n", err)
		}
	}()

	// 清理过期的日志文件
	go func() {
		if err := CleanOldLogFiles(dir); err != nil {
			// 清理失败不影响主流程，只记录错误
			fmt.Printf("清理过期日志文件失败: %v\n", err)
		}
	}()

	return nil
}

// compressLogFile 压缩日志文件
func compressLogFile(logFilePath string) error {
	// 打开源文件
	srcFile, err := os.Open(logFilePath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// 创建压缩文件
	gzipPath := logFilePath + ".gz"
	gzipFile, err := os.Create(gzipPath)
	if err != nil {
		return err
	}
	defer gzipFile.Close()

	// 创建gzip写入器
	gzipWriter := gzip.NewWriter(gzipFile)
	defer gzipWriter.Close()

	// 复制内容
	if _, err := io.Copy(gzipWriter, srcFile); err != nil {
		return err
	}

	// 关闭gzip写入器（确保所有数据都写入）
	if err := gzipWriter.Close(); err != nil {
		return err
	}

	// 删除源文件
	if err := os.Remove(logFilePath); err != nil {
		return err
	}

	return nil
}

// CleanOldLogFiles 清理过期的日志文件
func CleanOldLogFiles(logDir string) error {
	// 读取日志目录
	files, err := os.ReadDir(logDir)
	if err != nil {
		return err
	}

	// 过滤日志文件
	var logFiles []os.DirEntry
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		name := file.Name()
		// 只处理.log和.log.gz文件
		if strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".log.gz") {
			logFiles = append(logFiles, file)
		}
	}

	// 如果文件数量未超过限制，不需要清理
	if len(logFiles) <= MaxLogFiles {
		return nil
	}

	// 按修改时间排序（从旧到新）
	sort.Slice(logFiles, func(i, j int) bool {
		infoI, _ := logFiles[i].Info()
		infoJ, _ := logFiles[j].Info()
		return infoI.ModTime().Before(infoJ.ModTime())
	})

	// 删除最旧的文件，直到文件数量不超过限制
	filesToDelete := len(logFiles) - MaxLogFiles
	for i := 0; i < filesToDelete; i++ {
		filePath := filepath.Join(logDir, logFiles[i].Name())
		if err := os.Remove(filePath); err != nil {
			// 记录错误但继续删除其他文件
			fmt.Printf("删除过期日志文件失败: %s, %v\n", filePath, err)
		}
	}

	return nil
}

// CleanLogFilesByAge 按保留天数清理日志文件
func CleanLogFilesByAge(logDir string, maxAgeDays int) error {
	// 计算截止时间
	cutoffTime := time.Now().AddDate(0, 0, -maxAgeDays)

	// 读取日志目录
	files, err := os.ReadDir(logDir)
	if err != nil {
		return err
	}

	// 删除超过保留天数的文件
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		name := file.Name()
		// 只处理.log和.log.gz文件
		if !strings.HasSuffix(name, ".log") && !strings.HasSuffix(name, ".log.gz") {
			continue
		}

		info, err := file.Info()
		if err != nil {
			continue
		}

		// 检查文件修改时间是否早于截止时间
		if info.ModTime().Before(cutoffTime) {
			filePath := filepath.Join(logDir, name)
			if err := os.Remove(filePath); err != nil {
				fmt.Printf("删除过期日志文件失败: %s, %v\n", filePath, err)
			}
		}
	}

	return nil
}

// GetLogFileSize 获取日志文件大小
func GetLogFileSize(logFilePath string) (int64, error) {
	fileInfo, err := os.Stat(logFilePath)
	if err != nil {
		return 0, err
	}
	return fileInfo.Size(), nil
}

// GetLogFilesList 获取日志文件列表
func GetLogFilesList(logDir string) ([]string, error) {
	files, err := os.ReadDir(logDir)
	if err != nil {
		return nil, err
	}

	var logFiles []string
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		name := file.Name()
		if strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".log.gz") {
			logFiles = append(logFiles, name)
		}
	}

	// 按文件名排序（从新到旧）
	sort.Sort(sort.Reverse(sort.StringSlice(logFiles)))

	return logFiles, nil
}

// InitLogManagement 初始化日志管理（启动定期清理任务）
func InitLogManagement() {
	// 启动定期清理任务（每天凌晨3点执行）
	go func() {
		for {
			now := time.Now()
			// 计算下次执行时间（明天凌晨3点）
			nextRun := time.Date(now.Year(), now.Month(), now.Day()+1, 3, 0, 0, 0, now.Location())
			duration := nextRun.Sub(now)

			time.Sleep(duration)

			// 执行日志清理
			logDir := config.AppConfig.Server.LogDir
			if err := CleanOldLogFiles(logDir); err != nil {
				fmt.Printf("定期清理日志文件失败: %v\n", err)
			}
			if err := CleanLogFilesByAge(logDir, MaxLogFileAge); err != nil {
				fmt.Printf("定期按天数清理日志文件失败: %v\n", err)
			}
		}
	}()
}
