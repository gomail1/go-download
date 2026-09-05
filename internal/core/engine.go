package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go-download-server/internal/config"
	"go-download-server/internal/event"
	"go-download-server/internal/logger"
	"go-download-server/utils"
)

// Engine defines the core engine interface
type Engine interface {
	AddTask(ctx context.Context, req *AddTaskRequest) (*Task, error)
	GetTask(id string) (*Task, error)
	ListTasks() []*Task
	StartTask(ctx context.Context, id string) error
	PauseTask(id string) error
	ResumeTask(ctx context.Context, id string) error
	CancelTask(id string) error
	RemoveTask(id string) error
	GetStatistics() *EngineStatistics
	Close() error
}

// AddTaskRequest defines the request to add a task
type AddTaskRequest struct {
	URL              string                 `json:"url"`
	SavePath         string                 `json:"save_path,omitempty"`
	Overwrite        bool                   `json:"overwrite,omitempty"`
	Protocol         string                 `json:"protocol,omitempty"`
	Config           *TaskConfig            `json:"config,omitempty"`
	Traits           []string               `json:"traits,omitempty"`
	ProtocolSpecific map[string]interface{} `json:"protocol_specific,omitempty"`
}

// EngineStatistics defines the statistics of the engine
type EngineStatistics struct {
	TotalTasks      int   `json:"total_tasks"`
	ActiveTasks     int   `json:"active_tasks"`
	CompletedTasks  int   `json:"completed_tasks"`
	FailedTasks     int   `json:"failed_tasks"`
	TotalDownloaded int64 `json:"total_downloaded"`
	TotalUploaded   int64 `json:"total_uploaded"`
}

// QuadEngine implements the Engine interface
type QuadEngine struct {
	mu             sync.RWMutex
	tasks          map[string]*Task
	protocolMgr    ProtocolManager
	persistenceMgr *PersistenceManager
	statistics     *EngineStatistics
	resourceCtrl   *ResourceController
	chunkManager   ChunkManager
	connPools      map[string]*ConnectionPool
	isRunning      bool
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup // 跟踪进行中的下载协程，便于 Close 优雅等待
}

// NewQuadEngine creates a new QuadEngine instance
func NewQuadEngine(protocolMgr ProtocolManager) *QuadEngine {
	// Initialize random seed
	rand.Seed(time.Now().UnixNano())

	// Create default data directory - 任务数据统一存放到 config/tasks 目录下
	dataDir := filepath.Join("config", "tasks")
	// 支持通过环境变量自定义数据目录
	if envDataDir := os.Getenv("QUADFETCH_DATA_DIR"); envDataDir != "" {
		dataDir = envDataDir
	}

	// Initialize persistence manager
	persistenceMgr := NewPersistenceManager(dataDir)

	// Load existing tasks from disk
	tasks, err := persistenceMgr.LoadAllTasks()
	if err != nil {
		logger.Errorf("Failed to load tasks: %v", err)
		tasks = make([]*Task, 0)
	}

	// Create task map
	taskMap := make(map[string]*Task)
	for _, task := range tasks {
		taskMap[task.ID] = task
	}

	// Initialize resource controller with default config
	resourceConfig := ResourceConfig{
		Global: struct {
			MaxConnections int
			MaxFileHandles int
			MaxMemoryMB    int
		}{
			MaxConnections: 100,
			MaxFileHandles: 1000,
			MaxMemoryMB:    512,
		},
		Protocol: struct {
			HTTP struct {
				MaxConnections int
				MaxFileHandles int
			}
			BT struct {
				MaxConnections int
				MaxFileHandles int
			}
		}{
			HTTP: struct {
				MaxConnections int
				MaxFileHandles int
			}{
				MaxConnections: 50,
				MaxFileHandles: 500,
			},
			BT: struct {
				MaxConnections int
				MaxFileHandles int
			}{
				MaxConnections: 100,
				MaxFileHandles: 1000,
			},
		},
	}
	resourceCtrl := NewResourceController(resourceConfig)

	// Initialize chunk manager with default configuration
	chunkManagerConfig := ChunkStrategyConfig{
		DefaultStrategy: ChunkStrategyDynamic,
		MinChunkSize:    1 * 1024 * 1024,  // 1MB
		MaxChunkSize:    50 * 1024 * 1024, // 50MB
		MaxChunks:       100,
	}
	chunkManager := NewDefaultChunkManager(chunkManagerConfig)

	// Initialize connection pools for each protocol
	connPools := make(map[string]*ConnectionPool)

	ctx, cancel := context.WithCancel(context.Background())

	// Initialize statistics
	stats := &EngineStatistics{
		TotalTasks: len(tasks),
	}

	// Count completed and failed tasks
	for _, task := range tasks {
		switch task.Status {
		case TaskStatusCompleted:
			stats.CompletedTasks++
		case TaskStatusFailed:
			stats.FailedTasks++
		case TaskStatusDownloading:
			stats.ActiveTasks++
		case TaskStatusWaiting:
			stats.ActiveTasks++
		}
	}

	// Create engine instance
	e := &QuadEngine{
		tasks:          taskMap,
		protocolMgr:    protocolMgr,
		persistenceMgr: persistenceMgr,
		statistics:     stats,
		resourceCtrl:   resourceCtrl,
		chunkManager:   chunkManager,
		connPools:      connPools,
		isRunning:      true,
		ctx:            ctx,
		cancel:         cancel,
	}

	// Auto-start unfinished tasks after engine is created.
	// 覆盖进程崩溃/强杀可能遗留的全部中间态：
	//   waiting     —— 已入队但尚未开始下载
	//   preparing   —— 已入队、正在解析元数据/切分分块阶段被中断
	//                   （此前该状态不在恢复范围内，重启后任务会永久卡死在 preparing，用户只能删任务重建，#5）
	//   downloading —— 下载中断开；HTTP 多线程下载会依据 .downloading.json 自动续传
	for _, task := range tasks {
		recoverStatus := task.Status // 锁外取值供日志使用，避免与 StartTask 并发写产生读竞争
		switch task.Status {
		case TaskStatusDownloading, TaskStatusPreparing, TaskStatusWaiting:
			// 自动重新启动未完成任务
			go func(t *Task) {
				logger.Infof("Recovering unfinished task %s from status %s after restart", t.ID, recoverStatus)
				// 创建新的上下文，避免主上下文被取消
				taskCtx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if err := e.StartTask(taskCtx, t.ID); err != nil {
					logger.Errorf("Failed to restart task %s: %v", t.ID, err)
					// 更新任务状态为失败并记录原因，避免任务悬在中间态
					e.mu.Lock()
					t.Status = TaskStatusFailed
					t.Error = "重启自动恢复失败: " + err.Error()
					e.mu.Unlock()
					// 保存任务状态
					e.persistenceMgr.SaveTask(t)
				}
			}(task)
		}
	}

	return e
}

