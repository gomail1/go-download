package httpx

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"sync"
	"time"

	"go-download-server/internal/core"
	"go-download-server/internal/logger"
)

// rateLimiter implements a simple token bucket rate limiter
// that wraps an io.Reader to limit the read speed

type rateLimiter struct {
	reader     io.Reader
	limit      int64 // bytes per second
	bucket     int64 // current tokens in bucket
	lastRefill time.Time
	mu         sync.Mutex
}

// newRateLimiter creates a new rate limiter
func newRateLimiter(reader io.Reader, limit int64) *rateLimiter {
	return &rateLimiter{
		reader:     reader,
		limit:      limit,
		bucket:     limit, // initial tokens
		lastRefill: time.Now(),
	}
}

// Read implements the io.Reader interface with rate limiting
func (rl *rateLimiter) Read(p []byte) (n int, err error) {
	// No limit if limit is 0
	if rl.limit <= 0 {
		return rl.reader.Read(p)
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Refill the bucket
	rl.refill()

	// Determine how many bytes to read
	if int64(len(p)) > rl.bucket {
		p = p[:rl.bucket]
	}

	// Read from the underlying reader
	n, err = rl.reader.Read(p)
	if n > 0 {
		// Consume tokens
		rl.bucket -= int64(n)
	}

	return
}

// refill adds tokens to the bucket based on elapsed time
func (rl *rateLimiter) refill() {
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill)
	rl.lastRefill = now

	// Calculate tokens to add
	tokensToAdd := int64(elapsed.Seconds() * float64(rl.limit))
	if tokensToAdd > 0 {
		// Don't exceed bucket capacity
		rl.bucket += tokensToAdd
		if rl.bucket > rl.limit {
			rl.bucket = rl.limit
		}
	}
}

// HTTPProtocol implements the core.Protocol interface for HTTP/HTTPS
type HTTPProtocol struct {
	client       *http.Client
	isRunning    bool
	isPaused     bool
	status       core.Status
	statistics   core.Statistics
	config       core.ProtocolConfig
	connPool     *core.ConnectionPool
	resourceCtrl *core.ResourceController
}

// NewHTTPProtocol creates a new HTTPProtocol instance
func NewHTTPProtocol() *HTTPProtocol {
	// 创建CookieJar，支持Cookie持久化
	jar, _ := cookiejar.New(nil)

	return &HTTPProtocol{
		client: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar, // 添加Cookie支持
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxConnsPerHost:     100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		status: core.Status{
			IsRunning: false,
			IsPaused:  false,
		},
		statistics: core.Statistics{
			StartTime: time.Now(),
		},
	}
}

// CanHandle checks if the URL can be handled by HTTP protocol
func (h *HTTPProtocol) CanHandle(url string) bool {
	return strings.HasPrefix(strings.ToLower(url), "http://") || strings.HasPrefix(strings.ToLower(url), "https://")
}

