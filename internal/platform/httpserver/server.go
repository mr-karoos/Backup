package httpserver

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Server wraps standard library http.Server with configured timeouts and graceful shutdown support.
type Server struct {
	httpServer *http.Server
}

// New creates and configures a new Server instance.
func New(addr string, handler http.Handler) *Server {
	return &Server{
		httpServer: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadTimeout:       15 * time.Second,
			ReadHeaderTimeout: 5 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
		},
	}
}

// Start runs the HTTP server. It returns http.ErrServerClosed upon normal graceful shutdown.
func (s *Server) Start() error {
	err := s.httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown gracefully shuts down the server without interrupting active connections.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