// AddTask adds a new download task
func (e *QuadEngine) AddTask(ctx context.Context, req *AddTaskRequest) (*Task, error) {
	if !e.isRunning {
		return nil, errors.New("engine is not running")
	}

	// Validate request
	if req.URL == "" {
		return nil, errors.New("url is required")
	}

	// SSRF 防护：仅允许 http/https/ftp/magnet 协议；
	// 对远程地址校验目标主机非内网/回环/链路本地/保留地址；
	// 拒绝 file:// 等危险协议；本地文件路径（如上传的 .torrent 文件）放行。
	if u, parseErr := url.Parse(req.URL); parseErr == nil {
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "ftp", "magnet":
			if err := utils.ValidateRemoteDownloadURL(req.URL); err != nil {
				return nil, err
			}
		case "file":
			return nil, errors.New("不允许使用 file:// 协议")
		case "":
			// 本地文件路径（如上传的 .torrent 文件），跳过 SSRF 校验
		default:
			return nil, fmt.Errorf("不支持的下载协议: %s", u.Scheme)
		}
	}

	// Find appropriate protocol
	_, protocolName, err := e.protocolMgr.FindProtocol(req.URL)
	if err != nil {
		return nil, err
	}

	// Get global config
	cfg := config.Get()

	// Create default task config if not provided
	if req.Config == nil {
		req.Config = &TaskConfig{
			SavePath:      "pending/download-user", // 固定保存路径，不允许客户端修改
			Overwrite:     false,
			MaxRetries:    cfg.Cow.Stability.MaxRetries,
			RetryDelay:    parseDuration(cfg.Cow.Stability.RetryDelay),
			VerifyHash:    cfg.Cow.Stability.VerifyHash,
			ResumeEnabled: cfg.Cow.Stability.ResumeEnabled,
			MaxThreads:    cfg.Orange.Efficiency.MaxThreads,
			ChunkStrategy: cfg.Orange.Efficiency.ChunkStrategy,
			PreAllocate:   cfg.Orange.Efficiency.PreAllocate,
			SpeedLimit:    0, // Unlimited by default
		}
	} else {
		// Always use fixed save path, ignore client-provided save path
		req.Config.SavePath = "pending/download-user" // 固定保存路径，不允许客户端修改
		// Fill in missing config with defaults from global config
		if req.Config.MaxRetries <= 0 {
			req.Config.MaxRetries = cfg.Cow.Stability.MaxRetries
		}
		if req.Config.RetryDelay <= 0 {
			req.Config.RetryDelay = parseDuration(cfg.Cow.Stability.RetryDelay)
		}
		if req.Config.MaxThreads <= 0 {
			req.Config.MaxThreads = cfg.Orange.Efficiency.MaxThreads
		}
		if req.Config.ChunkStrategy == "" {
			req.Config.ChunkStrategy = cfg.Orange.Efficiency.ChunkStrategy
		}
	}

	// Create task
	task := &Task{
		ID:               generateTaskID(),
		URL:              req.URL,
		Protocol:         protocolName,
		Status:           TaskStatusWaiting,
		Progress:         &Progress{},
		Statistics:       &Statistics{},
		Config:           req.Config,
		CreatedAt:        getCurrentTime(),
		Traits:           req.Traits,
		ProtocolSpecific: req.ProtocolSpecific,
		ProtocolInstance: nil,
	}

	// Save task to memory
	e.mu.Lock()
	e.tasks[task.ID] = task
	e.statistics.TotalTasks++
	e.mu.Unlock()

	// Save task to disk
	err = e.persistenceMgr.SaveTask(task)
	if err != nil {
		logger.Errorf("Failed to save task: %v", err)
		// Continue execution, don't fail the task creation
	}

	// Publish event
	event.Publish(event.Event{
		Type: event.EventTaskCreated,
		Data: snapshotTask(task),
	})

	logger.Infof("Task added: %s, protocol: %s", task.ID, protocolName)

	// 自动启动任务
	go func() {
		// 创建新的上下文，避免AddTask的上下文被取消
		taskCtx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if err := e.StartTask(taskCtx, task.ID); err != nil {
			logger.Errorf("Failed to start task %s: %v", task.ID, err)
			// 更新任务状态为失败
			e.mu.Lock()
			task.Status = TaskStatusFailed
			task.Error = "Failed to start task: " + err.Error()
			e.mu.Unlock()
			// 保存任务状态
			err = e.persistenceMgr.SaveTask(task)
			if err != nil {
				logger.Errorf("Failed to save task %s: %v", task.ID, err)
			}
		}
	}()

	return task, nil
}

