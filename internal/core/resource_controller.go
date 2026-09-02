package core

import (
	"fmt"
	"sync"
	"time"
)

// ResourceType defines the type of resource

type ResourceType string

const (
	ResourceTypeNetwork ResourceType = "network"
	ResourceTypeFile    ResourceType = "file"
	ResourceTypeMemory  ResourceType = "memory"
	ResourceTypeCPU     ResourceType = "cpu"
	ResourceTypeDiskIO  ResourceType = "disk_io"
)

// ResourceLimit defines the limits for a resource

type ResourceLimit struct {
	Max      int64        `json:"max"`
	Current  int64        `json:"current"`
	Type     ResourceType `json:"type"`
	Protocol string       `json:"protocol,omitempty"`
}

// ResourceController manages resource allocation and limits

type ResourceController struct {
	limits         map[ResourceType]*ResourceLimit
	protocolLimits map[string]map[ResourceType]*ResourceLimit
	mu             sync.RWMutex
	config         ResourceConfig
}

// ResourceConfig defines the configuration for resource management

type ResourceConfig struct {
	Global struct {
		MaxConnections int
		MaxFileHandles int
		MaxMemoryMB    int
	}
	Protocol struct {
		HTTP struct {
			MaxConnections int
			MaxFileHandles int
		}
		BT struct {
			MaxConnections int
			MaxFileHandles int
		}
	}
}

// NewResourceController creates a new resource controller

func NewResourceController(config ResourceConfig) *ResourceController {
	return &ResourceController{
		limits:         make(map[ResourceType]*ResourceLimit),
		protocolLimits: make(map[string]map[ResourceType]*ResourceLimit),
		config:         config,
	}
}

// Allocate allocates a resource

func (rc *ResourceController) Allocate(resourceType ResourceType, size int64, protocol string) error {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// Check global limit
	globalLimit, exists := rc.limits[resourceType]
	if exists {
		if globalLimit.Current+size > globalLimit.Max {
			return ErrResourceExhausted
		}
	}

	// Check protocol-specific limit
	if protocol != "" {
		if _, exists := rc.protocolLimits[protocol]; !exists {
			rc.protocolLimits[protocol] = make(map[ResourceType]*ResourceLimit)
		}
		protoLimit, exists := rc.protocolLimits[protocol][resourceType]
		if exists {
			if protoLimit.Current+size > protoLimit.Max {
				return ErrResourceExhausted
			}
		}
	}

	// Allocate global resource
	if !exists {
		rc.limits[resourceType] = &ResourceLimit{
			Max:     int64(rc.getGlobalMax(resourceType)),
			Current: 0,
			Type:    resourceType,
		}
		globalLimit = rc.limits[resourceType]
	}
	globalLimit.Current += size

	// Allocate protocol-specific resource
	if protocol != "" {
		protoLimitMap := rc.protocolLimits[protocol]
		if _, exists := protoLimitMap[resourceType]; !exists {
			protoLimitMap[resourceType] = &ResourceLimit{
				Max:      int64(rc.getProtocolMax(resourceType, protocol)),
				Current:  0,
				Type:     resourceType,
				Protocol: protocol,
			}
		}
		protoLimitMap[resourceType].Current += size
	}

	return nil
}

// Release releases a resource

func (rc *ResourceController) Release(resourceType ResourceType, size int64, protocol string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// Release global resource
	if globalLimit, exists := rc.limits[resourceType]; exists {
		globalLimit.Current -= size
		if globalLimit.Current < 0 {
			globalLimit.Current = 0
		}
	}

	// Release protocol-specific resource
	if protocol != "" {
		if protoLimitMap, exists := rc.protocolLimits[protocol]; exists {
			if protoLimit, exists := protoLimitMap[resourceType]; exists {
				protoLimit.Current -= size
				if protoLimit.Current < 0 {
					protoLimit.Current = 0
				}
			}
		}
	}
}

// GetUsage gets the current resource usage

func (rc *ResourceController) GetUsage() map[ResourceType]*ResourceLimit {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	// Create a copy
	usage := make(map[ResourceType]*ResourceLimit)
	for k, v := range rc.limits {
		usage[k] = &ResourceLimit{
			Max:     v.Max,
			Current: v.Current,
			Type:    v.Type,
		}
	}

	return usage
}

// WaitForResource waits until the resource is available

