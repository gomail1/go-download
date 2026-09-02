package handlers

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go-download-server/config"
	"go-download-server/utils"
)

// countingResponseWriter 包装http.ResponseWriter，记录实际写入的字节数
type countingResponseWriter struct {
	http.ResponseWriter
	written int64
	status  int
}

func (cw *countingResponseWriter) Write(b []byte) (int, error) {
	if cw.status == 0 {
		cw.status = http.StatusOK
	}
	n, err := cw.ResponseWriter.Write(b)
	cw.written += int64(n)
	return n, err
}

func (cw *countingResponseWriter) WriteHeader(statusCode int) {
	cw.status = statusCode
	cw.ResponseWriter.WriteHeader(statusCode)
}

// Flush 实现http.Flusher接口，确保流式传输正常工作
func (cw *countingResponseWriter) Flush() {
	if flusher, ok := cw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// 预定义自动化工具User-Agent关键字map，用于快速检测
var automatedAgents = map[string]bool{
	"curl":        true,
	"wget":        true,
	"python":      true,
	"requests":    true,
	"scrapy":      true,
	"robot":       true,
	"bot":         true,
	"spider":      true,
	"crawler":     true,
	"fetch":       true,
	"axios":       true,
	"node-fetch":  true,
	"http-client": true,
	"golang":      true,
	"go-http":     true,
	"java-http":   true,
	"ruby":        true,
	"perl":        true,
	"phantomjs":   true,
	"splash":      true,
	"selenium":    true,
	"webdriver":   true,
}

// 文件下载处理函数
func DownloadHandler(w http.ResponseWriter, r *http.Request) {
	// 脚本下载检测
	userAgent := r.Header.Get("User-Agent")
	clientIP := utils.GetClientIP(r)

	// 0. 检查IP是否被封禁
	if blocked, reason := IsIPBlocked(clientIP); blocked {
		utils.Log(utils.LogLevelSecurity, "anonymous", "guest", "download_blocked", fmt.Sprintf("IP: %s 已被封禁，拒绝下载，原因: %s", clientIP, reason))
		http.Error(w, fmt.Sprintf("您的IP已被封禁: %s", reason), http.StatusForbidden)
		return
	}

	// 0.1 检查IP流量限额
	if limited, reason, limitType := CheckIPLimit(clientIP); limited {
		utils.Log(utils.LogLevelSecurity, "anonymous", "guest", "download_limited", fmt.Sprintf("IP: %s 超过流量限额被拒绝，类型: %s, 原因: %s", clientIP, limitType, reason))
		// 如果启用了自动封禁，则自动封禁该IP
		if config.AppConfig.IPLimit.AutoBlock {
			blockReason := config.AppConfig.IPLimit.AutoBlockReason
			if blockReason == "" {
				blockReason = "超过流量限额自动封禁"
			}
			BlockIP(clientIP, blockReason)
			utils.Log(utils.LogLevelSecurity, "anonymous", "guest", "ip_auto_blocked", fmt.Sprintf("IP: %s 因超过流量限额被自动封禁", clientIP))
		}
		http.Error(w, fmt.Sprintf("下载被限制: %s", reason), http.StatusTooManyRequests)
		return
	}

	// 1. 检测自动化工具User-Agent
	lowerUA := strings.ToLower(userAgent)
	for agent := range automatedAgents {
		if strings.Contains(lowerUA, agent) {
			utils.Log(utils.LogLevelSecurity, "anonymous", "guest", "download_blocked", fmt.Sprintf("IP: %s 使用自动化工具下载被阻止，User-Agent: %s", clientIP, userAgent))
			http.Error(w, "自动化下载被禁止", http.StatusForbidden)
			return
		}
	}

	// 2. 检测请求特征
	// 检查是否缺少浏览器特有的请求头
	if userAgent == "" || !strings.Contains(lowerUA, "mozilla") {
		utils.Log(utils.LogLevelSecurity, "anonymous", "guest", "download_blocked", fmt.Sprintf("IP: %s 请求缺少浏览器特征被阻止，User-Agent: %s", clientIP, userAgent))
		http.Error(w, "自动化下载被禁止", http.StatusForbidden)
		return
	}

	// 获取文件路径
	// 注意：r.URL.Query().Get() 已经自动进行了URL解码，不需要再手动解码
	// 否则会导致二次解码，把 + 字符错误地解码为空格
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "缺少文件路径", http.StatusBadRequest)
		return
	}

	// 使用安全路径验证工具，防止路径遍历攻击
	safePath := utils.ValidateSafePath(config.AppConfig.Server.DownloadDir, path)
	if !safePath.IsSafe {
		utils.Log(utils.LogLevelSecurity, "anonymous", "guest", "path_traversal_blocked",
			fmt.Sprintf("IP: %s 下载路径遍历被阻止，path=%s，原因: %v", clientIP, path, safePath.Error))
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// 使用验证后的安全路径
	fullPath := safePath.FullPath
	path = safePath.RelativePath

	// 统一路径分隔符为正斜杠，确保统计数据和分类映射的key一致
	// 避免Windows上反斜杠与Linux/macOS上正斜杠导致的不一致问题
	path = utils.NormalizePath(path)

	// 检查文件是否存在
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		log.Printf("文件检查错误: %v\n", err)
		http.Error(w, "文件不存在", http.StatusNotFound)
		return
	}

	// 检查是否为目录
	if fileInfo.IsDir() {
		http.Error(w, "不能下载目录", http.StatusBadRequest)
		return
	}

	// 获取文件名并确保正确编码
	fileName := filepath.Base(fullPath)

	// 设置响应头，确保文件名正确编码
	// 使用 PathEscape 而不是 QueryEscape，因为 filename* 参数使用 URL 路径编码规则
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(fileName)))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))

	// 使用自定义ResponseWriter记录实际传输的字节数
	// 这解决了多线程下载工具（如IDM、迅雷）通过Range请求分块下载时，
	// 总流量被重复计算为文件大小×线程数的问题
	cw := &countingResponseWriter{ResponseWriter: w}

	// 发送文件 - 这是核心功能，必须确保正常工作
	// http.ServeFile会自动处理Range请求，只发送请求的部分
	http.ServeFile(cw, r, fullPath)

	// 获取实际传输的字节数
	actualBytes := cw.written
	
	// 对于206 Partial Content（Range请求），只记录实际传输的字节数
	// 对于200 OK（完整下载），actualBytes应该等于文件大小
	// 如果actualBytes为0（可能是HEAD请求或错误），不记录统计
	if actualBytes > 0 {
		// 同步更新下载统计，确保数据被正确记录
		// 从请求中获取IP地址
		logIP := utils.GetClientIP(r)

		// 增加下载次数，使用实际传输的字节数而不是文件完整大小
		// 这样多线程下载的总流量就是各部分实际传输字节数之和，
		// 而不是文件大小×线程数
		IncrementDownloadCount(path, logIP, actualBytes)

		// 记录IP下载统计
		RecordIPDownload(logIP, actualBytes)

		// 立即保存统计数据，确保数据被持久化
		SaveStatsData()

		// 记录用户下载操作日志
		utils.LogUserAction(r, "download_file", fmt.Sprintf("下载文件: %s, 实际传输: %d字节", path, actualBytes))
	}
}