// GetTask gets a task by ID
func (e *QuadEngine) GetTask(id string) (*Task, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	task, ok := e.tasks[id]
	if !ok {
		return nil, errors.New("task not found")
	}
	return task, nil
}

// ListTasks lists all tasks, including those from disk
func (e *QuadEngine) ListTasks() []*Task {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Update statistics based on current memory tasks
	e.statistics.TotalTasks = len(e.tasks)
	e.statistics.ActiveTasks = 0
	e.statistics.CompletedTasks = 0
	e.statistics.FailedTasks = 0
	for _, task := range e.tasks {
		switch task.Status {
		case TaskStatusDownloading:
			e.statistics.ActiveTasks++
		case TaskStatusCompleted:
			e.statistics.CompletedTasks++
		case TaskStatusFailed:
			e.statistics.FailedTasks++
		case TaskStatusPreparing:
			e.statistics.ActiveTasks++
		}
	}

	taskList := make([]*Task, 0, len(e.tasks))
	for _, task := range e.tasks {
		taskList = append(taskList, task)
	}
	return taskList
}

// StartTask starts a task
func (e *QuadEngine) StartTask(ctx context.Context, id string) error {
	e.mu.Lock()
	task, ok := e.tasks[id]
	e.mu.Unlock()

	if !ok {
		return errors.New("task not found: " + id)
	}

	// Check if task can be started
	switch task.Status {
	case TaskStatusCompleted:
		return errors.New("task already completed")
	case TaskStatusCancelled:
		return errors.New("task already cancelled")
	}

	// Cancel previous task context if exists
	e.mu.Lock()
	if task.cancelFunc != nil {
		task.cancelFunc()
		task.cancelFunc = nil
	}
	e.mu.Unlock()

	// Create a new context for this task，派生自引擎上下文，
	// 这样在引擎 Close() 取消 e.ctx 时可一并终止进行中的下载协程。
	taskCtx, cancel := context.WithCancel(e.ctx)
	e.mu.Lock()
	task.cancelFunc = cancel
	e.mu.Unlock()

	// Update task status to preparing
	e.mu.Lock()
	task.Status = TaskStatusPreparing
	now := time.Now()
	task.StartedAt = &now
	taskCopy := snapshotTask(task) // Create a copy for persistence
	e.mu.Unlock()

	// Save task to disk
	err := e.persistenceMgr.SaveTask(taskCopy)
	if err != nil {
		logger.Errorf("Failed to save task: %v", err)
	}

	logger.Infof("Starting task: %s", id)

	// Create a new protocol instance for this task
	protocol, err := e.protocolMgr.GetProtocol(task.Protocol)
	if err != nil {
		return err
	}

	// Set resource controller for the protocol
	protocol.SetResourceController(e.resourceCtrl)

	// Get or create connection pool for this protocol
	e.mu.Lock()
	connPool, exists := e.connPools[task.Protocol]
	if !exists {
		// Create new connection pool config
		connPoolConfig := ConnectionPoolConfig{
			MaxConnections: 50,
			MaxIdleTime:    5 * time.Minute,
			MaxLifetime:    30 * time.Minute,
		}
		connPool = NewConnectionPool(task.Protocol, connPoolConfig)
		e.connPools[task.Protocol] = connPool
		// Start cleanup ticker
		connPool.StartCleanupTicker()
	}
	e.mu.Unlock()

	// Set connection pool for the protocol
	protocol.SetConnectionPool(connPool)

	// Save protocol instance for this task
	e.mu.Lock()
	task.ProtocolInstance = protocol
	e.mu.Unlock()

	// Get metadata if not available
	if task.Metadata == nil {
		metadata, err := protocol.GetMetadata(taskCtx, task.URL)
		if err != nil {
			// Update task status to failed if metadata fetch fails
			e.mu.Lock()
			task.Status = TaskStatusFailed
			task.Error = "获取元数据失败: " + err.Error()
			taskCopy = snapshotTask(task)
			e.mu.Unlock()

			// Save updated task
			err = e.persistenceMgr.SaveTask(taskCopy)
			if err != nil {
				logger.Errorf("Failed to save task: %v", err)
			}

			return err
		}

		e.mu.Lock()
		task.Metadata = metadata
		taskCopy = snapshotTask(task)
		e.mu.Unlock()

		// Save updated task
		err = e.persistenceMgr.SaveTask(taskCopy)
		if err != nil {
			logger.Errorf("Failed to save task: %v", err)
		}
	}

	// Split task into chunks using chunk manager
	chunks, err := e.chunkManager.Split(task)
	if err != nil {
		return err
	}

	// Update task with chunks and progress information
	e.mu.Lock()
	task.Chunks = chunks
	task.Progress.TotalChunks = len(chunks)
	// 设置任务状态为下载中
	task.Status = TaskStatusDownloading
	// 保留已有的进度信息（仅设置初始值，如果是新任务）
	if task.Progress.TotalSize == 0 {
		task.Progress.TotalSize = task.Metadata.Size
	}
	// 如果是恢复任务，保留已下载的进度，否则设置初始值
	if task.Progress.Downloaded == 0 {
		task.Progress.Percentage = 0
		task.Progress.Downloaded = 0
		task.Progress.Speed = 0
		task.Progress.ETA = 0
		task.Progress.CurrentChunk = 0
	}
	taskCopy = snapshotTask(task)
	e.mu.Unlock()

	// Save task to disk
	err = e.persistenceMgr.SaveTask(taskCopy)
	if err != nil {
		logger.Errorf("Failed to save task: %v", err)
	}

	// Ensure download path exists before starting download
	if err := os.MkdirAll(task.Config.SavePath, 0755); err != nil {
		logger.Errorf("Failed to create download path: %v", err)
		// Update task status to failed
		e.mu.Lock()
		task.Status = TaskStatusFailed
		task.Error = "Failed to create download path: " + err.Error()
		taskCopy = snapshotTask(task)
		e.mu.Unlock()
		// Save task to disk
		err = e.persistenceMgr.SaveTask(taskCopy)
		// Publish task failed event
		event.Publish(event.Event{
			Type: event.EventTaskCompleted,
			Data: snapshotTask(task),
		})
		return err
	}

	// Start download in a goroutine
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		progressChan := make(chan Progress)

		// Progress update handling goroutine
		progressDone := make(chan struct{})
		go func() {
			for {
				select {
				case progress, ok := <-progressChan:
					if !ok {
						// Channel closed, exit
						close(progressDone)
						return
					}
					// Update task progress
					e.mu.Lock()
					task.Progress.Percentage = progress.Percentage
					task.Progress.Downloaded = progress.Downloaded
					task.Progress.TotalSize = progress.TotalSize
					task.Progress.Speed = progress.Speed
					task.Progress.ETA = progress.ETA
					task.Progress.CurrentChunk = progress.CurrentChunk
					task.Progress.TotalChunks = progress.TotalChunks
					task.Progress.Status = progress.Status
					task.Progress.ActivePeers = progress.ActivePeers
					task.Progress.TotalPeers = progress.TotalPeers
					// 在锁内生成深拷贝快照，避免与写协程共享同一批引用字段造成 data race
					snap := snapshotTask(task)
					e.mu.Unlock()
					// Publish progress event
					event.Publish(event.Event{
						Type: event.EventTaskProgress,
						Data: snap,
					})
				case <-taskCtx.Done():
					// Context canceled, exit
					close(progressDone)
					return
				}
			}
		}()

		// Start download
		err := protocol.Download(taskCtx, task, progressChan)

		// Wait for progress handler to finish
		close(progressChan)
		<-progressDone

		// Handle download result
		e.mu.Lock()

		if err != nil {
			// 主动取消/暂停导致的 context canceled 不算失败：
			// PauseTask/CancelTask 已将状态置为 Paused/Cancelled，这里保持不变
			if taskCtx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
				if task.Status != TaskStatusPaused && task.Status != TaskStatusCancelled {
					task.Status = TaskStatusFailed
					task.Error = err.Error()
					e.statistics.FailedTasks++
				}
				logger.Infof("Task %s terminated by context cancellation (paused/cancelled)", id)
			} else {
				task.Status = TaskStatusFailed
				task.Error = err.Error()
				// 检查是否是WAF导致的失败
				if strings.Contains(err.Error(), "context canceled") || strings.Contains(err.Error(), "timeout") {
					task.Error = "下载失败: 可能是目标网站的WAF安全验证导致。建议：1. 在浏览器中完成验证后复制Cookie；2. 使用浏览器直接下载；3. 检查网络连接"
				}
				e.statistics.FailedTasks++
				logger.Errorf("Task failed: %s, error: %v", id, err)
			}
		} else if task.Status == TaskStatusPaused || task.Status == TaskStatusCancelled {
			// 任务已被暂停/取消（例如 CancelTask 未及取消 ctx 时文件已下载完成），保留终态
			logger.Infof("Task %s finished download but was paused/cancelled, keeping status %s", id, task.Status)
		} else {
			task.Status = TaskStatusCompleted
			completedAt := time.Now()
			task.CompletedAt = &completedAt
			e.statistics.CompletedTasks++
			e.statistics.TotalDownloaded += task.Metadata.Size
			// Ensure progress is 100% when task is completed
			task.Progress.Percentage = 100
			task.Progress.Downloaded = task.Metadata.Size
			task.Progress.ETA = 0
			logger.Infof("Task completed: %s", id)

			// Move file to pending directory for review if it's not already there
			// Get original file path
			originalFilePath := filepath.Join(task.Config.SavePath, task.Metadata.Filename)
			// Create pending directory for download-user
			pendingDir := filepath.Join("pending", "download-user")
			if err := os.MkdirAll(pendingDir, 0755); err != nil {
				logger.Errorf("Failed to create pending directory: %v", err)
			} else {
				// Move file to pending directory only if it's not already there
				pendingFilePath := filepath.Join(pendingDir, task.Metadata.Filename)
				if originalFilePath != pendingFilePath {
					// 先尝试使用Rename（同分区快速移动）
					if err := os.Rename(originalFilePath, pendingFilePath); err != nil {
						// Rename失败（可能是跨设备），使用复制+删除的方式
						logger.Warnf("Rename failed (possibly cross-device), using copy+delete: %v", err)
						if err := copyFile(originalFilePath, pendingFilePath); err != nil {
							logger.Errorf("Failed to copy file to pending directory: %v", err)
						} else {
							// 复制成功后删除源文件
							if err := os.Remove(originalFilePath); err != nil {
								logger.Errorf("Failed to remove original file after copy: %v", err)
							} else {
								logger.Infof("File copied to pending directory for review: %s", pendingFilePath)
							}
						}
					} else {
						logger.Infof("File moved to pending directory for review: %s", pendingFilePath)
					}
				} else {
					// File is already in pending directory, no need to move
					logger.Infof("File is already in pending directory for review: %s", pendingFilePath)
				}
			}

			// 下载完成后清理临时文件
			cleanupTempFilesAfterDownload(task.Config.SavePath, task.Metadata.Filename)
		}

		// 终止任务上下文，释放关联的 context 节点与资源（避免 context 泄漏）
		if task.cancelFunc != nil {
			task.cancelFunc()
			task.cancelFunc = nil
		}

		// Save final task status
		taskCopy := snapshotTask(task)
		e.mu.Unlock()
		err = e.persistenceMgr.SaveTask(taskCopy)
		// Publish task completed/failed event
		event.Publish(event.Event{
			Type: event.EventTaskCompleted,
			Data: snapshotTask(task),
		})
	}()

	return nil
}