func (rc *ResourceController) WaitForResource(resourceType ResourceType, size int64, protocol string, timeout time.Duration) error {
	start := time.Now()

	for {
		err := rc.Allocate(resourceType, size, protocol)
		if err == nil {
			return nil
		}

		if time.Since(start) > timeout {
			return ErrResourceTimeout
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// getGlobalMax gets the global max limit for a resource type

func (rc *ResourceController) getGlobalMax(resourceType ResourceType) int {
	switch resourceType {
	case ResourceTypeNetwork:
		return rc.config.Global.MaxConnections
	case ResourceTypeFile:
		return rc.config.Global.MaxFileHandles
	case ResourceTypeMemory:
		return rc.config.Global.MaxMemoryMB
	default:
		return 1000 // Default limit
	}
}

// getProtocolMax gets the protocol-specific max limit for a resource type

func (rc *ResourceController) getProtocolMax(resourceType ResourceType, protocol string) int {
	switch protocol {
	case "http":
		switch resourceType {
		case ResourceTypeNetwork:
			return rc.config.Protocol.HTTP.MaxConnections
		case ResourceTypeFile:
			return rc.config.Protocol.HTTP.MaxFileHandles
		default:
			return 100 // Default per-protocol limit
		}
	case "bittorrent":
		switch resourceType {
		case ResourceTypeNetwork:
			return rc.config.Protocol.BT.MaxConnections
		case ResourceTypeFile:
			return rc.config.Protocol.BT.MaxFileHandles
		default:
			return 100 // Default per-protocol limit
		}
	default:
		return 100 // Default per-protocol limit
	}
}

// ConnectionPool manages a pool of connections

type ConnectionPool struct {
	connections chan *Connection
	mu          sync.Mutex
	config      ConnectionPoolConfig
	protocol    string
	done        chan struct{} // Used to signal goroutines to exit
}

// Connection represents a network connection

type Connection struct {
	ID        string      `json:"id"`
	Protocol  string      `json:"protocol"`
	CreatedAt time.Time   `json:"created_at"`
	LastUsed  time.Time   `json:"last_used"`
	IsActive  bool        `json:"is_active"`
	Data      interface{} `json:"data"` // Protocol-specific connection data
}

// ConnectionPoolConfig defines the configuration for connection pooling

type ConnectionPoolConfig struct {
	MaxConnections int
	MaxIdleTime    time.Duration
	MaxLifetime    time.Duration
}

// NewConnectionPool creates a new connection pool

func NewConnectionPool(protocol string, config ConnectionPoolConfig) *ConnectionPool {
	return &ConnectionPool{
		connections: make(chan *Connection, config.MaxConnections),
		config:      config,
		protocol:    protocol,
		done:        make(chan struct{}),
	}
}

// Get gets a connection from the pool, or creates a new one if needed

func (cp *ConnectionPool) Get() (*Connection, error) {
	select {
	case conn := <-cp.connections:
		// Check if connection is expired
		if time.Since(conn.CreatedAt) > cp.config.MaxLifetime {
			return cp.createNewConnection()
		}
		// Update last used time
		conn.LastUsed = time.Now()
		conn.IsActive = true
		return conn, nil
	default:
		// Create new connection
		return cp.createNewConnection()
	}
}

// Put returns a connection to the pool

func (cp *ConnectionPool) Put(conn *Connection) {
	conn.IsActive = false
	conn.LastUsed = time.Now()

	// Check if connection should be discarded
	if time.Since(conn.CreatedAt) > cp.config.MaxLifetime {
		return
	}

	// Try to return to pool, discard if full
	select {
	case cp.connections <- conn:
		// Successfully returned to pool
	default:
		// Pool is full, discard connection
	}
}

// Close closes all connections in the pool

func (cp *ConnectionPool) Close() {
	// Signal all goroutines to exit
	close(cp.done)

	// Wait a short time for goroutines to finish
	time.Sleep(100 * time.Millisecond)

	// Close and drain the connections channel
	close(cp.connections)
	for conn := range cp.connections {
		// Close the actual connection (protocol-specific)
		// This would need to be implemented by the protocol
		_ = conn // Silence unused variable warning
	}
}

// createNewConnection creates a new connection

func (cp *ConnectionPool) createNewConnection() (*Connection, error) {
	conn := &Connection{
		ID:        generateConnectionID(),
		Protocol:  cp.protocol,
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
		IsActive:  true,
	}

	return conn, nil
}

// generateConnectionID generates a unique connection ID

func generateConnectionID() string {
	// Simple implementation, similar to generateTaskID
	timestamp := time.Now().UnixNano()
	random := make([]byte, 6)
	for i := range random {
		random[i] = byte(65 + time.Now().UnixNano()%26)
	}
	return fmt.Sprintf("conn-%d-%s", timestamp, random)
}

// CleanupIdleConnections removes idle connections from the pool

func (cp *ConnectionPool) CleanupIdleConnections() {
	now := time.Now()
	var activeConnections []*Connection

	// Collect all connections while holding the lock
	cp.mu.Lock()

	// Create a temporary slice to hold all connections
	allConnections := make([]*Connection, 0, len(cp.connections))

	// Use a label to break out of the for loop
collectLoop:
	for {
		select {
		case conn := <-cp.connections:
			allConnections = append(allConnections, conn)
		default:
			break collectLoop // Break out of the for loop
		}
	}

	// Release the lock before processing connections
	cp.mu.Unlock()

	// Filter active connections
	for _, conn := range allConnections {
		if now.Sub(conn.LastUsed) < cp.config.MaxIdleTime {
			activeConnections = append(activeConnections, conn)
		}
	}

	// Return active connections to the pool
	for _, conn := range activeConnections {
		// Try to return to pool, discard if full
		select {
		case cp.connections <- conn:
			// Successfully returned to pool
		default:
			// Pool is full, discard connection
		}
	}
}

// StartCleanupTicker starts a ticker to periodically cleanup idle connections

func (cp *ConnectionPool) StartCleanupTicker() {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		for {
			select {
			case <-ticker.C:
				cp.CleanupIdleConnections()
			case <-cp.done:
				ticker.Stop()
				return
			}
		}
	}()
}