// GetMetadata gets the metadata of the URL
func (h *HTTPProtocol) GetMetadata(ctx context.Context, url string) (*core.Metadata, error) {
	// 先尝试使用HEAD请求获取元数据
	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		logger.Errorf("Failed to create HEAD request for %s: %v", url, err)
		return nil, err
	}

	// 添加浏览器请求头
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	logger.Infof("Sending HEAD request to: %s", url)
	resp, err := h.client.Do(req)
	if err != nil {
		logger.Errorf("HEAD request failed for %s: %v", url, err)
		return nil, err
	}
	defer resp.Body.Close()

	logger.Infof("HEAD request response status: %d", resp.StatusCode)
	logger.Infof("HEAD request Content-Type: %s", resp.Header.Get("Content-Type"))
	logger.Infof("HEAD request Content-Length: %d", resp.ContentLength)

	// 检查是否是HTML页面（安全检测页面）
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") {
		// 如果是HTML，使用GET请求尝试获取实际的文件
		getReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, err
		}

		// 复制相同的请求头
		getReq.Header = req.Header.Clone()

		getResp, err := h.client.Do(getReq)
		if err != nil {
			return nil, err
		}
		defer getResp.Body.Close()

		// 检查GET请求的结果
		getContentType := getResp.Header.Get("Content-Type")
		if strings.Contains(getContentType, "text/html") {
			// 仍然是HTML，返回默认的元数据，尝试直接下载
			filename := getFilenameFromResponse(getResp, url)
			return &core.Metadata{
				Filename: filename,
				Size:     0, // 无法确定实际大小
				MimeType: "application/octet-stream",
				Headers:  make(map[string]string),
				ProtocolSpecific: map[string]interface{}{
					"status_code": getResp.StatusCode,
					"headers":     getResp.Header,
				},
			}, nil
		}

		// GET请求返回了实际的文件，使用GET请求的结果
		filename := getFilenameFromResponse(getResp, url)
		size := getResp.ContentLength

		metadata := &core.Metadata{
			Filename: filename,
			Size:     size,
			MimeType: getContentType,
			Headers:  make(map[string]string),
			ProtocolSpecific: map[string]interface{}{
				"status_code": getResp.StatusCode,
				"headers":     getResp.Header,
			},
		}

		// 复制相关头信息
		for key, values := range getResp.Header {
			if len(values) > 0 {
				metadata.Headers[key] = values[0]
			}
		}

		logger.Debugf("Metadata retrieved with GET for %s: %+v", url, metadata)
		return metadata, nil
	}

	// HEAD请求成功，返回元数据
	filename := getFilenameFromResponse(resp, url)
	size := resp.ContentLength

	metadata := &core.Metadata{
		Filename: filename,
		Size:     size,
		MimeType: contentType,
		Headers:  make(map[string]string),
		ProtocolSpecific: map[string]interface{}{
			"status_code": resp.StatusCode,
			"headers":     resp.Header,
		},
	}

	// 复制相关头信息
	for key, values := range resp.Header {
		if len(values) > 0 {
			metadata.Headers[key] = values[0]
		}
	}

	logger.Debugf("Metadata retrieved with HEAD for %s: %+v", url, metadata)
	return metadata, nil
}

