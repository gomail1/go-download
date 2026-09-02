package ftp

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-download-server/internal/core"
)

// FTPProtocol implements the core.Protocol interface for FTP

type FTPProtocol struct {
	client       *textproto.Conn
	host         string
	port         int
	user         string
	password     string
	isRunning    bool
	isPaused     bool
	status       core.Status
	statistics   core.Statistics
	config       core.ProtocolConfig
	resourceCtrl *core.ResourceController
	connPool     *core.ConnectionPool
}

// NewFTPProtocol creates a new FTPProtocol instance

func NewFTPProtocol() *FTPProtocol {
	return &FTPProtocol{
		port:     21,           // Default FTP port
		user:     "anonymous",  // Default FTP user
		password: "anonymous@", // Default FTP password
		status: core.Status{
			IsRunning: false,
			IsPaused:  false,
		},
		statistics: core.Statistics{
			StartTime: time.Now(),
		},
	}
}

// CanHandle checks if the URL can be handled by FTP protocol

func (f *FTPProtocol) CanHandle(url string) bool {
	return strings.HasPrefix(strings.ToLower(url), "ftp://")
}

// GetMetadata gets the metadata of the URL

func (f *FTPProtocol) GetMetadata(ctx context.Context, url string) (*core.Metadata, error) {
	// Parse FTP URL
	// Example: ftp://user:password@host:port/path
	// For now, we'll just return basic metadata
	metadata := &core.Metadata{
		Filename: filepath.Base(url),
		Size:     -1,                         // Unknown size for now
		MimeType: "application/octet-stream", // Default MIME type
		ProtocolSpecific: map[string]interface{}{
			"url": url,
		},
	}

	return metadata, nil
}

// Download downloads the file from the URL

// Download downloads the file from the URL
func (f *FTPProtocol) Download(ctx context.Context, task *core.Task, progress chan<- core.Progress) error {
	// Set status
	f.isRunning = true
	f.isPaused = false
	f.status.IsRunning = true

	defer func() {
		f.isRunning = false
		f.status.IsRunning = false
		if f.client != nil {
			f.client.Close()
		}
	}()

	// Get metadata if not available
	if task.Metadata == nil {
		metadata, err := f.GetMetadata(ctx, task.URL)
		if err != nil {
			return err
		}
		task.Metadata = metadata
	}

	// Parse FTP URL to get host, port, path, user, password
	host, port, path, user, password, err := f.parseFTPURL(task.URL)
	if err != nil {
		return err
	}

	// Create destination file
	destPath := task.Config.SavePath + "/" + task.Metadata.Filename
	file, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	// Start time
	startTime := time.Now()
	f.statistics.StartTime = startTime

	// Update progress channel with initial status
	progress <- core.Progress{
		Percentage:   0,
		Downloaded:   0,
		TotalSize:    task.Metadata.Size,
		Speed:        0,
		ETA:          0,
		CurrentChunk: 0,
		TotalChunks:  1,
	}

	// Allocate network resource（若引擎未注入控制器则跳过，避免 nil panic）
	if f.resourceCtrl != nil {
		if err = f.resourceCtrl.Allocate(core.ResourceTypeNetwork, 1, "ftp"); err != nil {
			return err
		}
		// Release network resource when done
		defer f.resourceCtrl.Release(core.ResourceTypeNetwork, 1, "ftp")
	}

	// Connect to FTP server
	err = f.connect(host, port)
	if err != nil {
		return err
	}

	// Authenticate
	err = f.authenticate(user, password)
	if err != nil {
		return err
	}

	// Set binary mode
	err = f.setBinaryMode()
	if err != nil {
		return err
	}

	// Get file size if not known
	if task.Metadata.Size == -1 {
		fileSize, err := f.getFileSize(path)
		if err != nil {
			return err
		}
		task.Metadata.Size = fileSize
	}

	// Pre-allocate file size if known
	if task.Metadata.Size > 0 && task.Config.PreAllocate {
		err = file.Truncate(task.Metadata.Size)
		if err != nil {
			return err
		}
	}

	// Start download
	err = f.startDownload(ctx, path, file, task, progress, startTime)
	if err != nil {
		return err
	}

	// Update final statistics
	f.statistics.EndTime = new(time.Time)
	*f.statistics.EndTime = time.Now()
	f.statistics.Duration = f.statistics.EndTime.Sub(f.statistics.StartTime)
	f.statistics.Downloaded = task.Metadata.Size

	return nil
}

