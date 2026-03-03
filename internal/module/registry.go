package module

import (
	"context"
	"fmt"
	"net/http"
	"sort"
)

type Module interface {
	Name() string
	Enabled() bool
	RegisterRoutes(mux *http.ServeMux)
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

type Registry struct {
	modules map[string]Module
}

func NewRegistry() *Registry {
	return &Registry{modules: make(map[string]Module)}
}

func (r *Registry) Register(m Module) error {
	name := m.Name()
	if name == "" {
		return fmt.Errorf("module name is required")
	}
	if _, exists := r.modules[name]; exists {
		return fmt.Errorf("module %s already registered", name)
	}
	r.modules[name] = m
	return nil
}

func (r *Registry) RegisterRoutes(mux *http.ServeMux) {
	names := r.Names()
	for _, name := range names {
		m := r.modules[name]
		if m.Enabled() {
			m.RegisterRoutes(mux)
		}
	}
}

func (r *Registry) StartAll(ctx context.Context) error {
	for _, name := range r.Names() {
		m := r.modules[name]
		if !m.Enabled() {
			continue
		}
		if err := m.Start(ctx); err != nil {
			return fmt.Errorf("start module %s: %w", name, err)
		}
	}
	return nil
}

func (r *Registry) StopAll(ctx context.Context) error {
	names := r.Names()
	for i := len(names) - 1; i >= 0; i-- {
		m := r.modules[names[i]]
		if !m.Enabled() {
			continue
		}
		if err := m.Stop(ctx); err != nil {
			return fmt.Errorf("stop module %s: %w", names[i], err)
		}
	}
	return nil
}

func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.modules))
	for name := range r.modules {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *Registry) Status() map[string]bool {
	out := make(map[string]bool, len(r.modules))
	for name, m := range r.modules {
		out[name] = m.Enabled()
	}
	return out
}
