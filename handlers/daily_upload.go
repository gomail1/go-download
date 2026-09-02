package handlers

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go-download-server/config"
	"go-download-server/constants"
)

// DailyUploadInfo 存储用户每日上传信息
type DailyUploadInfo struct {
	UploadedSize int64     `json:"uploaded_size"` // 今日已上传大小
	LastUpdated  time.Time `json:"last_updated"`  // 最后更新时间
}

// UploadMap 定义用户上传信息映射类型
type UploadMap map[string]*DailyUploadInfo

// 存储所有用户的每日上传信息
var (
	dailyUploadMap   UploadMap = make(map[string]*DailyUploadInfo)
	dailyUploadMutex sync.Mutex
	// 数据文件路径，统一放到config目录
	dailyUploadDataFile = filepath.Join("config", "daily_upload.json")
)

// InitDailyUpload 初始化每日上传数据
func InitDailyUpload() {
	// 创建config目录（如果不存在）
	os.MkdirAll("config", 0755)

	// 加载保存的数据
	loadDailyUploadData()

	// 计算今日已上传文件的总大小
	calculateTodayUploadSize()

	// 启动定时器，每天午夜重置上传量
	go startDailyResetTimer()
}

// 保存每日上传数据到磁盘
func saveDailyUploadData() {
	dailyUploadMutex.Lock()
	defer dailyUploadMutex.Unlock()

	// 创建临时映射，只保存今天的数据
	today := time.Now().Format("2006-01-02")
	dataToSave := make(map[string]*DailyUploadInfo)

	for username, info := range dailyUploadMap {
		// 只保存今天的数据
		if info.LastUpdated.Format("2006-01-02") == today {
			dataToSave[username] = info
		}
	}

	// 序列化数据
	data, err := json.MarshalIndent(dataToSave, "", "  ")
	if err != nil {
		log.Printf("保存每日上传数据失败: %v\n", err)
		return
	}

	// 确保config目录存在
	os.MkdirAll("config", 0755)

	// 写入文件
	if err := os.WriteFile(dailyUploadDataFile, data, 0644); err != nil {
		log.Printf("写入每日上传数据文件失败: %v\n", err)
	}
}

// 从磁盘加载每日上传数据
func loadDailyUploadData() {
	dailyUploadMutex.Lock()
	defer dailyUploadMutex.Unlock()

	// 检查文件是否存在
	if _, err := os.Stat(dailyUploadDataFile); os.IsNotExist(err) {
		log.Printf("每日上传数据文件不存在，将重新计算: %v\n", err)
		return
	}

	// 读取文件内容
	data, err := os.ReadFile(dailyUploadDataFile)
	if err != nil {
		log.Printf("读取每日上传数据文件失败: %v\n", err)
		return
	}

	// 反序列化数据
	var savedData map[string]*DailyUploadInfo
	if err := json.Unmarshal(data, &savedData); err != nil {
		log.Printf("解析每日上传数据失败: %v\n", err)
		return
	}

	// 检查并过滤过期数据
	today := time.Now().Format("2006-01-02")
	for username, info := range savedData {
		// 只保留今天的数据
		if info.LastUpdated.Format("2006-01-02") == today {
			dailyUploadMap[username] = info
		}
	}
}

// 启动每日重置定时器
func startDailyResetTimer() {
	for {
		// 计算到明天凌晨的时间
		tomorrow := time.Now().Add(24 * time.Hour)
		resetTime := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, tomorrow.Location())
		duration := resetTime.Sub(time.Now())

		// 等待到明天凌晨
		time.Sleep(duration)

		// 重置所有用户的上传量
		dailyUploadMutex.Lock()
		for _, info := range dailyUploadMap {
			info.UploadedSize = 0
			info.LastUpdated = time.Now()
		}
		dailyUploadMutex.Unlock()

		// 保存重置后的数据
		saveDailyUploadData()

		// 重新计算今日已上传大小
		calculateTodayUploadSize()
	}
}

