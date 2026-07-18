package service

import (
	"sync"
)

type LogService struct {
	mu          sync.RWMutex
	subscribers map[string]chan string
	buffer      []string
	bufferSize  int
}

func NewLogService(bufferSize int) *LogService {
	return &LogService{
		subscribers: make(map[string]chan string),
		buffer:      make([]string, 0, bufferSize),
		bufferSize:  bufferSize,
	}
}

func (s *LogService) Write(p []byte) (n int, err error) {
	line := string(p)

	s.mu.Lock()
	// Add to buffer
	if len(s.buffer) >= s.bufferSize {
		s.buffer = s.buffer[1:]
	}
	s.buffer = append(s.buffer, line)

	// Broadcast to subscribers
	for _, ch := range s.subscribers {
		select {
		case ch <- line:
		default:
			// Subscriber too slow, skip to avoid blocking the whole app
		}
	}
	s.mu.Unlock()

	return len(p), nil
}

func (s *LogService) Subscribe(id string) (chan string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan string, 1000)
	s.subscribers[id] = ch

	// Return copy of buffer
	bufCopy := make([]string, len(s.buffer))
	copy(bufCopy, s.buffer)

	return ch, bufCopy
}

func (s *LogService) Unsubscribe(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ch, ok := s.subscribers[id]; ok {
		close(ch)
		delete(s.subscribers, id)
	}
}
