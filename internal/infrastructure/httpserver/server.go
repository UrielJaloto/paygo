package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

const idleTimeout = 60 * time.Second

type Server struct {
	server          *http.Server
	shutdownTimeout time.Duration
}

func New(addr string, handler http.Handler, readHeaderTimeout, shutdownTimeout time.Duration) *Server {
	return &Server{
		server: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: readHeaderTimeout,
			IdleTimeout:       idleTimeout,
		},
		shutdownTimeout: shutdownTimeout,
	}
}

func Listen(addr string) (net.Listener, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", addr, err)
	}
	return listener, nil
}

func (s *Server) Run(ctx context.Context, listener net.Listener) error {
	served := make(chan error, 1)
	go func() {
		served <- s.server.Serve(listener)
	}()
	select {
	case err := <-served:
		return fmt.Errorf("serving http: %w", err)
	case <-ctx.Done():
		return s.shutdown(ctx)
	}
}

func (s *Server) shutdown(ctx context.Context) error {
	shutdownContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.shutdownTimeout)
	defer cancel()
	if err := s.server.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shutting down http server: %w", errors.Join(err, s.server.Close()))
	}
	return nil
}
