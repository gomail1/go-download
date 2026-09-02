package core

import (
	"fmt"
	"math"
	"time"
)

// ChunkStrategyType defines the type of chunking strategy
type ChunkStrategyType string

const (
	// ChunkStrategyFixed means fixed size chunks
	ChunkStrategyFixed ChunkStrategyType = "fixed"
	// ChunkStrategyDynamic means dynamically sized chunks based on file size
	ChunkStrategyDynamic ChunkStrategyType = "dynamic"
	// ChunkStrategyAdaptive means adaptive chunks that can change during download
	ChunkStrategyAdaptive ChunkStrategyType = "adaptive"
)

// ChunkStrategy defines the configuration for chunking

type ChunkStrategy struct {
	// Strategy type
	Type ChunkStrategyType `json:"type"`

	// Fixed chunk settings
	FixedSize int64 `json:"fixed_size"`

	// Dynamic chunk settings
	MinChunkSize int64 `json:"min_chunk_size"`
	MaxChunkSize int64 `json:"max_chunk_size"`
	MaxChunks    int   `json:"max_chunks"`

	// Adaptive chunk settings
	TargetSpeed    int64         `json:"target_speed"`
	AdjustInterval time.Duration `json:"adjust_interval"`

	// Protocol specific settings
	ProtocolSpecific map[string]interface{} `json:"protocol_specific"`
}

// ChunkManager defines the interface for managing chunks
type ChunkManager interface {
	// Split splits a task into chunks based on the strategy
	Split(task *Task) ([]*Chunk, error)

	// Merge merges chunks into a single file
	Merge(chunks []*Chunk, outputPath string) error

	// Verify verifies the integrity of the merged file
	Verify(task *Task) (bool, error)

	// AdjustChunks dynamically adjusts chunks based on download progress
	AdjustChunks(task *Task, progress map[string]*Progress) error
}

// DefaultChunkManager implements the ChunkManager interface with default strategies
type DefaultChunkManager struct {
	config ChunkStrategyConfig
}

// ChunkStrategyConfig defines the configuration for chunk management
type ChunkStrategyConfig struct {
	DefaultStrategy ChunkStrategyType `json:"default_strategy"`
	MinChunkSize    int64             `json:"min_chunk_size"`
	MaxChunkSize    int64             `json:"max_chunk_size"`
	MaxChunks       int               `json:"max_chunks"`
}

// NewDefaultChunkManager creates a new DefaultChunkManager instance
func NewDefaultChunkManager(config ChunkStrategyConfig) *DefaultChunkManager {
	return &DefaultChunkManager{
		config: config,
	}
}

// Split splits a task into chunks based on the configured strategy
func (cm *DefaultChunkManager) Split(task *Task) ([]*Chunk, error) {
	if task.Metadata == nil {
		return nil, fmt.Errorf("task metadata is nil")
	}

	fileSize := task.Metadata.Size

	// Handle unknown file size
	if fileSize <= 0 {
		// For unknown size, use a single chunk
		chunks := []*Chunk{
			{
				ID:     "chunk-1",
				TaskID: task.ID,
				Offset: 0,
				Size:   -1, // Unknown size
				Status: ChunkStatusPending,
			},
		}
		return chunks, nil
	}

	// Get or create chunk strategy for this task
	strategy := task.Config.ChunkStrategy
	var chunkStrategy ChunkStrategyType
	if strategy == "" {
		chunkStrategy = cm.config.DefaultStrategy
	} else {
		chunkStrategy = ChunkStrategyType(strategy)
	}

	// Determine number of chunks based on strategy
	var chunks []*Chunk
	var err error

	switch chunkStrategy {
	case ChunkStrategyFixed:
		chunks, err = cm.splitFixed(fileSize, task)
	case ChunkStrategyDynamic:
		chunks, err = cm.splitDynamic(fileSize, task)
	case ChunkStrategyAdaptive:
		// Adaptive strategy starts with dynamic chunks
		chunks, err = cm.splitDynamic(fileSize, task)
	default:
		// Default to dynamic strategy
		chunks, err = cm.splitDynamic(fileSize, task)
	}

	if err != nil {
		return nil, err
	}

	// Set chunk IDs and task IDs
	for i, chunk := range chunks {
		chunk.ID = fmt.Sprintf("chunk-%d", i+1)
		chunk.TaskID = task.ID
		chunk.Status = ChunkStatusPending
	}

	return chunks, nil
}