// 计算今日已上传文件的总大小
func calculateTodayUploadSize() {
	today := time.Now().Format("2006-01-02")

	// 检查配置中的目录路径
	pendingRoot := config.AppConfig.Server.PendingDir
	downloadRoot := config.AppConfig.Server.DownloadDir

	// 创建临时映射存储今日上传大小
	userUploadMap := make(map[string]int64)

	// 1. 计算待审核目录中的文件
	// 遍历所有用户目录
	userDirs, err := os.ReadDir(pendingRoot)
	if err == nil {
		// 遍历每个用户目录
		for _, userDir := range userDirs {
			if !userDir.IsDir() {
				continue
			}

			username := userDir.Name()
			userPendingDir := filepath.Join(pendingRoot, username)

			// 遍历用户待审核目录中的所有文件
			walkErr := filepath.Walk(userPendingDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !info.IsDir() {
					// 跳过BT客户端的临时数据库文件
					if info.Name() == ".torrent.bolt.db" {
						return nil
					}
					// 检查文件修改时间是否为今天
					fileDate := info.ModTime().Format("2006-01-02")
					if fileDate == today {
						// 累加文件大小
						userUploadMap[username] += info.Size()
					}
				}
				return nil
			})

			if walkErr != nil {
				log.Printf("遍历用户 %s 的待审核目录失败: %v\n", username, walkErr)
			}
		}
	} else {
		log.Printf("无法读取待审核目录: %v\n", err)
	}

	// 2. 计算已审核目录中的文件
	// 遍历下载目录中的所有文件
	walkErr := filepath.Walk(downloadRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			// 检查文件修改时间是否为今天
			fileDate := info.ModTime().Format("2006-01-02")
			if fileDate == today {
				// 假设所有已审核文件都是admin上传的
				userUploadMap["admin"] += info.Size()
			}
		}
		return nil
	})

	if walkErr != nil {
		log.Printf("遍历下载目录失败: %v\n", walkErr)
	}

	// 更新每日上传数据
	dailyUploadMutex.Lock()
	for username, size := range userUploadMap {
		// 获取或创建用户上传信息
		info, exists := dailyUploadMap[username]
		if !exists {
			info = &DailyUploadInfo{
				UploadedSize: 0,
				LastUpdated:  time.Now(),
			}
			dailyUploadMap[username] = info
		}

		// 只有当计算出的大小大于当前记录时才更新（避免覆盖已保存的数据）
		if size > info.UploadedSize {
			info.UploadedSize = size
			info.LastUpdated = time.Now()
		}
	}
	dailyUploadMutex.Unlock()

	// 保存数据到磁盘
	saveDailyUploadData()
}

// AddDailyUpload 添加用户今日上传大小
func AddDailyUpload(username string, size int64) {
	dailyUploadMutex.Lock()

	// 获取当前日期
	currentDate := time.Now().Format("2006-01-02")

	// 获取用户上传信息
	info, exists := dailyUploadMap[username]
	if !exists {
		// 创建新的上传信息
		info = &DailyUploadInfo{
			UploadedSize: 0,
			LastUpdated:  time.Now(),
		}
		dailyUploadMap[username] = info
	} else {
		// 检查是否需要重置（跨天）
		if info.LastUpdated.Format("2006-01-02") != currentDate {
			// 重置上传信息
			info.UploadedSize = 0
			info.LastUpdated = time.Now()
		}
	}

	// 添加上传大小
	info.UploadedSize += size
	info.LastUpdated = time.Now()
	dailyUploadMutex.Unlock()

	// 立即落盘，避免崩溃/重启后当日上传量丢失导致限额被绕过
	// （上传操作频率不高，单次小 JSON 写入开销可忽略）
	saveDailyUploadData()
}

// GetDailyUpload 获取用户今日已上传大小
func GetDailyUpload(username string) int64 {
	dailyUploadMutex.Lock()
	defer dailyUploadMutex.Unlock()

	// 获取当前日期
	currentDate := time.Now().Format("2006-01-02")

	// 获取用户上传信息
	info, exists := dailyUploadMap[username]
	if !exists {
		return 0
	}

	// 检查是否需要重置（跨天）
	if info.LastUpdated.Format("2006-01-02") != currentDate {
		// 重置上传信息
		info.UploadedSize = 0
		info.LastUpdated = time.Now()

		// 不立即保存数据到磁盘，减少I/O操作
		// 数据会在定时器重置时保存
		// saveDailyUploadData()
		return 0
	}

	return info.UploadedSize
}

// CheckDailyUploadLimit 检查用户是否超过每日上传限制
func CheckDailyUploadLimit(username string, size int64) (bool, int64) {
	// 管理员没有上传限制
	if username == "admin" {
		return false, 0
	}

	// 获取今日已上传大小
	uploaded := GetDailyUpload(username)

	// 检查是否超过限制
	if uploaded+size > constants.DailyUploadLimit {
		return true, uploaded
	}

	return false, uploaded
}

// GetRemainingUpload 获取用户今日剩余可上传大小
func GetRemainingUpload(username string) int64 {
	// 管理员没有上传限制
	if username == "admin" {
		return constants.MaxFileSizeUnlimited
	}

	// 获取今日已上传大小
	uploaded := GetDailyUpload(username)
	remaining := constants.DailyUploadLimit - uploaded
	if remaining < 0 {
		remaining = 0
	}

	return remaining
}
