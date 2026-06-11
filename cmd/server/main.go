// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

// Package main is the entry point for the GraphQL Multi-DataSource API server.
// It orchestrates the full initialization chain: config -> logging -> tracing ->
// Redis -> adapters -> datasource manager -> cache -> rate limiter -> metrics ->
// health -> audit -> sanitizer -> middleware -> GraphQL schema -> HTTP server ->
// graceful shutdown.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/go-chi/chi/v5"
	clickhouseadapter "github.com/michaelwang123/mountainKing/internal/adapter/clickhouse"
	mockadapter "github.com/michaelwang123/mountainKing/internal/adapter/mock"
	"github.com/michaelwang123/mountainKing/internal/adapter/prometheus"
	"github.com/michaelwang123/mountainKing/internal/adapter/starrocks"
	"github.com/michaelwang123/mountainKing/internal/audit"
	"github.com/michaelwang123/mountainKing/internal/cache"
	"github.com/michaelwang123/mountainKing/internal/config"
	"github.com/michaelwang123/mountainKing/internal/datasource"
	"github.com/michaelwang123/mountainKing/internal/graphql/generated"
	"github.com/michaelwang123/mountainKing/internal/graphql/resolver"
	"github.com/michaelwang123/mountainKing/internal/health"
	"github.com/michaelwang123/mountainKing/internal/middleware"
	"github.com/michaelwang123/mountainKing/internal/observability"
	"github.com/michaelwang123/mountainKing/internal/ratelimit"
	redisclient "github.com/michaelwang123/mountainKing/internal/redis"
	"github.com/michaelwang123/mountainKing/internal/sanitize"
	"github.com/michaelwang123/mountainKing/internal/server"
	"github.com/michaelwang123/mountainKing/internal/template"
	"github.com/michaelwang123/mountainKing/pkg/retry"
	goredis "github.com/redis/go-redis/v9"
)

