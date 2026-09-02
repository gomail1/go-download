package core

import (
	"encoding/json"
	"os"
	"path/filepath"

	"go-download-server/internal/logger"
)

// PersistenceManager handles task persistence
type PersistenceManager struct {
	dataDir string
}

// NewPersistenceManager creates a new PersistenceManager instance
func NewPersistenceManager(dataDir string) *PersistenceManager {
	// Ensure data directory exists
	err := os.MkdirAll(dataDir, 0755)
	if err != nil {
		logger.Fatalf("Failed to create data directory: %v", err)
	}

	return &PersistenceManager{
		dataDir: dataDir,
	}
}

// SaveTask saves a task to disk
func (pm *PersistenceManager) SaveTask(task *Task) error {
	// Ensure data directory exists
	err := os.MkdirAll(pm.dataDir, 0755)
	if err != nil {
		return err
	}

	// Create task file path
	// 任务文件直接存储在 dataDir 下，而不是 dataDir/tasks 下
	taskPath := filepath.Join(pm.dataDir, task.ID+"_task.json")

	// Marshal task to JSON
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}

	// Write to file
	return os.WriteFile(taskPath, data, 0644)
}

// LoadTask loads a task from disk
func (pm *PersistenceManager) LoadTask(id string) (*Task, error) {
	// Create task file path
	// 任务文件直接存储在 dataDir 下，而不是 dataDir/tasks 下
	taskPath := filepath.Join(pm.dataDir, id+"_task.json")

	// Check if file exists
	if _, err := os.Stat(taskPath); os.IsNotExist(err) {
		return nil, err
	}

	// Read file
	data, err := os.ReadFile(taskPath)
	if err != nil {
		return nil, err
	}

	// Unmarshal JSON to task
	var task Task
	err = json.Unmarshal(data, &task)
	if err != nil {
		return nil, err
	}

	return &task, nil
}

// LoadAllTasks loads all tasks from disk
func (pm *PersistenceManager) LoadAllTasks() ([]*Task, error) {
	// Ensure data directory exists
	err := os.MkdirAll(pm.dataDir, 0755)
	if err != nil {
		return nil, err
	}

	// Get all task files
	// 任务文件直接存储在 dataDir 下，而不是 dataDir/tasks 下
	files, err := filepath.Glob(filepath.Join(pm.dataDir, "*_task.json"))
	if err != nil {
		return nil, err
	}

	// Load each task
	var tasks []*Task
	for _, file := range files {
		// Read file
		data, err := os.ReadFile(file)
		if err != nil {
			logger.Warnf("Failed to read task file %s: %v", file, err)
			continue
		}

		// Unmarshal JSON to task
		var task Task
		err = json.Unmarshal(data, &task)
		if err != nil {
			logger.Warnf("Failed to unmarshal task file %s: %v", file, err)
			continue
		}

		// Add to tasks list
		tasks = append(tasks, &task)
	}

	return tasks, nil
}

// DeleteTask deletes a task from disk
func (pm *PersistenceManager) DeleteTask(id string) error {
	// Create task file path
	// 任务文件直接存储在 dataDir 下，而不是 dataDir/tasks 下
	taskPath := filepath.Join(pm.dataDir, id+"_task.json")

	// Delete file if exists
	if _, err := os.Stat(taskPath); err == nil {
		if err := os.Remove(taskPath); err != nil {
			return err
		}
	}

	// 同时删除对应的进度文件
	progressPath := filepath.Join(pm.dataDir, id+"_progress.json")
	if _, err := os.Stat(progressPath); err == nil {
		os.Remove(progressPath)
	}

	return nil
}

// SaveProgress saves task progress to disk
func (pm *PersistenceManager) SaveProgress(taskID string, progress *Progress) error {
	// Create progress file path
	// 任务文件直接存储在 dataDir 下，而不是 dataDir/tasks 下
	progressPath := filepath.Join(pm.dataDir, taskID+"_progress.json")

	// Marshal progress to JSON
	data, err := json.MarshalIndent(progress, "", "  ")
	if err != nil {
		return err
	}

	// Write to file
	return os.WriteFile(progressPath, data, 0644)
}

// LoadProgress loads task progress from disk
func (pm *PersistenceManager) LoadProgress(taskID string) (*Progress, error) {
	// Create progress file path
	// 任务文件直接存储在 dataDir 下，而不是 dataDir/tasks 下
	progressPath := filepath.Join(pm.dataDir, taskID+"_progress.json")

	// Check if file exists
	if _, err := os.Stat(progressPath); os.IsNotExist(err) {
		return nil, err
	}

	// Read file
	data, err := os.ReadFile(progressPath)
	if err != nil {
		return nil, err
	}

	// Unmarshal JSON to progress
	var progress Progress
	err = json.Unmarshal(data, &progress)
	if err != nil {
		return nil, err
	}

	return &progress, nil
}
