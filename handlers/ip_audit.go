package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"regexp"
	"time"

	"go-download-server/config"
	"go-download-server/constants"
	"go-download-server/session"
	"go-download-server/utils"
)

// ---------- IP 统计自检修复(界面入口) ----------

// IPStatsAudit 一次「当前统计数据 vs 历史日志真值」对账的结果。
// 用于让管理员在界面上一键判断存量 ip_stats.json 是否被旧版
// (Range 分片重复计数 / 启动回填叠加)污染,并决定是否重建。
type IPStatsAudit struct {
	CurrentIPs       int     `json:"current_ips"`       // 当前 ip_stats.json 中的 IP 数
	CurrentDownloads int64   `json:"current_downloads"` // 当前累计下载次数
	CurrentBandwidth int64   `json:"current_bandwidth"` // 当前累计下载流量(字节)
	LogIPs           int     `json:"log_ips"`           // 日志还原的 IP 数
	LogDownloads     int64   `json:"log_downloads"`     // 日志按 (IP+文件)60s 窗口合并后的逻辑下载次数(真值)
	LogRawLines      int64   `json:"log_raw_lines"`     // 日志原始 download_file 行数(含分片,仅参考)
	LogBandwidth     int64   `json:"log_bandwidth"`     // 日志带宽全额(字节)
	LogFiles         int     `json:"log_files"`         // 参与扫描的日志文件数
	EarliestLogDate  string  `json:"earliest_log_date"` // 现存最早日志日期 YYYY-MM-DD(空=无日志)
	Ratio            float64 `json:"ratio"`             // 当前次数 / 日志逻辑真值(虚高倍数)
	OlderHistoryIPs  int     `json:"older_history_ips"` // first_seen 早于日志保留期的 IP 数
	HasLogs          bool    `json:"has_logs"`
	NeedFix          bool    `json:"need_fix"`
	Verdict          string  `json:"verdict"`
}

// logFileRe 匹配 server_YYYYMMDD.log 与轮转压缩的 server_YYYYMMDD.log.gz
var auditLogFileRe = regexp.MustCompile(`server_(\d{8})(?:\.log(?:\.gz)?)$`)

// auditLogMeta 返回日志目录的基础情况:文件数、原始 download_file 行数、最早日志日期。
func auditLogMeta() (files int, rawLines int64, earliest string) {
	logDir := config.AppConfig.Server.LogDir
	names := []string{}
	if plain, err := filepath.Glob(filepath.Join(logDir, "server_*.log")); err == nil {
		names = append(names, plain...)
	}
	if gz, err := filepath.Glob(filepath.Join(logDir, "server_*.log.gz")); err == nil {
		names = append(names, gz...)
	}
	var earliestT time.Time
	for _, name := range names {
		files++
		m := auditLogFileRe.FindStringSubmatch(filepath.Base(name))
		if len(m) >= 2 {
			if t, err := time.Parse("20060102", m[1]); err == nil {
				if earliestT.IsZero() || t.Before(earliestT) {
					earliestT = t
				}
			}
		}
		if lines, err := readLogLines(name); err == nil {
			for _, line := range lines {
				if _, ok := parseDownloadLogLine(line); ok {
					rawLines++
				}
			}
		}
	}
	if !earliestT.IsZero() {
		earliest = earliestT.Format("2006-01-02")
	}
	return files, rawLines, earliest
}

