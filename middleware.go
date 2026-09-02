package main

import (
	"bufio"
	"compress/gzip"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"go-download-server/utils"
)

// gzipResponseWriter 包装http.ResponseWriter以支持Gzip压缩
type gzipResponseWriter struct {
	http.ResponseWriter
	writer *gzip.Writer
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	return w.writer.Write(b)
}

func (w *gzipResponseWriter) Flush() {
	w.writer.Flush()
}

// GzipMiddleware Gzip压缩中间件
func GzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 检查客户端是否支持Gzip
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		// 不对文件下载进行压缩（避免性能问题）
		if strings.HasPrefix(r.URL.Path, "/download") || strings.HasPrefix(r.URL.Path, "/s/") {
			next.ServeHTTP(w, r)
			return
		}

		// WebSocket 升级需要 Hijack，跳过 gzip 包装
		if strings.HasPrefix(r.URL.Path, "/ws/") {
			next.ServeHTTP(w, r)
			return
		}

		// 设置Gzip响应头
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")

		// 创建Gzip写入器
		gz := gzip.NewWriter(w)
		defer gz.Close()

		// 包装ResponseWriter
		gzw := &gzipResponseWriter{
			ResponseWriter: w,
			writer:         gz,
		}

		next.ServeHTTP(gzw, r)
	})
}

// CacheMiddleware 静态资源缓存中间件
func CacheMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 只对静态资源设置缓存
		if strings.HasPrefix(r.URL.Path, "/static/") {
			// 根据文件扩展名设置缓存时间
			cacheTime := getCacheTime(r.URL.Path)
			if cacheTime > 0 {
				w.Header().Set("Cache-Control", "public, max-age="+itoa(cacheTime))
				w.Header().Set("Expires", time.Now().Add(time.Duration(cacheTime)*time.Second).UTC().Format(http.TimeFormat))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// getCacheTime 根据文件扩展名返回缓存时间（秒）
func getCacheTime(path string) int {
	switch {
	case strings.HasSuffix(path, ".css"):
		return 86400 // 1天
	case strings.HasSuffix(path, ".js"):
		return 86400 // 1天
	case strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".jpg") || strings.HasSuffix(path, ".jpeg") || strings.HasSuffix(path, ".gif") || strings.HasSuffix(path, ".svg") || strings.HasSuffix(path, ".webp"):
		return 604800 // 7天
	case strings.HasSuffix(path, ".woff") || strings.HasSuffix(path, ".woff2") || strings.HasSuffix(path, ".ttf") || strings.HasSuffix(path, ".eot"):
		return 2592000 // 30天
	case strings.HasSuffix(path, ".ico"):
		return 2592000 // 30天
	default:
		return 0
	}
}

// itoa 简单的整数转字符串（避免引入strconv）
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// SecurityHeadersMiddleware 安全响应头中间件
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 添加安全响应头
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// 对HTML页面添加Content-Security-Policy
		if strings.HasSuffix(r.URL.Path, "/") || strings.HasSuffix(r.URL.Path, ".html") || !strings.Contains(r.URL.Path, ".") {
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self';")
		}

		next.ServeHTTP(w, r)
	})
}

// LoggingMiddleware 请求日志中间件
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// 包装ResponseWriter以捕获状态码
		rw := &statusResponseWriter{
			ResponseWriter: w,
			status:         http.StatusOK,
		}

		next.ServeHTTP(rw, r)

		// 计算响应时间
		duration := time.Since(start)

		// 记录应用性能监控数据
		// 只记录非静态资源请求，避免静态资源请求影响性能统计
		if !strings.HasPrefix(r.URL.Path, "/static/") {
			success := rw.status < 400
			utils.GetMonitor().RecordRequest(success, duration)
		}

		// 记录请求日志（只记录非静态资源请求）
		if !strings.HasPrefix(r.URL.Path, "/static/") {
			clientIP := getClientIP(r)
			// 使用标准日志包记录
			logRequest(clientIP, r.Method, r.URL.Path, rw.status, duration)
		}
	})
}

// statusResponseWriter 包装http.ResponseWriter以捕获状态码
type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusResponseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Hijack 转发底层连接劫持，WebSocket 升级必需
func (w *statusResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return h.Hijack()
}

// Flush 支持流式响应（转发给底层 writer，若支持）
func (w *statusResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// getClientIP 获取客户端IP
func getClientIP(r *http.Request) string {
	// 检查X-Forwarded-For头
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// 取第一个IP
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	// 检查X-Real-IP头
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	// 返回远程地址
	return r.RemoteAddr
}

// logRequest 记录请求日志（简单实现，实际应使用项目的日志系统）
func logRequest(ip, method, path string, status int, duration time.Duration) {
	// 这里可以集成到项目的日志系统中
	// 目前只记录异常请求（状态码>=400）
	if status >= 400 {
		// 使用标准日志包
		// log.Printf("[%s] %s %s - %d (%v)", ip, method, path, status, duration)
	}
}

// CompressReader 包装io.Reader以支持Gzip解压（用于请求体解压）
type CompressReader struct {
	reader io.Reader
	closer io.Closer
}

func (c *CompressReader) Read(p []byte) (n int, err error) {
	return c.reader.Read(p)
}

func (c *CompressReader) Close() error {
	if c.closer != nil {
		return c.closer.Close()
	}
	return nil
}
