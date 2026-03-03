package module

import (
	"context"
	"net/http"
)

// BasicModule is a lightweight module implementation.
type BasicModule struct {
	ModuleName string
	OnEnabled  bool
	RegisterFn func(mux *http.ServeMux)
	StartFn    func(ctx context.Context) error
	StopFn     func(ctx context.Context) error
}

func (m BasicModule) Name() string {
	return m.ModuleName
}

func (m BasicModule) Enabled() bool {
	return m.OnEnabled
}

func (m BasicModule) RegisterRoutes(mux *http.ServeMux) {
	if m.RegisterFn != nil {
		m.RegisterFn(mux)
	}
}

func (m BasicModule) Start(ctx context.Context) error {
	if m.StartFn != nil {
		return m.StartFn(ctx)
	}
	return nil
}

func (m BasicModule) Stop(ctx context.Context) error {
	if m.StopFn != nil {
		return m.StopFn(ctx)
	}
	return nil
}
