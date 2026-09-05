package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go-download-server/config"
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

// compressHeatmapDownloadPoints 压缩存量热力图 download 点：
// 旧版本每个 Range 分片传输都会追加一个 download 点（与下载计数同源的分片重复 bug），
// 使热力图近 7 天「下载」柱/热度虚高。按与实时计数一致的口径——同 (IP, 文件) 滑动 60s 窗口内的
// 后续分片点压缩为单点，share 等其他类型不受影响。
// 幂等：压缩后同 (IP,文件) 相邻点间隔必然 >60s，再次执行不会重复删除。启动加载数据后自动执行一次。
func compressHeatmapDownloadPoints() {
	type hk struct{ ip, path string }
	last := make(map[hk]time.Time)
	out := make([]HeatmapPoint, 0, len(statsData.HeatmapData))
	removed := 0

	// HeatmapData 若按原始顺序做滑动窗口，历史跨进程/多份写入可能造成时间乱序，
	// 把更早的真实窗口点误判为「窗口内」删除。先做时间稳定排序，
	// 使压缩窗口语义与实时 markLogicalDownload（按时间顺序滑动）完全一致。
	statsMutex.Lock()
	sort.SliceStable(statsData.HeatmapData, func(i, j int) bool {
		return statsData.HeatmapData[i].Timestamp.Before(statsData.HeatmapData[j].Timestamp)
	})
	for _, p := range statsData.HeatmapData {
		if p.Type != "download" {
			out = append(out, p)
			continue
		}
		k := hk{p.IP, p.Path}
		ts := p.Timestamp
		if lastT, ok := last[k]; ok && ts.Sub(lastT) <= downloadMergeWindow {
			removed++ // 窗口内同 (IP,文件) 的后续分片点 → 丢弃，只保留首个
			continue
		}
		last[k] = ts
		out = append(out, p)
	}
	if removed > 0 {
		statsData.HeatmapData = out
	}
	statsMutex.Unlock()

	if removed > 0 {
		saveStatsData()
		log.Printf("压缩了 %d 条重复热力图 download 点(60s 合并口径)，剩余 %d 条", removed, len(out))
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
			cleanupLogicalDownloads()
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

			// IP 下载统计兜底落盘(有变更时保存,防止崩溃/强杀丢失最近的下载与封禁记录)
			PeriodicSaveIPStats()
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
	// 存量热力点压缩：修复前每个 Range 分片都追加 download 点的重复虚高(与下载计数同源 bug)。
	// 幂等压缩，仅压缩 download 类型，share 等不受影响。
	compressHeatmapDownloadPoints()
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

// downloadMergeWindow 同一 (IP, 文件) 传输的合并窗口：窗口内的多次传输视为同一次「逻辑下载」。
// 多线程工具（IDM/迅雷等）会并发发出多个 Range 分片请求（间隔毫秒~秒级），断点续传也可能分多段完成；
// 若按 HTTP 请求计数，一次完整下载会被记成 N 次。窗口内只计 1 次下载次数，
// 流量仍按每段实际传输字节累加（不受影响）。暂停较久（超过窗口）后恢复的续传视为新的下载。
const downloadMergeWindow = 60 * time.Second

// logicalDownloadTracker 记录各 (IP, 文件) 最近一次传输时间，用于合并窗口判定。
var (
	logicalDownloadMu      sync.Mutex
	logicalDownloadTracker = make(map[string]time.Time)
)

// markLogicalDownload 登记一次传输。返回 true 表示开启了一次新的逻辑下载（下载次数应 +1）；
// false 表示属于窗口内同一逻辑下载的后续分片（不再重复计次数，但字节数仍累计）。
func markLogicalDownload(ip, normalizedPath string) bool {
	logicalDownloadMu.Lock()
	defer logicalDownloadMu.Unlock()

	key := ip + "\x00" + normalizedPath
	now := time.Now()
	if last, ok := logicalDownloadTracker[key]; ok && now.Sub(last) <= downloadMergeWindow {
		// 同一逻辑下载的后续分片：刷新最后活动时间，不计新次数
		logicalDownloadTracker[key] = now
		return false
	}
	logicalDownloadTracker[key] = now
	return true
}

// cleanupLogicalDownloads 清理已过期的下载会话记录，防止 map 无限增长。
// 由 startPeriodicTasks 的清理任务周期调用。
func cleanupLogicalDownloads() {
	logicalDownloadMu.Lock()
	defer logicalDownloadMu.Unlock()
	threshold := time.Now().Add(-downloadMergeWindow * 2)
	for key, last := range logicalDownloadTracker {
		if last.Before(threshold) {
			delete(logicalDownloadTracker, key)
		}
	}
}

// IncrementDownloadCount 增加文件下载次数和带宽统计。
//
// 返回值：true 表示本次传输开启了一次新的逻辑下载（DownloadCount 已 +1）；
// false 表示属于同一逻辑下载的后续分片（仅累计带宽，不再重复计次数）。
func IncrementDownloadCount(path string, ip string, fileSize int64) bool {
	// 参数验证
	if path == "" {
		return false
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

	// 合并窗口判定：同一 (IP,文件) 的后续分片不再重复计次数
	isNew := markLogicalDownload(ip, normalizedPath)

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

	// 增加下载次数（仅新的逻辑下载）和更新带宽统计（每次传输都累计实际字节）
	if isNew {
		stats.DownloadCount++
		// 添加热力图数据点：仅新的逻辑下载记一点。
		// 旧版本每个 Range 分片都会追加一点，导致热力图近 7 天下载热度虚高(与下载计数同源的分片重复 bug)。
		// 同 60s 合并窗口内的分片不再重复加点，使热力图口径与 IP 统计/审计日志一致。
		statsData.HeatmapData = append(statsData.HeatmapData, HeatmapPoint{
			Type:      "download",
			Path:      normalizedPath,
			Timestamp: time.Now(),
			IP:        ip,
			FileSize:  fileSize,
		})
	}
	stats.LastDownloadTime = time.Now()
	stats.TotalBandwidth += fileSize

	// 设置需要保存的标志位
	needsSave = true

	return isNew
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

// ---------- 文件统计(stats.json)历史日志重建(修复分片重复计数污染) ----------

// tempFileStats 日志回填过程用的临时聚合结构(文件维度)
type tempFileStats struct {
	count     int64          // 逻辑下载次数(按 (IP+文件) 60s 合并窗口折算)
	bandwidth int64          // 带宽全额(每次传输逐字节累加,与实时一致)
	lastDl    time.Time      // 该文件最后一次下载时间
	points    []HeatmapPoint // 每次逻辑下载对应的热力点(窗口内首个传输)
}

// collectFileStatsFromLogs 扫描全部历史审计日志(server_*.log 与 .gz),
// 按 (IP+文件) 60s 合并窗口折算每个文件的「逻辑下载」次数与带宽(与实时计数同口径)。
// 每次逻辑下载在窗口内首个传输时刻生成一个 download 热力点,供 stats.json 重建。
// 仅扫描不落盘,由调用方决定合并进现有数据还是整体重建。
func collectFileStatsFromLogs() map[string]*tempFileStats {
	logDir := config.AppConfig.Server.LogDir
	var files []string
	if plain, err := filepath.Glob(filepath.Join(logDir, "server_*.log")); err == nil {
		files = append(files, plain...)
	}
	if gz, err := filepath.Glob(filepath.Join(logDir, "server_*.log.gz")); err == nil {
		files = append(files, gz...)
	}
	sort.Strings(files)

	tracker := make(map[string]time.Time) // key = ip\x00文件 → 该窗口内最近一次传输时间
	result := make(map[string]*tempFileStats)

	for _, file := range files {
		lines, err := readLogLines(file)
		if err != nil {
			continue
		}
		for _, line := range lines {
			rec, ok := parseDownloadLogLine(line)
			if !ok {
				continue
			}
			// 日志行没有文件名时无法归集到文件统计(旧版少数行缺字段),跳过
			if rec.file == "" {
				continue
			}

			key := rec.ip + "\x00" + rec.file
			isNew := true
			if last, exists := tracker[key]; exists && rec.ts.Sub(last) <= downloadMergeWindow {
				isNew = false
			}
			tracker[key] = rec.ts // 滑动窗口:每次传输都刷新最近时间

			fs, exists := result[rec.file]
			if !exists {
				fs = &tempFileStats{lastDl: rec.ts}
				result[rec.file] = fs
			}
			if isNew {
				fs.count++
				fs.points = append(fs.points, HeatmapPoint{
					Type:      "download",
					Path:      rec.file,
					Timestamp: rec.ts,
					IP:        rec.ip,
					FileSize:  rec.size,
				})
			}
			fs.bandwidth += rec.size
			if rec.ts.After(fs.lastDl) {
				fs.lastDl = rec.ts
			}
		}
	}
	return result
}

// RebuildFileStatsFromLogs 从历史日志强制重建文件下载统计(stats.json),用于修复
// 旧版「每个 Range 分片传输都 +1 下载次数 / 热力点 + 累加整文件大小」污染的存量数据。
//
// 策略(与 RebuildIPStatsFromLogs 对称):
//  1. 备份现有 stats.json 为 stats.json.bak.<时间戳>;
//  2. 分享统计(ShareCount/LastShareTime)与上传时间(UploadTime)与分片 bug 无关,保留原值;
//  3. download_count / total_bandwidth / last_download_time / download 热力点
//     全部按日志 60s 合并口径重建(带宽按每片实际传输字节累加,不重复);
//  4. 日志未覆盖的条目(可能仅被分享过)保留 download_count=0,避免把分享文件误删。
//
// 幂等:重建后再执行结果不变。可重复执行(每次都会先备份)。
func RebuildFileStatsFromLogs() (backupPath string, err error) {
	// 备份现有 stats.json(存在才备份)
	if _, statErr := os.Stat(statsDataFile); statErr == nil {
		backupPath = filepath.Join(filepath.Dir(statsDataFile), "stats.json.bak."+time.Now().Format("20060102_150405"))
		data, readErr := os.ReadFile(statsDataFile)
		if readErr != nil {
			return "", fmt.Errorf("读取原统计数据失败: %v", readErr)
		}
		if writeErr := os.WriteFile(backupPath, data, 0644); writeErr != nil {
			return "", fmt.Errorf("备份原统计数据失败: %v", writeErr)
		}
		fmt.Printf("[统计] 已备份原统计数据: %s\n", backupPath)
	}

	collected := collectFileStatsFromLogs()

	statsMutex.Lock()
	// 分享/上传元数据与分片 bug 无关,从现有条目快照保留
	// 日志未覆盖的条目(可能仅被分享过/超保留期):download 相关清零,分享字段保留,避免误删文件
	for path := range statsData.FileStatsMap {
		if _, exists := collected[path]; exists {
			continue
		}
		collected[path] = &tempFileStats{count: 0, bandwidth: 0}
	}

	// 重建 FileStatsMap
	newMap := make(map[string]*FileStats, len(collected))
	for path, tmp := range collected {
		old := statsData.FileStatsMap[path]
		fs := &FileStats{
			Path:           path,
			DownloadCount:  tmp.count,
			TotalBandwidth: tmp.bandwidth,
		}
		if old != nil {
			fs.ShareCount = old.ShareCount
			fs.LastShareTime = old.LastShareTime
			fs.UploadTime = old.UploadTime
		}
		if tmp.lastDl.IsZero() {
			fs.LastDownloadTime = time.Now()
		} else {
			fs.LastDownloadTime = tmp.lastDl
		}
		newMap[path] = fs
	}
	statsData.FileStatsMap = newMap

	// 重建热力图 download 点:share/upload/admin_action 等其他类型点不受分片 bug 影响,保留
	var kept []HeatmapPoint
	for _, p := range statsData.HeatmapData {
		if p.Type != "download" {
			kept = append(kept, p)
		}
	}
	for _, tmp := range collected {
		kept = append(kept, tmp.points...)
	}
	// 时间升序,便于前端按时间区间取数
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].Timestamp.Before(kept[j].Timestamp) })
	statsData.HeatmapData = kept
	statsMutex.Unlock()

	saveStatsData()
	fmt.Printf("[统计] 文件统计重建完成: %d 个文件的下载次数/带宽/热力点已按日志 60s 口径重建\n", len(collected))
	return backupPath, nil
}
