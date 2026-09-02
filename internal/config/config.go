package config

import (
	"fmt"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
	"go-download-server/internal/logger"
)

// Config holds all configuration for the application
type Config struct {
	Core     CoreConfig     `mapstructure:"core"`
	UI       UIConfig       `mapstructure:"ui"`
	Cow      CowConfig      `mapstructure:"cow"`
	Orange   OrangeConfig   `mapstructure:"orange"`
	Wanderer WandererConfig `mapstructure:"wanderer"`
	Sparkle  SparkleConfig  `mapstructure:"sparkle"`
	BT       BTConfig       `mapstructure:"bt"` // 添加BT配置部分
}

// CoreConfig holds core configuration
type CoreConfig struct {
	Name               string `mapstructure:"name"`
	Version            string `mapstructure:"version"`
	LogLevel           string `mapstructure:"log_level"`
	LogPath            string `mapstructure:"log_path"`
	MaxConcurrentTasks int    `mapstructure:"max_concurrent_tasks"`
	DefaultSavePath    string `mapstructure:"default_save_path"`
}

// UIConfig holds UI configuration
type UIConfig struct {
	Enabled            []string `mapstructure:"enabled"`
	WebEnabled         bool     `mapstructure:"web_enabled"`
	WebPort            int      `mapstructure:"web_port"`
	WebAuth            bool     `mapstructure:"web_auth"`
	TUIRefreshInterval string   `mapstructure:"tui_refresh_interval"`
}

// CowConfig holds cow stability configuration
type CowConfig struct {
	Stability StabilityConfig `mapstructure:"stability"`
}

// StabilityConfig holds stability configuration
type StabilityConfig struct {
	MaxRetries    int    `mapstructure:"max_retries"`
	RetryDelay    string `mapstructure:"retry_delay"`
	VerifyHash    bool   `mapstructure:"verify_hash"`
	ResumeEnabled bool   `mapstructure:"resume_enabled"`
}

// OrangeConfig holds orange efficiency configuration
type OrangeConfig struct {
	Efficiency EfficiencyConfig `mapstructure:"efficiency"`
}

// EfficiencyConfig holds efficiency configuration
type EfficiencyConfig struct {
	MaxThreads    int    `mapstructure:"max_threads"`
	ChunkStrategy string `mapstructure:"chunk_strategy"`
	BufferSize    string `mapstructure:"buffer_size"`
	PreAllocate   bool   `mapstructure:"pre_allocate"`
}

// WandererConfig holds wanderer network configuration
type WandererConfig struct {
	Network NetworkConfig `mapstructure:"network"`
}

// NetworkConfig holds network configuration
type NetworkConfig struct {
	DHTEnabled        bool     `mapstructure:"dht_enabled"`
	DHTBootstrapNodes []string `mapstructure:"dht_bootstrap_nodes"`
	MaxPeers          int      `mapstructure:"max_peers"`
	ListenPort        int      `mapstructure:"listen_port"`
}

// SparkleConfig holds sparkle parser configuration
type SparkleConfig struct {
	Parser ParserConfig `mapstructure:"parser"`
}

// ParserConfig holds parser configuration
type ParserConfig struct {
	AutoDetect bool `mapstructure:"auto_detect"`
	DeepParse  bool `mapstructure:"deep_parse"`
	SniffVideo bool `mapstructure:"sniff_video"`
	SniffAudio bool `mapstructure:"sniff_audio"`
}

// BTConfig holds BitTorrent protocol specific configuration
type BTConfig struct {
	Debug            bool   `mapstructure:"debug"`             // 是否启用BT调试模式
	DebugLogPath     string `mapstructure:"debug_log_path"`    // 调试日志路径
	MetadataTimeout  int    `mapstructure:"metadata_timeout"`  // 元数据获取超时时间(秒)
	ProgressInterval int    `mapstructure:"progress_interval"` // 进度更新间隔(毫秒)
}

// GlobalConfig holds the global configuration instance
var GlobalConfig Config

