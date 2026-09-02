package handlers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-download-server/config"
)

// IP下载统计信息
type IPDownloadStats struct {
	IP                 string    `json:"ip"`
	DownloadCount      int64     `json:"download_count"`
	TotalBandwidth     int64     `json:"total_bandwidth"`
	FirstSeen          time.Time `json:"first_seen"`
	LastSeen           time.Time `json:"last_seen"`
	LastDownloadTime   time.Time `json:"last_download_time"`
	Blocked            bool      `json:"blocked"`
	BlockReason        string    `json:"block_reason,omitempty"`
	BlockedAt          time.Time `json:"blocked_at,omitempty"`
	// 每日使用统计
	DailyDate          string    `json:"daily_date"`           // 统计日期（YYYY-MM-DD）
	DailyDownloadCount int64     `json:"daily_download_count"`  // 今日下载次数
	DailyBandwidth     int64     `json:"daily_bandwidth"`       // 今日下载流量
	// 每小时使用统计
	HourlyDate         string    `json:"hourly_date"`          // 统计小时（YYYY-MM-DD HH）
	HourlyDownloadCount int64    `json:"hourly_download_count"` // 本小时下载次数
	HourlyBandwidth    int64     `json:"hourly_bandwidth"`      // 本小时下载流量
}

// IP统计数据存储结构
type IPStatsData struct {
	IPStats map[string]*IPDownloadStats `json:"ip_stats"`
}

var (
	ipStatsData   IPStatsData
	ipStatsMutex  sync.RWMutex
	ipStatsLoaded bool
)

// IP统计数据文件路径
func getIPStatsFilePath() string {
	return filepath.Join(config.AppConfig.Server.LogDir, "ip_stats.json")
}

// 加载IP统计数据
func loadIPStats() {
	ipStatsMutex.Lock()
	defer ipStatsMutex.Unlock()

	if ipStatsLoaded {
		return
	}

	ipStatsData.IPStats = make(map[string]*IPDownloadStats)

	filePath := getIPStatsFilePath()
	data, err := os.ReadFile(filePath)
	if err != nil {
		ipStatsLoaded = true
		return
	}

	if err := json.Unmarshal(data, &ipStatsData); err != nil {
		ipStatsLoaded = true
		return
	}

	if ipStatsData.IPStats == nil {
		ipStatsData.IPStats = make(map[string]*IPDownloadStats)
	}

	ipStatsLoaded = true
}

// 保存IP统计数据
func saveIPStats() {
	ipStatsMutex.RLock()
	defer ipStatsMutex.RUnlock()

	filePath := getIPStatsFilePath()

	// 确保目录存在
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}

	data, err := json.MarshalIndent(ipStatsData, "", "  ")
	if err != nil {
		return
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return
	}
}

// 记录IP下载
func RecordIPDownload(ip string, fileSize int64) {
	loadIPStats()

	ipStatsMutex.Lock()
	defer ipStatsMutex.Unlock()

	now := time.Now()

	stats, exists := ipStatsData.IPStats[ip]
	if !exists {
		stats = &IPDownloadStats{
			IP:        ip,
			FirstSeen: now,
		}
		ipStatsData.IPStats[ip] = stats
	}

	stats.DownloadCount++
	stats.TotalBandwidth += fileSize
	stats.LastSeen = now
	stats.LastDownloadTime = now

	// 更新每日统计
	dailyDate := now.Format("2006-01-02")
	if stats.DailyDate != dailyDate {
		stats.DailyDate = dailyDate
		stats.DailyDownloadCount = 0
		stats.DailyBandwidth = 0
	}
	stats.DailyDownloadCount++
	stats.DailyBandwidth += fileSize

	// 更新每小时统计
	hourlyDate := now.Format("2006-01-02 15")
	if stats.HourlyDate != hourlyDate {
		stats.HourlyDate = hourlyDate
		stats.HourlyDownloadCount = 0
		stats.HourlyBandwidth = 0
	}
	stats.HourlyDownloadCount++
	stats.HourlyBandwidth += fileSize

	// 异步保存（每10次下载保存一次，减少IO）
	if stats.DownloadCount%10 == 0 {
		go saveIPStats()
	}
}

