package core

import (
	"context"
	"fmt"
	"time"
)

// Error definitions
var (
	ErrResourceExhausted = fmt.Errorf("resource exhausted")
	ErrResourceTimeout   = fmt.Errorf("resource timeout")
)

// TaskStatus defines the status of a task
type TaskStatus string

// Task statuses
const (
	TaskStatusWaiting     TaskStatus = "waiting"
	TaskStatusPreparing   TaskStatus = "preparing"
	TaskStatusDownloading TaskStatus = "downloading"
	TaskStatusPaused      TaskStatus = "paused"
	TaskStatusCompleted   TaskStatus = "completed"
	TaskStatusFailed      TaskStatus = "failed"
	TaskStatusCancelled   TaskStatus = "cancelled"
)

// Task defines a download task
type Task struct {
	ID               string                 `json:"id"`
	URL              string                 `json:"url"`
	Protocol         string                 `json:"protocol"`
	Status           TaskStatus             `json:"status"`
	Metadata         *Metadata              `json:"metadata"`
	Progress         *Progress              `json:"progress"`
	Statistics       *Statistics            `json:"statistics"`
	Config           *TaskConfig            `json:"config"`
	Chunks           []*Chunk               `json:"chunks"`
	CreatedAt        time.Time              `json:"created_at"`
	StartedAt        *time.Time             `json:"started_at"`
	CompletedAt      *time.Time             `json:"completed_at"`
	Error            string                 `json:"error,omitempty"`
	Traits           []string               `json:"traits"`
	ProtocolSpecific map[string]interface{} `json:"protocol_specific,omitempty"`
	ProtocolInstance Protocol               `json:"-"` // Not serialized, per-task protocol instance
	cancelFunc       context.CancelFunc     `json:"-"` // Not serialized, cancel function for task context
}

// Metadata defines the metadata of a download
type Metadata struct {
	Filename         string                 `json:"filename"`
	Size             int64                  `json:"size"`
	MimeType         string                 `json:"mime_type"`
	Checksum         string                 `json:"checksum,omitempty"`
	ChecksumType     string                 `json:"checksum_type,omitempty"`
	Headers          map[string]string      `json:"headers,omitempty"`
	ProtocolSpecific map[string]interface{} `json:"protocol_specific,omitempty"`
}

// Progress defines the progress of a download
type Progress struct {
	Percentage   float64       `json:"percentage"`
	Downloaded   int64         `json:"downloaded"`
	TotalSize    int64         `json:"total_size"`
	Speed        int64         `json:"speed"` // bytes per second
	ETA          time.Duration `json:"eta"`
	CurrentChunk int           `json:"current_chunk"`
	TotalChunks  int           `json:"total_chunks"`
	Status       string        `json:"status"`       // 下载状态描述，如"正在查找公共Tracker"
	ActivePeers  int           `json:"active_peers"` // 当前活跃节点数
	TotalPeers   int           `json:"total_peers"`  // 总节点数
}

// Statistics defines the statistics of a download
type Statistics struct {
	Downloaded      int64         `json:"downloaded"`
	Uploaded        int64         `json:"uploaded"`
	Connections     int           `json:"connections"`
	ChunksCompleted int           `json:"chunks_completed"`
	ChunksTotal     int           `json:"chunks_total"`
	RetryCount      int           `json:"retry_count"`
	StartTime       time.Time     `json:"start_time"`
	EndTime         *time.Time    `json:"end_time,omitempty"`
	Duration        time.Duration `json:"duration"`
}

// TaskConfig defines the configuration for a task
type TaskConfig struct {
	SavePath         string                 `json:"save_path"`
	Overwrite        bool                   `json:"overwrite"`
	MaxRetries       int                    `json:"max_retries"`
	RetryDelay       time.Duration          `json:"retry_delay"`
	VerifyHash       bool                   `json:"verify_hash"`
	ResumeEnabled    bool                   `json:"resume_enabled"`
	MaxThreads       int                    `json:"max_threads"`
	ChunkStrategy    string                 `json:"chunk_strategy"`
	PreAllocate      bool                   `json:"pre_allocate"`
	SpeedLimit       int64                  `json:"speed_limit"` // bytes per second, 0 means unlimited
	ProtocolSpecific map[string]interface{} `json:"protocol_specific,omitempty"`
}

// Chunk defines a download chunk
type Chunk struct {
	ID         string      `json:"id"`
	TaskID     string      `json:"task_id"`
	Offset     int64       `json:"offset"`
	Size       int64       `json:"size"`
	Downloaded int64       `json:"downloaded"`
	Status     ChunkStatus `json:"status"`
	Checksum   string      `json:"checksum,omitempty"`
	Retries    int         `json:"retries"`
}

// ChunkStatus defines the status of a chunk
type ChunkStatus string

// Chunk statuses
const (
	ChunkStatusPending     ChunkStatus = "pending"
	ChunkStatusDownloading ChunkStatus = "downloading"
	ChunkStatusCompleted   ChunkStatus = "completed"
	ChunkStatusFailed      ChunkStatus = "failed"
	ChunkStatusCancelled   ChunkStatus = "cancelled"
)

// ProtocolConfig defines the configuration for a protocol
type ProtocolConfig struct {
	Name    string                 `json:"name"`
	Options map[string]interface{} `json:"options"`
}

// Capabilities defines the capabilities of a protocol
type Capabilities struct {
	CanResume           bool     `json:"can_resume"`
	CanVerify           bool     `json:"can_verify"`
	SupportsChunks      bool     `json:"supports_chunks"`
	SupportsP2P         bool     `json:"supports_p2p"`
	SupportedURLSchemes []string `json:"supported_url_schemes"`
}

// TraitStats defines statistics for a trait
type TraitStats struct {
	CowStable         int `json:"cow_stable"`
	OrangeBoost       int `json:"orange_boost"`
	WandererConnected int `json:"wanderer_connected"`
	SparkleAgile      int `json:"sparkle_agile"`
}
