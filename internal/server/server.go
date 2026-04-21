// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

// Package server provides the HTTP server for the GraphQL API service.
// It wires up chi routing, gqlgen handler configuration, request-level
// timeout context, and graceful shutdown orchestration.
package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/go-chi/chi/v5"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"go.uber.org/zap"

	"github.com/michaelwang123/mountainKing/internal/config"
	"github.com/michaelwang123/mountainKing/internal/datasource"
	"github.com/michaelwang123/mountainKing/internal/graphql/resolver"
)

// ShutdownFunc is a function that performs cleanup during graceful shutdown.
// TracingProvider.Shutdown and similar teardown hooks implement this signature.
type ShutdownFunc func(ctx context.Context) error

// Server holds all dependencies for the HTTP server.
type Server struct {
	serverConfig  config.ServerConfig
	graphqlConfig config.GraphQLConfig
	shutdownCfg   config.ShutdownConfig
	dsManager     *datasource.DataSourceManager
	resolver      *resolver.Resolver
	logger        *zap.Logger
	schema        graphql.ExecutableSchema

	// Optional dependencies — nil-safe during shutdown.
	tracingShutdown ShutdownFunc
	metricsFlush    ShutdownFunc

	httpServer *http.Server
}

// NewServer creates a new Server with the given dependencies.
func NewServer(
	serverCfg config.ServerConfig,
	graphqlCfg config.GraphQLConfig,
	shutdownCfg config.ShutdownConfig,
	dsManager *datasource.DataSourceManager,
	res *resolver.Resolver,
	schema graphql.ExecutableSchema,
	logger *zap.Logger,
) *Server {
	return &Server{
		serverConfig:  serverCfg,
		graphqlConfig: graphqlCfg,
		shutdownCfg:   shutdownCfg,
		dsManager:     dsManager,
		resolver:      res,
		schema:        schema,
		logger:        logger,
	}
}

// SetTracingShutdown sets the tracing provider shutdown function.
func (s *Server) SetTracingShutdown(fn ShutdownFunc) {
	s.tracingShutdown = fn
}

// SetMetricsFlush sets the metrics flush function.
func (s *Server) SetMetricsFlush(fn ShutdownFunc) {
	s.metricsFlush = fn
}

// SetupRoutes creates and returns a chi router with all routes registered.
func (s *Server) SetupRoutes() *chi.Mux {
	r := chi.NewRouter()

	gqlHandler := s.NewGraphQLHandler()

	// POST /graphql — always enabled.
	r.Post("/graphql", s.WithRequestTimeout(gqlHandler))

	// GET /graphql — controlled by allow_get_queries config.
	if s.serverConfig.AllowGetQueries {
		r.Get("/graphql", s.WithRequestTimeout(gqlHandler))
	}

	// GET /playground — only in development mode.
	if s.serverConfig.Mode == "development" {
		r.Get("/playground", playground.Handler("GraphQL Playground", "/graphql"))
	}

	// Health, ready, and metrics endpoints are registered by the caller (main.go)
	// with real implementations. No placeholders needed here.

	return r
}

// NewGraphQLHandler creates a gqlgen handler with complexity limit, depth limit,
// and introspection control from config. Exported for use by main.go.
func (s *Server) NewGraphQLHandler() http.Handler {
	srv := handler.New(s.schema)

	// Transports.
	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.POST{})
	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.MultipartForm{})

	// Automatic persisted queries (APQ) — use in-memory LRU cache.
	srv.SetQueryCache(lru.New[*ast.QueryDocument](1000))

	// Complexity limit.
	if s.graphqlConfig.MaxQueryComplexity > 0 {
		srv.Use(extension.FixedComplexityLimit(s.graphqlConfig.MaxQueryComplexity))
	}

	// Query depth limit — enforced via AroundOperations middleware.
	if s.graphqlConfig.MaxQueryDepth > 0 {
		maxDepth := s.graphqlConfig.MaxQueryDepth
		srv.AroundOperations(func(ctx context.Context, next graphql.OperationHandler) graphql.ResponseHandler {
			oc := graphql.GetOperationContext(ctx)
			if oc != nil && oc.Doc != nil {
				depth := calcSelectionSetDepth(oc.Doc.Operations)
				if depth > maxDepth {
					return graphql.OneShot(&graphql.Response{
						Errors: gqlerror.List{{
							Message: fmt.Sprintf("query depth %d exceeds maximum allowed depth %d", depth, maxDepth),
							Extensions: map[string]interface{}{
								"code":           "VALIDATION_DEPTH_EXCEEDED",
								"classification": "VALIDATION",
							},
						}},
					})
				}
			}
			return next(ctx)
		})
	}

	// Introspection: handler.New does not enable introspection by default.
	// Add the extension only when the config explicitly enables it.
	if s.graphqlConfig.IntrospectionEnabled {
		srv.Use(extension.Introspection{})
	}

	return srv
}

