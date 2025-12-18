package config

import (
	"encoding/json"
	"fmt"
	"go-download-server/constants"
	"os"
	"path/filepath"
	"strings"
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
	HttpsPort   int    `json:"https_port"`
	CertFile    string `json:"cert_file"`
	KeyFile     string `json:"key_file"`
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
var AppConfig Config

// 用户配置映射
var UserConfigMap map[string]UserConfig

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
	if err == nil {
		configPath := filepath.Join(currentDir, "config/config.json")
		file, err := os.Open(configPath)
		if err == nil {
			defer file.Close()
			// 解析配置文件
			if err := json.NewDecoder(file).Decode(&AppConfig); err == nil {
				// 初始化用户配置映射
				UserConfigMap = make(map[string]UserConfig)
				for _, user := range AppConfig.Users {
					UserConfigMap[user.Username] = user
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

				return nil
			}
		}
	}

	// 如果当前工作目录没有配置文件，再尝试从执行目录加载
	configPath := filepath.Join(GetExecDir(), "config/config.json")
	file, err := os.Open(configPath)
	if err != nil {
		return fmt.Errorf("无法打开配置文件: %w", err)
	}
	defer file.Close()

	// 解析配置文件
	if err := json.NewDecoder(file).Decode(&AppConfig); err != nil {
		return fmt.Errorf("无法解析配置文件: %w", err)
	}

	// 初始化用户配置映射
	UserConfigMap = make(map[string]UserConfig)
	for _, user := range AppConfig.Users {
		UserConfigMap[user.Username] = user
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

	return nil
}

// 保存配置文件
func SaveConfig() error {
	// 确保config目录存在
	if err := os.MkdirAll("config", 0755); err != nil {
		return fmt.Errorf("无法创建config目录: %w", err)
	}

	// 首先尝试保存到当前工作目录
	currentDir, err := os.Getwd()
	if err == nil {
		configPath := filepath.Join(currentDir, "config/config.json")
		file, err := os.Create(configPath)
		if err == nil {
			defer file.Close()
			// 将配置序列化为JSON格式并写入文件
			encoder := json.NewEncoder(file)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(&AppConfig); err == nil {
				return nil
			}
		}
	}

	// 如果当前工作目录保存失败，尝试保存到执行目录
	configPath := filepath.Join(GetExecDir(), "config/config.json")
	file, err := os.Create(configPath)
	if err != nil {
		return fmt.Errorf("无法创建配置文件: %w", err)
	}
	defer file.Close()

	// 将配置序列化为JSON格式并写入文件
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(&AppConfig); err != nil {
		return fmt.Errorf("无法写入配置文件: %w", err)
	}

	return nil
}