// version and buildTime are injected at build time via ldflags.
var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	configPath := flag.String("config", "", "path to config file")
	healthCheck := flag.Bool("health", false, "run health check and exit (for container HEALTHCHECK)")
	flag.Parse()

	// Health check mode: probe localhost and exit with 0/1.
	if *healthCheck {
		runHealthProbe()
		return
	}

	// Priority: CLI flag > GRAPHQL_CONFIG_PATH env var > default "config.yaml"
	cfgFile := "config.yaml"
	if *configPath != "" {
		cfgFile = *configPath
	} else if envPath := os.Getenv("GRAPHQL_CONFIG_PATH"); envPath != "" {
		cfgFile = envPath
	}
	// 1. Load config (Viper YAML + env vars).
	cfg, err := config.LoadConfig(cfgFile)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	if _, err := config.ValidateConfig(cfg); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	// 2. Init structured logging (zap).
	logger, err := observability.NewLogger(observability.LoggerConfig{
		Level:  cfg.Logging.Level,
		Format: cfg.Logging.Format,
	})
	if err != nil {
		log.Fatalf("failed to init logger: %v", err)
	}
	defer func() { _ = logger.Sync() }()
	logger.Info("starting GraphQL Multi-DataSource API server")

	// 3. Init TracingProvider (OTLP).
	tracingProvider, err := observability.InitTracing(cfg.Tracing)
	if err != nil {
		logger.Fatal("failed to init tracing", zap.Error(err))
	}

	// 4. Init shared Redis client (if needed for distributed rate limiting or Redis cache).
	var redisClient *goredis.Client
	if needsRedis(cfg) {
		addr := resolveRedisAddr(cfg)
		redisClient, err = redisclient.NewRedisClient(config.RedisConfig{
			Addr:     addr,
			Password: resolveRedisPassword(cfg),
			DB:       resolveRedisDB(cfg),
		})
		if err != nil {
			logger.Fatal("failed to create redis client", zap.Error(err))
		}
		hook := observability.NewRedisTracingHook(tracingProvider.Tracer(), addr)
		redisClient.AddHook(hook)

		pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if pingErr := redisclient.Ping(pingCtx, redisClient); pingErr != nil {
			logger.Warn("redis ping failed, features requiring Redis may degrade", zap.Error(pingErr))
		} else {
			logger.Info("redis client connected", zap.String("addr", addr))
		}
		pingCancel()
	}

	// 5. Register adapters (StarRocks, Prometheus) to AdapterRegistry.
	registry := datasource.NewAdapterRegistry()
	if err := registry.Register("starrocks", starrocks.Factory(logger.Logger)); err != nil {
		logger.Fatal("failed to register starrocks adapter", zap.Error(err))
	}
	if err := registry.Register("prometheus", prometheus.Factory(logger.Logger)); err != nil {
		logger.Fatal("failed to register prometheus adapter", zap.Error(err))
	}
	if err := registry.Register("mock", mockadapter.Factory()); err != nil {
		logger.Fatal("failed to register mock adapter", zap.Error(err))
	}
	if err := registry.Register("clickhouse", clickhouseadapter.Factory(logger.Logger)); err != nil {
		logger.Fatal("failed to register clickhouse adapter", zap.Error(err))
	}

	// 6. Init DataSourceManager.
	retryCfg := retry.Config{
		MaxRetries:    cfg.Retry.MaxRetries,
		RetryInterval: cfg.Retry.RetryInterval,
	}
	dsManager := datasource.NewDataSourceManager(registry, cfg.Datasources, retryCfg, logger.Logger)
	if err := dsManager.Init(context.Background()); err != nil {
		logger.Fatal("failed to init datasource manager", zap.Error(err))
	}

	// Development mode: warn if the expected mock datasource is not available.
	// The starrocks GraphQL resolver routes to "analytics_db" by default.
	if cfg.Server.Mode == "development" {
		if status := dsManager.Status("analytics_db"); status == nil {
			logger.Warn("development mode: no datasource named 'analytics_db' found; " +
				"GraphQL starrocks queries will fail. Check config.dev.yaml datasources section.")
		}
	}

	// 7. Init CacheLayer (memory or Redis backend).
	var cacheLayer *cache.CacheLayer
	if cfg.Cache.Enabled {
		var backend cache.Cache
		switch cfg.Cache.Backend {
		case "redis":
			if redisClient == nil {
				logger.Fatal("redis cache backend requires redis, but redis is not configured")
			}
			backend = cache.NewRedisCache(redisClient)
		default:
			mc, mcErr := cache.NewMemoryCache(cache.MemoryCacheConfig{
				MaxEntries:    cfg.Cache.Memory.MaxEntries,
				MaxMemorySize: cfg.Cache.Memory.MaxMemorySize,
			})
			if mcErr != nil {
				logger.Fatal("failed to create memory cache", zap.Error(mcErr))
			}
			backend = mc
		}
		ttlConfig := make(map[string]time.Duration)
		for dsName, dsCacheCfg := range cfg.Cache.PerDatasource {
			ttlConfig[dsName] = dsCacheCfg.TTL
		}
		// Merge template-level cache TTLs into TTLConfig before creating CacheLayer.
		// This must happen before NewCacheLayer to avoid data races on the ttlConfig map.
		if cfg.SQLTemplates.Enabled {
			for _, tmplCfg := range cfg.SQLTemplates.Templates {
				if tmplCfg.CacheTTL != nil {
					ttlConfig["template:"+tmplCfg.Name] = *tmplCfg.CacheTTL
				}
			}
		}
		cacheLayer = cache.NewCacheLayer(cache.CacheLayerConfig{
			Backend:    backend,
			TTLConfig:  ttlConfig,
			DefaultTTL: cfg.Cache.DefaultTTL,
			JitterPct:  cfg.Cache.TTLJitterPercent,
			EmptyTTL:   cfg.Cache.EmptyResultTTL,
			Logger:     logger.Logger,
		})
	}

	// 8. Init RateLimiter (local or distributed with fallback).
	var rateLimiter ratelimit.RateLimiter
	switch cfg.RateLimit.Mode {
	case "distributed":
		if redisClient == nil {
			logger.Fatal("distributed rate limiting requires redis")
		}
		dist := ratelimit.NewDistributedRateLimiter(redisClient, cfg.RateLimit.RequestsPerWindow, cfg.RateLimit.WindowSize)
		local := ratelimit.NewKeyedRateLimiter(cfg.RateLimit.RequestsPerWindow, cfg.RateLimit.WindowSize, 100000)
		rateLimiter = ratelimit.NewFallbackRateLimiter(dist, local, 30*time.Second, logger.Logger)
	default:
		rateLimiter = ratelimit.NewKeyedRateLimiter(cfg.RateLimit.RequestsPerWindow, cfg.RateLimit.WindowSize, 100000)
	}

	// 9. Init MetricsCollector.
	metricsCollector := observability.NewMetricsCollector(&observability.MetricsConfig{
		CustomLabels: cfg.Metrics.CustomLabels,
	})

	// 10. Init HealthChecker.
	healthChecker := health.NewHealthChecker(dsManager, version, buildTime)

	// 11. Init AuditLogger.
	auditLogger, err := audit.NewAuditLogger(cfg.Logging.Audit)
	if err != nil {
		logger.Fatal("failed to init audit logger", zap.Error(err))
	}
	defer func() { _ = auditLogger.Close() }()

	// 12. Init Sanitizer.
	sanitizer, err := sanitize.NewSanitizer(cfg.Sanitization)
	if err != nil {
		logger.Fatal("failed to init sanitizer", zap.Error(err))
	}
	_ = sanitizer

	// 13. Build authenticator and auth failure limiter.
	var authenticator middleware.Authenticator
	switch cfg.Auth.Method {
	case "jwt":
		authenticator, err = middleware.NewJWTAuthenticator(cfg.Auth.JWT, cfg.Auth.JWT.Algorithm)
		if err != nil {
			logger.Fatal("failed to init JWT authenticator", zap.Error(err))
		}
	case "apikey":
		authenticator, err = middleware.NewAPIKeyAuthenticator(cfg.Auth.APIKey)
		if err != nil {
			logger.Fatal("failed to init API Key authenticator", zap.Error(err))
		}
	default:
		logger.Info("no auth method configured, authentication disabled")
	}

	var authFailureLimiter *middleware.AuthFailureLimiter
	if cfg.AuthFailure.Enabled {
		authFailureLimiter, err = middleware.NewAuthFailureLimiter(cfg.AuthFailure, cfg.Auth.TrustedProxies)
		if err != nil {
			logger.Fatal("failed to init auth failure limiter", zap.Error(err))
		}
		defer authFailureLimiter.Stop()
	}

	// 14. Create GraphQL schema + resolvers.
	// --- Mutation initialization ---
	// Validate mutation config at startup (fail-fast on misconfiguration).
	if err := config.ValidateMutationsConfig(cfg); err != nil {
		logger.Fatal("invalid mutation configuration", zap.Error(err))
	}

	// Create the atomic MutationsConfig pointer (needed even when disabled for hot-reload toggle).
	var mutationConfigPtr atomic.Pointer[config.MutationsConfig]
	mutationsCfg := cfg.Mutations
	mutationConfigPtr.Store(&mutationsCfg)

	var mutationSQLBuilder *starrocks.MutationSQLBuilder
	var mutationValidator *starrocks.MutationValidator
	var writableTableValidator *starrocks.WritableTableValidator
	var mutationRateLimiter ratelimit.RateLimiter

	if cfg.Mutations.Enabled {
		// Find the datasource config for the mutation datasource.
		var mutationDSConfig config.DataSourceConfig
		for _, ds := range cfg.Datasources {
			if ds.Name == cfg.Mutations.DatasourceName {
				mutationDSConfig = ds
				break
			}
		}

		// Convert config.DataSourceConfig → datasource.DataSourceConfig for parser functions.
		dsCfg := datasource.DataSourceConfig{
			Name:       mutationDSConfig.Name,
			Type:       mutationDSConfig.Type,
			Enabled:    mutationDSConfig.Enabled,
			Connection: mutationDSConfig.Connection,
			Options:    mutationDSConfig.Options,
		}

		// Parse writable tables from datasource options.
		writableTables, wtErr := starrocks.ParseWritableTables(dsCfg)
		if wtErr != nil {
			logger.Fatal("failed to parse writable tables", zap.Error(wtErr))
		}

		// Parse allowed tables from datasource options.
		allowedTables, atErr := starrocks.ParseAllowedTables(dsCfg)
		if atErr != nil {
			logger.Fatal("failed to parse allowed tables for mutation datasource", zap.Error(atErr))
		}

		// Validate writable tables are a subset of allowed tables.
		if subErr := starrocks.ValidateWritableSubset(writableTables, allowedTables); subErr != nil {
			logger.Fatal("writable tables validation failed", zap.Error(subErr))
		}

		// Create mutation components.
		writableTableValidator = starrocks.NewWritableTableValidator(writableTables, allowedTables)
		mutationSQLBuilder = &starrocks.MutationSQLBuilder{}
		mutationValidator = starrocks.NewMutationValidator(cfg.Mutations.MaxBatchSize, cfg.Mutations.MaxSQLLength)

		// Create mutation-specific rate limiter (local mode, matching global pattern).
		mutationRateLimiter = ratelimit.NewKeyedRateLimiter(
			cfg.Mutations.RateLimit.RequestsPerWindow,
			cfg.Mutations.RateLimit.WindowSize,
			100000,
		)

		logger.Info("mutation support initialized",
			zap.String("datasource", cfg.Mutations.DatasourceName),
			zap.Int("max_batch_size", cfg.Mutations.MaxBatchSize),
			zap.Int("max_affected_rows", cfg.Mutations.MaxAffectedRows),
		)
	} else {
		logger.Info("mutations are disabled")
	}

	// Register hot-reload callback for mutations.enabled to atomically swap config.
	// This allows toggling mutations on/off without restart.
	hotReloader := config.NewHotReloader(cfgFile, logger.Logger)
	hotReloader.OnChange("mutations.enabled", func(key string, value any) {
		// Re-read the full mutations config on any hot-reload change.
		// In practice, the HotReloader re-reads the entire config file,
		// so we construct a new MutationsConfig from the updated values.
		enabled, _ := value.(bool)
		current := mutationConfigPtr.Load()
		newCfg := *current
		newCfg.Enabled = enabled
		mutationConfigPtr.Store(&newCfg)
		logger.Info("mutations.enabled hot-reloaded", zap.Bool("enabled", enabled))
	})
	if hrErr := hotReloader.Start(); hrErr != nil {
		logger.Warn("failed to start config hot-reloader, live config changes disabled", zap.Error(hrErr))
	}

	// Create TemplateEngine if sql_templates is enabled.
	var templateEngine *template.TemplateEngine
	var templateWatcher *template.TemplateWatcher
	if cfg.SQLTemplates.Enabled {
		srDS, dsErr := dsManager.Get(cfg.SQLTemplates.DatasourceName)
		if dsErr != nil {
			if cfg.Server.Mode == "development" {
				logger.Warn("sql_templates: configured datasource unavailable, template engine disabled",
					zap.String("datasource_name", cfg.SQLTemplates.DatasourceName),
					zap.Error(dsErr))
				// templateEngine remains nil — template queries will return "not available" error
			} else {
				logger.Fatal("sql_templates enabled but configured datasource not available",
					zap.String("datasource_name", cfg.SQLTemplates.DatasourceName),
					zap.Error(dsErr))
			}
		} else {
			srAdapter, ok := srDS.(*starrocks.Adapter)
			if !ok {
				if cfg.Server.Mode == "development" {
					logger.Warn("sql_templates requires a StarRocks datasource, but got type "+srDS.Type()+"; template engine disabled",
						zap.String("datasource_name", cfg.SQLTemplates.DatasourceName))
				} else {
					logger.Fatal("sql_templates requires a StarRocks datasource",
						zap.String("datasource_name", cfg.SQLTemplates.DatasourceName))
				}
			} else {
				templateMetrics := template.NewTemplateMetrics(metricsCollector.Registry(), metricsCollector.CustomLabels())

				templateEngine, err = template.NewTemplateEngine(template.TemplateEngineConfig{
					Config:         cfg.SQLTemplates,
					GraphQLCfg:     cfg.GraphQL,
					DatasourceName: srAdapter.Name(),
					Executor:       srAdapter,
					CacheLayer:     cacheLayer,
					Sanitizer:      sanitizer,
					AuditLogger:    auditLogger,
					Metrics:        templateMetrics,
					Tracer:         tracingProvider.Tracer(),
					Logger:         logger.Logger,
				})
				if err != nil {
					logger.Fatal("failed to init template engine", zap.Error(err))
				}
				logger.Info("template engine initialized",
					zap.String("datasource", srAdapter.Name()),
					zap.Int("template_count", len(cfg.SQLTemplates.Templates)))

				// Start TemplateWatcher for fsnotify-based hot reload.
				watchDirs := []string{cfg.SQLTemplates.BaseDir}
				sharedDir := cfg.SQLTemplates.SharedDir
				if sharedDir == "" {
					sharedDir = filepath.Join(cfg.SQLTemplates.BaseDir, "_shared")
				}
				if sharedDir != cfg.SQLTemplates.BaseDir {
					watchDirs = append(watchDirs, sharedDir)
				}
				var twErr error
				templateWatcher, twErr = template.NewTemplateWatcher(templateEngine, watchDirs, logger.Logger)
				if twErr != nil {
					logger.Warn("failed to create template watcher, fsnotify hot-reload disabled", zap.Error(twErr))
				} else {
					templateWatcher.Start()
					logger.Info("template watcher started", zap.Strings("watch_dirs", watchDirs))
				}
			}
		}
	}

	res := &resolver.Resolver{
		DSManager:              dsManager,
		GraphQLConfig:          cfg.GraphQL,
		TemplateEngine:         templateEngine,
		MutationSQLBuilder:     mutationSQLBuilder,
		MutationValidator:      mutationValidator,
		WritableTableValidator: writableTableValidator,
		MutationConfig:         &mutationConfigPtr,
		MutationRateLimiter:    mutationRateLimiter,
		AuditLogger:            auditLogger,
		MetricsCollector:       metricsCollector,
		Logger:                 logger.Logger,
	}
	if cacheLayer != nil {
		res.CacheClearer = cacheLayer
	}
	schema := generated.NewExecutableSchema(generated.Config{Resolvers: res})

	// 15. Build HTTP server with middleware chain.
	srv := server.NewServer(cfg.Server, cfg.GraphQL, cfg.Shutdown, dsManager, res, schema, logger.Logger)
	srv.SetTracingShutdown(tracingProvider.Shutdown)

	// Build a single chi router: middleware first, then routes.
	// Chi requires all middleware to be defined before any routes.
	router := chi.NewRouter()

	// Apply middleware chain: RequestID -> BodyLimit -> MaxConcurrent -> CORS -> CSRF -> Auth -> AuthFailureLimiter -> RateLimit -> Compression.
	router.Use(middleware.RequestID)
	router.Use(middleware.BodyLimit(cfg.Server.MaxRequestBodySize))
	router.Use(middleware.MaxConcurrentRequests(cfg.Server.MaxConcurrentRequests))
	router.Use(middleware.CORS(cfg.CORS))
	router.Use(middleware.CSRFProtection(cfg.Server.AllowGetQueries, cfg.Server.Mode))
	if authenticator != nil {
		router.Use(middleware.AuthMiddleware(authenticator))
	}
	if authFailureLimiter != nil {
		router.Use(newAuthFailureLimiterMiddleware(authFailureLimiter))
	}
	// Pass nil interface (not nil pointer) when authFailureLimiter is not initialized,
	// to avoid Go's nil interface vs nil pointer pitfall.
	var ipExtractor middleware.IPExtractor
	if authFailureLimiter != nil {
		ipExtractor = authFailureLimiter
	}
	router.Use(middleware.RateLimitMiddleware(rateLimiter, ipExtractor))
	router.Use(middleware.Compression(cfg.Compression))

	// Register health/ready/metrics with real handlers.
	router.Get("/health", healthChecker.LivenessCheck)
	router.Get("/ready", healthChecker.ReadinessCheck)
	router.Get("/metrics", metricsCollector.Handler().ServeHTTP)

	// Register GraphQL routes via server's handler setup.
	gqlHandler := srv.NewGraphQLHandler()
	router.Post("/graphql", srv.WithRequestTimeout(gqlHandler))
	if cfg.Server.AllowGetQueries || cfg.Server.Mode == "development" {
		router.Get("/graphql", srv.WithRequestTimeout(gqlHandler))
	}
	if cfg.Server.Mode == "development" {
		router.Get("/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/playground", http.StatusFound)
		})
		// Use custom playground with pre-configured tabs instead of gqlgen built-in
		router.Get("/playground", server.CustomPlaygroundHandler("/graphql", server.DefaultPlaygroundTabs()))
	}

	// 16. Start HTTP server directly with our middleware-configured router.
	// We bypass srv.Start() because it calls SetupRoutes() again, creating
	// a new router without our middleware chain.
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}

	// Print startup banner before listening.
	printBanner(cfg, logger, dsManager)

	go func() {
		if srvErr := httpSrv.ListenAndServe(); srvErr != nil && srvErr != http.ErrServerClosed {
			logger.Fatal("HTTP server error", zap.Error(srvErr))
		}
	}()

	// 17. Graceful shutdown (SIGTERM/SIGINT).
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	sig := <-quit
	logger.Info("received shutdown signal", zap.String("signal", sig.String()))

	maxWait := cfg.Shutdown.MaxWaitTime
	if maxWait <= 0 {
		maxWait = 30 * time.Second
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), maxWait)
	defer shutdownCancel()
	logger.Info("shutting down HTTP server", zap.Duration("max_wait", maxWait))
	if shutErr := httpSrv.Shutdown(shutdownCtx); shutErr != nil {
		logger.Error("HTTP server shutdown error", zap.Error(shutErr))
	}

	// TracingProvider.Shutdown (independent 5s timeout).
	if tpErr := tracingProvider.Shutdown(context.Background()); tpErr != nil {
		logger.Error("tracing provider shutdown error", zap.Error(tpErr))
	}

	// Stop config hot-reloader.
	hotReloader.Stop()

	// Close TemplateWatcher before TemplateEngine.
	if templateWatcher != nil {
		if twErr := templateWatcher.Stop(); twErr != nil {
			logger.Error("template watcher stop error", zap.Error(twErr))
		}
	}

	// Close TemplateEngine before datasource connections.
	if templateEngine != nil {
		if teErr := templateEngine.Close(); teErr != nil {
			logger.Error("template engine close error", zap.Error(teErr))
		}
	}

	// Close all data source connections.
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer closeCancel()
	if closeErr := dsManager.CloseAll(closeCtx); closeErr != nil {
		logger.Error("datasource close error", zap.Error(closeErr))
	}

	logger.Info("shutdown complete")
}