// PauseTask pauses a task
func (e *QuadEngine) PauseTask(id string) error {
	e.mu.Lock()
	task, ok := e.tasks[id]
	if !ok {
		e.mu.Unlock()
		return errors.New("task not found: " + id)
	}

	// 允许在下载中或准备中（如 BT 正在获取元数据/Tracker）暂停
	if task.Status != TaskStatusDownloading && task.Status != TaskStatusPreparing {
		e.mu.Unlock()
		return errors.New("task is not downloading or preparing")
	}

	// Cancel the download context to immediately stop the download
	if task.cancelFunc != nil {
		task.cancelFunc()
		task.cancelFunc = nil
	}

	// Call protocol's Pause method to actually pause the download
	if task.ProtocolInstance != nil {
		task.ProtocolInstance.Pause()
	}

	// Update task status
	task.Status = TaskStatusPaused
	taskCopy := snapshotTask(task)
	e.mu.Unlock()

	// Save task to disk
	err := e.persistenceMgr.SaveTask(taskCopy)
	if err != nil {
		logger.Errorf("Failed to save task: %v", err)
	}

	logger.Infof("Task paused: %s", id)
	return nil
}

// ResumeTask resumes a task
func (e *QuadEngine) ResumeTask(ctx context.Context, id string) error {
	e.mu.Lock()
	task, ok := e.tasks[id]
	if !ok {
		e.mu.Unlock()
		return errors.New("task not found: " + id)
	}

	if task.Status != TaskStatusPaused {
		e.mu.Unlock()
		return errors.New("task is not paused")
	}

	// Reset task status to waiting
	task.Status = TaskStatusWaiting
	taskCopy := snapshotTask(task)
	e.mu.Unlock()

	// Save task to disk
	err := e.persistenceMgr.SaveTask(taskCopy)
	if err != nil {
		logger.Errorf("Failed to save task: %v", err)
	}

	// Call StartTask to properly resume the download
	return e.StartTask(ctx, id)
}

