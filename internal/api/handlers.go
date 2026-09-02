package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go-download-server/internal/core"
	"go-download-server/internal/logger"
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Task paused successfully"})
}

// ResumeTask handles PUT /api/tasks/:id/resume
func (s *Server) ResumeTask(c *gin.Context) {
	id := c.Param("id")
	err := s.coreEngine.ResumeTask(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Task resumed successfully"})
}

// DeleteTask handles DELETE /api/tasks/:id
func (s *Server) DeleteTask(c *gin.Context) {
	id := c.Param("id")
	err := s.coreEngine.RemoveTask(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Task deleted successfully"})
}

// GetStatistics handles GET /api/stats
func (s *Server) GetStatistics(c *gin.Context) {
	stats := s.coreEngine.GetStatistics()
	c.JSON(http.StatusOK, stats)
}

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
	for _, file := range files {
		// Save the file to a temporary location
		tmpPath := "./tmp/" + file.Filename
		if err := c.SaveUploadedFile(file, tmpPath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save torrent file: " + file.Filename})
			return
		}

		// Create a task using the local file path as URL
		req := &core.AddTaskRequest{
			URL: tmpPath,
		}

		task, err := s.coreEngine.AddTask(c.Request.Context(), req)
		if err != nil {
			logger.Errorf("创建任务失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create task for file " + file.Filename + ": " + err.Error()})
			return
		}

		results = append(results, task)
	}

	c.JSON(http.StatusCreated, results)
}
