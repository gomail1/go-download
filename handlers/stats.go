package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go-download-server/utils"
)

// FileStats 存储单个文件的统计信息
type FileStats struct {
	Path             string    `json:"path"`                  // 文件路径
	ShareCount       int64     `json:"share_count"`           // 分享次数
	DownloadCount    int64     `json:"download_count"`        // 下载次数
	LastShareTime    time.Time `json:"last_share_time"`       // 最后分享时间
	LastDownloadTime time.Time `json:"last_download_time"`    // 最后下载时间
	TotalBandwidth   int64     `json:"total_bandwidth"`       // 总流量消耗
	UploadTime       time.Time `json:"upload_time,omitempty"` // 上传时间
}

// 热力图数据保留天数
const heatmapRetentionDays = 30

// 在StatsData结构体中添加每日流量统计
// StatsData 存储所有统计数据
type StatsData struct {
	FileStatsMap map[string]*FileStats `json:"file_stats_map"` // 文件统计映射
	HeatmapData  []HeatmapPoint        `json:"heatmap_data"`   // 热力图数据点
	// DailyTrafficMap map[string]*DailyTrafficStats `json:"daily_traffic_map"` // 每日流量统计
}

// HeatmapPoint 热力图数据点
type HeatmapPoint struct {
	Type          string    `json:"type"`                     // 类型：share/download/upload/admin_action
	Path          string    `json:"path"`                     // 文件路径
	Timestamp     time.Time `json:"timestamp"`                // 时间戳
	IP            string    `json:"ip,omitempty"`             // IP地址（可选，会被匿名化处理）
	UserAgent     string    `json:"user_agent,omitempty"`     // 用户代理（可选）
	Username      string    `json:"username,omitempty"`       // 用户名（可选）
	FileSize      int64     `json:"file_size,omitempty"`      // 文件大小（可选）
	ActionDetails string    `json:"action_details,omitempty"` // 操作详情（可选）
}

// 全局变量
var (
	statsData StatsData = StatsData{
		FileStatsMap: make(map[string]*FileStats),
		HeatmapData:  make([]HeatmapPoint, 0),
		// DailyTrafficMap: make(map[string]*DailyTrafficStats), // 已经移除
	}
	statsMutex    sync.RWMutex // 使用读写锁替代互斥锁，提高并发性能
	statsDataFile string       // 将在InitStats中初始化为绝对路径
	needsSave     bool         // 标志位，表示数据是否需要保存
)

// cleanupOldHeatmapData 清理旧的热力图数据
func cleanupOldHeatmapData() {
	statsMutex.Lock()
	defer statsMutex.Unlock()

	// 计算保留时间的阈值
	threshold := time.Now().AddDate(0, 0, -heatmapRetentionDays)

	// 过滤保留最新的数据
	var filteredData []HeatmapPoint
	for _, point := range statsData.HeatmapData {
		if point.Timestamp.After(threshold) {
			filteredData = append(filteredData, point)
		}
	}

	// 如果有数据被删除，更新数据并设置需要保存的标志位
	if len(filteredData) != len(statsData.HeatmapData) {
		statsData.HeatmapData = filteredData
		needsSave = true
		log.Printf("清理了 %d 条旧热力图数据，保留了 %d 条数据", len(statsData.HeatmapData)-len(filteredData), len(filteredData))
	}
}

// DeleteStatsForPath 删除指定路径的统计数据
// 当文件或目录被删除时调用，避免统计数据中积累大量已删除文件的信息
func DeleteStatsForPath(path string) {
	statsMutex.Lock()
	defer statsMutex.Unlock()

	// 删除文件统计信息
	delete(statsData.FileStatsMap, path)

	// 删除该路径下所有子路径的统计信息
	var pathsToDelete []string
	for p := range statsData.FileStatsMap {
		if strings.HasPrefix(p, path+string(os.PathSeparator)) {
			pathsToDelete = append(pathsToDelete, p)
		}
	}

	for _, p := range pathsToDelete {
		delete(statsData.FileStatsMap, p)
	}

	// 如果有数据被删除，设置需要保存的标志位
	if len(pathsToDelete) > 0 || statsData.FileStatsMap[path] != nil {
		needsSave = true
		log.Printf("清理了 %d 个路径的统计数据", len(pathsToDelete)+1)
	}
}