// parseFTPURL parses an FTP URL into host, port, path, user, password
func (f *FTPProtocol) parseFTPURL(url string) (host string, port int, path string, user string, password string, err error) {
	// Remove ftp:// prefix
	url = strings.TrimPrefix(url, "ftp://")

	// Default values
	port = 21
	user = "anonymous"
	password = "anonymous@"

	// Check for user:password@host
	if idx := strings.Index(url, "@"); idx != -1 {
		// Parse user:password
		credentials := url[:idx]
		url = url[idx+1:]

		if colonIdx := strings.Index(credentials, ":"); colonIdx != -1 {
			user = credentials[:colonIdx]
			password = credentials[colonIdx+1:]
		} else {
			user = credentials
		}
	}

	// Split host:port/path
	if idx := strings.Index(url, "/"); idx != -1 {
		hostPort := url[:idx]
		path = url[idx:]

		// Parse host:port
		if colonIdx := strings.Index(hostPort, ":"); colonIdx != -1 {
			host = hostPort[:colonIdx]
			portStr := hostPort[colonIdx+1:]
			_, err = fmt.Sscanf(portStr, "%d", &port)
			if err != nil {
				return "", 0, "", "", "", fmt.Errorf("invalid port: %s", portStr)
			}
		} else {
			host = hostPort
		}
	} else {
		host = url
		path = "/"
	}

	return host, port, path, user, password, nil
}

// connect connects to the FTP server
func (f *FTPProtocol) connect(host string, port int) error {
	// Create connection string
	addr := fmt.Sprintf("%s:%d", host, port)

	// Connect to server
	conn, err := textproto.Dial("tcp", addr)
	if err != nil {
		return err
	}

	// Read welcome message - simplified for now
	line, err := conn.ReadLine()
	if err != nil {
		conn.Close()
		return fmt.Errorf("welcome message error: %s", err)
	}

	// Check if welcome message starts with 220
	if !strings.HasPrefix(line, "220") {
		conn.Close()
		return fmt.Errorf("welcome message error: %s", line)
	}

	f.client = conn
	f.host = host
	f.port = port

	return nil
}

// authenticate authenticates with the FTP server
func (f *FTPProtocol) authenticate(user, password string) error {
	// Send USER command
	_, err := f.client.Cmd("USER %s", user)
	if err != nil {
		return fmt.Errorf("USER command error: %s", err)
	}

	// Read USER response
	userLine, err := f.client.ReadLine()
	if err != nil {
		return fmt.Errorf("USER response error: %s", err)
	}
	if !strings.HasPrefix(userLine, "331") {
		return fmt.Errorf("USER command error: %s", userLine)
	}

	// Send PASS command
	_, err = f.client.Cmd("PASS %s", password)
	if err != nil {
		return fmt.Errorf("PASS command error: %s", err)
	}

	// Read PASS response
	passLine, err := f.client.ReadLine()
	if err != nil {
		return fmt.Errorf("PASS response error: %s", err)
	}
	if !strings.HasPrefix(passLine, "230") {
		return fmt.Errorf("PASS command error: %s", passLine)
	}

	return nil
}

