package core

import (
	"errors"
	"go-download-server/internal/logger"
	"sync"
)

// SimpleProtocolManager implements the ProtocolManager interface
type SimpleProtocolManager struct {
	mu        sync.RWMutex
	factories map[string]ProtocolFactory
}

// NewSimpleProtocolManager creates a new SimpleProtocolManager instance
func NewSimpleProtocolManager() *SimpleProtocolManager {
	return &SimpleProtocolManager{
		factories: make(map[string]ProtocolFactory),
	}
}

// RegisterProtocol registers a protocol factory
func (pm *SimpleProtocolManager) RegisterProtocol(name string, factory ProtocolFactory) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.factories[name] = factory
	logger.Infof("Protocol registered: %s", name)
}

// UnregisterProtocol unregisters a protocol factory
func (pm *SimpleProtocolManager) UnregisterProtocol(name string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	delete(pm.factories, name)
	logger.Infof("Protocol unregistered: %s", name)
}

// GetProtocol gets a protocol instance by name
func (pm *SimpleProtocolManager) GetProtocol(name string) (Protocol, error) {
	pm.mu.RLock()
	factory, ok := pm.factories[name]
	pm.mu.RUnlock()

	if !ok {
		return nil, errors.New("protocol not found: " + name)
	}

	return factory(), nil
}

// FindProtocol finds a suitable protocol for the given URL
func (pm *SimpleProtocolManager) FindProtocol(url string) (Protocol, string, error) {
	pm.mu.RLock()
	factories := make(map[string]ProtocolFactory)
	for name, factory := range pm.factories {
		factories[name] = factory
	}
	pm.mu.RUnlock()

	// 优先检查 HTTP/HTTPS（文件直链最常见），其余协议按注册顺序回退匹配
	priorityOrder := []string{"http", "https"}

	// 先检查优先顺序中的协议
	for _, name := range priorityOrder {
		if factory, exists := factories[name]; exists {
			protocol := factory()
			if protocol.CanHandle(url) {
				return protocol, name, nil
			}
		}
	}

	// 再检查其他协议（如果有的话）
	for name, factory := range factories {
		// 跳过已经检查过的协议
		alreadyChecked := false
		for _, checkedName := range priorityOrder {
			if name == checkedName {
				alreadyChecked = true
				break
			}
		}
		if alreadyChecked {
			continue
		}
		protocol := factory()
		if protocol.CanHandle(url) {
			return protocol, name, nil
		}
	}

	return nil, "", errors.New("no suitable protocol found for url: " + url)
}

// GetAllProtocols returns the names of all registered protocols
func (pm *SimpleProtocolManager) GetAllProtocols() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	names := make([]string, 0, len(pm.factories))
	for name := range pm.factories {
		names = append(names, name)
	}
	return names
}