// CancelTask cancels a task
func (e *QuadEngine) CancelTask(id string) error {
	e.mu.Lock()
	task, ok := e.tasks[id]
	if !ok {
		e.mu.Unlock()
		return errors.New("task not found: " + id)
	}

	if task.Status == TaskStatusCompleted || task.Status == TaskStatusCancelled {
		e.mu.Unlock()
		return errors.New("task already completed or cancelled")
	}

	// Update task status
	task.Status = TaskStatusCancelled
	task.Error = "cancelled by user"

	// 取消下载上下文，终止进行中的下载协程
	// （否则下载会继续跑完，完成后状态还会被覆盖为 Completed）
	if task.cancelFunc != nil {
		task.cancelFunc()
		task.cancelFunc = nil
	}

	taskCopy := snapshotTask(task)
	e.mu.Unlock()

	// Save task to disk
	err := e.persistenceMgr.SaveTask(taskCopy)
	if err != nil {
		logger.Errorf("Failed to save task: %v", err)
	}

	logger.Infof("Task cancelled: %s", id)
	return nil
}

// RemoveTask removes a task and cleans up resources
func (e *QuadEngine) RemoveTask(id string) error {
	logger.Infof("RemoveTask called with id: %s", id)

	e.mu.Lock()
	task, ok := e.tasks[id]
	if !ok {
		logger.Errorf("Task not found: %s", id)
		e.mu.Unlock()
		return errors.New("task not found: " + id)
	}

	// Cancel the download if it's running
	if task.cancelFunc != nil {
		task.cancelFunc()
		logger.Infof("Cancelled download for task: %s", id)
	}

	// 保存协议实例引用，在锁外调用Cancel（避免死锁）
	var protocolInstance Protocol
	if task.ProtocolInstance != nil {
		protocolInstance = task.ProtocolInstance
	}

	// Clean up protocol-specific resources (especially for BT)
	if task.Protocol == "bittorrent" || task.Protocol == "bt" || task.Protocol == "magnet" {
		logger.Infof("Cleaning up BT task resources: %s", id)
	}

	// Remove task from memory
	taskCopy := snapshotTask(task) // Create a copy for cleanup outside the lock
	delete(e.tasks, id)
	e.statistics.TotalTasks--
	// 更新统计数据中的活跃任务数
	switch taskCopy.Status {
	case TaskStatusDownloading, TaskStatusWaiting:
		e.statistics.ActiveTasks--
	case TaskStatusCompleted:
		e.statistics.CompletedTasks--
	case TaskStatusFailed:
		e.statistics.FailedTasks--
	}
	logger.Infof("Removed task from memory: %s", id)
	e.mu.Unlock()

	// 在锁外调用协议的Cancel方法，清理协议资源（BT的goroutine、网络连接等）
	if protocolInstance != nil {
		logger.Infof("Calling protocol Cancel for task: %s, protocol: %s", id, taskCopy.Protocol)
		if err := protocolInstance.Cancel(); err != nil {
			logger.Errorf("Failed to cancel protocol instance for task %s: %v", id, err)
		} else {
			logger.Infof("Protocol instance cancelled successfully for task: %s", id)
		}
	}

	// Clean up temporary files and cache
	if taskCopy.Config != nil && taskCopy.Config.SavePath != "" {
		// For BT tasks, there might be .torrent files, cache directories, etc.
		// For HTTP/FTP, there might be .part files or temporary chunks
		logger.Infof("Cleaning up task files: %s, path: %s", id, taskCopy.Config.SavePath)

		// Clean up common temporary file patterns
		if taskCopy.Metadata != nil && taskCopy.Metadata.Filename != "" {
			// 构建完整的文件路径
			fullFilePath := filepath.Join(taskCopy.Config.SavePath, taskCopy.Metadata.Filename)

			// 清理.part文件（HTTP/FTP临时文件）
			partFile := fullFilePath + ".part"
			if _, err := os.Stat(partFile); err == nil {
				os.Remove(partFile)
				logger.Infof("Removed part file: %s", partFile)
			}

			// 清理torrent文件（BT种子文件）
			torrentFile := fullFilePath + ".torrent"
			if _, err := os.Stat(torrentFile); err == nil {
				os.Remove(torrentFile)
				logger.Infof("Removed torrent file: %s", torrentFile)
			}

			// 清理BT缓存目录（任务特定的缓存）
			if taskCopy.Protocol == "bittorrent" || taskCopy.Protocol == "bt" {
				// 清理bt_cache目录下的任务特定缓存
				btCacheDir := filepath.Join(taskCopy.Config.SavePath, "bt_cache", id)
				if _, err := os.Stat(btCacheDir); err == nil {
					os.RemoveAll(btCacheDir)
					logger.Infof("Removed BT cache directory: %s", btCacheDir)
				}
			}
		}

		// 递归清理整个savePath目录下的所有.part文件
		logger.Infof("Recursively cleaning .part files in: %s", taskCopy.Config.SavePath)
		err := filepath.Walk(taskCopy.Config.SavePath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				logger.Errorf("Error walking path %s: %v", path, err)
				return err
			}

			// 如果是文件，并且以.part结尾，删除它
			if !info.IsDir() && strings.HasSuffix(info.Name(), ".part") {
				if err := os.Remove(path); err != nil {
					logger.Errorf("Failed to remove part file %s: %v", path, err)
				} else {
					logger.Infof("Removed part file: %s", path)
				}
			}

			return nil
		})

		if err != nil {
			logger.Errorf("Failed to recursively clean .part files: %v", err)
		}

		// 清理以文件名为名的目录（BT/HTTP下载会创建此目录存放.part文件）
		if taskCopy.Metadata != nil && taskCopy.Metadata.Filename != "" {
			fileDir := filepath.Join(taskCopy.Config.SavePath, taskCopy.Metadata.Filename)
			if info, err := os.Stat(fileDir); err == nil && info.IsDir() {
				// 检查目录是否为空
				files, err := os.ReadDir(fileDir)
				if err == nil && len(files) == 0 {
					// 目录为空，直接删除
					if err := os.Remove(fileDir); err != nil {
						logger.Errorf("Failed to remove empty file directory: %s, error: %v", fileDir, err)
					} else {
						logger.Infof("Removed empty file directory: %s", fileDir)
					}
				} else if err == nil && len(files) > 0 {
					// 目录不为空，对于BT任务直接删除整个目录（BT下载的文件都在这里）
					if taskCopy.Protocol == "bittorrent" || taskCopy.Protocol == "bt" || taskCopy.Protocol == "magnet" {
						logger.Infof("Removing BT download directory (not empty): %s", fileDir)
						if err := os.RemoveAll(fileDir); err != nil {
							logger.Errorf("Failed to remove BT download directory: %s, error: %v", fileDir, err)
						} else {
							logger.Infof("Removed BT download directory: %s", fileDir)
						}
					} else {
						// 对于其他任务，只删除.part相关文件，保留可能的完整文件
						logger.Infof("File directory not empty, keeping non-part files: %s (files: %d)", fileDir, len(files))
						// 递归删除目录下所有.part文件
						filepath.Walk(fileDir, func(path string, info os.FileInfo, err error) error {
							if err != nil {
								return nil
							}
							if !info.IsDir() && strings.HasSuffix(info.Name(), ".part") {
								os.Remove(path)
								logger.Infof("Removed part file in subdirectory: %s", path)
							}
							return nil
						})
						// 再次检查目录是否为空，如果为空则删除
						files2, _ := os.ReadDir(fileDir)
						if len(files2) == 0 {
							os.Remove(fileDir)
							logger.Infof("Removed file directory after cleaning part files: %s", fileDir)
						}
					}
				}
			}
		}

		// 清理.parts文件（下载进度/位图文件）
		if taskCopy.Metadata != nil && taskCopy.Metadata.Filename != "" {
			partsFile := filepath.Join(taskCopy.Config.SavePath, taskCopy.Metadata.Filename+".part.parts")
			if _, err := os.Stat(partsFile); err == nil {
				os.Remove(partsFile)
				logger.Infof("Removed parts file: %s", partsFile)
			}

			// 清理.downloading.json状态文件（HTTP多线程下载的进度文件）
			downloadingStateFile := filepath.Join(taskCopy.Config.SavePath, taskCopy.Metadata.Filename+".downloading.json")
			if _, err := os.Stat(downloadingStateFile); err == nil {
				os.Remove(downloadingStateFile)
				logger.Infof("Removed downloading state file: %s", downloadingStateFile)
			}
			// 清理.downloading.json.tmp临时文件
			downloadingStateTmpFile := downloadingStateFile + ".tmp"
			if _, err := os.Stat(downloadingStateTmpFile); err == nil {
				os.Remove(downloadingStateTmpFile)
				logger.Infof("Removed downloading state tmp file: %s", downloadingStateTmpFile)
			}
		}

		// 检查savePath目录是否为空，如果为空则删除该目录
		// 注意：这里我们只删除任务特定的目录，而不是共享目录
		// 共享目录如"pending/download-user"不应该被删除
		if taskCopy.Protocol == "bittorrent" || taskCopy.Protocol == "bt" {
			// 对于BT任务，检查bt_cache目录下的任务特定目录
			btTaskDir := filepath.Join(taskCopy.Config.SavePath, "bt_cache", id)
			if _, err := os.Stat(btTaskDir); err == nil {
				// 这个目录已经在前面被删除了
				logger.Infof("BT task directory already removed: %s", btTaskDir)
			}
		}

		// 对于其他任务类型，如果savePath是任务特定的临时目录，则检查是否为空并删除
		// 这里我们需要判断savePath是否是共享目录
		isSharedDir := strings.Contains(taskCopy.Config.SavePath, "pending/download-user") ||
			strings.Contains(taskCopy.Config.SavePath, "downloads") ||
			taskCopy.Config.SavePath == "tasks"

		if !isSharedDir {
			// 检查目录是否为空
			files, err := os.ReadDir(taskCopy.Config.SavePath)
			if err == nil && len(files) == 0 {
				// 目录为空，可以删除
				if err := os.Remove(taskCopy.Config.SavePath); err != nil {
					logger.Errorf("Failed to remove empty directory: %s, error: %v", taskCopy.Config.SavePath, err)
				} else {
					logger.Infof("Removed empty directory: %s", taskCopy.Config.SavePath)
				}
			}
		}
	}

	// Remove task from disk
	logger.Infof("Calling DeleteTask on persistence manager for id: %s", id)
	err := e.persistenceMgr.DeleteTask(id)
	if err != nil {
		logger.Errorf("Failed to delete task from disk: %v", err)
	} else {
		logger.Infof("Successfully deleted task from disk: %s", id)
	}

	// Also remove any progress or temporary files related to the task
	taskDir := e.persistenceMgr.dataDir
	logger.Infof("Checking for additional task files in directory: %s", taskDir)
	// Remove any other files with this task ID in the tasks directory
	// 检查实际存储位置：任务文件直接存储在 taskDir 下，而不是 taskDir/tasks 下
	files, err := filepath.Glob(filepath.Join(taskDir, id+"_*"))
	if err != nil {
		logger.Errorf("Error globbing task files: %v", err)
	} else {
		logger.Infof("Found %d additional task files to delete", len(files))
		for _, f := range files {
			logger.Infof("Removing additional task file: %s", f)
			if err := os.Remove(f); err != nil {
				logger.Errorf("Failed to remove file: %s, error: %v", f, err)
			} else {
				logger.Infof("Successfully removed file: %s", f)
			}
		}
	}

	logger.Infof("Task removed completely: %s", id)
	return nil
}

