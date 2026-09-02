package core

import (
	"context"
)

// Protocol defines the interface that all protocols must implement
type Protocol interface {
	// Basic methods
	CanHandle(url string) bool
	GetMetadata(ctx context.Context, url string) (*Metadata, error)

	// Download control
	Download(ctx context.Context, task *Task, progress chan<- Progress) error
	Pause() error
	Resume() error
	Cancel() error

	// Status query
	GetStatus() Status
	GetStatistics() Statistics

	// Configuration
	ApplyConfig(config ProtocolConfig) error
	GetCapabilities() Capabilities

	// Resource management
	SetResourceController(rc *ResourceController)
	SetConnectionPool(pool *ConnectionPool)
}

// Status defines the status of a protocol instance
type Status struct {
	IsRunning bool   `json:"is_running"`
	IsPaused  bool   `json:"is_paused"`
	Error     string `json:"error,omitempty"`
	Message   string `json:"message,omitempty"`
}

// ProtocolFactory defines a factory for creating protocol instances
type ProtocolFactory func() Protocol

// ProtocolManager manages protocol instances and factories
type ProtocolManager interface {
	RegisterProtocol(name string, factory ProtocolFactory)
	UnregisterProtocol(name string)
	GetProtocol(name string) (Protocol, error)
	FindProtocol(url string) (Protocol, string, error)
	GetAllProtocols() []string
}
