package event

import (
	"go-download-server/internal/logger"
	"sync"
)

// EventType defines the type of event
type EventType string

// Event types
const (
	EventTaskCreated   EventType = "task.created"
	EventTaskStarted   EventType = "task.started"
	EventTaskProgress  EventType = "task.progress"
	EventTaskPaused    EventType = "task.paused"
	EventTaskCompleted EventType = "task.completed"
	EventTaskFailed    EventType = "task.failed"
	EventTaskCancelled EventType = "task.cancelled"
	EventResourceAlert EventType = "resource.alert"
	EventProtocolError EventType = "protocol.error"
	EventConfigUpdated EventType = "config.updated"
)

// Event defines an event with type and data
type Event struct {
	Type EventType   `json:"type"`
	Data interface{} `json:"data"`
}

// EventHandler defines a function that handles events
type EventHandler func(event Event)

// EventBus handles event publishing and subscription
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[EventType][]EventHandler
}

// GlobalEventBus is the global event bus instance
var GlobalEventBus *EventBus

// Init initializes the event bus
func Init() {
	GlobalEventBus = &EventBus{
		subscribers: make(map[EventType][]EventHandler),
	}
	logger.Info("Event bus initialized")
}

// Subscribe subscribes to an event type
func Subscribe(eventType EventType, handler EventHandler) {
	GlobalEventBus.mu.Lock()
	defer GlobalEventBus.mu.Unlock()

	GlobalEventBus.subscribers[eventType] = append(GlobalEventBus.subscribers[eventType], handler)
	logger.Debugf("Subscriber added for event type: %s", eventType)
}

// Unsubscribe unsubscribes from an event type
func Unsubscribe(eventType EventType, handler EventHandler) {
	GlobalEventBus.mu.Lock()
	defer GlobalEventBus.mu.Unlock()

	handlers := GlobalEventBus.subscribers[eventType]
	for i, h := range handlers {
		if &h == &handler {
			GlobalEventBus.subscribers[eventType] = append(handlers[:i], handlers[i+1:]...)
			logger.Debugf("Subscriber removed for event type: %s", eventType)
			return
		}
	}
}

// Publish publishes an event to all subscribers
func Publish(event Event) {
	GlobalEventBus.mu.RLock()
	handlers := GlobalEventBus.subscribers[event.Type]
	GlobalEventBus.mu.RUnlock()

	logger.Debugf("Publishing event: %s", event.Type)
	for _, handler := range handlers {
		// Execute handlers in goroutines to avoid blocking
		go handler(event)
	}
}

// GetSubscriberCount returns the number of subscribers for an event type
func GetSubscriberCount(eventType EventType) int {
	GlobalEventBus.mu.RLock()
	defer GlobalEventBus.mu.RUnlock()

	return len(GlobalEventBus.subscribers[eventType])
}
