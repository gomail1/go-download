package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go-download-server/constants"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// 配置文件结构体
type UserConfig struct {
	Username           string `json:"username"`
	Password           string `json:"password"`
	Role               string `json:"role"`
	MaxFileSize        int64  `json:"max_file_size"`
	AgreedToTerms      bool   `json:"agreed_to_terms"`
	AgreedTermsVersion string `json:"agreed_terms_version"`
	AgreedTermsTime    string `json:"agreed_terms_time"`
}

type ServerConfig struct {
	Port        int    `json:"port"`
	HttpsPort   int    `json:"https_port"`
	CertFile    string `json:"cert_file"`
	KeyFile     string `json:"key_file"`
	DownloadDir string `json:"download_dir"`
	PendingDir  string `json:"pending_dir"`
	LogDir      string `json:"log_dir"`
	IconCacheDir string `json:"icon_cache_dir"`
	// TrustProxy 仅当部署于可信反向代理（nginx 等）之后时置 true，
	// 此时才信任 X-Forwarded-For / X-Real-IP 头；直连部署保持 false，
	// 否则攻击者可伪造这些头绕过 IP 封禁与限流。
	TrustProxy bool `json:"trust_proxy"`
	// APIKey 外部程序调用 /api/stats 等接口的独立密钥（可选）。
	// 配置后优先使用该密钥认证，不再用管理员密码；未配置时保持向后兼容。
	APIKey string `json:"api_key"`
	LogFile     string `json:"log_file"`
	ServerName  string `json:"server_name"`
}

// QuadFetch 服务配置
type QuadFetchConfig struct {
	Enabled     bool   `json:"enabled"`
	APIBaseURL  string `json:"api_base_url"`
	SecretKey   string `json:"secret_key,omitempty"`
	DefaultUser string `json:"default_user"`
}

// 免责协议配置
type LegalConfig struct {
	TermsEnabled bool   `json:"terms_enabled"`
	TermsVersion string `json:"terms_version"`
	TermsContent string `json:"terms_content"`
	FooterText   string `json:"footer_text"`
	BrowserTips  string `json:"browser_tips"`
}

// IP流量限额配置
type IPLimitConfig struct {
	Enabled            bool   `json:"enabled"`              // 是否启用IP限额
	DailyMaxBandwidth  int64  `json:"daily_max_bandwidth"`  // 每个IP每天最大下载流量（字节），0表示不限制
	DailyMaxDownloads  int64  `json:"daily_max_downloads"`  // 每个IP每天最大下载次数，0表示不限制
	HourlyMaxBandwidth int64  `json:"hourly_max_bandwidth"` // 每个IP每小时最大下载流量（字节），0表示不限制
	HourlyMaxDownloads int64  `json:"hourly_max_downloads"` // 每个IP每小时最大下载次数，0表示不限制
	AutoBlock          bool   `json:"auto_block"`           // 超过限额是否自动封禁IP
	AutoBlockReason    string `json:"auto_block_reason"`    // 自动封禁原因
}

type Config struct {
	Users     []UserConfig    `json:"users"`
	Server    ServerConfig    `json:"server"`
	QuadFetch QuadFetchConfig `json:"quadfetch"`
	Legal     LegalConfig     `json:"legal"`
	IPLimit   IPLimitConfig   `json:"ip_limit"`
}

// 全局配置实例
var AppConfig Config

// 用户配置映射
var UserConfigMap map[string]UserConfig

// UsersMu 保护 AppConfig.Users 与 UserConfigMap 的并发读写
var UsersMu sync.RWMutex

// GetUserCount 返回当前用户数量（并发安全）
func GetUserCount() int {
	UsersMu.RLock()
	defer UsersMu.RUnlock()
	return len(AppConfig.Users)
}

