package handlers

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go-download-server/config"
)

// IP下载统计信息
type IPDownloadStats struct {
	IP               string    `json:"ip"`
	DownloadCount    int64     `json:"download_count"`
	TotalBandwidth   int64     `json:"total_bandwidth"`
	FirstSeen        time.Time `json:"first_seen"`
	LastSeen         time.Time `json:"last_seen"`
	LastDownloadTime time.Time `json:"last_download_time"`
	Blocked          bool      `json:"blocked"`
	BlockReason      string    `json:"block_reason,omitempty"`
	BlockedAt        time.Time `json:"blocked_at,omitempty"`
	// 每日使用统计
	DailyDate          string `json:"daily_date"`           // 统计日期（YYYY-MM-DD）
	DailyDownloadCount int64  `json:"daily_download_count"` // 今日下载次数
	DailyBandwidth     int64  `json:"daily_bandwidth"`      // 今日下载流量
	// 每小时使用统计
	HourlyDate          string `json:"hourly_date"`           // 统计小时（YYYY-MM-DD HH）
	HourlyDownloadCount int64  `json:"hourly_download_count"` // 本小时下载次数
	HourlyBandwidth     int64  `json:"hourly_bandwidth"`      // 本小时下载流量
}

// IP统计数据存储结构
type IPStatsData struct {
	IPStats map[string]*IPDownloadStats `json:"ip_stats"`
}

var (
	ipStatsData   IPStatsData
	ipStatsMutex  sync.RWMutex
	ipStatsLoaded bool
	// ipStatsDirty 标记内存统计数据有变更但尚未落盘,由周期任务兜底保存(防崩溃丢最近记录)
	ipStatsDirty atomic.Bool
	// ipStatsPersistMu 串行化 ip_stats.json 的「重命名(重建备份)/落盘」文件操作,
	// 避免界面上点「执行修复」重建与周期兜底保存并发竞争同一文件路径
	ipStatsPersistMu sync.Mutex
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
	// 与重建的备份重命名互斥,防止同一文件路径上并发写
	ipStatsPersistMu.Lock()
	defer ipStatsPersistMu.Unlock()

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

	// 原子写:先写临时文件再改名,避免写一半崩溃/断电留下损坏的 JSON(损坏会被当成空数据重新回填)
	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		return
	}
}

// PeriodicSaveIPStats 供周期任务调用:内存有变更(脏标记)时落盘一次,
// 兜底崩溃/强杀导致的最近下载记录丢失(实时仅按批/封禁时保存)。
func PeriodicSaveIPStats() {
	if ipStatsDirty.Swap(false) {
		saveIPStats()
	}
}