// startPeriodicTasks 启动定期任务（清理和保存）
func startPeriodicTasks() {
	// 每24小时执行一次清理任务
	cleanupTicker := time.NewTicker(24 * time.Hour)
	// 每5分钟执行一次保存任务
	saveTicker := time.NewTicker(5 * time.Minute)
	defer cleanupTicker.Stop()
	defer saveTicker.Stop()

	for {
		select {
		case <-cleanupTicker.C:
			MergeDuplicateStats()
			cleanupOldHeatmapData()
		case <-saveTicker.C:
			// 检查是否需要保存
			statsMutex.Lock()
			if needsSave {
				// 保存前释放锁，避免长时间持有锁
				needsSave = false
				statsMutex.Unlock()
				saveStatsData()
			} else {
				statsMutex.Unlock()
			}
		}
	}
}

// InitStats 初始化统计数据
func InitStats() {
	// 获取当前工作目录
	wd, err := os.Getwd()
	if err != nil {
		log.Printf("获取工作目录失败: %v，使用程序目录", err)
		// 如果获取失败，尝试使用可执行文件目录
		execPath, execErr := os.Executable()
		if execErr != nil {
			log.Printf("获取程序路径也失败: %v，使用默认路径", execErr)
			statsDataFile = filepath.Join("config", "stats.json")
		} else {
			baseDir := filepath.Dir(execPath)
			statsDataFile = filepath.Join(baseDir, "config", "stats.json")
		}
	} else {
		// 优先使用当前工作目录（项目目录）
		statsDataFile = filepath.Join(wd, "config", "stats.json")
	}

	log.Printf("统计数据文件路径: %s\n", statsDataFile)

	// 确保config目录存在
	configDir := filepath.Dir(statsDataFile)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		log.Printf("创建配置目录失败: %v\n", err)
	}

	// 加载保存的数据
	loadStatsData()

	// 启动定期任务（清理和保存）
	go startPeriodicTasks()
}

// 保存统计数据到磁盘
func saveStatsData() {
	// 读锁保护，避免与 IncrementShareCount 等写入并发导致序列化撕裂
	statsMutex.RLock()
	data, err := json.MarshalIndent(statsData, "", "  ")
	statsMutex.RUnlock()
	if err != nil {
		log.Printf("[saveStatsData] 序列化统计数据失败: %v\n", err)
		return
	}

	// 确保config目录存在
	configDir := filepath.Dir(statsDataFile)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		log.Printf("[saveStatsData] 创建配置目录失败: %v\n", err)
		return
	}

	// 尝试打开文件用于写入，如果不存在会创建
	if err := os.WriteFile(statsDataFile, data, 0644); err != nil {
		log.Printf("[saveStatsData] 写入统计数据文件失败: %v\n", err)
		return
	}
}

// 从磁盘加载统计数据
func loadStatsData() {
	statsMutex.Lock()
	defer statsMutex.Unlock()

	// 检查文件是否存在
	if _, err := os.Stat(statsDataFile); os.IsNotExist(err) {
		// 初始化空数据
		statsData = StatsData{
			FileStatsMap: make(map[string]*FileStats),
			HeatmapData:  make([]HeatmapPoint, 0),
		}
		// 主动保存以创建文件
		statsMutex.Unlock()
		saveStatsData()   // 这个函数会重新获取锁
		statsMutex.Lock() // 重新获取锁
		return
	}

	// 读取文件内容
	data, err := os.ReadFile(statsDataFile)
	if err != nil {
		log.Printf("读取统计数据文件失败: %v\n", err)
		return
	}
	// 反序列化数据
	if err := json.Unmarshal(data, &statsData); err != nil {
		log.Printf("解析统计数据失败: %v\n", err)
		// 初始化空数据
		statsData = StatsData{
			FileStatsMap: make(map[string]*FileStats),
			HeatmapData:  make([]HeatmapPoint, 0),
		}
	}

	// 确保映射不为nil
	if statsData.FileStatsMap == nil {
		statsData.FileStatsMap = make(map[string]*FileStats)
	}
	if statsData.HeatmapData == nil {
		statsData.HeatmapData = make([]HeatmapPoint, 0)
	}
	// if statsData.DailyTrafficMap == nil {
	// 	statsData.DailyTrafficMap = make(map[string]*DailyTrafficStats)
	// 	log.Printf("DailyTrafficMap为nil，已初始化为空map\n")
	// }

	// 修复数据完整性问题 - 确保每个FileStats条目都有正确的Path字段
	hasDataIssues := false
	for key, stats := range statsData.FileStatsMap {
		if stats == nil {
			delete(statsData.FileStatsMap, key)
			hasDataIssues = true
		} else if stats.Path != key {
			stats.Path = key // 确保Path字段与Map的键保持一致
			hasDataIssues = true
		}
	}

	// 如果发现数据问题，保存修复后的数据
	if hasDataIssues {
		saveStatsData()
	}

	// 在加载数据后执行一次重复条目合并
	// 注意：我们已经在持有锁的情况下，而MergeDuplicateStats内部也会获取锁，
	// 这会导致死锁。所以我们需要先释放锁，然后再调用MergeDuplicateStats
	statsMutex.Unlock()
	MergeDuplicateStats()
	statsMutex.Lock()
}