// SyncUserConfigMap 将 AppConfig.Users 重新同步到 UserConfigMap。
// 必须在修改 AppConfig.Users（增/删/改）后调用，否则登录鉴权与 session 校验
// 仍使用旧的 UserConfigMap 数据（历史 bug：改密后旧密码仍可登录）。
func SyncUserConfigMap() {
	UsersMu.Lock()
	defer UsersMu.Unlock()
	syncUserConfigMapLocked()
}

// SyncUserConfigMapLocked 在已持有 UsersMu 写锁的前提下同步映射。
// 由 handlers 包在 UsersMu.Lock() 保护修改 AppConfig.Users 后调用，
// 避免重复加锁导致自死锁（Go sync.Mutex 不可重入）。
func SyncUserConfigMapLocked() {
	syncUserConfigMapLocked()
}

// syncUserConfigMapLocked 在已持有 UsersMu 写锁的前提下同步映射
func syncUserConfigMapLocked() {
	newMap := make(map[string]UserConfig, len(AppConfig.Users))
	for _, user := range AppConfig.Users {
		// 设置默认的最大文件大小，如果配置文件中没有指定或者指定为0
		if user.MaxFileSize == 0 {
			switch strings.ToLower(user.Role) {
			case "admin":
				user.MaxFileSize = constants.MaxFileSizeUnlimited
			case "normal":
				user.MaxFileSize = constants.MaxFileSizeNormal
			}
		}
		newMap[user.Username] = user
	}
	UserConfigMap = newMap
}