// GetStatistics gets the engine statistics
func (e *QuadEngine) GetStatistics() *EngineStatistics {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Create a copy to avoid race conditions
	stats := *e.statistics
	return &stats
}

// Close closes the engine
func (e *QuadEngine) Close() error {
	if !e.isRunning {
		return errors.New("engine is already closed")
	}

	// 先将进行中的任务标记为已取消，使其下载协程在 ctx 取消后保留终态（而非标记为失败）
	e.mu.Lock()
	e.isRunning = false
	for _, task := range e.tasks {
		if task.Status == TaskStatusDownloading || task.Status == TaskStatusWaiting || task.Status == TaskStatusPreparing {
			task.Status = TaskStatusCancelled
			task.Error = "engine closed"
		}
	}
	e.mu.Unlock()

	// 取消引擎上下文，终止所有进行中的下载协程（taskCtx 派生自 e.ctx）
	e.cancel()

	// 等待所有下载协程真正退出，避免使用 time.Sleep 带来的竞态与不确定性
	e.wg.Wait()

	// Close all connection pools
	e.mu.Lock()
	for name, pool := range e.connPools {
		pool.Close()
		logger.Infof("Connection pool closed: %s", name)
	}

	// Clear connection pools
	e.connPools = make(map[string]*ConnectionPool)
	e.mu.Unlock()

	logger.Info("Engine closed")
	return nil
}