// 记录IP下载
// RecordIPDownload 记录 IP 维度的下载统计（带宽与次数）。
// isNew 表示本次传输是否开启了一次新的逻辑下载（由调用方经下载合并窗口判定）：
// 仅新的逻辑下载使 DownloadCount/DailyDownloadCount/HourlyDownloadCount +1，
// 避免多线程/断点续传的分片请求把次数与按次限额虚高；带宽仍按每段实际传输累加。
func RecordIPDownload(ip string, fileSize int64, isNew bool) {
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

	if isNew {
		stats.DownloadCount++
	}
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
	if isNew {
		stats.DailyDownloadCount++
	}
	stats.DailyBandwidth += fileSize

	// 更新每小时统计
	hourlyDate := now.Format("2006-01-02 15")
	if stats.HourlyDate != hourlyDate {
		stats.HourlyDate = hourlyDate
		stats.HourlyDownloadCount = 0
		stats.HourlyBandwidth = 0
	}
	if isNew {
		stats.HourlyDownloadCount++
	}
	stats.HourlyBandwidth += fileSize

	// 标记有变更,由周期任务统一落盘兜底(避免每请求写盘 IO,崩溃最多丢失一个保存周期内的记录)
	ipStatsDirty.Store(true)
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

	// 按最后下载时间排序（最近的IP排在前面）
	for i := 0; i < len(statsList); i++ {
		for j := i + 1; j < len(statsList); j++ {
			if statsList[j].LastDownloadTime.After(statsList[i].LastDownloadTime) {
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

// ---------- 历史日志回填(一次性,仅在 ip_stats.json 为空时执行) ----------

// ipImportMergeWindow 与实时计数口径一致:同一 (IP, 文件) 在窗口内的多次 HTTP 传输
// (多线程 Range 分片/断点续传)视为一次「逻辑下载」。修复分片重复计数之前的旧日志
// 仍按此窗口回填,避免次数虚高(带宽逐行照常累计)。
const ipImportMergeWindow = 60 * time.Second

// logDownloadRecord 一条可计入 IP 统计的下载日志行解析结果
type logDownloadRecord struct {
	ts   time.Time
	ip   string
	file string // 下载文件名(可能为空)
	size int64  // 实际传输字节
}

var (
	// 日志行: 时间 [级别] [用户名] [角色] 操作 详情
	downloadLogLineRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})\s*\[(.*?)\]\s*\[(.*?)\]\s*\[(.*?)\]\s*(\S+)\s*(.*)$`)
	downloadLogIPRe   = regexp.MustCompile(`IP[:\s]+([0-9a-fA-F:.]+)`)
	downloadLogSizeRe = regexp.MustCompile(`实际传输[:\s]*(\d+)\s*字节`)
	downloadLogFileRe = regexp.MustCompile(`下载文件:\s*(.*?)(?:,\s*实际传输|\[协议|$)`)
)

// parseDownloadLogLine 解析一行审计日志,仅当其为文件下载记录且能提取到 IP 时返回 true。
// 行示例:
//
//	2026-09-01 21:38:18 [info] [admin] [admin] download_file 下载文件: a.exe, 实际传输: 1048576字节 [协议: https, 端口: 443, IP: 1.2.3.4]
func parseDownloadLogLine(line string) (logDownloadRecord, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return logDownloadRecord{}, false
	}
	matches := downloadLogLineRe.FindStringSubmatch(line)
	if len(matches) < 7 {
		return logDownloadRecord{}, false
	}
	action := matches[5]
	details := matches[6]
	// 仅统计文件分享下载:当前/历史版本动作均为 download_file,详情含「下载文件:」;
	// 收紧判定可避免把任务下载管理(resume/pause 等含「下载」字样的操作)误计为一次 IP 下载
	if action != "download_file" && !strings.Contains(details, "下载文件:") {
		return logDownloadRecord{}, false
	}
	ipMatches := downloadLogIPRe.FindStringSubmatch(details)
	if len(ipMatches) < 2 {
		return logDownloadRecord{}, false
	}
	ts, err := time.Parse("2006-01-02 15:04:05", matches[1])
	if err != nil {
		return logDownloadRecord{}, false
	}
	var size int64
	if sizeMatches := downloadLogSizeRe.FindStringSubmatch(details); len(sizeMatches) >= 2 {
		if n, err := strconv.ParseInt(sizeMatches[1], 10, 64); err == nil {
			size = n
		}
	}
	file := ""
	if fileMatches := downloadLogFileRe.FindStringSubmatch(details); len(fileMatches) >= 2 {
		file = strings.TrimSpace(fileMatches[1])
	}
	return logDownloadRecord{ts: ts, ip: ipMatches[1], file: file, size: size}, true
}

// tempIPStats 回填过程用的临时聚合结构
type tempIPStats struct {
	count          int64
	bandwidth      int64
	dailyCount     int64
	dailyBandwidth int64
	firstSeen      time.Time
	lastDownload   time.Time
}

// collectIPStatsFromLogs 扫描全部历史审计日志(server_*.log 与轮转压缩的 server_*.log.gz),
// 按 (IP+文件) 60s 合并窗口折算「逻辑下载」次数(与实时计数口径一致),带宽逐行累加。
// 仅扫描不落盘,由调用方决定合并进现有数据还是整体重建。
func collectIPStatsFromLogs() map[string]*tempIPStats {
	logDir := config.AppConfig.Server.LogDir
	var files []string
	if plain, err := filepath.Glob(filepath.Join(logDir, "server_*.log")); err == nil {
		files = append(files, plain...)
	}
	// 超过 10MB 被轮转压缩的历史日志同样参与回填,避免漏统计
	if gz, err := filepath.Glob(filepath.Join(logDir, "server_*.log.gz")); err == nil {
		files = append(files, gz...)
	}
	// 文件名含日期/时间戳,字典序 ≈ 时间升序,保证合并窗口按时间推进判定
	sort.Strings(files)

	today := time.Now().Format("2006-01-02")
	tracker := make(map[string]time.Time) // key = ip\x00文件 → 该窗口内最近一次传输时间
	result := make(map[string]*tempIPStats)

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

			key := rec.ip + "\x00" + rec.file
			isNew := true
			if last, exists := tracker[key]; exists && rec.ts.Sub(last) <= ipImportMergeWindow {
				// 同一逻辑下载的后续分片/续传段,仅累计流量,不计次数
				isNew = false
			}
			tracker[key] = rec.ts // 与实时 markLogicalDownload 一致:窗口内刷新最近时间(滑动窗口)

			st, exists := result[rec.ip]
			if !exists {
				st = &tempIPStats{firstSeen: rec.ts, lastDownload: rec.ts}
				result[rec.ip] = st
			}
			if isNew {
				st.count++
			}
			st.bandwidth += rec.size
			if rec.ts.Format("2006-01-02") == today {
				if isNew {
					st.dailyCount++
				}
				st.dailyBandwidth += rec.size
			}
			if rec.ts.Before(st.firstSeen) {
				st.firstSeen = rec.ts
			}
			if rec.ts.After(st.lastDownload) {
				st.lastDownload = rec.ts
			}
		}
	}
	return result
}

// readLogLines 读取日志文件全部行;.gz 结尾的压缩轮转文件自动解压
func readLogLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var reader io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return strings.Split(string(data), "\n"), nil
}

// fillIPStatsEntry 将回填聚合结果写入一条统计条目(仅统计字段,不影响封禁状态)
func fillIPStatsEntry(entry *IPDownloadStats, temp *tempIPStats, today string) {
	entry.DownloadCount = temp.count
	entry.TotalBandwidth = temp.bandwidth
	entry.FirstSeen = temp.firstSeen
	entry.LastSeen = temp.lastDownload
	entry.LastDownloadTime = temp.lastDownload
	entry.DailyDate = today
	entry.DailyDownloadCount = temp.dailyCount
	entry.DailyBandwidth = temp.dailyBandwidth
}

// ImportIPStatsFromLogs 历史日志回填入口。
// 仅在 ip_stats.json 尚无任何数据时执行——此时日志是唯一统计来源,回填不会与实时双写重叠;
// 一旦已有数据(程序正常运行过)则跳过,杜绝同一下载事件被「实时 +1 与回填 +1」重复计数。
// 返回值:本次回填的 IP 数(跳过/无可用记录时为 0)。
func ImportIPStatsFromLogs() int {
	loadIPStats()
	if len(ipStatsData.IPStats) > 0 {
		fmt.Printf("[IP统计] 已有 IP 统计数据(%d 个 IP),跳过历史日志回填(避免重复计数)\n", len(ipStatsData.IPStats))
		return 0
	}

	collected := collectIPStatsFromLogs()
	if len(collected) == 0 {
		fmt.Println("[IP统计] 历史日志中无可用下载记录,跳过回填")
		return 0
	}

	today := time.Now().Format("2006-01-02")
	ipStatsMutex.Lock()
	for ip, temp := range collected {
		// 防御:若并发下已出现条目,跳过该 IP,绝不累加(避免双计)
		if _, exists := ipStatsData.IPStats[ip]; exists {
			continue
		}
		entry := &IPDownloadStats{IP: ip}
		fillIPStatsEntry(entry, temp, today)
		ipStatsData.IPStats[ip] = entry
	}
	ipStatsMutex.Unlock()

	saveIPStats()
	fmt.Printf("[IP统计] 从历史日志回填完成,共 %d 个 IP(次数已按 %ds 合并窗口折算)\n", len(collected), int(ipImportMergeWindow/time.Second))
	return len(collected)
}

// RebuildIPStatsFromLogs 强制重建 IP 下载统计,用于修复被旧版回填逻辑重复计数污染的存量数据。
// 步骤:备份旧 ip_stats.json 为 ip_stats.json.bak.<时间戳> → 保留全部封禁状态 →
// 按 (IP+文件) 60s 合并口径从全部历史日志(含 .gz)重建次数与流量。
// 触发途径:启动参数 -rebuild-ipstats / 配置 rebuild_ipstats_on_boot=true / 管理界面「自检修复」。
// 可重复执行(每次都会先备份)。返回:备份文件路径(可能为空)、保留的封禁数、错误。
func RebuildIPStatsFromLogs() (backupPath string, preserved int, err error) {
	logDir := config.AppConfig.Server.LogDir
	filePath := getIPStatsFilePath()

	loadIPStats()

	// 快照旧数据中的封禁状态(封禁与计数污染无关,必须保留)
	type blockedInfo struct {
		reason string
		at     time.Time
	}
	blocked := make(map[string]blockedInfo)
	ipStatsMutex.RLock()
	for ip, st := range ipStatsData.IPStats {
		if st != nil && st.Blocked {
			blocked[ip] = blockedInfo{reason: st.BlockReason, at: st.BlockedAt}
		}
	}
	ipStatsMutex.RUnlock()

	// 备份旧文件(存在才备份);与 saveIPStats 串行,防止运行中重建与周期落盘竞争同一路径
	ipStatsPersistMu.Lock()
	if _, statErr := os.Stat(filePath); statErr == nil {
		backupPath = filepath.Join(logDir, "ip_stats.json.bak."+time.Now().Format("20060102_150405"))
		if renameErr := os.Rename(filePath, backupPath); renameErr != nil {
			ipStatsPersistMu.Unlock()
			return "", 0, fmt.Errorf("备份原 IP 统计数据失败: %v", renameErr)
		}
		fmt.Printf("[IP统计] 已备份原统计数据: %s\n", backupPath)
	}
	ipStatsPersistMu.Unlock()

	collected := collectIPStatsFromLogs()
	today := time.Now().Format("2006-01-02")

	ipStatsMutex.Lock()
	ipStatsData.IPStats = make(map[string]*IPDownloadStats, len(collected)+len(blocked))
	// 先放回封禁占位(统计从日志重建)
	for ip, info := range blocked {
		ipStatsData.IPStats[ip] = &IPDownloadStats{
			IP:          ip,
			Blocked:     true,
			BlockReason: info.reason,
			BlockedAt:   info.at,
			FirstSeen:   info.at,
		}
	}
	// 再写入回填统计:封禁条目只补统计字段,非封禁 IP 建新条目
	for ip, temp := range collected {
		if existing, exists := ipStatsData.IPStats[ip]; exists {
			fillIPStatsEntry(existing, temp, today)
			continue
		}
		entry := &IPDownloadStats{IP: ip}
		fillIPStatsEntry(entry, temp, today)
		ipStatsData.IPStats[ip] = entry
	}
	ipStatsMutex.Unlock()

	saveIPStats()
	fmt.Printf("[IP统计] IP 统计重建完成: %d 个 IP 计入,保留 %d 个封禁状态\n", len(collected), len(blocked))
	return backupPath, len(blocked), nil
}

// InitIPStats 初始化 IP 下载统计(启动时加载)
func InitIPStats() {
	loadIPStats()
	if len(ipStatsData.IPStats) == 0 {
		// 全新部署/统计被清空:历史日志是唯一来源,做一次性回填
		ImportIPStatsFromLogs()
	} else {
		fmt.Printf("[IP统计] IP 下载统计已加载(%d 个 IP)\n", len(ipStatsData.IPStats))
	}
}