// Download downloads the file from the URL using multi-threading
func (h *HTTPProtocol) Download(ctx context.Context, task *core.Task, progress chan<- core.Progress) error {
	logger.Infof("HTTPProtocol.Download called for task: %s, URL: %s", task.ID, task.URL)

	// Set status
	h.isRunning = true
	h.isPaused = false
	h.status.IsRunning = true

	defer func() {
		h.isRunning = false
		h.status.IsRunning = false
	}()

	// Get metadata if not available
	if task.Metadata == nil {
		logger.Infof("Getting metadata for task: %s", task.ID)
		metadata, err := h.GetMetadata(ctx, task.URL)
		if err != nil {
			logger.Errorf("Failed to get metadata for task %s: %v", task.ID, err)
			return err
		}
		task.Metadata = metadata
		logger.Infof("Got metadata for task %s: Size: %d, Filename: %s", task.ID, metadata.Size, metadata.Filename)
	}

	// Get total size
	totalSize := task.Metadata.Size
	logger.Infof("Task %s, total size: %d", task.ID, totalSize)

	// Create destination file
	destPath := task.Config.SavePath + "/" + task.Metadata.Filename
	logger.Infof("Creating destination file: %s", destPath)
	file, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		logger.Errorf("Failed to create destination file %s: %v", destPath, err)
		return err
	}
	defer file.Close()

	// Start time
	startTime := time.Now()
	h.statistics.StartTime = startTime

	// Check if we can use multi-threading (requires known file size and server support for range requests)
	if totalSize > 0 {
		// Determine number of threads
		maxThreads := task.Config.MaxThreads
		if maxThreads <= 0 {
			maxThreads = 4 // Default threads
		}
		if maxThreads > 32 {
			maxThreads = 32 // Maximum threads limit
		}

		// Pre-allocate file size if known - this prevents file corruption
		if task.Config.PreAllocate {
			err = file.Truncate(totalSize)
			if err != nil {
				logger.Errorf("Failed to pre-allocate file: %v", err)
				return err
			}
		}

		// Calculate chunk size
		chunkSize := totalSize / int64(maxThreads)
		remaining := totalSize % int64(maxThreads)

		// Create chunks
		var chunks []*core.Chunk
		var offset int64
		for i := 0; i < maxThreads; i++ {
			chunk := &core.Chunk{
				ID:     fmt.Sprintf("chunk-%d", i),
				TaskID: task.ID,
				Offset: offset,
				Size:   chunkSize,
				Status: core.ChunkStatusPending,
			}

			// Add remaining bytes to last chunk
			if i == maxThreads-1 {
				chunk.Size += remaining
			}

			chunks = append(chunks, chunk)
			offset += chunkSize
		}

		// Update task statistics
		task.Progress.TotalSize = totalSize
		task.Progress.TotalChunks = len(chunks)
		h.statistics.Connections = maxThreads

		// Progress update channel - for small files, update more frequently
		progressUpdateTicker := time.NewTicker(100 * time.Millisecond)
		defer progressUpdateTicker.Stop()

		// Wait group for chunks
		var wg sync.WaitGroup

		logger.Infof("Starting %d chunks for task %s", len(chunks), task.ID)

		// Download each chunk in parallel
		for _, chunk := range chunks {
			wg.Add(1)
			logger.Infof("Launching chunk %s in goroutine", chunk.ID)
			go h.downloadChunk(ctx, chunk, file, task, &wg)
		}

		// Progress reporting goroutine
		go func() {
			for {
				select {
				case <-progressUpdateTicker.C:
					h.reportProgress(startTime, chunks, progress, task)
				case <-ctx.Done():
					return
				}
			}
		}()

		// Wait for all chunks to complete
		wg.Wait()

		// Final progress report
		h.reportProgress(startTime, chunks, progress, task)

		// Update final status
		h.statistics.EndTime = new(time.Time)
		*h.statistics.EndTime = time.Now()
		h.statistics.Duration = h.statistics.EndTime.Sub(h.statistics.StartTime)
		h.statistics.Downloaded = totalSize
	} else {
		// Single thread download for unknown file size
		h.statistics.Connections = 1

		// Update task statistics
		task.Progress.TotalChunks = 1
		task.Progress.TotalSize = -1 // Unknown size

		// Create a single chunk for the entire file
		chunk := &core.Chunk{
			ID:     "chunk-0",
			TaskID: task.ID,
			Offset: 0,
			Size:   -1, // Unknown size
			Status: core.ChunkStatusDownloading,
		}

		// Create request
		req, err := http.NewRequestWithContext(ctx, "GET", task.URL, nil)
		if err != nil {
			return err
		}

		// 添加完整的浏览器请求头，模拟真实浏览器
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
		req.Header.Set("Accept-Encoding", "gzip, deflate, br")
		req.Header.Set("Connection", "keep-alive")
		req.Header.Set("Upgrade-Insecure-Requests", "1")
		req.Header.Set("Sec-Fetch-Dest", "document")
		req.Header.Set("Sec-Fetch-Mode", "navigate")
		req.Header.Set("Sec-Fetch-Site", "none")
		req.Header.Set("Sec-Fetch-User", "?1")
		req.Header.Set("Cache-Control", "max-age=0")

		// Send request
		resp, err := h.client.Do(req)
		if err != nil {
			chunk.Status = core.ChunkStatusFailed
			return err
		}
		defer resp.Body.Close()

		// Create buffer
		buffer := make([]byte, 8192)
		var downloaded int64

		// Progress update ticker - for small files, update more frequently
		progressUpdateTicker := time.NewTicker(100 * time.Millisecond)
		defer progressUpdateTicker.Stop()

		// Create rate limiter if speed limit is set
		var body io.Reader
		if task.Config.SpeedLimit > 0 {
			body = newRateLimiter(resp.Body, task.Config.SpeedLimit)
		} else {
			body = resp.Body
		}

		// Download loop
		for {
			// Check if paused
			if h.isPaused {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// Check if context is canceled
			select {
			case <-ctx.Done():
				chunk.Status = core.ChunkStatusCancelled
				return ctx.Err()
			default:
			}

			// Read data with rate limiting
			n, err := body.Read(buffer)
			if err != nil {
				if err == io.EOF {
					break // Download completed
				}
				chunk.Status = core.ChunkStatusFailed
				return err
			}

			// Write to file
			_, err = file.Write(buffer[:n])
			if err != nil {
				chunk.Status = core.ChunkStatusFailed
				return err
			}

			// Update downloaded bytes
			downloaded += int64(n)

			// Update progress
			select {
			case <-progressUpdateTicker.C:
				elapsed := time.Since(startTime).Seconds()
				if elapsed < 0.1 {
					elapsed = 0.1 // Avoid division by zero
				}
				speed := int64(float64(downloaded) / elapsed)

				var eta time.Duration
				if speed > 0 && task.Metadata.Size > 0 {
					remaining := task.Metadata.Size - downloaded
					eta = time.Duration(float64(remaining)/float64(speed)) * time.Second
				}

				percentage := 0.0
				if task.Metadata.Size > 0 {
					percentage = float64(downloaded) / float64(task.Metadata.Size) * 100
				}

				task.Progress.Downloaded = downloaded
				task.Progress.Percentage = percentage
				task.Progress.Speed = speed
				task.Progress.ETA = eta
				task.Progress.CurrentChunk = 1

				progress <- core.Progress{
					Percentage:   percentage,
					Downloaded:   downloaded,
					TotalSize:    task.Metadata.Size,
					Speed:        speed,
					ETA:          eta,
					CurrentChunk: 1,
					TotalChunks:  1,
				}
			case <-ctx.Done():
				chunk.Status = core.ChunkStatusCancelled
				return ctx.Err()
			default:
				// Non-blocking select, continue downloading
			}
		}

		// Update final progress
		elapsed := time.Since(startTime)
		speed := int64(float64(downloaded) / elapsed.Seconds())

		// Update metadata size now that we know it
		task.Metadata.Size = downloaded

		task.Progress.Downloaded = downloaded
		task.Progress.Percentage = 100
		task.Progress.Speed = speed
		task.Progress.ETA = 0
		task.Progress.CurrentChunk = 1

		progress <- core.Progress{
			Percentage:   100,
			Downloaded:   downloaded,
			TotalSize:    downloaded,
			Speed:        speed,
			ETA:          0,
			CurrentChunk: 1,
			TotalChunks:  1,
		}

		// Update final status
		h.statistics.EndTime = new(time.Time)
		*h.statistics.EndTime = time.Now()
		h.statistics.Duration = h.statistics.EndTime.Sub(h.statistics.StartTime)
		h.statistics.Downloaded = downloaded
	}

	return nil
}