// Init initializes the configuration
func Init(configPath string) error {
	v := viper.New()

	// Set default values
	setDefaults(v)

	// Set config file
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		// Set default config paths
		v.SetConfigName("config")
		v.SetConfigType("toml")
		v.AddConfigPath("./")
		v.AddConfigPath("~/.config/qf/")
		v.AddConfigPath("/etc/qf/")
	}

	// Read config file
	err := v.ReadInConfig()
	if err != nil {
		// Config file not found is not an error, use defaults
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			logger.Warn("No config file found, using default values")
		} else {
			return err
		}
	} else {
		// Watch config file for changes
		v.WatchConfig()
		v.OnConfigChange(func(e fsnotify.Event) {
			logger.Info(fmt.Sprintf("Config file changed: %s, reloading...", e.Name))

			// Create a temporary config for validation
			var tempConfig Config
			err := v.Unmarshal(&tempConfig)
			if err != nil {
				logger.Errorf("Failed to reload config: %v", err)
				return
			}

			// Validate the new config
			err = ValidateConfig(tempConfig)
			if err != nil {
				logger.Errorf("Invalid config after change: %v", err)
				return
			}

			// Apply the new config
			GlobalConfig = tempConfig

			logger.Info("Config reloaded successfully")
			// Update log level
			logger.Init(GlobalConfig.Core.LogLevel)
		})
	}

	// Unmarshal the config (always do this, even if config file not found)
	err = v.Unmarshal(&GlobalConfig)
	if err != nil {
		return fmt.Errorf("unable to decode config: %v", err)
	}

	// Validate the config
	err = ValidateConfig(GlobalConfig)
	if err != nil {
		return err
	}

	return nil
}

// setDefaults sets default values for the configuration
func setDefaults(v *viper.Viper) {
	// Core defaults
	v.SetDefault("core.name", "QuadFetch")
	v.SetDefault("core.version", "1.0.0")
	v.SetDefault("core.log_level", "info")
	v.SetDefault("core.log_path", "logs/qf.log")
	v.SetDefault("core.max_concurrent_tasks", 5)
	v.SetDefault("core.default_save_path", "pending/download-user")

	// UI defaults
	v.SetDefault("ui.enabled", []string{"cli", "tui"})
	v.SetDefault("ui.web_enabled", false)
	v.SetDefault("ui.web_port", 8080)
	v.SetDefault("ui.web_auth", false)
	v.SetDefault("ui.tui_refresh_interval", "500ms")

	// Cow defaults
	v.SetDefault("cow.stability.max_retries", 3)
	v.SetDefault("cow.stability.retry_delay", "5s")
	v.SetDefault("cow.stability.verify_hash", true)
	v.SetDefault("cow.stability.resume_enabled", true)

	// Orange defaults
	v.SetDefault("orange.efficiency.max_threads", 8)
	v.SetDefault("orange.efficiency.chunk_strategy", "dynamic")
	v.SetDefault("orange.efficiency.buffer_size", "16MB")
	v.SetDefault("orange.efficiency.pre_allocate", true)

	// Wanderer defaults
	v.SetDefault("wanderer.network.dht_enabled", true)
	v.SetDefault("wanderer.network.dht_bootstrap_nodes", []string{
		"router.bittorrent.com:6881",
		"dht.transmissionbt.com:6881",
		"router.utorrent.com:6881",
		"dht.libtorrent.org:25401",
		"dht.anacrolix.com:42069",
	})
	v.SetDefault("wanderer.network.max_peers", 50)
	v.SetDefault("wanderer.network.listen_port", 58888)

	// Sparkle defaults
	v.SetDefault("sparkle.parser.auto_detect", true)
	v.SetDefault("sparkle.parser.deep_parse", false)
	v.SetDefault("sparkle.parser.sniff_video", true)
	v.SetDefault("sparkle.parser.sniff_audio", true)

	// BT defaults
	v.SetDefault("bt.debug", false)           // 默认禁用调试模式
	v.SetDefault("bt.debug_log_path", "")     // 默认使用主日志
	v.SetDefault("bt.metadata_timeout", 30)   // 默认30秒超时
	v.SetDefault("bt.progress_interval", 500) // 默认500毫秒更新间隔
}

// Get returns the global configuration
func Get() Config {
	return GlobalConfig
}

// ValidateConfig validates the configuration
func ValidateConfig(cfg Config) error {
	// Validate core config
	if cfg.Core.MaxConcurrentTasks <= 0 {
		return fmt.Errorf("max_concurrent_tasks must be greater than 0")
	}

	// Validate BT config
	if cfg.BT.MetadataTimeout <= 0 {
		return fmt.Errorf("bt.metadata_timeout must be greater than 0")
	}
	if cfg.BT.ProgressInterval <= 0 {
		return fmt.Errorf("bt.progress_interval must be greater than 0")
	}

	return nil
}