// IncrementShareCount 增加文件分享次数
func IncrementShareCount(path string, ip string) {
	// 参数验证
	if path == "" {
		return
	}

	// 标准化路径分隔符
	normalizedPath := utils.NormalizePath(path)
	statsMutex.Lock()
	defer statsMutex.Unlock()

	// 获取或创建文件统计信息
	stats, exists := statsData.FileStatsMap[normalizedPath]
	if !exists {
		stats = &FileStats{
			Path:             normalizedPath, // 确保Path字段与Map的键保持一致
			ShareCount:       0,
			DownloadCount:    0,
			LastShareTime:    time.Now(),
			LastDownloadTime: time.Now(),
			TotalBandwidth:   0,
		}
		statsData.FileStatsMap[normalizedPath] = stats
	} else {
		// 数据完整性验证 - 确保Path字段与Map的键保持一致
		if stats.Path != normalizedPath {
			stats.Path = normalizedPath
		}
	}

	// 增加分享次数
	stats.ShareCount++
	stats.LastShareTime = time.Now()

	// 添加热力图数据点
	heatmapPoint := HeatmapPoint{
		Type:      "share",
		Path:      normalizedPath,
		Timestamp: time.Now(),
		IP:        ip,
	}
	statsData.HeatmapData = append(statsData.HeatmapData, heatmapPoint)

	// 设置需要保存的标志位
	needsSave = true
}

// IncrementDownloadCount 增加文件下载次数和带宽统计
func IncrementDownloadCount(path string, ip string, fileSize int64) {
	// 参数验证
	if path == "" {
		return
	}
	if fileSize < 0 {
		fileSize = 0
	}

	// 处理路径，确保与 GetFileDownloadCount 使用相同的路径格式
	// 清理路径，移除多余的分隔符
	cleanPath := filepath.Clean(path)
	// 移除相对路径前缀，处理Windows和Linux的不同情况
	// 处理.前缀（Windows）和./前缀（Linux）
	cleanPath = strings.TrimPrefix(cleanPath, "./")
	cleanPath = strings.TrimPrefix(cleanPath, ".\\")
	// 标准化路径分隔符
	normalizedPath := utils.NormalizePath(cleanPath)
	statsMutex.Lock()
	defer statsMutex.Unlock()

	// 获取或创建文件统计信息
	stats, exists := statsData.FileStatsMap[normalizedPath]
	if !exists {
		stats = &FileStats{
			Path:             normalizedPath,
			ShareCount:       0,
			DownloadCount:    0,
			LastShareTime:    time.Now(),
			LastDownloadTime: time.Now(),
			TotalBandwidth:   0,
		}
		statsData.FileStatsMap[normalizedPath] = stats
	} else {
		// 数据完整性验证
		if stats.Path != normalizedPath {
			stats.Path = normalizedPath
		}
	}

	// 增加下载次数和更新带宽统计
	stats.DownloadCount++
	stats.LastDownloadTime = time.Now()
	stats.TotalBandwidth += fileSize

	// 添加热力图数据点
	heatmapPoint := HeatmapPoint{
		Type:      "download",
		Path:      normalizedPath,
		Timestamp: time.Now(),
		IP:        ip,
		FileSize:  fileSize,
	}
	statsData.HeatmapData = append(statsData.HeatmapData, heatmapPoint)

	// 设置需要保存的标志位
	needsSave = true
}