// 检查IP是否超过流量限额
// 返回：是否超过限额、错误信息、限额类型（daily/hourly）
func CheckIPLimit(ip string) (bool, string, string) {
	// 如果未启用IP限额，直接返回不超过
	if !config.AppConfig.IPLimit.Enabled {
		return false, "", ""
	}

	loadIPStats()

	ipStatsMutex.RLock()
	defer ipStatsMutex.RUnlock()

	stats, exists := ipStatsData.IPStats[ip]
	if !exists {
		return false, "", ""
	}

	now := time.Now()

	// 检查每日限额
	dailyDate := now.Format("2006-01-02")
	if stats.DailyDate == dailyDate {
		// 检查每日下载次数限额
		if config.AppConfig.IPLimit.DailyMaxDownloads > 0 && stats.DailyDownloadCount >= config.AppConfig.IPLimit.DailyMaxDownloads {
			return true, fmt.Sprintf("今日下载次数已达上限（%d次）", config.AppConfig.IPLimit.DailyMaxDownloads), "daily"
		}
		// 检查每日流量限额
		if config.AppConfig.IPLimit.DailyMaxBandwidth > 0 && stats.DailyBandwidth >= config.AppConfig.IPLimit.DailyMaxBandwidth {
			return true, fmt.Sprintf("今日下载流量已达上限（%s）", formatBandwidth(config.AppConfig.IPLimit.DailyMaxBandwidth)), "daily"
		}
	}

	// 检查每小时限额
	hourlyDate := now.Format("2006-01-02 15")
	if stats.HourlyDate == hourlyDate {
		// 检查每小时下载次数限额
		if config.AppConfig.IPLimit.HourlyMaxDownloads > 0 && stats.HourlyDownloadCount >= config.AppConfig.IPLimit.HourlyMaxDownloads {
			return true, fmt.Sprintf("本小时下载次数已达上限（%d次）", config.AppConfig.IPLimit.HourlyMaxDownloads), "hourly"
		}
		// 检查每小时流量限额
		if config.AppConfig.IPLimit.HourlyMaxBandwidth > 0 && stats.HourlyBandwidth >= config.AppConfig.IPLimit.HourlyMaxBandwidth {
			return true, fmt.Sprintf("本小时下载流量已达上限（%s）", formatBandwidth(config.AppConfig.IPLimit.HourlyMaxBandwidth)), "hourly"
		}
	}

	return false, "", ""
}

