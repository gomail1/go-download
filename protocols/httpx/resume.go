package httpx

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-download-server/internal/core"
	"go-download-server/internal/logger"
)

// ChunkState 表示单个下载块的状态
type ChunkState struct {
	ID         string `json:"id"`
	Offset     int64  `json:"offset"`
	Size       int64  `json:"size"`
	Downloaded int64  `json:"downloaded"`
	Status     string `json:"status"` // pending, downloading, completed, failed
}

// DownloadState 表示整个下载任务的状态
type DownloadState struct {
	TaskID     string       `json:"task_id"`
	URL        string       `json:"url"`
	TotalSize  int64        `json:"total_size"`
	Filename   string       `json:"filename"`
	Chunks     []ChunkState `json:"chunks"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
	Checksum   string       `json:"checksum,omitempty"`   // 预期的校验值
	ChecksumType string     `json:"checksum_type,omitempty"` // md5, sha256
}

// getStateFilePath 获取状态文件路径
func getStateFilePath(destPath string) string {
	return destPath + ".downloading.json"
}

// loadDownloadState 加载下载状态
func loadDownloadState(destPath string) (*DownloadState, error) {
	statePath := getStateFilePath(destPath)
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // 没有状态文件，是新下载
		}
		return nil, err
	}

	var state DownloadState
	if err := json.Unmarshal(data, &state); err != nil {
		logger.Warnf("下载状态文件损坏，将重新下载: %v", err)
		return nil, nil
	}

	logger.Infof("加载下载状态: 任务=%s, 已完成块=%d/%d", state.TaskID, countCompletedChunks(state.Chunks), len(state.Chunks))
	return &state, nil
}

// saveDownloadState 保存下载状态
func saveDownloadState(destPath string, state *DownloadState) error {
	state.UpdatedAt = time.Now()
	statePath := getStateFilePath(destPath)

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	// 先写入临时文件，再重命名，避免写入过程中崩溃导致文件损坏
	tmpPath := statePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, statePath)
}

// deleteDownloadState 删除下载状态文件
func deleteDownloadState(destPath string) {
	statePath := getStateFilePath(destPath)
	os.Remove(statePath)
	os.Remove(statePath + ".tmp")
}

// countCompletedChunks 统计已完成的块数
func countCompletedChunks(chunks []ChunkState) int {
	count := 0
	for _, c := range chunks {
		if c.Status == "completed" {
			count++
		}
	}
	return count
}

// initDownloadState 初始化下载状态
func initDownloadState(task *core.Task, totalSize int64, maxThreads int) *DownloadState {
	// 计算块大小
	chunkSize := totalSize / int64(maxThreads)
	remaining := totalSize % int64(maxThreads)

	var chunks []ChunkState
	var offset int64
	for i := 0; i < maxThreads; i++ {
		size := chunkSize
		if i == maxThreads-1 {
			size += remaining
		}
		chunks = append(chunks, ChunkState{
			ID:         fmt.Sprintf("chunk-%d", i),
			Offset:     offset,
			Size:       size,
			Downloaded: 0,
			Status:     "pending",
		})
		offset += size
	}

	// 从URL或元数据中提取校验信息
	checksum, checksumType := extractChecksum(task)

	return &DownloadState{
		TaskID:       task.ID,
		URL:          task.URL,
		TotalSize:    totalSize,
		Filename:     task.Metadata.Filename,
		Chunks:       chunks,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Checksum:     checksum,
		ChecksumType: checksumType,
	}
}

// extractChecksum 从URL或元数据中提取校验信息
func extractChecksum(task *core.Task) (string, string) {
	// 检查URL中是否包含校验信息（如 #md5=xxx 或 ?checksum=md5:xxx）
	url := task.URL

	// 检查URL fragment
	if idx := strings.Index(url, "#"); idx != -1 {
		fragment := url[idx+1:]
		if strings.HasPrefix(fragment, "md5=") {
			return strings.TrimPrefix(fragment, "md5="), "md5"
		}
		if strings.HasPrefix(fragment, "sha256=") {
			return strings.TrimPrefix(fragment, "sha256="), "sha256"
		}
	}

	// 检查URL query参数
	if idx := strings.Index(url, "?"); idx != -1 {
		query := url[idx+1:]
		params := strings.Split(query, "&")
		for _, p := range params {
			kv := strings.SplitN(p, "=", 2)
			if len(kv) == 2 {
				if kv[0] == "md5" {
					return kv[1], "md5"
				}
				if kv[0] == "sha256" {
					return kv[1], "sha256"
				}
				if kv[0] == "checksum" {
					// 格式: md5:xxx 或 sha256:xxx
					parts := strings.SplitN(kv[1], ":", 2)
					if len(parts) == 2 {
						return parts[1], parts[0]
					}
				}
			}
		}
	}

	// 检查元数据中的校验信息
	if task.Metadata != nil && task.Metadata.Checksum != "" {
		return task.Metadata.Checksum, task.Metadata.ChecksumType
	}

	return "", ""
}

// verifyFileChecksum 校验文件完整性
func verifyFileChecksum(destPath string, expectedChecksum string, checksumType string) error {
	if expectedChecksum == "" {
		logger.Infof("未提供校验值，跳过完整性校验")
		return nil
	}

	logger.Infof("开始校验文件完整性: 类型=%s, 预期值=%s", checksumType, expectedChecksum)

	file, err := os.Open(destPath)
	if err != nil {
		return fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	var actualChecksum string
	switch strings.ToLower(checksumType) {
	case "md5":
		h := md5.New()
		if _, err := io.Copy(h, file); err != nil {
			return fmt.Errorf("计算MD5失败: %w", err)
		}
		actualChecksum = hex.EncodeToString(h.Sum(nil))
	case "sha256":
		h := sha256.New()
		if _, err := io.Copy(h, file); err != nil {
			return fmt.Errorf("计算SHA256失败: %w", err)
		}
		actualChecksum = hex.EncodeToString(h.Sum(nil))
	default:
		logger.Warnf("不支持的校验类型: %s，跳过校验", checksumType)
		return nil
	}

	// 比较校验值（不区分大小写）
	if !strings.EqualFold(actualChecksum, expectedChecksum) {
		return fmt.Errorf("文件完整性校验失败: 预期=%s, 实际=%s", expectedChecksum, actualChecksum)
	}

	logger.Infof("文件完整性校验通过: %s", actualChecksum)
	return nil
}

// ensureDirExists 确保目录存在
func ensureDirExists(path string) error {
	dir := filepath.Dir(path)
	return os.MkdirAll(dir, 0755)
}