// GetFileStats 获取文件统计信息
func GetFileStats(path string) *FileStats {
	// 标准化路径分隔符
	normalizedPath := utils.NormalizePath(path)

	statsMutex.RLock()
	defer statsMutex.RUnlock()

	stats, exists := statsData.FileStatsMap[normalizedPath]
	if !exists {
		// 返回空统计信息
		return &FileStats{
			Path:             normalizedPath,
			ShareCount:       0,
			DownloadCount:    0,
			LastShareTime:    time.Now(),
			LastDownloadTime: time.Now(),
			TotalBandwidth:   0,
		}
	}

	return stats
}

// GetAllFileStats 获取所有文件统计信息
func GetAllFileStats() map[string]*FileStats {
	statsMutex.RLock()
	defer statsMutex.RUnlock()

	// 创建副本以避免外部修改原始数据
	statsCopy := make(map[string]*FileStats)
	for path, stats := range statsData.FileStatsMap {
		statsCopy[path] = &FileStats{
			Path:             stats.Path,
			ShareCount:       stats.ShareCount,
			DownloadCount:    stats.DownloadCount,
			LastShareTime:    stats.LastShareTime,
			LastDownloadTime: stats.LastDownloadTime,
			TotalBandwidth:   stats.TotalBandwidth,
		}
	}

	return statsCopy
}