// 飞牛系统环境检测
func IsFeiniuSystem() bool {
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
func GetExecDir() string {
	if IsFeiniuSystem() {
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

// 加载配置文件
func LoadConfig() error {
	// 首先尝试从当前工作目录加载配置文件
	currentDir, err := os.Getwd()
	var configPath string
	var file *os.File

	// 尝试从当前工作目录加载
	if err == nil {
		configPath = filepath.Join(currentDir, "config/config.json")
		file, err = os.Open(configPath)
	}

	// 如果当前工作目录没有配置文件，尝试从执行目录加载
	if err != nil {
		configPath = filepath.Join(GetExecDir(), "config/config.json")
		file, err = os.Open(configPath)
		if err != nil {
			return fmt.Errorf("无法打开配置文件: %w", err)
		}
	}
	defer file.Close()

	// 解析配置文件
	if err := json.NewDecoder(file).Decode(&AppConfig); err != nil {
		return fmt.Errorf("无法解析配置文件: %w", err)
	}

	// 初始化用户配置映射
	UserConfigMap = make(map[string]UserConfig)
	for _, user := range AppConfig.Users {
		// 设置默认的最大文件大小，如果配置文件中没有指定或者指定为0
		if user.MaxFileSize == 0 {
			switch strings.ToLower(user.Role) {
			case "admin":
				user.MaxFileSize = constants.MaxFileSizeUnlimited
			case "normal":
				user.MaxFileSize = constants.MaxFileSizeNormal
			}
		}
		UserConfigMap[user.Username] = user
	}

	// 设置默认的服务器配置
	if AppConfig.Server.Port == 0 {
		AppConfig.Server.Port = constants.Port
	}

	// 设置默认的HTTPS配置
	if AppConfig.Server.HttpsPort == 0 {
		AppConfig.Server.HttpsPort = constants.HttpsPort
	}
	if AppConfig.Server.CertFile == "" {
		AppConfig.Server.CertFile = constants.DefaultCertFile
	}
	if AppConfig.Server.KeyFile == "" {
		AppConfig.Server.KeyFile = constants.DefaultKeyFile
	}
	if AppConfig.Server.ServerName == "" {
		AppConfig.Server.ServerName = constants.ServerName
	}

	// 设置默认的图标缓存目录（存储在配置目录下，升级时无需新增路径映射）
	if AppConfig.Server.IconCacheDir == "" {
		AppConfig.Server.IconCacheDir = "./config/icons/cache"
	}

	// 设置默认的QuadFetch服务配置
	if AppConfig.QuadFetch.Enabled == false {
		AppConfig.QuadFetch.Enabled = true
	}
	if AppConfig.QuadFetch.APIBaseURL == "" {
		AppConfig.QuadFetch.APIBaseURL = "http://localhost:8080/api"
	}
	if AppConfig.QuadFetch.DefaultUser == "" {
		AppConfig.QuadFetch.DefaultUser = "download-user"
	}

	// 设置默认的免责协议配置
	if AppConfig.Legal.TermsEnabled == false {
		AppConfig.Legal.TermsEnabled = true
	}
	if AppConfig.Legal.TermsVersion == "" {
		AppConfig.Legal.TermsVersion = "1.0"
	}
	if AppConfig.Legal.TermsContent == "" {
		AppConfig.Legal.TermsContent = "欢迎使用本服务。使用本服务即表示您同意遵守所有条款和条件。"
	}
	if AppConfig.Legal.FooterText == "" {
		AppConfig.Legal.FooterText = "© 2026 Go 下载站 · 保留所有权利 · 本网站提供的文件均来源于互联网渠道"
	}
	if AppConfig.Legal.BrowserTips == "" {
		AppConfig.Legal.BrowserTips = "建议您使用Edge、Chrome 80+、FireFox 86+、360极速模式等主流浏览器浏览本网站"
	}

	// 处理飞牛环境下的目录路径
	if IsFeiniuSystem() {
		// 确保在飞牛环境下使用绝对路径或相对于正确工作目录的路径
		baseDir := GetExecDir()

		// 转换相对路径为绝对路径
		if !filepath.IsAbs(AppConfig.Server.DownloadDir) {
			AppConfig.Server.DownloadDir = filepath.Join(baseDir, AppConfig.Server.DownloadDir)
		}
		if !filepath.IsAbs(AppConfig.Server.PendingDir) {
			AppConfig.Server.PendingDir = filepath.Join(baseDir, AppConfig.Server.PendingDir)
		}
		if !filepath.IsAbs(AppConfig.Server.LogDir) {
			AppConfig.Server.LogDir = filepath.Join(baseDir, AppConfig.Server.LogDir)
		}
		if !filepath.IsAbs(AppConfig.Server.IconCacheDir) {
			AppConfig.Server.IconCacheDir = filepath.Join(baseDir, AppConfig.Server.IconCacheDir)
		}
		if !filepath.IsAbs(AppConfig.Server.CertFile) {
			AppConfig.Server.CertFile = filepath.Join(baseDir, AppConfig.Server.CertFile)
		}
		if !filepath.IsAbs(AppConfig.Server.KeyFile) {
			AppConfig.Server.KeyFile = filepath.Join(baseDir, AppConfig.Server.KeyFile)
		}
	}

	return nil
}

// 保存配置文件
func SaveConfig() error {
	// 序列化时加读锁，避免与用户增删改并发导致 JSON 撕裂
	UsersMu.RLock()
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(&AppConfig); err != nil {
		UsersMu.RUnlock()
		return fmt.Errorf("无法序列化配置: %w", err)
	}
	UsersMu.RUnlock()

	// 首先尝试保存到当前工作目录
	currentDir, err := os.Getwd()
	if err == nil {
		configDir := filepath.Join(currentDir, "config")
		// 确保当前工作目录下的config目录存在
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return fmt.Errorf("无法创建当前工作目录的config目录: %w", err)
		}

		configPath := filepath.Join(configDir, "config.json")
		if err := atomicWriteFile(configPath, buf.Bytes(), 0644); err == nil {
			return nil
		}
	}

	// 如果当前工作目录保存失败，尝试保存到执行目录
	execDir := GetExecDir()
	configDir := filepath.Join(execDir, "config")
	// 确保执行目录下的config目录存在
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("无法创建执行目录的config目录: %w", err)
	}

	configPath := filepath.Join(configDir, "config.json")
	if err := atomicWriteFile(configPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("无法写入配置文件: %w", err)
	}

	return nil
}

// atomicWriteFile 原子写文件：先写临时文件再 rename，避免崩溃时损坏原文件
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmpPath := path + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}