// downloadChunk downloads a single chunk
func (h *HTTPProtocol) downloadChunk(ctx context.Context, chunk *core.Chunk, file *os.File, task *core.Task, wg *sync.WaitGroup) error {
	defer wg.Done()

	logger.Infof("downloadChunk called for chunk %s", chunk.ID)

	// Check if paused before starting
	if h.isPaused {
		logger.Infof("Chunk %s paused before start", chunk.ID)
		return nil
	}

	// Allocate network resource
	err := h.resourceCtrl.Allocate(core.ResourceTypeNetwork, 1, "http")
	if err != nil {
		logger.Errorf("Failed to allocate network resource for chunk %s: %v", chunk.ID, err)
		chunk.Status = core.ChunkStatusFailed
		return err
	}
	logger.Infof("Network resource allocated for chunk %s", chunk.ID)
	// Release network resource when done
	defer h.resourceCtrl.Release(core.ResourceTypeNetwork, 1, "http")

	// Create request with Range header
	logger.Infof("Creating HTTP request for chunk %s", chunk.ID)
	req, err := http.NewRequestWithContext(ctx, "GET", task.URL, nil)
	if err != nil {
		logger.Errorf("Failed to create HTTP request for chunk %s: %v", chunk.ID, err)
		chunk.Status = core.ChunkStatusFailed
		return err
	}

	// Set Range header
	rangeHeader := fmt.Sprintf("bytes=%d-%d", chunk.Offset, chunk.Offset+chunk.Size-1)
	req.Header.Set("Range", rangeHeader)
	logger.Infof("Range header set for chunk %s: %s", chunk.ID, rangeHeader)

	// 添加完整的浏览器请求头，模拟真实浏览器
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Cache-Control", "no-cache")

	// Send request
	logger.Infof("Sending HTTP request for chunk %s to URL: %s", chunk.ID, task.URL)
	resp, err := h.client.Do(req)
	if err != nil {
		logger.Errorf("Failed to send HTTP request for chunk %s: %v", chunk.ID, err)
		chunk.Status = core.ChunkStatusFailed
		return err
	}
	logger.Infof("Received response for chunk %s: StatusCode=%d, ContentLength=%d", chunk.ID, resp.StatusCode, resp.ContentLength)
	defer resp.Body.Close()

	// Check if server supports range requests
	if resp.StatusCode != http.StatusPartialContent {
		logger.Errorf("Server returned status %d for chunk %s, expected 206 Partial Content", resp.StatusCode, chunk.ID)
		chunk.Status = core.ChunkStatusFailed
		return fmt.Errorf("server does not support range requests")
	}

	// Update chunk status
	chunk.Status = core.ChunkStatusDownloading

	logger.Infof("Chunk %s started: Offset=%d, Size=%d", chunk.ID, chunk.Offset, chunk.Size)

	// Create buffer
	buffer := make([]byte, 8192)
	var downloaded int64

	// Create rate limiter if speed limit is set
	var body io.Reader
	if task.Config.SpeedLimit > 0 {
		// Divide speed limit by the number of threads to get per-thread limit
		perThreadLimit := task.Config.SpeedLimit / int64(task.Config.MaxThreads)
		if perThreadLimit <= 0 {
			perThreadLimit = 1
		}
		body = newRateLimiter(resp.Body, perThreadLimit)
	} else {
		body = resp.Body
	}

	// Start downloading
	logger.Infof("Starting download loop for chunk %s", chunk.ID)
	for {
		// Check if paused
		if h.isPaused {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Check if context is canceled
		select {
		case <-ctx.Done():
			logger.Infof("Context cancelled for chunk %s", chunk.ID)
			chunk.Status = core.ChunkStatusCancelled
			return ctx.Err()
		default:
		}

		// Check if paused
		if h.isPaused {
			logger.Infof("Chunk %s paused during download", chunk.ID)
			return nil
		}

		// Read data with rate limiting
		n, err := body.Read(buffer)
		if err != nil {
			if err == io.EOF {
				logger.Infof("Chunk %s reached EOF: Downloaded=%d bytes, Expected=%d bytes", chunk.ID, downloaded, chunk.Size)
				// Check if we downloaded the entire chunk
				if downloaded < chunk.Size {
					logger.Errorf("Chunk %s incomplete: Downloaded=%d bytes, Expected=%d bytes", chunk.ID, downloaded, chunk.Size)
					chunk.Status = core.ChunkStatusFailed
					return fmt.Errorf("incomplete chunk download: received %d bytes, expected %d bytes", downloaded, chunk.Size)
				}
				break // Chunk completed
			}
			logger.Errorf("Error reading data for chunk %s: %v", chunk.ID, err)
			chunk.Status = core.ChunkStatusFailed
			return err
		}

		// Write to file at specific offset - WriteAt is thread-safe for different offsets
		writeOffset := chunk.Offset + downloaded
		_, err = file.WriteAt(buffer[:n], writeOffset)
		if err != nil {
			logger.Errorf("Error writing data for chunk %s at offset %d: %v", chunk.ID, writeOffset, err)
			chunk.Status = core.ChunkStatusFailed
			return err
		}

		// Update downloaded bytes
		downloaded += int64(n)
		chunk.Downloaded = downloaded

		// Log progress every 1MB
		if downloaded%(1024*1024) == 0 {
			logger.Infof("Chunk %s progress: Downloaded=%d bytes", chunk.ID, downloaded)
		}
	}

	// Update chunk status
	chunk.Status = core.ChunkStatusCompleted
	return nil
}

// reportProgress calculates and reports progress
func (h *HTTPProtocol) reportProgress(startTime time.Time, chunks []*core.Chunk, progress chan<- core.Progress, task *core.Task) {
	// Calculate total downloaded
	var totalDownloaded int64
	var completedChunks int

	for _, chunk := range chunks {
		if chunk.Status == core.ChunkStatusCompleted {
			totalDownloaded += chunk.Size
			completedChunks++
		} else {
			totalDownloaded += chunk.Downloaded
		}
	}

	// Calculate elapsed time
	elapsed := time.Since(startTime).Seconds()
	if elapsed < 0.1 {
		elapsed = 0.1 // Avoid division by zero
	}

	// Calculate speed
	totalSize := task.Metadata.Size
	speed := int64(float64(totalDownloaded) / elapsed)

	// Calculate ETA
	var eta time.Duration
	if speed > 0 {
		remaining := totalSize - totalDownloaded
		eta = time.Duration(float64(remaining)/float64(speed)) * time.Second
	}

	// Calculate percentage
	percentage := 0.0
	if totalSize > 0 {
		percentage = float64(totalDownloaded) / float64(totalSize) * 100
	}

	// Update task progress
	task.Progress.Downloaded = totalDownloaded
	task.Progress.Percentage = percentage
	task.Progress.Speed = speed
	task.Progress.ETA = eta
	task.Progress.CurrentChunk = completedChunks

	// 添加日志记录，便于调试进度更新问题
	logger.Debugf("Task %s progress: %.1f%%, Downloaded: %d/%d, Speed: %d bytes/s",
		task.ID, percentage, totalDownloaded, totalSize, speed)

	// Send progress update
	progress <- core.Progress{
		Percentage:   percentage,
		Downloaded:   totalDownloaded,
		TotalSize:    totalSize,
		Speed:        speed,
		ETA:          eta,
		CurrentChunk: completedChunks,
		TotalChunks:  len(chunks),
	}
}

// Pause pauses the download
func (h *HTTPProtocol) Pause() error {
	if !h.isRunning {
		return fmt.Errorf("not running")
	}
	h.isPaused = true
	h.status.IsPaused = true
	return nil
}

// Resume resumes the download
func (h *HTTPProtocol) Resume() error {
	if !h.isRunning {
		return fmt.Errorf("not running")
	}
	h.isPaused = false
	h.status.IsPaused = false
	return nil
}

// Cancel cancels the download
func (h *HTTPProtocol) Cancel() error {
	h.isRunning = false
	h.status.IsRunning = false
	return nil
}

// GetStatus gets the current status
func (h *HTTPProtocol) GetStatus() core.Status {
	return h.status
}

// GetStatistics gets the current statistics
func (h *HTTPProtocol) GetStatistics() core.Statistics {
	return h.statistics
}

// ApplyConfig applies the configuration
func (h *HTTPProtocol) ApplyConfig(config core.ProtocolConfig) error {
	h.config = config
	return nil
}

// GetCapabilities gets the capabilities of the protocol
func (h *HTTPProtocol) GetCapabilities() core.Capabilities {
	return core.Capabilities{
		CanResume:           true,
		CanVerify:           true,
		SupportsChunks:      true,
		SupportsP2P:         false,
		SupportedURLSchemes: []string{"http", "https"},
	}
}

// SetResourceController sets the resource controller for the protocol
func (h *HTTPProtocol) SetResourceController(rc *core.ResourceController) {
	h.resourceCtrl = rc
}

// SetConnectionPool sets the connection pool for the protocol
func (h *HTTPProtocol) SetConnectionPool(pool *core.ConnectionPool) {
	h.connPool = pool
}

// getFilenameFromResponse gets the filename from the response or URL
func getFilenameFromResponse(resp *http.Response, url string) string {
	// Try to get filename from Content-Disposition header
	cd := resp.Header.Get("Content-Disposition")
	if cd != "" {
		// Simple parsing for filename="xxx" format
		if idx := strings.Index(cd, "filename="); idx != -1 {
			filename := cd[idx+9:]
			if strings.HasPrefix(filename, "\"") {
				filename = strings.TrimPrefix(filename, "\"")
				if idx := strings.Index(filename, "\""); idx != -1 {
					filename = filename[:idx]
				}
			}
			return sanitizeFilename(filename)
		}
	}

	// Fallback: get filename from URL
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		filename := parts[len(parts)-1]
		// Remove query parameters
		if idx := strings.Index(filename, "?"); idx != -1 {
			filename = filename[:idx]
		}
		// Remove URL fragments
		if idx := strings.Index(filename, "#"); idx != -1 {
			filename = filename[:idx]
		}
		if filename != "" {
			return sanitizeFilename(filename)
		}
	}

	return "download"
}