// generateTaskID generates a unique task ID using timestamp and random string
func generateTaskID() string {
	timestamp := time.Now().UnixNano()
	random := make([]byte, 8)
	for i := range random {
		random[i] = byte(65 + rand.Intn(26)) // A-Z
	}
	return fmt.Sprintf("task-%d-%s", timestamp, random)
}

// getCurrentTime gets the current time
func getCurrentTime() time.Time {
	return time.Now()
}

// snapshotTask 深拷贝一个 Task，用于事件广播与持久化序列化。
// Task 的 Metadata/Progress/Statistics/Config/Chunks 均为指针或切片，
// 浅拷贝会让锁外操作与并发写协程共享同一批引用类型，导致 data race。
// 这里通过 JSON 往返得到完全独立的副本；ProtocolInstance 与 cancelFunc 已标记为 json:"-" 不参与序列化。
func snapshotTask(t *Task) *Task {
	if t == nil {
		return nil
	}
	data, err := json.Marshal(t)
	if err != nil {
		c := *t
		return &c
	}
	var copy Task
	if err := json.Unmarshal(data, &copy); err != nil {
		c := *t
		return &c
	}
	return &copy
}

// parseDuration parses a duration string to time.Duration
func parseDuration(durationStr string) time.Duration {
	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		logger.Warnf("Invalid duration format: %s, using default 1s", durationStr)
		return time.Second
	}
	return duration
}

