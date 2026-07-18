package integration

import (
	"sync"
)

type MasterKeyProvider interface {
	GetMasterKey(userID string, integrationID string) ([]byte, error)
}

type Registry struct {
	providers map[string]Provider
	mu        sync.RWMutex
}

func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.ServiceType()] = p
}

func (r *Registry) Get(serviceType string) Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.providers[serviceType]
}