// setBinaryMode sets binary transfer mode
func (f *FTPProtocol) setBinaryMode() error {
	// Send TYPE I command for binary mode
	_, err := f.client.Cmd("TYPE I")
	if err != nil {
		return fmt.Errorf("TYPE I command error: %s", err)
	}

	// Read TYPE response
	typeLine, err := f.client.ReadLine()
	if err != nil {
		return fmt.Errorf("TYPE I response error: %s", err)
	}
	if !strings.HasPrefix(typeLine, "200") {
		return fmt.Errorf("TYPE I command error: %s", typeLine)
	}

	return nil
}

// getFileSize gets the size of a file from the FTP server
func (f *FTPProtocol) getFileSize(path string) (int64, error) {
	// Send SIZE command
	_, err := f.client.Cmd("SIZE %s", path)
	if err != nil {
		return -1, fmt.Errorf("SIZE command error: %s", err)
	}

	// Read SIZE response
	line, err := f.client.ReadLine()
	if err != nil {
		return -1, fmt.Errorf("SIZE response error: %s", err)
	}

	// Parse the size from the response
	var code int
	var size int64
	_, err = fmt.Sscanf(line, "%d %d", &code, &size)
	if err != nil {
		return -1, fmt.Errorf("parse size error: %s", err)
	}

	if code != 213 {
		return -1, fmt.Errorf("SIZE command error: %d", code)
	}

	return size, nil
}

// startDownload 通过 PASV 模式建立数据连接，真实地从 FTP 服务器拉取文件。
// 流程：PASV 取被动端口 -> RETR 请求文件 -> 读控制通道 1xx 预备响应 ->
// 从数据连接流式写入本地文件 -> 读控制通道 226 收尾响应。
func (f *FTPProtocol) startDownload(ctx context.Context, path string, file *os.File, task *core.Task, progress chan<- core.Progress, startTime time.Time) error {
	// 1) 建立被动数据连接（必须在 RETR 之前）
	dataConn, err := f.openPassiveDataConn()
	if err != nil {
		return err
	}
	defer dataConn.Close()

	// 2) 发送 RETR 请求文件
	if _, err = f.client.Cmd("RETR %s", path); err != nil {
		return fmt.Errorf("RETR 命令错误: %s", err)
	}
	// 读取预备响应（150/125），服务器随后才会开始传数据
	prelim, err := f.client.ReadLine()
	if err != nil {
		return fmt.Errorf("RETR 预备响应错误: %s", err)
	}
	if !strings.HasPrefix(prelim, "1") {
		return fmt.Errorf("RETR 命令被拒绝: %s", prelim)
	}

	// 3) 从数据连接流式写入本地文件
	totalSize := task.Metadata.Size
	var downloaded int64
	buf := make([]byte, 32*1024)

	progressTicker := time.NewTicker(500 * time.Millisecond)
	defer progressTicker.Stop()

	for {
		// 暂停 / 取消优先处理
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-progressTicker.C:
			f.reportProgress(startTime, downloaded, totalSize, progress)
		default:
		}
		for f.isPaused {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				time.Sleep(100 * time.Millisecond)
			}
		}

		n, readErr := dataConn.Read(buf)
		if n > 0 {
			if _, werr := file.Write(buf[:n]); werr != nil {
				return fmt.Errorf("写入文件错误: %s", werr)
			}
			downloaded += int64(n)
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return fmt.Errorf("数据连接读取错误: %s", readErr)
		}
	}

	// 4) 读取收尾响应（226 Transfer complete）
	finalLine, err := f.client.ReadLine()
	if err != nil {
		return fmt.Errorf("RETR 收尾响应错误: %s", err)
	}
	if !strings.HasPrefix(finalLine, "2") {
		return fmt.Errorf("RETR 收尾响应异常: %s", finalLine)
	}

	// 大小未知时以实际下载量回填，保证进度与落盘一致
	if totalSize <= 0 {
		task.Metadata.Size = downloaded
	}

	f.reportProgress(startTime, downloaded, task.Metadata.Size, progress)
	return nil
}