// 格式化带宽显示
func formatBandwidth(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// 检查IP是否被封禁
func IsIPBlocked(ip string) (bool, string) {
	loadIPStats()

	ipStatsMutex.RLock()
	defer ipStatsMutex.RUnlock()

	stats, exists := ipStatsData.IPStats[ip]
	if !exists {
		return false, ""
	}

	return stats.Blocked, stats.BlockReason
}

// 封禁IP
func BlockIP(ip string, reason string) error {
	loadIPStats()

	ipStatsMutex.Lock()
	defer ipStatsMutex.Unlock()

	now := time.Now()

	stats, exists := ipStatsData.IPStats[ip]
	if !exists {
		stats = &IPDownloadStats{
			IP:        ip,
			FirstSeen: now,
		}
		ipStatsData.IPStats[ip] = stats
	}

	stats.Blocked = true
	stats.BlockReason = reason
	stats.BlockedAt = now
	stats.LastSeen = now

	go saveIPStats()

	return nil
}

// 解封IP
func UnblockIP(ip string) error {
	loadIPStats()

	ipStatsMutex.Lock()
	defer ipStatsMutex.Unlock()

	stats, exists := ipStatsData.IPStats[ip]
	if !exists {
		return fmt.Errorf("IP不存在: %s", ip)
	}

	stats.Blocked = false
	stats.BlockReason = ""
	stats.BlockedAt = time.Time{}

	go saveIPStats()

	return nil
}

// 获取所有IP统计（按下载次数排序）
func GetAllIPStats() []IPDownloadStats {
	loadIPStats()

	ipStatsMutex.RLock()
	defer ipStatsMutex.RUnlock()

	var statsList []IPDownloadStats
	for _, stats := range ipStatsData.IPStats {
		statsList = append(statsList, *stats)
	}

	// 按下载次数排序
	for i := 0; i < len(statsList); i++ {
		for j := i + 1; j < len(statsList); j++ {
			if statsList[j].DownloadCount > statsList[i].DownloadCount {
				statsList[i], statsList[j] = statsList[j], statsList[i]
			}
		}
	}

	return statsList
}

// 获取IP统计
func GetIPStats(ip string) (IPDownloadStats, bool) {
	loadIPStats()

	ipStatsMutex.RLock()
	defer ipStatsMutex.RUnlock()

	stats, exists := ipStatsData.IPStats[ip]
	if !exists {
		return IPDownloadStats{}, false
	}

	return *stats, true
}

// 获取封禁的IP列表
func GetBlockedIPs() []IPDownloadStats {
	loadIPStats()

	ipStatsMutex.RLock()
	defer ipStatsMutex.RUnlock()

	var blockedList []IPDownloadStats
	for _, stats := range ipStatsData.IPStats {
		if stats.Blocked {
			blockedList = append(blockedList, *stats)
		}
	}

	return blockedList
}

// 获取导入标记文件路径
func getImportMarkerFilePath() string {
	return filepath.Join(config.AppConfig.Server.LogDir, ".ip_stats_imported")
}

// 检查是否已经从日志导入过
func hasBeenImported() bool {
	_, err := os.Stat(getImportMarkerFilePath())
	return err == nil
}

// 标记为已导入
func markAsImported() {
	markerFile := getImportMarkerFilePath()
	os.WriteFile(markerFile, []byte(time.Now().Format("2006-01-02 15:04:05")), 0644)
}

// 从历史日志导入IP统计数据
// 只导入一次，避免重复统计
func ImportIPStatsFromLogs() int {
	if hasBeenImported() {
		fmt.Println("[IP统计] 已从历史日志导入过，跳过")
		return 0
	}

	logDir := config.AppConfig.Server.LogDir
	logFiles, err := filepath.Glob(filepath.Join(logDir, "server_*.log"))
	if err != nil || len(logFiles) == 0 {
		fmt.Println("[IP统计] 未找到历史日志文件，跳过导入")
		markAsImported()
		return 0
	}

	fmt.Printf("[IP统计] 发现 %d 个历史日志文件，开始导入...\n", len(logFiles))

	// 临时存储从日志中提取的IP统计
	type tempIPStats struct {
		count              int64
		bandwidth          int64
		dailyCount         int64
		dailyBandwidth     int64
		firstSeen          time.Time
		lastDownload       time.Time
	}
	tempStats := make(map[string]*tempIPStats)

	// 今日日期（用于统计今日数据）
	todayDate := time.Now().Format("2006-01-02")

	// 日志格式正则: 时间 [级别] [用户名] [角色] 操作 详情
	// 示例: 2026-09-01 21:38:18 [info] [admin] [admin] download_file 下载文件: xxx, 实际传输: 1048576字节 [协议: https, 端口: 443, IP: 127.0.0.1]
	logRe := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})\s*\[(.*?)\]\s*\[(.*?)\]\s*\[(.*?)\]\s*(\w+)\s*(.*)$`)
	ipRe := regexp.MustCompile(`IP[:\s]+([0-9a-fA-F:.]+)`)
	// 提取实际传输字节数: 实际传输: 1048576字节
	bandwidthRe := regexp.MustCompile(`实际传输[:\s]*(\d+)\s*字节`)

	importedCount := 0
	for _, logFile := range logFiles {
		data, err := os.ReadFile(logFile)
		if err != nil {
			continue
		}

		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			matches := logRe.FindStringSubmatch(line)
			if len(matches) < 7 {
				continue
			}

			timestamp := matches[1]
			action := matches[5]
			details := matches[6]

			// 只统计下载相关的操作
			if !strings.Contains(action, "download") && !strings.Contains(details, "下载") {
				continue
			}

			// 提取IP地址
			ipMatches := ipRe.FindStringSubmatch(details)
			if len(ipMatches) < 2 {
				continue
			}
			ip := ipMatches[1]

			// 提取实际传输字节数（如果有）
			var fileSize int64 = 0
			bandwidthMatches := bandwidthRe.FindStringSubmatch(details)
			if len(bandwidthMatches) >= 2 {
				if size, err := strconv.ParseInt(bandwidthMatches[1], 10, 64); err == nil {
					fileSize = size
				}
			}

			// 解析时间
			logTime, err := time.Parse("2006-01-02 15:04:05", timestamp)
			if err != nil {
				continue
			}

			// 统计
			if _, exists := tempStats[ip]; !exists {
				tempStats[ip] = &tempIPStats{
					count:          0,
					bandwidth:      0,
					dailyCount:     0,
					dailyBandwidth: 0,
					firstSeen:      logTime,
					lastDownload:   logTime,
				}
			}
			stats := tempStats[ip]
			stats.count++
			stats.bandwidth += fileSize
			// 统计今日数据
			if logTime.Format("2006-01-02") == todayDate {
				stats.dailyCount++
				stats.dailyBandwidth += fileSize
			}
			if logTime.Before(stats.firstSeen) {
				stats.firstSeen = logTime
			}
			if logTime.After(stats.lastDownload) {
				stats.lastDownload = logTime
			}
			importedCount++
		}
	}

	// 合并到现有IP统计中（去重，不覆盖已有的封禁状态）
	loadIPStats()
	ipStatsMutex.Lock()
	for ip, temp := range tempStats {
		if existing, exists := ipStatsData.IPStats[ip]; exists {
			// 已存在的IP，累加下载次数和带宽（历史日志导入）
			existing.DownloadCount += temp.count
			existing.TotalBandwidth += temp.bandwidth
			// 累加今日数据
			if temp.dailyCount > 0 {
				existing.DailyDownloadCount += temp.dailyCount
				existing.DailyBandwidth += temp.dailyBandwidth
				existing.DailyDate = todayDate
			}
			if temp.firstSeen.Before(existing.FirstSeen) {
				existing.FirstSeen = temp.firstSeen
			}
			if temp.lastDownload.After(existing.LastDownloadTime) {
				existing.LastDownloadTime = temp.lastDownload
				existing.LastSeen = temp.lastDownload
			}
		} else {
			// 新IP，直接添加
			ipStatsData.IPStats[ip] = &IPDownloadStats{
				IP:                 ip,
				DownloadCount:      temp.count,
				TotalBandwidth:     temp.bandwidth,
				FirstSeen:          temp.firstSeen,
				LastSeen:           temp.lastDownload,
				LastDownloadTime:   temp.lastDownload,
				Blocked:            false,
				DailyDate:          todayDate,
				DailyDownloadCount: temp.dailyCount,
				DailyBandwidth:     temp.dailyBandwidth,
			}
		}
	}
	ipStatsMutex.Unlock()

	// 保存
	saveIPStats()

	// 标记为已导入
	markAsImported()

	fmt.Printf("[IP统计] 从历史日志导入完成，共处理 %d 条下载记录，涉及 %d 个IP\n", importedCount, len(tempStats))
	return importedCount
}

// 初始化IP统计（启动时加载）
func InitIPStats() {
	loadIPStats()
	// 从历史日志导入IP统计（只导入一次）
	ImportIPStatsFromLogs()
	fmt.Println("[IP统计] IP下载统计模块已加载")
}
