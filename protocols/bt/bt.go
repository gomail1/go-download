package bt

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"go-download-server/internal/config"
	"go-download-server/internal/core"
	"go-download-server/internal/logger"
)

// maxTorrentFileSize 限制从远程下载的 .torrent 种子文件大小（5MB），防止恶意超大文件撑爆内存
const maxTorrentFileSize int64 = 5 * 1024 * 1024

var (
	sharedClient     *torrent.Client
	sharedClientOnce sync.Once
	sharedClientErr  error
)

type BTClient struct {
	client *torrent.Client
}

type BTProtocol struct {
	client       *BTClient
	isRunning    bool
	isPaused     bool
	status       core.Status
	statistics   core.Statistics
	config       core.ProtocolConfig
	resourceCtrl *core.ResourceController
	connPool     *core.ConnectionPool
}

func getSharedClient() (*torrent.Client, error) {
	sharedClientOnce.Do(func() {
		cfg := torrent.NewDefaultClientConfig()
		// 优化磁力链接下载配置
		cfg.DisableTCP = false // 启用TCP - 必须启用，HTTPS tracker依赖TCP
		cfg.DisableUTP = false // 启用UTP

		// 应用配置中的网络设置
		appConfig := config.Get()
		// 设置默认DataDir为配置中的默认保存路径
		cfg.DataDir = appConfig.Core.DefaultSavePath
		// 如果配置中的路径为空，使用备用默认路径
		if cfg.DataDir == "" {
			cfg.DataDir = "pending/download-user"
		}
		cfg.NoUpload = false        // 允许上传，提高下载速度
		cfg.Seed = false            // 下载完成后不做种
		cfg.ListenPort = 0          // 随机端口
		cfg.Bep20 = "QuadFetch/1.0" // 使用QuadFetch客户端标识，符合PT站点要求
		// 优化DHT配置
		cfg.NoDHT = false       // 启用DHT - 对于磁力链接至关重要
		cfg.DisableIPv6 = false // 启用IPv6
		// 注意：当DisableIPv6为false时，anacrolix/torrent库会同时监听IPv4和IPv6
		// 不需要额外设置IPv4相关选项，因为默认会同时支持
		cfg.NoDefaultPortForwarding = false // 启用端口转发

		// 优化连接配置 - 增加连接数提高下载速度
		cfg.EstablishedConnsPerTorrent = 60      // 每个种子的最大已建立连接数
		cfg.HalfOpenConnsPerTorrent = 25         // 每个种子的半开放连接数
		cfg.TotalHalfOpenConns = 200             // 总半开放连接数
		cfg.HandshakesTimeout = 60 * time.Second // 握手超时时间
		cfg.TorrentPeersHighWater = 100          // 每个种子的最大peer数
		cfg.TorrentPeersLowWater = 40            // peer低水位线

		// 优化下载配置
		cfg.DisableAggressiveUpload = false // 启用激进上传，提高下载优先级
		cfg.NoUpload = false                // 允许上传，提高下载优先级
		cfg.UpnpID = "QuadFetch"            // 设置UPnP设备ID
		cfg.DisableWebtorrent = false       // 启用WebTorrent支持

		// 增加日志记录，帮助调试
		logger.Infof("Creating BT client with DataDir: %s, DHT enabled: %v, TCP enabled: %v, UTP enabled: %v",
			cfg.DataDir, !cfg.NoDHT, !cfg.DisableTCP, !cfg.DisableUTP)

		// 确保默认DataDir存在
		if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
			logger.Errorf("Failed to create default DataDir: %v", err)
			sharedClientErr = fmt.Errorf("failed to create data directory: %v", err)
			return
		}

		sharedClient, sharedClientErr = torrent.NewClient(cfg)
		if sharedClientErr != nil {
			logger.Errorf("Failed to create torrent client: %v", sharedClientErr)
			sharedClientErr = fmt.Errorf("failed to create torrent client: %v", sharedClientErr)
			return
		}
	})

	return sharedClient, sharedClientErr
}