// copyFile copies a file from src to dst, used for cross-device file moves
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer sourceFile.Close()

	// Create destination directory if it doesn't exist
	dstDir := filepath.Dir(dst)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	destFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destFile.Close()

	// Copy file content
	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return fmt.Errorf("failed to copy file content: %w", err)
	}

	// Sync to ensure all data is written to disk
	if err := destFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync destination file: %w", err)
	}

	return nil
}

// cleanupTempFilesAfterDownload cleans up temporary files after download completes
func cleanupTempFilesAfterDownload(savePath, filename string) {
	if savePath == "" || filename == "" {
		return
	}

	logger.Infof("Cleaning up temporary files after download: %s", filename)

	// 清理.part文件
	partFile := filepath.Join(savePath, filename+".part")
	if _, err := os.Stat(partFile); err == nil {
		os.Remove(partFile)
		logger.Infof("Removed part file after download: %s", partFile)
	}

	// 清理.parts文件（下载进度/位图文件）
	partsFile := filepath.Join(savePath, filename+".part.parts")
	if _, err := os.Stat(partsFile); err == nil {
		os.Remove(partsFile)
		logger.Infof("Removed parts file after download: %s", partsFile)
	}

	// 清理.downloading.json状态文件
	downloadingStateFile := filepath.Join(savePath, filename+".downloading.json")
	if _, err := os.Stat(downloadingStateFile); err == nil {
		os.Remove(downloadingStateFile)
		logger.Infof("Removed downloading state file after download: %s", downloadingStateFile)
	}

	// 清理.downloading.json.tmp临时文件
	downloadingStateTmpFile := downloadingStateFile + ".tmp"
	if _, err := os.Stat(downloadingStateTmpFile); err == nil {
		os.Remove(downloadingStateTmpFile)
		logger.Infof("Removed downloading state tmp file after download: %s", downloadingStateTmpFile)
	}

	// 清理.torrent文件
	torrentFile := filepath.Join(savePath, filename+".torrent")
	if _, err := os.Stat(torrentFile); err == nil {
		os.Remove(torrentFile)
		logger.Infof("Removed torrent file after download: %s", torrentFile)
	}

	// 清理以文件名为名的目录（如果目录为空）
	fileDir := filepath.Join(savePath, filename)
	if info, err := os.Stat(fileDir); err == nil && info.IsDir() {
		files, err := os.ReadDir(fileDir)
		if err == nil && len(files) == 0 {
			os.Remove(fileDir)
			logger.Infof("Removed empty file directory after download: %s", fileDir)
		}
	}
}
