package constants

// 服务器配置常量
const (
	Port        = 9980
	HttpsPort   = 9443
	DownloadDir = "./downloads"
	PendingDir  = "./pending"
	LogDir      = "./logs"
	LogFile     = "server.log"
	// 飞牛系统部署路径配置
	ServerName = "Go 下载站"
	// 版本信息
	Version   = "v0.0.3"
	Developer = "gomail1"
	// HTTPS证书默认路径
	DefaultCertFile = "./ssl/cert.pem"
	DefaultKeyFile  = "./ssl/key.pem"
)

// 用户角色类型
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

// 用户角色类型
type UserRole int
