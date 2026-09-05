package utils

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// SafePath 安全路径验证结果
type SafePath struct {
	FullPath     string // 完整绝对路径
	RelativePath string // 相对于基础目录的相对路径
	IsSafe       bool   // 是否安全
	Error        error  // 错误信息
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

// MaxFilenameLength 文件名最大长度（含扩展名），避免 Windows 260 路径上限与文件系统限制。
const MaxFilenameLength = 200

// SanitizeRemoteFilename 清洗来自远程（HTTP 头 / URL / BT 元数据 / FTP）的文件名，
// 防御路径穿越（../、绝对路径、分隔符）、控制字符、Windows 保留名与超长文件名。
// 返回可直接用于 filepath.Join 的安全文件名。
func SanitizeRemoteFilename(name string) string {
	// 1. 先用 Base 剥离任何目录前缀（含 ../）
	name = filepath.Base(name)
	// 2. 过滤 Windows / 类 Unix 非法与危险字符
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", "..", "_",
		"<", "_", ">", "_", ":", "_", "\"", "_",
		"|", "_", "?", "_", "*", "_", "\x00", "_",
	)
	name = replacer.Replace(name)
	// 3. 去除控制字符（0x00-0x1F、0x7F）
	name = removeControlChars(name)
	// 4. 去除首尾空白与多余点
	name = strings.TrimSpace(name)
	name = strings.Trim(name, ".")
	// 5. Windows 保留名（CON/PRN/AUX/NUL/COM1-9/LPT1-9）
	name = sanitizeReservedName(name)
	// 6. 长度截断（保留扩展名）
	if len(name) > MaxFilenameLength {
		ext := filepath.Ext(name)
		keep := MaxFilenameLength - len(ext)
		if keep < 1 {
			keep = MaxFilenameLength
			ext = ""
		}
		// 按字节截断可能切断多字节 UTF-8 字符（中文/emoji 文件名），
		// 导致 Linux 落盘非法字节、Windows 转 UTF-16 时乱码；
		// 从截断点向前回退到完整字符边界，保证始终是合法 UTF-8。
		cut := keep
		for cut > 0 && !utf8.RuneStart(name[cut]) {
			cut--
		}
		name = name[:cut] + ext
	}
	// 7. 兜底
	if name == "" || name == "." {
		name = "download"
	}
	return name
}

// removeControlChars 移除字符串中的控制字符。
func removeControlChars(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// sanitizeReservedName 为 Windows 保留名添加前缀，避免写入被拒绝或覆盖设备文件。
func sanitizeReservedName(name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	upper := strings.ToUpper(base)
	for _, r := range []string{"CON", "PRN", "AUX", "NUL"} {
		if upper == r {
			return "_" + name
		}
	}
	for i := 1; i <= 9; i++ {
		if upper == fmt.Sprintf("COM%d", i) || upper == fmt.Sprintf("LPT%d", i) {
			return "_" + name
		}
	}
	return name
}

// IsSafePathComponent 判断一个（目录/文件）名是否可作为安全的路径分量，
// 用于拒绝 BT 种子等外部可控名称中的路径穿越。
// 允许内部出现的正常子目录分隔（如 "a/b"），但禁止 ".."、绝对路径、反斜杠。
func IsSafePathComponent(name string) bool {
	if name == "" {
		return false
	}
	if strings.Contains(name, "..") {
		return false
	}
	if strings.ContainsAny(name, "\\") {
		return false
	}
	if filepath.IsAbs(name) {
		return false
	}
	return true
}

// EnsureWithinDir 判断 fullPath 是否位于 baseDir 之内（防目录逃逸）。
func EnsureWithinDir(baseDir, fullPath string) bool {
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absBase, absPath)
	if err != nil {
		return false
	}
	return rel != ".." && rel != "." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// SafeJoinFile 先清洗文件名再拼接，并校验结果仍在 baseDir 内。
func SafeJoinFile(baseDir, name string) (string, error) {
	clean := SanitizeRemoteFilename(name)
	joined := filepath.Join(baseDir, clean)
	if !EnsureWithinDir(baseDir, joined) {
		return "", fmt.Errorf("非法的文件路径: %s", name)
	}
	return joined, nil
}

// isPublicIP 判断 IP 是否为公网地址（拒绝回环/私网/链路本地/未指定/组播等）。
func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	return true
}

// ValidateRemoteDownloadURL 校验远程下载 URL，防止 SSRF。
// 仅允许 http/https/ftp/magnet 协议；对 http/https/ftp 解析目标主机，
// 拒绝任何解析到内网/回环/链路本地/未指定地址的 IP。
func ValidateRemoteDownloadURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("URL 解析失败: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "ftp":
		// 需要校验主机
	case "magnet":
		return nil // 磁力链接走 P2P/DHT，无直接主机连接
	default:
		return fmt.Errorf("不支持的协议: %s", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("缺少主机名")
	}
	// 若已是 IP 字面量，直接校验
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return fmt.Errorf("目标地址为内网/保留地址，已拒绝: %s", ip)
		}
		return nil
	}
	// 域名：解析后逐个校验（fail-closed）
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("无法解析主机: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("主机无可用 IP: %s", host)
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return fmt.Errorf("主机 %s 解析到内网/保留地址 %s，已拒绝", host, ip)
		}
	}
	return nil
}

// SafeDialContext 防 SSRF 的拨号函数：每次连接前重新解析主机并校验 IP 非内网/保留地址，
// 可防御重定向绕过与 DNS rebinding（每次 dial 都重新解析并校验真实 IP）。
func SafeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return nil, fmt.Errorf("拒绝连接到内网/保留地址: %s", ip)
		}
		return (&net.Dialer{Timeout: 30 * time.Second}).DialContext(ctx, network, addr)
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return nil, fmt.Errorf("拒绝连接到内网/保留地址: %s", ip)
		}
	}
	return (&net.Dialer{Timeout: 30 * time.Second}).DialContext(ctx, network, addr)
}