// splitFixed splits a file into fixed size chunks
func (cm *DefaultChunkManager) splitFixed(fileSize int64, task *Task) ([]*Chunk, error) {
	// Determine chunk size
	chunkSize := cm.config.MinChunkSize
	if task.Config != nil && task.Config.ChunkStrategy == string(ChunkStrategyFixed) {
		// If fixed size is specified in task config, use it
		if fixedSize, ok := task.ProtocolSpecific["fixed_chunk_size"].(int64); ok && fixedSize > 0 {
			chunkSize = fixedSize
		}
	}

	// Calculate number of chunks
	numChunks := int(math.Ceil(float64(fileSize) / float64(chunkSize)))

	// Ensure we don't exceed max chunks
	if numChunks > cm.config.MaxChunks {
		numChunks = cm.config.MaxChunks
		chunkSize = int64(math.Ceil(float64(fileSize) / float64(numChunks)))
	}

	return cm.createChunks(fileSize, chunkSize, numChunks), nil
}

// splitDynamic splits a file into dynamically sized chunks
func (cm *DefaultChunkManager) splitDynamic(fileSize int64, task *Task) ([]*Chunk, error) {
	// Calculate number of threads
	maxThreads := task.Config.MaxThreads
	if maxThreads <= 0 {
		maxThreads = 8 // Default threads
	}

	// Calculate initial chunk size based on file size and threads
	baseChunkSize := fileSize / int64(maxThreads)

	// Apply min/max constraints
	chunkSize := baseChunkSize
	if chunkSize < cm.config.MinChunkSize {
		chunkSize = cm.config.MinChunkSize
	} else if chunkSize > cm.config.MaxChunkSize {
		chunkSize = cm.config.MaxChunkSize
	}

	// Calculate number of chunks
	numChunks := int(math.Ceil(float64(fileSize) / float64(chunkSize)))

	// Ensure we don't exceed max chunks
	if numChunks > cm.config.MaxChunks {
		numChunks = cm.config.MaxChunks
		chunkSize = int64(math.Ceil(float64(fileSize) / float64(numChunks)))
	}

	return cm.createChunks(fileSize, chunkSize, numChunks), nil
}

// createChunks creates chunks with the given parameters
func (cm *DefaultChunkManager) createChunks(fileSize, chunkSize int64, numChunks int) []*Chunk {
	chunks := make([]*Chunk, 0, numChunks)
	var offset int64

	for i := 0; i < numChunks; i++ {
		remaining := fileSize - offset
		currentChunkSize := chunkSize

		// Last chunk might be smaller
		if currentChunkSize > remaining {
			currentChunkSize = remaining
		}

		chunk := &Chunk{
			Offset: offset,
			Size:   currentChunkSize,
		}

		chunks = append(chunks, chunk)
		offset += currentChunkSize
	}

	return chunks
}

// Merge merges chunks into a single file
func (cm *DefaultChunkManager) Merge(chunks []*Chunk, outputPath string) error {
	// This is a placeholder implementation
	// In a real implementation, we would:
	// 1. Open the output file
	// 2. For each chunk:
	//    a. Open the chunk file
	//    b. Read chunk data
	//    c. Write to output file at correct offset
	//    d. Close chunk file
	// 3. Close output file
	return fmt.Errorf("merge not implemented yet")
}

// Verify verifies the integrity of the merged file
func (cm *DefaultChunkManager) Verify(task *Task) (bool, error) {
	// This is a placeholder implementation
	// In a real implementation, we would:
	// 1. Calculate the hash of the merged file
	// 2. Compare with the expected hash from metadata
	return false, fmt.Errorf("verify not implemented yet")
}

// AdjustChunks dynamically adjusts chunks based on download progress
func (cm *DefaultChunkManager) AdjustChunks(task *Task, progress map[string]*Progress) error {
	// This is a placeholder implementation for adaptive chunking
	// In a real implementation, we would:
	// 1. Analyze download progress for each chunk
	// 2. Identify slow or fast chunks
	// 3. Split slow chunks into smaller chunks
	// 4. Merge fast chunks into larger chunks
	// 5. Update the task's chunks
	return nil
}

// NewDefaultChunkStrategy creates a default chunk strategy
func NewDefaultChunkStrategy() ChunkStrategy {
	return ChunkStrategy{
		Type:           ChunkStrategyDynamic,
		FixedSize:      10 * 1024 * 1024, // 10MB
		MinChunkSize:   1 * 1024 * 1024,  // 1MB
		MaxChunkSize:   50 * 1024 * 1024, // 50MB
		MaxChunks:      100,
		TargetSpeed:    10 * 1024 * 1024, // 10MB/s
		AdjustInterval: 30 * time.Second,
	}
}
