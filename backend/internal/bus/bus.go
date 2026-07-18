package bus

import (
	"context"
	"sync"
)

// Event represents a generic event in the system
type Event struct {
	Topic   string
	Payload interface{}
}

// Handler is a function that processes an event
type Handler func(ctx context.Context, event Event)

// Bus handles event distribution
type Bus struct {
	mu          sync.RWMutex
	subscribers map[string][]Handler
}

// NewBus creates a new universal event bus
func NewBus() *Bus {
	return &Bus{
		subscribers: make(map[string][]Handler),
	}
}

// Subscribe adds a handler for a specific topic
func (b *Bus) Subscribe(topic string, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[topic] = append(b.subscribers[topic], handler)
}

// Publish broadcasts an event to all subscribers of a topic asynchronously
func (b *Bus) Publish(ctx context.Context, topic string, payload interface{}) {
	b.mu.RLock()
	handlers, ok := b.subscribers[topic]
	b.mu.RUnlock()

	if !ok {
		return
	}

	event := Event{Topic: topic, Payload: payload}
	for _, handler := range handlers {
		h := handler
		go h(ctx, event)
	}
}