// MergeDuplicateStats 合并重复的统计条目
func MergeDuplicateStats() {
	statsMutex.Lock()
	defer statsMutex.Unlock()

	// 创建一个映射来跟踪标准化路径对应的统计信息
	normalizedStats := make(map[string]*FileStats)
	var keysToRemove []string

	// 先收集所有需要标准化的条目
	type pathPair struct {
		originalPath   string
		normalizedPath string
		stats          *FileStats
	}

	var entriesToProcess []pathPair

	// 收集所有条目
	for originalPath, stats := range statsData.FileStatsMap {
		// 标准化路径
		normalizedPath := utils.NormalizePath(originalPath)
		entriesToProcess = append(entriesToProcess, pathPair{originalPath, normalizedPath, stats})
	}

	// 处理路径标准化和重复条目合并
	for _, entry := range entriesToProcess {
		originalPath := entry.originalPath
		normalizedPath := entry.normalizedPath
		stats := entry.stats

		// 检查是否已存在该标准化路径的统计信息
		if existingStats, exists := normalizedStats[normalizedPath]; exists {
			// 如果已存在该标准化路径的统计信息，则合并数据

			// 合并计数器
			existingStats.ShareCount += stats.ShareCount
			existingStats.DownloadCount += stats.DownloadCount
			existingStats.TotalBandwidth += stats.TotalBandwidth

			// 更新最后操作时间
			if stats.LastShareTime.After(existingStats.LastShareTime) {
				existingStats.LastShareTime = stats.LastShareTime
			}
			if stats.LastDownloadTime.After(existingStats.LastDownloadTime) {
				existingStats.LastDownloadTime = stats.LastDownloadTime
			}

			// 记录需要删除的键（原始路径）
			keysToRemove = append(keysToRemove, originalPath)
		} else {
			// 如果不存在，则添加到映射中
			normalizedStats[normalizedPath] = stats

			// 如果标准化后的路径与原路径不同，需要更新映射
			if normalizedPath != originalPath {
				// 从原映射中删除
				delete(statsData.FileStatsMap, originalPath)
				// 添加到标准化路径的映射中
				statsData.FileStatsMap[normalizedPath] = stats
				// 更新条目中的路径为标准化路径
				stats.Path = normalizedPath
			}
		}
	}

	// 额外检查：处理具有相同文件名但不同目录前缀的情况
	// 提取文件名并检查是否应该合并
	filenameMap := make(map[string][]string) // 文件名 -> 路径列表

	// 构建文件名到路径的映射
	for path := range statsData.FileStatsMap {
		// 提取文件名（最后一个斜杠后面的部分）
		parts := strings.Split(strings.ReplaceAll(path, "\\", "/"), "/")
		if len(parts) > 0 {
			filename := parts[len(parts)-1]
			filenameMap[filename] = append(filenameMap[filename], path)
		}
	}

	// 对于具有相同文件名的条目，如果其中一个路径是另一个的子路径，则合并它们
	for _, paths := range filenameMap {
		if len(paths) > 1 {
			// 检查是否需要合并
			for i := 0; i < len(paths); i++ {
				for j := i + 1; j < len(paths); j++ {
					path1 := paths[i]
					path2 := paths[j]

					// 检查路径是否指向同一文件（一个路径是否是另一个的子路径）
					if isSubPath(path1, path2) || isSubPath(path2, path1) {
						// 确定保留哪个路径（选择较短的作为主路径）
						var removePath string
						var keepStats, removeStats *FileStats

						if len(path1) <= len(path2) {
							removePath = path2
							keepStats = statsData.FileStatsMap[path1]
							removeStats = statsData.FileStatsMap[path2]
						} else {
							removePath = path1
							keepStats = statsData.FileStatsMap[path2]
							removeStats = statsData.FileStatsMap[path1]
						}

						// 合并统计信息
						keepStats.ShareCount += removeStats.ShareCount
						keepStats.DownloadCount += removeStats.DownloadCount
						keepStats.TotalBandwidth += removeStats.TotalBandwidth

						// 更新最后操作时间
						if removeStats.LastShareTime.After(keepStats.LastShareTime) {
							keepStats.LastShareTime = removeStats.LastShareTime
						}
						if removeStats.LastDownloadTime.After(keepStats.LastDownloadTime) {
							keepStats.LastDownloadTime = removeStats.LastDownloadTime
						}

						// 记录需要删除的键
						keysToRemove = append(keysToRemove, removePath)
					}
				}
			}
		}
	}

	// 删除重复的条目
	for _, key := range keysToRemove {
		delete(statsData.FileStatsMap, key)
	}

	// 更新热力图数据中的路径并删除指向已删除条目的记录
	// 先收集需要保留的热力图数据点
	var filteredHeatmapData []HeatmapPoint
	for _, point := range statsData.HeatmapData {
		// 标准化路径
		normalizedPath := utils.NormalizePath(point.Path)
		point.Path = normalizedPath

		// 检查此路径是否对应于已删除的条目
		shouldRemove := false
		for _, removedKey := range keysToRemove {
			// 检查完全匹配或者子路径关系
			// 需要检查两种情况：
			// 1. 热力图数据点路径等于已删除条目路径
			// 2. 热力图数据点路径是已删除条目路径的子路径（例如："downloads/Docker/file.png" 是 "Docker/file.png" 的子路径）
			normalizedRemovedKey := utils.NormalizePath(removedKey)
			if normalizedPath == normalizedRemovedKey || isSubPath(normalizedPath, normalizedRemovedKey) {
				shouldRemove = true
				break
			}
		}

		// 如果不是要删除的条目，则保留
		if !shouldRemove {
			filteredHeatmapData = append(filteredHeatmapData, point)
		}
	}

	// 更新热力图数据
	statsData.HeatmapData = filteredHeatmapData

	if len(keysToRemove) > 0 {
		// 保存更新后的数据
		saveStatsData()
	}
}

// isSubPath 检查 path1 是否是 path2 的子路径（忽略目录前缀）
// 例如："downloads/Docker/file.png" 是 "Docker/file.png" 的子路径
func isSubPath(path1, path2 string) bool {
	// 标准化路径分隔符
	normalizedPath1 := utils.NormalizePath(path1)
	normalizedPath2 := utils.NormalizePath(path2)

	// 检查 path1 是否是 path2 的子路径
	if len(normalizedPath1) > len(normalizedPath2) {
		// 检查 path1 是否以 path2 结尾且结尾前是路径分隔符
		if strings.HasSuffix(normalizedPath1, normalizedPath2) {
			prefixLen := len(normalizedPath1) - len(normalizedPath2)
			if prefixLen > 0 && (normalizedPath1[prefixLen-1] == '/') {
				return true
			}
		}
	}

	// 检查 path2 是否是 path1 的子路径
	if len(normalizedPath2) > len(normalizedPath1) {
		if strings.HasSuffix(normalizedPath2, normalizedPath1) {
			prefixLen := len(normalizedPath2) - len(normalizedPath1)
			if prefixLen > 0 && (normalizedPath2[prefixLen-1] == '/') {
				return true
			}
		}
	}

	return false
}

