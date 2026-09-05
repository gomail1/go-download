package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"go-download-server/internal/core"
	"go-download-server/internal/logger"
	"go-download-server/utils"
)

// GetTasks handles GET /api/tasks
func (s *Server) GetTasks(c *gin.Context) {
	tasks := s.coreEngine.ListTasks()
	c.JSON(http.StatusOK, tasks)
}

// CreateTask handles POST /api/tasks
func (s *Server) CreateTask(c *gin.Context) {
	var req core.AddTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	task, err := s.coreEngine.AddTask(c.Request.Context(), &req)
	if err != nil {
		logger.Errorf("创建任务失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, task)
}

// GetTask handles GET /api/tasks/:id
func (s *Server) GetTask(c *gin.Context) {
	id := c.Param("id")
	task, err := s.coreEngine.GetTask(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
		return
	}

	c.JSON(http.StatusOK, task)
}

// PauseTask handles PUT /api/tasks/:id/pause
func (s *Server) PauseTask(c *gin.Context) {
	id := c.Param("id")
	err := s.coreEngine.PauseTask(id)
	if err != nil {
		c.JSON(apiErrorStatus(err), gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Task paused successfully"})
}

// ResumeTask handles PUT /api/tasks/:id/resume
func (s *Server) ResumeTask(c *gin.Context) {
	id := c.Param("id")
	err := s.coreEngine.ResumeTask(c.Request.Context(), id)
	if err != nil {
		c.JSON(apiErrorStatus(err), gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Task resumed successfully"})
}

// DeleteTask handles DELETE /api/tasks/:id
func (s *Server) DeleteTask(c *gin.Context) {
	id := c.Param("id")
	err := s.coreEngine.RemoveTask(id)
	if err != nil {
		c.JSON(apiErrorStatus(err), gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Task deleted successfully"})
}

// apiErrorStatus 将引擎错误映射为合适的 HTTP 状态码：
// 任务不存在 -> 404，状态冲突（如非下载中暂停）-> 409，其余 -> 500。
func apiErrorStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "not found"):
		return http.StatusNotFound
	case strings.Contains(msg, "not downloading"), strings.Contains(msg, "not paused"),
		strings.Contains(msg, "already"):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// GetStatistics handles GET /api/stats
//
// 已废弃：此路由被 main.go 的 net/http /api/stats（handlers.StatsHandler）
// 精确注册抢占，从未生效。真实统计接口见 handlers.StatsHandler。
// 删除路由时保留方法本身（coreEngine.GetStatistics 属引擎能力，可后续
// 在无冲突路径上重新挂载，如 /api/tasks/statistics 需避免与 /tasks/:id 段冲突）。
// func (s *Server) GetStatistics(c *gin.Context) {
// 	stats := s.coreEngine.GetStatistics()
// 	c.JSON(http.StatusOK, stats)
// }

// UploadTorrentFile handles POST /api/tasks/upload
func (s *Server) UploadTorrentFile(c *gin.Context) {
	// Get all uploaded files
	form, err := c.MultipartForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse form data"})
		return
	}

	// Get all uploaded torrent files
	files := form.File["torrent"]
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No torrent files provided"})
		return
	}

	// Process all files
	var results []*core.Task
	// 集中存放上传的种子文件，避免路径遍历且便于管理
	torrentDir := filepath.Join("tmp", "torrents")
	if err := os.MkdirAll(torrentDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create torrent temp dir"})
		return
	}
	for _, file := range files {
		// 清洗文件名，杜绝 ../ 等目录穿越与非法字符
		safeName := utils.SanitizeRemoteFilename(file.Filename)
		tmpPath := filepath.Join(torrentDir, safeName)
		if err := c.SaveUploadedFile(file, tmpPath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save torrent file: " + safeName})
			return
		}

		// Create a task using the local file path as URL
		req := &core.AddTaskRequest{
			URL: tmpPath,
		}

		task, err := s.coreEngine.AddTask(c.Request.Context(), req)
		if err != nil {
			logger.Errorf("创建任务失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create task for file " + safeName + ": " + err.Error()})
			return
		}

		results = append(results, task)
	}

	c.JSON(http.StatusCreated, results)
}