// openPassiveDataConn 发送 PASV 命令，解析服务器返回的被动地址并拨号建立数据连接。
func (f *FTPProtocol) openPassiveDataConn() (net.Conn, error) {
	if _, err := f.client.Cmd("PASV"); err != nil {
		return nil, fmt.Errorf("PASV 命令错误: %s", err)
	}
	line, err := f.client.ReadLine()
	if err != nil {
		return nil, fmt.Errorf("PASV 响应错误: %s", err)
	}
	if !strings.HasPrefix(line, "227") {
		return nil, fmt.Errorf("PASV 命令被拒绝: %s", line)
	}

	// 解析 (h1,h2,h3,h4,p1,p2)
	start := strings.Index(line, "(")
	end := strings.Index(line, ")")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("PASV 响应解析失败: %s", line)
	}
	parts := strings.Split(line[start+1:end], ",")
	if len(parts) != 6 {
		return nil, fmt.Errorf("PASV 响应解析失败: %s", line)
	}
	var nums [6]int
	for i := 0; i < 6; i++ {
		if _, err = fmt.Sscanf(strings.TrimSpace(parts[i]), "%d", &nums[i]); err != nil {
			return nil, fmt.Errorf("PASV 响应解析失败: %s", err)
		}
	}
	ip := fmt.Sprintf("%d.%d.%d.%d", nums[0], nums[1], nums[2], nums[3])
	port := nums[4]*256 + nums[5]

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", ip, port), 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("FTP 数据连接失败: %s", err)
	}
	return conn, nil
}

// reportProgress reports download progress
func (f *FTPProtocol) reportProgress(startTime time.Time, downloaded, totalSize int64, progress chan<- core.Progress) {
	elapsed := time.Since(startTime).Seconds()
	if elapsed < 0.1 {
		elapsed = 0.1 // Avoid division by zero
	}

	speed := int64(float64(downloaded) / elapsed)

	var percentage float64
	var eta time.Duration
	if totalSize > 0 {
		percentage = float64(downloaded) / float64(totalSize) * 100
		if speed > 0 {
			remaining := totalSize - downloaded
			eta = time.Duration(float64(remaining)/float64(speed)) * time.Second
		}
	}

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

// Pause pauses the download

func (f *FTPProtocol) Pause() error {
	if !f.isRunning {
		return fmt.Errorf("not running")
	}
	f.isPaused = true
	f.status.IsPaused = true
	return nil
}

// Resume resumes the download

func (f *FTPProtocol) Resume() error {
	if !f.isRunning {
		return fmt.Errorf("not running")
	}
	f.isPaused = false
	f.status.IsPaused = false
	return nil
}

// Cancel cancels the download

func (f *FTPProtocol) Cancel() error {
	f.isRunning = false
	f.status.IsRunning = false
	return nil
}

// GetStatus gets the current status

func (f *FTPProtocol) GetStatus() core.Status {
	return f.status
}

// GetStatistics gets the current statistics

func (f *FTPProtocol) GetStatistics() core.Statistics {
	return f.statistics
}

// ApplyConfig applies the configuration

func (f *FTPProtocol) ApplyConfig(config core.ProtocolConfig) error {
	f.config = config
	return nil
}

// GetCapabilities gets the capabilities of the protocol

func (f *FTPProtocol) GetCapabilities() core.Capabilities {
	return core.Capabilities{
		CanResume:           true,
		CanVerify:           true,
		SupportsChunks:      true,
		SupportsP2P:         false,
		SupportedURLSchemes: []string{"ftp"},
	}
}

// SetResourceController sets the resource controller for the protocol

func (f *FTPProtocol) SetResourceController(rc *core.ResourceController) {
	f.resourceCtrl = rc
}

// SetConnectionPool sets the connection pool for the protocol

func (f *FTPProtocol) SetConnectionPool(pool *core.ConnectionPool) {
	f.connPool = pool
}
