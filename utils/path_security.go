package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SafePath 安全路径验证结果
type SafePath struct {
	FullPath    string // 完整绝对路径
	RelativePath string // 相对于基础目录的相对路径
	IsSafe      bool   // 是否安全
	Error       error  // 错误信息
}

// ValidateSafePath 验证路径是否安全，防止路径遍历攻击
// baseDir: 基础目录（如下载目录）
// userPath: 用户输入的路径
// 返回安全路径信息
func ValidateSafePath(baseDir, userPath string) *SafePath {
	result := &SafePath{
		IsSafe: false,
	}

	// 1. 清理路径，去除.和..等
	cleanedPath := filepath.Clean(userPath)

	// 2. 检查是否包含..（路径遍历）
	if strings.Contains(cleanedPath, "..") {
		result.Error = fmt.Errorf("路径包含非法字符: ..")
		return result
	}

	// 3. 获取基础目录的绝对路径
	absBaseDir, err := filepath.Abs(baseDir)
	if err != nil {
		result.Error = fmt.Errorf("获取基础目录绝对路径失败: %v", err)
		return result
	}

	// 4. 构建完整路径
	fullPath := filepath.Join(absBaseDir, cleanedPath)

	// 5. 获取完整路径的绝对路径
	absFullPath, err := filepath.Abs(fullPath)
	if err != nil {
		result.Error = fmt.Errorf("获取完整路径绝对路径失败: %v", err)
		return result
	}

	// 6. 验证完整路径是否在基础目录内
	// 使用filepath.Rel来验证，如果返回的路径以..开头，则说明不在基础目录内
	relPath, err := filepath.Rel(absBaseDir, absFullPath)
	if err != nil {
		result.Error = fmt.Errorf("计算相对路径失败: %v", err)
		return result
	}

	// 检查相对路径是否以..开头（说明在基础目录外）
	if strings.HasPrefix(relPath, "..") || relPath == ".." {
		result.Error = fmt.Errorf("路径不在允许的目录内")
		return result
	}

	// 7. 检查是否为符号链接（可选，根据需要启用）
	// 注意：这可能会影响正常的符号链接使用，默认不启用
	/*
	fileInfo, err := os.Lstat(absFullPath)
	if err == nil && fileInfo.Mode()&os.ModeSymlink != 0 {
		// 读取符号链接的目标
		target, err := os.Readlink(absFullPath)
		if err == nil {
			// 验证符号链接目标是否也在基础目录内
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(absFullPath), target)
			}
			absTarget, err := filepath.Abs(target)
			if err == nil {
				relTarget, err := filepath.Rel(absBaseDir, absTarget)
				if err != nil || strings.HasPrefix(relTarget, "..") {
					result.Error = fmt.Errorf("符号链接目标不在允许的目录内")
					return result
				}
			}
		}
	}
	*/

	// 8. 路径验证通过
	result.FullPath = absFullPath
	result.RelativePath = relPath
	result.IsSafe = true
	return result
}

// IsPathSafe 简单的路径安全检查
// baseDir: 基础目录
// userPath: 用户输入的路径
// 返回是否安全
func IsPathSafe(baseDir, userPath string) bool {
	result := ValidateSafePath(baseDir, userPath)
	return result.IsSafe
}

// SafeJoinPath 安全的路径拼接（避免与已有的SafeJoin重名）
// baseDir: 基础目录
// userPath: 用户输入的路径
// 返回安全的完整路径和错误信息
func SafeJoinPath(baseDir, userPath string) (string, error) {
	result := ValidateSafePath(baseDir, userPath)
	if !result.IsSafe {
		return "", result.Error
	}
	return result.FullPath, nil
}

// GetFileExtension 获取文件扩展名（小写，包含点）
func GetFileExtension(filename string) string {
	ext := filepath.Ext(filename)
	return strings.ToLower(ext)
}

// IsAllowedFileType 检查文件类型是否在允许列表中
// filename: 文件名
// allowedExtensions: 允许的扩展名列表（如 []string{".txt", ".pdf"}）
// 如果allowedExtensions为空，则允许所有类型
func IsAllowedFileType(filename string, allowedExtensions []string) bool {
	if len(allowedExtensions) == 0 {
		return true
	}

	ext := GetFileExtension(filename)
	for _, allowedExt := range allowedExtensions {
		if strings.EqualFold(ext, allowedExt) {
			return true
		}
	}
	return false
}

// IsDangerousFileType 检查是否为危险文件类型
func IsDangerousFileType(filename string) bool {
	dangerousExtensions := []string{
		".exe", ".bat", ".cmd", ".com", ".msi", ".msp", ".mst",
		".ps1", ".psm1", ".psd1", ".ps1xml", ".psc1", ".pssc", ".cdxml",
		".vbs", ".vbe", ".js", ".jse", ".wsf", ".wsh", ".msc",
		".dll", ".sys", ".drv", ".ocx", ".cpl", ".scr",
		".reg", ".inf", ".ini",
		".hta", ".html", ".htm", ".mht", ".mhtml",
		".jar", ".class", ".war", ".ear",
		".php", ".php3", ".php4", ".php5", ".phtml",
		".asp", ".aspx", ".ascx", ".ashx", ".asmx",
		".jsp", ".jspx", ".jsf", ".jws",
		".pl", ".pm", ".py", ".pyc", ".pyo", ".rb", ".rbw",
		".sh", ".bash", ".zsh", ".ksh", ".csh", ".fish",
		".sql", ".db", ".sqlite", ".sqlite3",
		".xml", ".xsl", ".xslt", ".xsd",
		".json", ".yaml", ".yml",
		".env", ".config", ".conf",
		".pem", ".key", ".crt", ".cer", ".p12", ".pfx", ".jks",
		".zip", ".rar", ".7z", ".tar", ".gz", ".bz2", ".xz",
		".torrent",
	}

	ext := GetFileExtension(filename)
	for _, dangerousExt := range dangerousExtensions {
		if strings.EqualFold(ext, dangerousExt) {
			return true
		}
	}
	return false
}

// SanitizeFilenameSafe 清理文件名，移除危险字符（避免与已有的SanitizeFilename重名）
func SanitizeFilenameSafe(filename string) string {
	// 移除路径分隔符
	filename = strings.ReplaceAll(filename, "/", "_")
	filename = strings.ReplaceAll(filename, "\\", "_")

	// 移除其他危险字符
	dangerousChars := []string{"..", ":", "*", "?", "\"", "<", ">", "|", "\x00"}
	for _, char := range dangerousChars {
		filename = strings.ReplaceAll(filename, char, "_")
	}

	// 移除开头的点（隐藏文件）
	filename = strings.TrimLeft(filename, ".")

	// 如果文件名为空，返回默认名称
	if filename == "" {
		filename = "unnamed_file"
	}

	return filename
}

// FileExists 检查文件是否存在
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsDirectory 检查路径是否为目录
func IsDirectory(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// GetDirectorySize 获取目录大小（字节）
func GetDirectorySize(path string) (int64, error) {
	var size int64

	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})

	return size, err
}
