package constants

// 服务器配置常量
const (
	Port        = 9980
	HttpsPort   = 1443
	DownloadDir = "./downloads"
	PendingDir  = "./pending"
	LogDir      = "./logs"
	LogFile     = "server.log"
	// 飞牛系统部署路径配置
	ServerName = "Go 下载站"
	Version    = "v1.2.0"
	Developer  = "gomail1"
	RepoURL    = "https://github.com/gomail1/go-download"
	// HTTPS证书默认路径
	DefaultCertFile      = "./ssl/cert.pem"
	DefaultKeyFile       = "./ssl/key.pem"
	MaxFileSizeUnlimited = -1 // 表示文件大小无限制
	// 文件大小限制
	MaxFileSizeNormal int64 = 20 * 1024 * 1024 * 1024 // 20GB - 单个文件最大大小
	DailyUploadLimit  int64 = 20 * 1024 * 1024 * 1024 // 20GB - 每日上传限制
)

// 用户角色类型
type UserRole int

// 用户角色常量
const (
	RoleNormal   UserRole = iota // 普通用户
	RoleAdmin                    // 管理员
	RoleSubAdmin                 // 二级管理员
)

// 权限常量
const (
	// 文件操作权限
	PermissionViewFiles     = 1 << iota // 查看文件列表
	PermissionUploadFiles               // 上传文件
	PermissionDownloadFiles             // 下载文件
	PermissionDeleteFiles               // 删除文件
	PermissionCreateDir                 // 创建目录
	PermissionApproveFiles              // 审核文件
	PermissionManageUsers               // 用户管理
	PermissionViewLogs                  // 查看日志
	PermissionViewStats                 // 查看统计信息
)

// 默认权限配置
const (
	// 管理员默认权限
	DefaultAdminPermissions = PermissionViewFiles | PermissionUploadFiles | PermissionDownloadFiles |
		PermissionDeleteFiles | PermissionCreateDir | PermissionApproveFiles |
		PermissionManageUsers | PermissionViewLogs | PermissionViewStats

	// 二级管理员默认权限
	DefaultSubAdminPermissions = PermissionViewFiles | PermissionUploadFiles | PermissionDownloadFiles |
		PermissionDeleteFiles | PermissionCreateDir | PermissionApproveFiles |
		PermissionViewLogs

	// 普通用户默认权限
	DefaultNormalPermissions = PermissionViewFiles | PermissionUploadFiles | PermissionDownloadFiles
)