// sanitizeFilename sanitizes a filename to make it valid
func sanitizeFilename(filename string) string {
	// Remove or replace invalid characters
	invalidChars := []string{"<", ">", ":", "\"", "/", "\\", "|", "?", "*"}
	for _, char := range invalidChars {
		filename = strings.ReplaceAll(filename, char, "_")
	}
	// Trim whitespace
	filename = strings.TrimSpace(filename)
	// Ensure filename is not empty
	if filename == "" {
		filename = "download"
	}
	return filename
}

// simulateProgress simulates progress for testing
func simulateProgress(progress chan<- core.Progress, totalSize int64) {
	if totalSize <= 0 {
		totalSize = 100 * 1024 * 1024 // 100MB default
	}

	var downloaded int64
	for downloaded < totalSize {
		// Simulate download speed
		time.Sleep(100 * time.Millisecond)
		downloaded += 1 * 1024 * 1024 // 1MB per 100ms

		if downloaded > totalSize {
			downloaded = totalSize
		}

		percentage := float64(downloaded) / float64(totalSize) * 100
		speed := int64(10 * 1024 * 1024) // 10MB/s
		eta := time.Duration((totalSize-downloaded)/speed) * time.Second

		progress <- core.Progress{
			Percentage:   percentage,
			Downloaded:   downloaded,
			TotalSize:    totalSize,
			Speed:        speed,
			ETA:          eta,
			CurrentChunk: 1,
			TotalChunks:  1,
		}
	}
}