func NewBTProtocol() *BTProtocol {
	client, err := getSharedClient()
	if err != nil {
		logger.Errorf("创建torrent客户端失败: %v", err)
		return &BTProtocol{
			status: core.Status{
				IsRunning: false,
				IsPaused:  false,
			},
			statistics: core.Statistics{
				StartTime: time.Now(),
			},
		}
	}

	return &BTProtocol{
		client: &BTClient{
			client: client,
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

// CanHandle checks if the URL can be handled by BitTorrent protocol

func (b *BTProtocol) CanHandle(url string) bool {
	// Check for .torrent file extension
	if strings.HasSuffix(strings.ToLower(url), ".torrent") {
		return true
	}

	// Check for magnet link
	if strings.HasPrefix(strings.ToLower(url), "magnet:") {
		return true
	}

	// Check for dynamic torrent file URLs (common patterns for torrent download pages)
	lowerURL := strings.ToLower(url)
	if strings.Contains(lowerURL, "/download.php") ||
		strings.Contains(lowerURL, "/download/") ||
		strings.Contains(lowerURL, "/get-torrent") ||
		strings.Contains(lowerURL, "/torrent/") {
		return true
	}

	return false
}

// GetMetadata gets the metadata of the URL

func (b *BTProtocol) GetMetadata(ctx context.Context, url string) (*core.Metadata, error) {
	appConfig := config.Get()

	if b.client == nil || b.client.client == nil {
		return nil, fmt.Errorf("torrent client not initialized")
	}

	var t *torrent.Torrent
	var err error
	var torrentData []byte

	// Handle different types of URLs
	lowerURL := strings.ToLower(url)

	// 首先检查是否是磁力链接
	if strings.HasPrefix(lowerURL, "magnet:") {
		// Handle magnet link with retry mechanism
		maxRetries := 3
		var addErr error
		for i := 1; i <= maxRetries; i++ {
			t, addErr = b.client.client.AddMagnet(url)
			if addErr == nil {
				break
			}
			// 重试间隔
			time.Sleep(2 * time.Second)
		}

		if addErr != nil {
			return nil, fmt.Errorf("failed to add magnet link after %d attempts: %v", maxRetries, addErr)
		}
	} else {
		// 检查是否是torrent文件URL
		isTorrentFile := strings.HasSuffix(lowerURL, ".torrent") ||
			strings.Contains(lowerURL, "/download.php") ||
			strings.Contains(lowerURL, "/download/") ||
			strings.Contains(lowerURL, "/get-torrent") ||
			strings.Contains(lowerURL, "/torrent/")

		if isTorrentFile {
			// Handle torrent file URL - download from network if needed
			if strings.HasPrefix(lowerURL, "http://") || strings.HasPrefix(lowerURL, "https://") {
				// Download torrent file from HTTP/HTTPS
				resp, err := http.Get(url)
				if err != nil {
					return nil, fmt.Errorf("failed to download torrent file: %v", err)
				}
				defer resp.Body.Close()
				torrentData, err = io.ReadAll(io.LimitReader(resp.Body, maxTorrentFileSize))
				if err != nil {
					return nil, fmt.Errorf("failed to read torrent response: %v", err)
				}
				if int64(len(torrentData)) >= maxTorrentFileSize {
					return nil, fmt.Errorf("torrent file exceeds size limit of %d bytes", maxTorrentFileSize)
				}
			} else {
				// Read from local file
				torrentData, err = os.ReadFile(url)
				if err != nil {
					return nil, fmt.Errorf("failed to read torrent file: %v", err)
				}
			}

			// Parse torrent data into metainfo
			mi, err := metainfo.Load(bytes.NewReader(torrentData))
			if err != nil {
				return nil, fmt.Errorf("failed to parse torrent data: %v", err)
			}
			t, err = b.client.client.AddTorrent(mi)
			if err != nil {
				return nil, fmt.Errorf("failed to add torrent: %v", err)
			}
		} else {
			return nil, fmt.Errorf("unsupported URL format")
		}
	}

	// 等待元数据下载完成
	// 注意：对于.torrent文件，元数据已经在本地解析完成，GotInfo()会立即触发
	metadataTimeout := time.Duration(appConfig.BT.MetadataTimeout) * time.Second

	// 增加磁力链接的元数据获取超时时间
	if strings.HasPrefix(strings.ToLower(url), "magnet:") {
		metadataTimeout = 300 * time.Second // 磁力链接使用300秒超时，增加到5分钟
	} else {
		// .torrent文件，使用较短的超时时间，因为元数据已经在本地
		metadataTimeout = 10 * time.Second
	}

	// 创建带超时的上下文
	metadataCtx, cancel := context.WithTimeout(context.Background(), metadataTimeout)
	defer cancel()

	// 等待元数据完成，使用带超时的上下文
	done := make(chan struct{})
	errCh := make(chan error)

	go func() {
		select {
		case <-t.GotInfo():
			// 元数据获取成功或本地解析完成
			close(done)
			return
		case <-metadataCtx.Done():
			// 超时，返回错误
			errCh <- fmt.Errorf("metadata timeout: %v", metadataCtx.Err())
			return
		}
	}()

	select {
	case <-done:
		// 元数据获取成功
	case <-metadataCtx.Done():
		// 元数据获取超时
		return nil, fmt.Errorf("failed to get metadata: %v", metadataCtx.Err())
	case err := <-errCh:
		// 元数据获取失败
		return nil, err
	}

	// 获取元数据信息
	torrentInfo := t.Info()
	if torrentInfo == nil {
		return nil, fmt.Errorf("failed to get torrent info")
	}

	// 构建元数据响应
	metadata := &core.Metadata{
		Filename: torrentInfo.Name,
		Size:     torrentInfo.TotalLength(),
		MimeType: "application/x-bittorrent",
	}

	return metadata, nil
}

// Download downloads the file from the URL

func (b *BTProtocol) Download(ctx context.Context, task *core.Task, progress chan<- core.Progress) error {
	b.isRunning = true
	b.isPaused = false
	b.status.IsRunning = true

	// 使用传统torrent客户端下载
	// 获取配置
	appConfig := config.Get()
	metadataTimeout := time.Duration(appConfig.BT.MetadataTimeout) * time.Second
	progressInterval := time.Duration(appConfig.BT.ProgressInterval) * time.Millisecond

	// 根据URL类型设置不同的元数据获取超时时间
	if strings.HasPrefix(strings.ToLower(task.URL), "magnet:") {
		metadataTimeout = 300 * time.Second // 磁力链接使用300秒超时，增加到5分钟
	} else {
		// .torrent文件，使用较短的超时时间，因为元数据已经在本地
		metadataTimeout = 10 * time.Second
	}

	defer func() {
		b.isRunning = false
		b.status.IsRunning = false
	}()

	// 获取保存路径
	baseSavePath := task.Config.SavePath

	// 确保保存目录存在
	if err := os.MkdirAll(baseSavePath, 0755); err != nil {
		return fmt.Errorf("failed to create save directory: %v", err)
	}

	// 使用与GetMetadata方法相同的客户端实例，共享DHT节点和元数据缓存
	if b.client == nil || b.client.client == nil {
		return fmt.Errorf("torrent client not initialized")
	}
	client := b.client.client

	// Add torrent or magnet to the client
	var downloadTorrent *torrent.Torrent
	var mi *metainfo.MetaInfo
	var torrentData []byte
	var resp *http.Response
	var err error

	lowerURL := strings.ToLower(task.URL)

	// 首先检查是否是磁力链接
	if strings.HasPrefix(lowerURL, "magnet:") {
		// Handle magnet link with retry mechanism
		downloadTorrent, err = client.AddMagnet(task.URL)
		if err != nil {
			return fmt.Errorf("failed to add magnet link: %v", err)
		}
	} else if strings.HasSuffix(lowerURL, ".torrent") {
		// Handle torrent file URL
		if strings.HasPrefix(lowerURL, "http://") || strings.HasPrefix(lowerURL, "https://") {
			// 从HTTP/HTTPS下载种子文件
			resp, err = http.Get(task.URL)
			if err != nil {
				return fmt.Errorf("failed to download torrent file: %v", err)
			}
			defer resp.Body.Close()
			torrentData, err = io.ReadAll(io.LimitReader(resp.Body, maxTorrentFileSize))
			if err != nil {
				return fmt.Errorf("failed to read torrent response: %v", err)
			}
			if int64(len(torrentData)) >= maxTorrentFileSize {
				return fmt.Errorf("torrent file exceeds size limit of %d bytes", maxTorrentFileSize)
			}
		} else {
			// 从本地文件读取种子
			torrentData, err = os.ReadFile(task.URL)
			if err != nil {
				return fmt.Errorf("failed to read torrent file: %v", err)
			}
		}

		mi, err = metainfo.Load(bytes.NewReader(torrentData))
		if err != nil {
			return fmt.Errorf("failed to parse torrent data: %v", err)
		}
		downloadTorrent, err = client.AddTorrent(mi)
		if err != nil {
			return fmt.Errorf("failed to add torrent: %v", err)
		}
	} else {
		return fmt.Errorf("unsupported URL format")
	}

	// 创建带超时的上下文
	metadataCtx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()

	// 等待元数据完成，使用带超时的上下文
	done := make(chan struct{})
	go func() {
		<-downloadTorrent.GotInfo()
		close(done)
	}()

	select {
	case <-done:
		// 元数据获取成功
	case <-metadataCtx.Done():
		// 元数据获取超时
		return fmt.Errorf("failed to get metadata: %v", metadataCtx.Err())
	}

	// Get torrent info
	info := downloadTorrent.Info()
	if info == nil {
		err = fmt.Errorf("failed to get torrent info after GotInfo signal")
		return err
	}

	// Start downloading all files
	downloadTorrent.DownloadAll()
	logger.Infof("Task %s: Started downloading %s, save path: %s", task.ID, info.Name, baseSavePath)
	logger.Infof("Task %s: Waiting for peers...", task.ID)

	// Progress update ticker - 使用配置中的间隔，确保为正数
	if progressInterval <= 0 {
		progressInterval = 500 * time.Millisecond // 默认500毫秒
	}
	progressTicker := time.NewTicker(progressInterval)
	defer progressTicker.Stop()

	// Start time for statistics
	startTime := time.Now()
	b.statistics.StartTime = startTime
	// Last progress log time
	lastProgressLog := time.Now()

	// Main download loop
	for {
		select {
		case <-ctx.Done():
			// Cancel the download
			downloadTorrent.Drop()
			return ctx.Err()
		case <-progressTicker.C:
			// Check if paused
			for b.isPaused {
				time.Sleep(100 * time.Millisecond)
				if ctx.Err() != nil {
					return ctx.Err()
				}
			}

			// Calculate progress
			var totalSize int64
			var downloaded int64

			// 获取总大小 - 优先使用之前获取的info，避免重复检查
			currentInfo := downloadTorrent.Info()
			if currentInfo != nil {
				totalSize = currentInfo.TotalLength()
			} else {
				totalSize = info.TotalLength()
			}

			downloaded = downloadTorrent.BytesCompleted()
			stats := downloadTorrent.Stats()

			// 使用PeerConns()获取活跃连接数，使用Stats()获取总节点数
			peerConns := downloadTorrent.PeerConns()
			activePeers := len(peerConns)
			totalPeers := stats.TotalPeers // 总节点数来自Stats()

			// 如果Stats()返回的总节点数为0，使用活跃连接数作为总节点数
			if totalPeers == 0 {
				totalPeers = activePeers
			}

			// Debug日志，仅在debug级别输出
			if time.Since(lastProgressLog) >= 10*time.Second {
				logger.Debugf("Task %s: Debug - Downloaded: %d/%d bytes, Active peers: %d/%d",
					task.ID, downloaded, totalSize, activePeers, totalPeers)
				lastProgressLog = time.Now()
			}

			var percentage float64
			if totalSize > 0 {
				percentage = float64(downloaded) / float64(totalSize) * 100
			}

			// Calculate speed
			elapsed := time.Since(startTime).Seconds()
			if elapsed < 0.1 {
				elapsed = 0.1 // Avoid division by zero
			}
			speed := int64(float64(downloaded) / elapsed)

			// Calculate ETA
			var eta time.Duration
			if speed > 0 && totalSize > downloaded {
				remaining := totalSize - downloaded
				eta = time.Duration(float64(remaining)/float64(speed)) * time.Second
			}

			// Log progress at info level every 5% increment or every 10 seconds, and always log if percentage is 0
			if int(percentage*20)%20 == 0 || time.Since(lastProgressLog) >= 10*time.Second {
				logger.Infof("Task %s: %.1f%% downloaded, %d/%d bytes, %d bytes/sec, ETA: %v, Peers: %d/%d",
					task.ID, percentage, downloaded, totalSize, speed, eta, activePeers, totalPeers)
				lastProgressLog = time.Now()
			}

			// Always log when percentage is 0 and it's the first iteration
			if percentage == 0 && time.Since(startTime) < 2*time.Second {
				logger.Infof("Task %s: Download started, waiting for peers...", task.ID)
			}

			// Always log when no peers connected
			if activePeers == 0 && time.Since(lastProgressLog) >= 10*time.Second {
				logger.Infof("Task %s: Waiting for peers... No active peers connected yet. Active: %d/%d, Downloaded: %d/%d bytes",
					task.ID, activePeers, totalPeers, downloaded, totalSize)
				lastProgressLog = time.Now()
			}

			// Determine download status description
			status := ""
			if speed == 0 {
				if activePeers == 0 {
					status = "正在查找公共Tracker和节点..."
				} else {
					status = "正在尝试连接到" + strconv.Itoa(activePeers) + "个节点..."
				}
			}

			// Send progress update
			progressUpdate := core.Progress{
				Percentage:   percentage,
				Downloaded:   downloaded,
				TotalSize:    totalSize,
				Speed:        speed,
				ETA:          eta,
				CurrentChunk: 1,
				TotalChunks:  1,
				Status:       status,
				ActivePeers:  activePeers,
				TotalPeers:   totalPeers,
			}

			select {
			case progress <- progressUpdate:
			default:
			}

			// 检查下载是否完成
			if downloadTorrent.BytesCompleted() >= downloadTorrent.Info().TotalLength() {
				// 再发送一次100%进度更新
				finalProgressUpdate := core.Progress{
					Percentage:   100.0,
					Downloaded:   totalSize,
					TotalSize:    totalSize,
					Speed:        speed,
					ETA:          0,
					CurrentChunk: 1,
					TotalChunks:  1,
				}

				select {
				case progress <- finalProgressUpdate:
				default:
				}

				// 退出循环
				goto downloadComplete
			}
		}
	}
downloadComplete:

	// 等待一小段时间以确保进度更新已被处理
	time.Sleep(100 * time.Millisecond)

	// 获取最终下载信息
	finalInfo := downloadTorrent.Info()
	if finalInfo == nil {
		finalInfo = info
	}

	// 获取下载的文件/目录路径
	finalPath := filepath.Join(baseSavePath, finalInfo.Name)
	partPath := finalPath + ".part"

	// 检查是否是目录类型的种子
	isDirTorrent := finalInfo.IsDir() || len(finalInfo.Files) > 1

	if isDirTorrent {
		// 目录类型的种子（多文件种子）
		logger.Infof("Task %s: Handling directory torrent...", task.ID)

		// 检查是否存在以种子名称命名的目录
		if _, err := os.Stat(finalPath); os.IsNotExist(err) {
			// 检查是否存在.part目录
			if _, err := os.Stat(partPath); err == nil {
				// 存在.part目录，尝试重命名
				logger.Infof("Task %s: Found .part directory, renaming to final name: %s -> %s", task.ID, partPath, finalPath)
				if err := os.Rename(partPath, finalPath); err != nil {
					logger.Errorf("Task %s: Failed to rename .part directory: %v", task.ID, err)
					return err
				}
				logger.Infof("Task %s: Successfully renamed .part directory to final name", task.ID)
			} else {
				// 检查保存目录中是否只有一个子目录
				files, err := os.ReadDir(baseSavePath)
				if err != nil {
					logger.Errorf("Task %s: Failed to read save directory: %v", task.ID, err)
					return err
				}

				// 过滤掉.torrent.bolt.db文件
				var validDirs []os.DirEntry
				for _, file := range files {
					if file.Name() != ".torrent.bolt.db" && file.IsDir() {
						validDirs = append(validDirs, file)
					}
				}

				if len(validDirs) == 1 {
					// 只有一个子目录，将其重命名为种子名称
					foundDir := validDirs[0]
					foundDirPath := filepath.Join(baseSavePath, foundDir.Name())
					logger.Infof("Task %s: Found single directory in save path, renaming to torrent name: %s -> %s", task.ID, foundDirPath, finalPath)
					if err := os.Rename(foundDirPath, finalPath); err != nil {
						logger.Errorf("Task %s: Failed to rename directory: %v", task.ID, err)
						return err
					}
					logger.Infof("Task %s: Successfully renamed directory to torrent name", task.ID)
				} else {
					// 多个目录，不需要重命名，可能是已经处理过的多文件种子
					logger.Infof("Task %s: Multiple directories found in save path, assuming download completed successfully", task.ID)
				}
			}
		} else {
			// 目录已经存在，检查目录下的文件是否需要处理
			logger.Infof("Task %s: Directory already exists, checking files at %s...", task.ID, finalPath)

			// 检查目录下是否存在.part文件
			files, err := os.ReadDir(finalPath)
			if err != nil {
				logger.Errorf("Task %s: Failed to read directory: %v", task.ID, err)
				return err
			}

			// 检查是否有需要重命名的.part文件
			for _, file := range files {
				if strings.HasSuffix(file.Name(), ".part") {
					// 找到.part文件，需要重命名
					partFilePath := filepath.Join(finalPath, file.Name())
					finalFilePath := strings.TrimSuffix(partFilePath, ".part")

					logger.Infof("Task %s: Found .part file in directory, renaming: %s -> %s", task.ID, partFilePath, finalFilePath)

					// 检查目标文件是否已存在
					if _, err := os.Stat(finalFilePath); err == nil {
						if task.Config.Overwrite {
							logger.Infof("Task %s: Overwriting existing file: %s", task.ID, finalFilePath)
							if err := os.Remove(finalFilePath); err != nil {
								logger.Errorf("Task %s: Failed to remove existing file: %v", task.ID, err)
								continue
							}
						} else {
							logger.Infof("Task %s: File already exists, skipping overwrite: %s", task.ID, finalFilePath)
							continue
						}
					}

					// 尝试重命名文件
					if err := os.Rename(partFilePath, finalFilePath); err != nil {
						logger.Errorf("Task %s: Failed to rename .part file: %v", task.ID, err)
						continue
					}

					logger.Infof("Task %s: Successfully renamed .part file: %s", task.ID, finalFilePath)
				}
			}

			logger.Infof("Task %s: Directory already exists in final state: %s", task.ID, finalPath)
		}
	} else {
		// 单文件种子
		logger.Infof("Task %s: Handling single-file torrent...", task.ID)

		// 首先检查是否存在最终文件
		if _, err := os.Stat(finalPath); os.IsNotExist(err) {
			// 检查是否存在.part文件
			if _, err := os.Stat(partPath); err == nil {
				// 存在.part文件，尝试将其重命名为最终文件名
				logger.Infof("Task %s: Found .part file, will rename to final name: %s -> %s", task.ID, partPath, finalPath)

				// 检查目标文件是否已存在
				if _, err := os.Stat(finalPath); err == nil {
					if task.Config.Overwrite {
						logger.Infof("Task %s: Overwriting existing file: %s", task.ID, finalPath)
						if err := os.Remove(finalPath); err != nil {
							logger.Errorf("Task %s: Failed to remove existing file: %v", task.ID, err)
							return err
						}
					} else {
						logger.Infof("Task %s: File already exists, skipping overwrite: %s", task.ID, finalPath)
						return nil
					}
				}

				// 尝试将.part文件重命名为最终文件名，添加重试机制
				maxRetries := 5
				var renameErr error
				for i := 0; i < maxRetries; i++ {
					renameErr = os.Rename(partPath, finalPath)
					if renameErr == nil {
						logger.Infof("Task %s: Successfully renamed .part file to final name: %s", task.ID, finalPath)
						break
					}

					// 如果重命名失败，等待一段时间后重试
					logger.Warnf("Task %s: Failed to rename .part file (attempt %d/%d): %v", task.ID, i+1, maxRetries, renameErr)
					time.Sleep(time.Duration(500*(i+1)) * time.Millisecond)
				}

				if renameErr != nil {
					// 如果重命名失败，尝试复制文件
					logger.Infof("Task %s: Failed to rename .part file, trying to copy instead: %v", task.ID, renameErr)

					// 复制文件
					srcFile, err := os.Open(partPath)
					if err != nil {
						logger.Errorf("Task %s: Failed to open source file: %v", task.ID, err)
						return err
					}
					defer srcFile.Close()

					dstFile, err := os.Create(finalPath)
					if err != nil {
						logger.Errorf("Task %s: Failed to create destination file: %v", task.ID, err)
						return err
					}
					defer dstFile.Close()

					// 使用io.Copy复制文件内容
					if _, err := io.Copy(dstFile, srcFile); err != nil {
						logger.Errorf("Task %s: Failed to copy file: %v", task.ID, err)
						return err
					}

					// 确保文件内容已写入磁盘
					if err := dstFile.Sync(); err != nil {
						logger.Warnf("Task %s: Failed to sync destination file: %v", task.ID, err)
					}

					// 复制成功后，删除源文件
					if err := os.Remove(partPath); err != nil {
						logger.Warnf("Task %s: Failed to remove source file after copy: %v", task.ID, err)
					}

					logger.Infof("Task %s: Successfully copied .part file to final location: %s", task.ID, finalPath)
				}
			} else {
				// 既没有最终文件也没有.part文件，检查保存目录中是否只有一个文件
				files, err := os.ReadDir(baseSavePath)
				if err != nil {
					logger.Errorf("Task %s: Failed to read save directory: %v", task.ID, err)
					return err
				}

				// 过滤掉.torrent.bolt.db文件
				var validFiles []os.DirEntry
				for _, file := range files {
					if file.Name() != ".torrent.bolt.db" && !file.IsDir() {
						validFiles = append(validFiles, file)
					}
				}

				if len(validFiles) == 1 {
					// 只有一个文件，将其重命名为种子名称
					foundFile := validFiles[0]
					foundFilePath := filepath.Join(baseSavePath, foundFile.Name())
					logger.Infof("Task %s: Found single file in save path, renaming to torrent name: %s -> %s", task.ID, foundFilePath, finalPath)
					if err := os.Rename(foundFilePath, finalPath); err != nil {
						logger.Errorf("Task %s: Failed to rename file: %v", task.ID, err)
						return err
					}
					logger.Infof("Task %s: Successfully renamed file to torrent name", task.ID)
				} else {
					// 找不到文件，返回错误
					logger.Errorf("Task %s: Downloaded file not found in save directory: %s", task.ID, baseSavePath)
					return fmt.Errorf("downloaded file not found")
				}
			}
		} else {
			// 文件已经是最终状态，不需要任何操作
			logger.Infof("Task %s: File already exists in final state: %s", task.ID, finalPath)
		}
	}

	// Update final statistics
	b.statistics.EndTime = new(time.Time)
	*b.statistics.EndTime = time.Now()
	b.statistics.Duration = b.statistics.EndTime.Sub(b.statistics.StartTime)
	b.statistics.Downloaded = downloadTorrent.BytesCompleted()

	logger.Infof("Task %s: Download completed successfully! File saved to: %s", task.ID, finalPath)
	logger.Infof("Task %s: Final statistics - Downloaded: %d bytes, Duration: %v, Speed: %d bytes/sec",
		task.ID, b.statistics.Downloaded, b.statistics.Duration,
		int64(float64(b.statistics.Downloaded)/b.statistics.Duration.Seconds()))

	return nil
}

// renameDownloadedDir handles directory torrents
func (b *BTProtocol) renameDownloadedDir(dirname, savePath string) error {
	tempPath := filepath.Join(savePath, dirname+".part")
	finalPath := filepath.Join(savePath, dirname)

	// 检查目录是否存在
	if _, err := os.Stat(tempPath); os.IsNotExist(err) {
		// 可能已经被自动重命名了
		if _, err := os.Stat(finalPath); err == nil {
			fmt.Printf("Directory already renamed to: %s\n", finalPath)
			return nil
		}
		return fmt.Errorf("temp directory not found: %s", tempPath)
	}

	// 尝试重命名
	fmt.Printf("Renaming directory %s -> %s\n", tempPath, finalPath)

	// 最大重试次数
	for i := 0; i < 3; i++ {
		if err := os.Rename(tempPath, finalPath); err != nil {
			if i == 2 {
				fmt.Printf("Directory rename failed: %v\n", err)
				return err
			}
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
		} else {
			fmt.Printf("Directory renamed successfully\n")
			return nil
		}
	}

	return nil
}

// Pause pauses the download

func (b *BTProtocol) Pause() error {
	if !b.isRunning {
		return fmt.Errorf("not running")
	}
	b.isPaused = true
	b.status.IsPaused = true
	return nil
}

// Resume resumes the download

func (b *BTProtocol) Resume() error {
	if !b.isRunning {
		return fmt.Errorf("not running")
	}
	b.isPaused = false
	b.status.IsPaused = false
	return nil
}

// Cancel cancels the download

func (b *BTProtocol) Cancel() error {
	b.isRunning = false
	b.status.IsRunning = false
	return nil
}

// GetStatus gets the current status

func (b *BTProtocol) GetStatus() core.Status {
	return b.status
}

// GetStatistics gets the current statistics

func (b *BTProtocol) GetStatistics() core.Statistics {
	return b.statistics
}

// ApplyConfig applies the configuration

func (b *BTProtocol) ApplyConfig(config core.ProtocolConfig) error {
	b.config = config
	return nil
}

// GetCapabilities gets the capabilities of the protocol

func (b *BTProtocol) GetCapabilities() core.Capabilities {
	return core.Capabilities{
		CanResume:           true,
		CanVerify:           true,
		SupportsChunks:      true,
		SupportsP2P:         true,
		SupportedURLSchemes: []string{"magnet", "torrent"},
	}
}

// SetResourceController sets the resource controller for the protocol

func (b *BTProtocol) SetResourceController(rc *core.ResourceController) {
	b.resourceCtrl = rc
}

// SetConnectionPool sets the connection pool for the protocol

func (b *BTProtocol) SetConnectionPool(pool *core.ConnectionPool) {
	b.connPool = pool
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	return destFile.Sync()
}

// copyAndRemove copies a file and removes the source
func copyAndRemove(src, dst string) error {
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

// renameDownloadedFile renames the downloaded file with retry logic
func (b *BTProtocol) renameDownloadedFile(filename, savePath string) error {
	tempPath := filepath.Join(savePath, filename+".part")
	finalPath := filepath.Join(savePath, filename)

	// 检查文件是否存在
	if _, err := os.Stat(tempPath); os.IsNotExist(err) {
		// 可能已经被自动重命名了
		if _, err := os.Stat(finalPath); err == nil {
			fmt.Printf("File already renamed to: %s\n", finalPath)
			return nil
		}
		return fmt.Errorf("temp file not found: %s", tempPath)
	}

	// 尝试重命名
	fmt.Printf("Renaming %s -> %s\n", tempPath, finalPath)

	// 最大重试次数
	for i := 0; i < 3; i++ {
		if err := os.Rename(tempPath, finalPath); err != nil {
			if i == 2 { // 最后一次尝试
				fmt.Printf("Rename failed, trying copy: %v\n", err)
				return copyAndRemove(tempPath, finalPath)
			}
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
		} else {
			fmt.Printf("File renamed successfully\n")
			return nil
		}
	}

	return nil
}