// AuditIPStats 执行一次对账(只读,不落盘)。
// 「日志真值」使用与实时计数完全一致的 (IP+文件)60s 滑动窗口口径(collectIPStatsFromLogs)。
func AuditIPStats() IPStatsAudit {
	res := IPStatsAudit{}

	// 当前数据快照
	ipStatsMutex.RLock()
	res.CurrentIPs = len(ipStatsData.IPStats)
	for _, st := range ipStatsData.IPStats {
		if st == nil {
			continue
		}
		res.CurrentDownloads += st.DownloadCount
		res.CurrentBandwidth += st.TotalBandwidth
	}
	ipStatsMutex.RUnlock()

	// 日志基础情况 + 逻辑真值
	res.LogFiles, res.LogRawLines, res.EarliestLogDate = auditLogMeta()
	collected := collectIPStatsFromLogs()
	res.LogIPs = len(collected)
	for _, t := range collected {
		res.LogDownloads += t.count
		res.LogBandwidth += t.bandwidth
	}
	res.HasLogs = res.LogFiles > 0

	// 早于日志保留期的历史 IP 数(重建会将统计截断到日志窗口)
	if res.EarliestLogDate != "" && res.CurrentIPs > 0 {
		if earliestT, err := time.Parse("2006-01-02", res.EarliestLogDate); err == nil {
			ipStatsMutex.RLock()
			for _, st := range ipStatsData.IPStats {
				if st != nil && !st.FirstSeen.IsZero() && st.FirstSeen.Before(earliestT) {
					res.OlderHistoryIPs++
				}
			}
			ipStatsMutex.RUnlock()
		}
	}

	// 虚高倍数与判定
	denom := res.LogDownloads
	if denom <= 0 {
		denom = 1
	}
	res.Ratio = math.Round(float64(res.CurrentDownloads)/float64(denom)*100) / 100

	switch {
	case !res.HasLogs:
		res.Verdict = "未找到历史审计日志(server_*.log / .gz)。全新部署或日志被清空时属正常;若怀疑数据异常,请先确认日志目录配置(log_dir)是否正确。"
	case res.CurrentDownloads == 0 && res.LogDownloads == 0:
		res.Verdict = "当前无统计且日志中也无下载记录,无需处理。"
	case res.CurrentDownloads > res.LogDownloads && res.Ratio >= 1.5:
		res.NeedFix = true
		extra := ""
		if res.OlderHistoryIPs > 0 {
			extra = fmt.Sprintf(" 注意:有 %d 个 IP 的活动早于现存日志(最早 %s),重建会将这些超期计数一并清除,请确认接受。", res.OlderHistoryIPs, res.EarliestLogDate)
		}
		res.Verdict = fmt.Sprintf("检测到当前下载次数约为日志真值的 %.1f 倍(当前 %d vs 日志逻辑 %d),符合旧版「Range 分片重复计数/启动回填叠加」污染特征,建议执行修复。%s", res.Ratio, res.CurrentDownloads, res.LogDownloads, extra)
	case res.OlderHistoryIPs > 0:
		res.Verdict = fmt.Sprintf("当前次数高于日志口径,但其中 %d 个 IP 的活动早于日志保留期(最早 %s),超出部分属合法历史累计;重建会将统计截断到日志窗口,请谨慎决定。", res.OlderHistoryIPs, res.EarliestLogDate)
	default:
		res.Verdict = "未检测到明显异常:当前统计与日志逻辑口径基本一致。"
	}
	return res
}

// ---------- HTTP 接口 ----------

// API: IP 统计自检(只读对账)。GET,管理员权限。
func APIIPStatsAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "message": "只允许GET请求"})
		return
	}
	user := session.GetCurrentUser(r)
	if user == nil || user.Role != constants.RoleAdmin {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": false, "message": "需要管理员权限"})
		return
	}
	audit := AuditIPStats()
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "audit": audit})
}

// API: 执行 IP 统计重建(界面修复按钮)。POST,管理员权限 + CSRF。
// 等价于启动参数 -rebuild-ipstats / 配置 rebuild_ipstats_on_boot=true。
func APIIPStatsRebuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "message": "只允许POST请求"})
		return
	}
	user := session.GetCurrentUser(r)
	if user == nil || user.Role != constants.RoleAdmin {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": false, "message": "需要管理员权限"})
		return
	}
	if !utils.ValidateCSRFTokenFromRequest(r) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "message": "CSRF令牌验证失败,请刷新页面后重试"})
		return
	}

	files, _, _ := auditLogMeta()
	if files == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": false, "message": "未找到历史日志,无法重建(避免误清空统计)。请确认日志目录存在后重试。"})
		return
	}

	backupPath, preserved, err := RebuildIPStatsFromLogs()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": false, "message": "重建失败: " + err.Error()})
		return
	}
	utils.LogUserAction(r, "rebuild_ip_stats", "从历史日志重建IP下载统计(自检修复)")

	// 返回修复后的新状态,前端直接展示
	audit := AuditIPStats()
	msg := "重建完成"
	if backupPath != "" {
		msg = fmt.Sprintf("重建完成,原数据已备份为 %s", filepath.Base(backupPath))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":           true,
		"message":           msg,
		"backup_file":       backupPath,
		"blocked_preserved": preserved,
		"audit":             audit,
	})
}

