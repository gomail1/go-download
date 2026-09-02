package core

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go-download-server/internal/config"
	"go-download-server/internal/event"
	"go-download-server/internal/logger"
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
}

// NewQuadEngine creates a new QuadEngine instance
func NewQuadEngine(protocolMgr ProtocolManager) *QuadEngine {
	// Initialize random seed
	rand.Seed(time.Now().UnixNano())

	// Create default data directory - 去掉 .quadfetch 前缀，直接使用 tasks 目录
	dataDir := "tasks"
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

	// Auto-start unfinished tasks after engine is created
	for _, task := range tasks {
		switch task.Status {
		case TaskStatusDownloading:
			// 自动重新启动下载中的任务
			go func(t *Task) {
				// 创建新的上下文，避免主上下文被取消
				taskCtx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if err := e.StartTask(taskCtx, t.ID); err != nil {
					logger.Errorf("Failed to restart task %s: %v", t.ID, err)
					// 更新任务状态为失败
					e.mu.Lock()
					t.Status = TaskStatusFailed
					e.mu.Unlock()
					// 保存任务状态
					e.persistenceMgr.SaveTask(t)
				}
			}(task)
		case TaskStatusWaiting:
			// 自动启动等待中的任务
			go func(t *Task) {
				// 创建新的上下文，避免主上下文被取消
				taskCtx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if err := e.StartTask(taskCtx, t.ID); err != nil {
					logger.Errorf("Failed to start task %s: %v", t.ID, err)
					// 更新任务状态为失败
					e.mu.Lock()
					t.Status = TaskStatusFailed
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
		Data: task,
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

	// Create a new context for this task
	taskCtx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	task.cancelFunc = cancel
	e.mu.Unlock()

	// Update task status to preparing
	e.mu.Lock()
	task.Status = TaskStatusPreparing
	now := time.Now()
	task.StartedAt = &now
	taskCopy := *task // Create a copy for persistence
	e.mu.Unlock()

	// Save task to disk
	err := e.persistenceMgr.SaveTask(&taskCopy)
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
			taskCopy = *task
			e.mu.Unlock()

			// Save updated task
			err = e.persistenceMgr.SaveTask(&taskCopy)
			if err != nil {
				logger.Errorf("Failed to save task: %v", err)
			}

			return err
		}

		e.mu.Lock()
		task.Metadata = metadata
		taskCopy = *task
		e.mu.Unlock()

		// Save updated task
		err = e.persistenceMgr.SaveTask(&taskCopy)
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
	taskCopy = *task
	e.mu.Unlock()

	// Save task to disk
	err = e.persistenceMgr.SaveTask(&taskCopy)
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
		taskCopy = *task
		e.mu.Unlock()
		// Save task to disk
		err = e.persistenceMgr.SaveTask(&taskCopy)
		// Publish task failed event
		event.Publish(event.Event{
			Type: event.EventTaskCompleted,
			Data: task,
		})
		return err
	}

	// Start download in a goroutine
	go func() {
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
					e.mu.Unlock()
					// Publish progress event
					event.Publish(event.Event{
						Type: event.EventTaskProgress,
						Data: task,
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
					if err := os.Rename(originalFilePath, pendingFilePath); err != nil {
						logger.Errorf("Failed to move file to pending directory: %v", err)
					} else {
						logger.Infof("File moved to pending directory for review: %s", pendingFilePath)
					}
				} else {
					// File is already in pending directory, no need to move
					logger.Infof("File is already in pending directory for review: %s", pendingFilePath)
				}
			}
		}

		// Save final task status
		taskCopy := *task
		e.mu.Unlock()
		err = e.persistenceMgr.SaveTask(&taskCopy)
		// Publish task completed/failed event
		event.Publish(event.Event{
			Type: event.EventTaskCompleted,
			Data: task,
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

	if task.Status != TaskStatusDownloading {
		e.mu.Unlock()
		return errors.New("task is not downloading")
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
	taskCopy := *task
	e.mu.Unlock()

	// Save task to disk
	err := e.persistenceMgr.SaveTask(&taskCopy)
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
	taskCopy := *task
	e.mu.Unlock()

	// Save task to disk
	err := e.persistenceMgr.SaveTask(&taskCopy)
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

	taskCopy := *task
	e.mu.Unlock()

	// Save task to disk
	err := e.persistenceMgr.SaveTask(&taskCopy)
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

	// Clean up protocol-specific resources (especially for BT)
	if task.Protocol == "bittorrent" || task.Protocol == "bt" {
		// For BT tasks, we need to clean up the torrent from the shared client
		// This ensures the task doesn't reappear after restart
		// The actual cleanup happens in the protocol implementation
		logger.Infof("Cleaning up BT task resources: %s", id)
	}

	// Remove task from memory
	taskCopy := *task // Create a copy for cleanup outside the lock
	delete(e.tasks, id)
	e.statistics.TotalTasks--
	logger.Infof("Removed task from memory: %s", id)
	e.mu.Unlock()

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

	// First set isRunning to false and cancel context
	e.mu.Lock()
	e.isRunning = false
	e.cancel()
	e.mu.Unlock()

	// Wait a short time for goroutines to finish
	time.Sleep(100 * time.Millisecond)

	// Cancel all running tasks
	e.mu.Lock()
	for _, task := range e.tasks {
		if task.Status == TaskStatusDownloading {
			task.Status = TaskStatusCancelled
			task.Error = "engine closed"
		}
	}

	// Close all connection pools
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

// parseDuration parses a duration string to time.Duration
func parseDuration(durationStr string) time.Duration {
	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		logger.Warnf("Invalid duration format: %s, using default 1s", durationStr)
		return time.Second
	}
	return duration
}