// GetHeatmapData 获取热力图数据
func GetHeatmapData() []HeatmapPoint {
	statsMutex.RLock()
	defer statsMutex.RUnlock()

	// 返回副本以避免并发问题
	result := make([]HeatmapPoint, len(statsData.HeatmapData))
	copy(result, statsData.HeatmapData)

	return result
}

// GetHeatmapDataByTimeRange 获取指定时间范围内的热力图数据
func GetHeatmapDataByTimeRange(startTime, endTime time.Time) []HeatmapPoint {
	statsMutex.RLock()
	defer statsMutex.RUnlock()

	// 过滤指定时间范围内的数据
	result := make([]HeatmapPoint, 0)
	for _, point := range statsData.HeatmapData {
		if point.Timestamp.After(startTime) && point.Timestamp.Before(endTime) {
			result = append(result, point)
		}
	}

	return result
}

// GetTotalDownloadCount 获取总下载次数
func GetTotalDownloadCount() int64 {
	statsMutex.RLock()
	defer statsMutex.RUnlock()

	var total int64 = 0
	for _, stats := range statsData.FileStatsMap {
		total += stats.DownloadCount
	}

	return total
}

// GetTotalDownloadSize 获取总下载大小（总流量）
func GetTotalDownloadSize() int64 {
	statsMutex.RLock()
	defer statsMutex.RUnlock()

	var total int64 = 0
	for _, stats := range statsData.FileStatsMap {
		total += stats.TotalBandwidth
	}

	return total
}

// GetHeatmapDataCount 获取热力图数据点数量
func GetHeatmapDataCount() int64 {
	statsMutex.RLock()
	defer statsMutex.RUnlock()

	return int64(len(statsData.HeatmapData))
}

// GetFileDownloadCount 获取指定文件的下载次数
func GetFileDownloadCount(path string) int64 {
	// 标准化路径分隔符
	normalizedPath := utils.NormalizePath(path)

	statsMutex.RLock()
	defer statsMutex.RUnlock()

	stats, exists := statsData.FileStatsMap[normalizedPath]
	if !exists {
		return 0
	}

	return stats.DownloadCount
}

// GetFileLastDownloadTime 获取指定文件的最后下载时间
func GetFileLastDownloadTime(path string) time.Time {
	// 标准化路径分隔符
	normalizedPath := utils.NormalizePath(path)

	statsMutex.RLock()
	defer statsMutex.RUnlock()

	stats, exists := statsData.FileStatsMap[normalizedPath]
	if !exists {
		return time.Time{}
	}

	return stats.LastDownloadTime
}

// GetFileTotalDownloadSize 获取指定文件的总下载流量
func GetFileTotalDownloadSize(path string) int64 {
	// 标准化路径分隔符
	normalizedPath := utils.NormalizePath(path)

	statsMutex.RLock()
	defer statsMutex.RUnlock()

	stats, exists := statsData.FileStatsMap[normalizedPath]
	if !exists {
		return 0
	}

	return stats.TotalBandwidth
}

// SaveStatsData 保存统计数据到磁盘（供外部调用）
func SaveStatsData() {
	saveStatsData()
}

// StatsHandler 处理获取所有统计数据的API请求
func StatsHandler(w http.ResponseWriter, r *http.Request) {
	// API认证检查
	if !utils.CheckAPIAuthentication(r) {
		http.Error(w, "API鉴权失败", http.StatusUnauthorized)
		return
	}

	// 设置响应头
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	// 获取所有文件统计信息
	allStats := GetAllFileStats()

	// 转换为JSON格式
	jsonData, err := json.MarshalIndent(allStats, "", "  ")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 发送响应
	w.WriteHeader(http.StatusOK)
	w.Write(jsonData)
}