// WithRequestTimeout wraps an http.Handler with a context.WithTimeout derived
// from the configured request_timeout. Exported for use by main.go.
func (s *Server) WithRequestTimeout(next http.Handler) http.HandlerFunc {
	timeout := s.serverConfig.RequestTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// PlaygroundHandler returns an http.HandlerFunc that serves the GraphQL Playground UI.
// Exported for use by main.go when registering routes directly.
func (s *Server) PlaygroundHandler() http.HandlerFunc {
	return playground.Handler("GraphQL Playground", "/graphql")
}

// placeholderHandler returns a simple handler that responds with 200 OK and
// a JSON body. These will be replaced by real implementations in later tasks.
func placeholderHandler(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok","endpoint":"%s"}`, name)
	}
}

// Start begins listening on the configured port. It starts the HTTP server
// in a goroutine and returns immediately.
func (s *Server) Start() error {
	router := s.SetupRoutes()

	s.httpServer = &http.Server{
		Addr:              fmt.Sprintf(":%d", s.serverConfig.Port),
		Handler:           router,
		ReadHeaderTimeout: s.serverConfig.ReadHeaderTimeout,
		ReadTimeout:       s.serverConfig.ReadTimeout,
		WriteTimeout:      s.serverConfig.WriteTimeout,
		IdleTimeout:       s.serverConfig.IdleTimeout,
	}

	s.logger.Info("starting HTTP server",
		zap.Int("port", s.serverConfig.Port),
		zap.String("mode", s.serverConfig.Mode),
	)

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Fatal("HTTP server error", zap.Error(err))
		}
	}()

	return nil
}

// WaitForShutdown blocks until SIGTERM or SIGINT is received, then performs
// graceful shutdown in the following order:
//  1. Stop accepting new connections (http.Server.Shutdown)
//  2. Wait for in-flight requests up to max_wait_time (default 30s)
//  3. TracingProvider.Shutdown with independent 5s timeout
//  4. Flush Metrics
//  5. DataSourceManager.CloseAll
//  6. Logger.Sync
func (s *Server) WaitForShutdown() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	sig := <-quit

	s.logger.Info("received shutdown signal", zap.String("signal", sig.String()))
	s.GracefulShutdown()
}

// GracefulShutdown performs the ordered shutdown sequence. It can be called
// directly for testing or from WaitForShutdown.
func (s *Server) GracefulShutdown() {
	// 1. Stop accepting new connections and wait for in-flight requests.
	maxWait := s.shutdownCfg.MaxWaitTime
	if maxWait <= 0 {
		maxWait = 30 * time.Second
	}

	if s.httpServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), maxWait)
		defer shutdownCancel()

		s.logger.Info("shutting down HTTP server", zap.Duration("max_wait", maxWait))
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("HTTP server shutdown error", zap.Error(err))
		}
	}

	// 2. TracingProvider.Shutdown with independent 5s timeout.
	if s.tracingShutdown != nil {
		tracingCtx, tracingCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer tracingCancel()

		s.logger.Info("shutting down tracing provider")
		if err := s.tracingShutdown(tracingCtx); err != nil {
			s.logger.Error("tracing provider shutdown error", zap.Error(err))
		}
	}

	// 3. Flush Metrics.
	if s.metricsFlush != nil {
		metricsCtx, metricsCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer metricsCancel()

		s.logger.Info("flushing metrics")
		if err := s.metricsFlush(metricsCtx); err != nil {
			s.logger.Error("metrics flush error", zap.Error(err))
		}
	}

	// 4. Close all data source connections.
	if s.dsManager != nil {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer closeCancel()

		s.logger.Info("closing all data source connections")
		if err := s.dsManager.CloseAll(closeCtx); err != nil {
			s.logger.Error("datasource close error", zap.Error(err))
		}
	}

	// 5. Sync logger buffers.
	s.logger.Info("shutdown complete")
	_ = s.logger.Sync()
}

// calcSelectionSetDepth computes the maximum nesting depth across all
// operations in a parsed GraphQL document.
func calcSelectionSetDepth(ops ast.OperationList) int {
	maxDepth := 0
	for _, op := range ops {
		d := selectionSetDepth(op.SelectionSet)
		if d > maxDepth {
			maxDepth = d
		}
	}
	return maxDepth
}

// selectionSetDepth recursively computes the depth of a selection set.
func selectionSetDepth(ss ast.SelectionSet) int {
	if len(ss) == 0 {
		return 0
	}
	maxChild := 0
	for _, sel := range ss {
		var childDepth int
		switch s := sel.(type) {
		case *ast.Field:
			childDepth = selectionSetDepth(s.SelectionSet)
		case *ast.InlineFragment:
			childDepth = selectionSetDepth(s.SelectionSet)
		case *ast.FragmentSpread:
			if s.Definition != nil {
				childDepth = selectionSetDepth(s.Definition.SelectionSet)
			}
		}
		if childDepth > maxChild {
			maxChild = childDepth
		}
	}
	return 1 + maxChild
}
