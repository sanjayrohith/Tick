package worker

import (
	"fmt"
	"sync"

	"github.com/sanjayrohith/tick/internal/domain"
)

// Registry maps handler names to the Handler that executes them.
//
// Safe for concurrent use: Register is expected at startup from a single
// goroutine, but Lookup runs on every claim from any number of worker
// goroutines.
type Registry struct {
	mu       sync.Mutex
	handlers map[string]Handler
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]Handler)}
}

// Register adds h under name. It panics on a duplicate name: two handlers
// silently sharing one name is a deploy-ordering mistake that must fail
// loudly at startup, not intermittently at claim time depending on which one
// won the map write.
func (r *Registry) Register(name string, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[name]; exists {
		panic(fmt.Sprintf("worker: handler %q already registered", name))
	}
	r.handlers[name] = h
}

// Lookup returns the handler registered under name, or a wrapped
// domain.ErrUnknownHandler when none is.
func (r *Registry) Lookup(name string) (Handler, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.handlers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", domain.ErrUnknownHandler, name)
	}
	return h, nil
}