// ---------- 文件下载统计(stats.json)自检修复 ----------

// FileStatsAudit 一次「stats.json 当前数据 vs 历史日志真值」的对账结果。
// stats.json 承载首页累计下载、文件列表每文件次数与热力图;旧版每个 Range 分片传输
// 都 +1 download_count/热力点并整文件大小累加带宽,造成与 IP 统计同源且更严重的虚高。
type FileStatsAudit struct {
	CurrentFiles      int     `json:"current_files"`       // 当前 stats.json 中的文件条目数
	CurrentDownloads  int64   `json:"current_downloads"`   // 当前累计下载次数(含虚高)
	CurrentBandwidth  int64   `json:"current_bandwidth"`   // 当前累计流量(字节,含整文件大小×分片虚高)
	CurrentHeatPoints int     `json:"current_heat_points"` // 当前 download 热力点数
	LogFiles          int     `json:"log_files"`           // 日志中出现的文件数
	LogDownloads      int64   `json:"log_downloads"`       // 日志 (IP+文件)60s 合并后的逻辑下载次数(真值)
	LogRawLines       int64   `json:"log_raw_lines"`       // 日志原始 download_file 行数(含分片,仅参考)
	LogBandwidth      int64   `json:"log_bandwidth"`       // 日志带宽全额(每片实际字节,字节)
	LogFilesCount     int     `json:"log_files_scanned"`   // 参与扫描的日志文件数
	EarliestLogDate   string  `json:"earliest_log_date"`   // 现存最早日志日期
	Ratio             float64 `json:"ratio"`               // 当前次数 / 日志逻辑真值(虚高倍数)
	BandwidthRatio    float64 `json:"bandwidth_ratio"`     // 当前带宽 / 日志带宽(虚高倍数)
	OlderHistoryFiles int     `json:"older_history_files"` // last_download 早于日志保留期、含下载的文件数
	HasLogs           bool    `json:"has_logs"`
	NeedFix           bool    `json:"need_fix"`
	Verdict           string  `json:"verdict"`
}

// AuditFileStats 执行一次 stats.json 对账(只读,不落盘)。
// 日志真值使用与实时计数完全一致的 (IP+文件)60s 滑动窗口口径(collectFileStatsFromLogs)。
func AuditFileStats() FileStatsAudit {
	res := FileStatsAudit{}
	statsMutex.RLock()
	res.CurrentFiles = len(statsData.FileStatsMap)
	for _, st := range statsData.FileStatsMap {
		if st == nil {
			continue
		}
		res.CurrentDownloads += st.DownloadCount
		res.CurrentBandwidth += st.TotalBandwidth
	}
	for _, p := range statsData.HeatmapData {
		if p.Type == "download" {
			res.CurrentHeatPoints++
		}
	}
	statsMutex.RUnlock()

	files, rawLines, earliest := auditLogMeta()
	res.LogFilesCount = files
	res.LogRawLines = rawLines
	res.EarliestLogDate = earliest
	res.HasLogs = files > 0

	collected := collectFileStatsFromLogs()
	res.LogFiles = len(collected)
	for _, t := range collected {
		res.LogDownloads += t.count
		res.LogBandwidth += t.bandwidth
	}

	if res.EarliestLogDate != "" && res.CurrentFiles > 0 {
		if earliestT, err := time.Parse("2006-01-02", res.EarliestLogDate); err == nil {
			statsMutex.RLock()
			for _, st := range statsData.FileStatsMap {
				if st != nil && st.DownloadCount > 0 && !st.LastDownloadTime.IsZero() && st.LastDownloadTime.Before(earliestT) {
					res.OlderHistoryFiles++
				}
			}
			statsMutex.RUnlock()
		}
	}

	denom := res.LogDownloads
	if denom <= 0 {
		denom = 1
	}
	res.Ratio = math.Round(float64(res.CurrentDownloads)/float64(denom)*100) / 100
	bwDenom := res.LogBandwidth
	if bwDenom <= 0 {
		bwDenom = 1
	}
	res.BandwidthRatio = math.Round(float64(res.CurrentBandwidth)/float64(bwDenom)*100) / 100

	switch {
	case !res.HasLogs:
		res.Verdict = "未找到历史审计日志(server_*.log / .gz),无法对账。全新部署或日志被清空时属正常。"
	case res.CurrentDownloads == 0 && res.LogDownloads == 0:
		res.Verdict = "当前无下载统计且日志中也无下载记录,无需处理。"
	case res.CurrentDownloads > res.LogDownloads && res.Ratio >= 1.5:
		res.NeedFix = true
		extra := ""
		if res.OlderHistoryFiles > 0 {
			extra = fmt.Sprintf(" 注意:%d 个文件的最后下载早于现存日志(最早 %s),重建会将这些超期计数一并清除,请确认接受。", res.OlderHistoryFiles, res.EarliestLogDate)
		}
		bwNote := ""
		if res.BandwidthRatio >= 1.5 {
			bwNote = fmt.Sprintf(" 带宽虚高更严重:当前 %.0f GB vs 日志实际 %.1f GB(%.1f 倍)——旧版每分片都按整文件大小累加流量,重建会一并修正。",
				float64(res.CurrentBandwidth)/1073741824, float64(res.LogBandwidth)/1073741824, res.BandwidthRatio)
		}
		res.Verdict = fmt.Sprintf("检测到文件下载次数约为日志真值的 %.1f 倍(当前 %d vs 日志逻辑 %d),符合旧版「Range 分片重复计数」污染特征,建议执行修复。%s%s",
			res.Ratio, res.CurrentDownloads, res.LogDownloads, bwNote, extra)
	default:
		res.Verdict = "未检测到明显异常:当前文件统计与日志逻辑口径基本一致。"
	}
	return res
}

// API: stats.json 自检(只读对账)。GET,管理员权限。
// 与 APIIPStatsAudit 一并返回,前端自检修复弹窗同时展示两块。
func APIStatsAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "message": "只允许GET请求"})
		return
	}
	user := session.GetCurrentUser(r)
	if user == nil || user.Role != constants.RoleAdmin {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": false, "message": "需要管理员权限"})
		return
	}
	audit := AuditFileStats()
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "audit": audit})
}

// API: 执行 stats.json 重建(界面修复按钮)。POST,管理员权限 + CSRF。
// 等价于启动参数 -rebuild-stats / 配置 rebuild_stats_on_boot=true。
func APIStatsRebuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "message": "只允许POST请求"})
		return
	}
	user := session.GetCurrentUser(r)
	if user == nil || user.Role != constants.RoleAdmin {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": false, "message": "需要管理员权限"})
		return
	}
	if !utils.ValidateCSRFTokenFromRequest(r) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"success": false, "message": "CSRF令牌验证失败,请刷新页面后重试"})
		return
	}

	files, _, _ := auditLogMeta()
	if files == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": false, "message": "未找到历史日志,无法重建(避免误清空统计)。请确认日志目录存在后重试。"})
		return
	}

	backupPath, err := RebuildFileStatsFromLogs()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": false, "message": "重建失败: " + err.Error()})
		return
	}
	utils.LogUserAction(r, "rebuild_file_stats", "从历史日志重建文件下载统计 stats.json(自检修复)")

	audit := AuditFileStats()
	msg := "重建完成"
	if backupPath != "" {
		msg = fmt.Sprintf("重建完成,原数据已备份为 %s", filepath.Base(backupPath))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"message":     msg,
		"backup_file": backupPath,
		"audit":       audit,
	})
}

// writeJSON 统一 JSON 响应
func writeJSON(w http.ResponseWriter, status int, payload map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}
