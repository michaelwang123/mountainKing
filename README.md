# GraphQL Multi-DataSource API

A production-grade GraphQL API server in Go that provides a unified query interface across multiple data sources �?currently StarRocks (OLAP) and Prometheus (metrics). Built with gqlgen, chi, and a comprehensive middleware stack for enterprise-grade security, observability, and resilience.

## Features

- Unified GraphQL API for querying StarRocks and Prometheus
- SQL Template Query Engine — execute complex multi-table JOIN, CTE, and window function queries via pre-defined Go `text/template` SQL templates with full SQL injection protection
- Relay-style cursor pagination and traditional offset/limit pagination
- Instant and range queries for Prometheus
- Cross-datasource parallel queries with isolated error handling
- JWT and API Key authentication with per-datasource authorization
- Token bucket rate limiting (local + distributed Redis with auto-fallback)
- Circuit breaker and retry with exponential backoff
- Request batching with configurable limits
- DataLoader-based N+1 query prevention
- In-memory (LRU) or Redis-backed caching with penetration/avalanche/stampede protection
- OpenTelemetry distributed tracing (OTLP export to Jaeger/Tempo)
- Prometheus metrics endpoint with custom labels
- Structured JSON logging (zap) with audit trail
- Sensitive data sanitization in logs and traces
- Hot-reloadable YAML configuration with env var overrides (12-Factor)
- SQL template hot-reload via fsnotify file watching and GraphQL Mutation (no restart required)
- Graceful shutdown with ordered teardown
- CSRF protection, CORS, gzip compression, body size limits
- Brute-force authentication failure protection
- Kubernetes-ready (startup/liveness/readiness probes, HPA)
- Multi-stage Docker build with distroless base image

## Prerequisites

- Go 1.25+
- (Optional) StarRocks instance
- (Optional) Prometheus instance
- (Optional) Redis �?for distributed caching/rate limiting

## Getting Started

For a comprehensive guide including first query examples, Docker setup, and development mode, see the [Getting Started Guide](official_document/getting-started.md).

Quick start:

```bash
# Clone
git clone https://github.com/michaelwang123/mountainKing.git
cd graphql-api

# Install dependencies
go mod download

# Run the server
go run cmd/server/main.go
```

The server listens on `:8080` by default. Set `GRAPHQL_SERVER_MODE=development` for Playground access at `/playground`.

## Configuration

Configuration is loaded from `config.yaml` with environment variable overrides using the `GRAPHQL_` prefix (12-Factor style). For the complete configuration reference, see [Configuration Reference](official_document/configuration.md).

```bash
export GRAPHQL_SERVER_PORT=9090
export GRAPHQL_LOGGING_LEVEL=debug
export GRAPHQL_GRAPHQL_INTROSPECTION_ENABLED=true
```

Hot-reloadable at runtime: log level, rate limit params, cache TTL.

Key config sections:

| Section | Description |
|---------|-------------|
| `server` | Port, mode, timeouts, batch limits |
| `graphql` | Introspection, complexity/depth limits, max result rows |
| `datasources` | StarRocks/Prometheus connection configs with whitelist |
| `auth` | JWT (HS256/RS256/ES256) or API Key authentication |
| `rate_limit` | Local or distributed (Redis) rate limiting |
| `cache` | Memory/Redis backend, TTL, jitter, per-datasource TTL |
| `sql_templates` | SQL template engine: base_dir, templates, parameters, cache TTL |
| `tracing` | OpenTelemetry OTLP export (gRPC/HTTP) |
| `metrics` | Custom Prometheus labels |
| `circuit_breaker` | Failure threshold, open duration |
| `retry` | Max retries, exponential backoff |
| `cors` | Cross-origin resource sharing |
| `compression` | gzip response compression |
| `logging` | Level, format, audit log |
| `sanitization` | Regex-based sensitive data masking |
| `shutdown` | Graceful shutdown max wait time |

## GraphQL Schema

### Queries

```graphql
# StarRocks OLAP query with filtering, sorting, and Relay pagination
starrocks(table: String!, fields: [String!], filters: [StarRocksFilter!],
          orderBy: [StarRocksOrderBy!], first: Int, after: String,
          offset: Int, limit: Int): StarRocksConnection!

# SQL Template query — execute pre-defined SQL templates with parameters
templateQuery(templateName: String!, parameters: JSON, fields: [String!],
              first: Int, offset: Int, orderBy: [TemplateOrderBy!]): TemplateQueryConnection!

# List all registered SQL templates
templateList(first: Int, offset: Int): [TemplateInfo!]!

# Prometheus instant query
prometheusInstant(query: String!, time: DateTime,
                  filters: [PrometheusLabelFilter!]): PrometheusInstantResult!

# Prometheus range query
prometheusRange(query: String!, startTime: DateTime!, endTime: DateTime!,
                step: String!, filters: [PrometheusLabelFilter!]): PrometheusRangeResult!
```

### Mutations

```graphql
# Clear cache (all or per-datasource). Requires mutation permission.
clearCache(datasource: String): Boolean!

# Reload SQL templates from disk. Requires mutation permission.
reloadTemplates: ReloadTemplatesResult!
```

### Endpoints

| Path | Method | Description |
|------|--------|-------------|
| `/graphql` | POST | GraphQL endpoint |
| `/graphql` | GET | GraphQL (if `allow_get_queries` enabled) |
| `/playground` | GET | GraphiQL (development mode only) |
| `/health` | GET | Liveness check (200/503) |
| `/ready` | GET | Readiness probe with datasource status (200/503) |
| `/metrics` | GET | Prometheus metrics |

For detailed query examples, pagination, error codes, and more, see the [GraphQL API Reference](official_document/graphql-api.md).

## Project Structure

```
cmd/server/              Entry point (main.go)
internal/
  adapter/
    prometheus/          Prometheus adapter, query builder, type mapper, validator
    starrocks/           StarRocks adapter, query builder, type mapper, whitelist
  audit/                 Audit logging
  cache/                 Cache interface, memory/Redis backends, Cache Layer, key gen
  config/                YAML config loading, validation, hot-reload (fsnotify)
  context/               Context keys (RequestID, AuthIdentity, TraceID)
  datasource/            DataSource interface, Manager, Registry, circuit breaker, reconnect
  errors/                Typed error codes (AUTH_*, VALIDATION_*, DATASOURCE_*, etc.)
  graphql/
    dataloader/          Per-request DataLoader for batched fetching
    generated/           gqlgen generated code
    resolver/            Query and Mutation resolvers
    scalar/              Custom scalars (DateTime, JSON)
    schema/              GraphQL schema files (.graphql)
  health/                Health check and readiness probe
  middleware/            Auth, authz, rate limit, CORS, CSRF, compression, body limit, request ID
  observability/         Prometheus metrics, OpenTelemetry tracing, structured logging, Redis hook
  ratelimit/             RateLimiter interface, local/distributed/fallback implementations
  redis/                 Shared Redis client
  sanitize/              Sensitive data masking
  server/                HTTP server, routing, graceful shutdown, batch query handling
  template/              SQL template engine: types, loader, registry, renderer, validator,
                         sanitizer, funcmap, pagination, cache, engine, watcher, metrics
pkg/
  retry/                 Retry logic with error classifier and exponential backoff
deploy/
  Dockerfile             Multi-stage build (distroless, non-root)
  docker-compose.yaml    Integration test environment
  k8s/                   Kubernetes manifests (Deployment, Service, ConfigMap, HPA)
  prometheus.yml         Prometheus scrape config
templates/                         SQL template files directory (configurable via sql_templates.base_dir)
  _shared/                         Shared template fragments (referenced via {{template}})
  fleet/                           Fleet report templates
  driver/                          Driver score templates
```

## Documentation

Comprehensive Apache-style project documentation is available in the [`official_document/`](official_document/) directory:

| Document | Description |
|----------|-------------|
| [Architecture Overview](official_document/architecture.md) | System architecture, component relationships, request flow |
| [Getting Started](official_document/getting-started.md) | Environment setup, build, run, first query |
| [Configuration Reference](official_document/configuration.md) | All config options, env var overrides, hot-reload |
| [GraphQL API Reference](official_document/graphql-api.md) | Schema, queries, mutations, pagination, error codes |
| [Security Guide](official_document/security.md) | Authentication, authorization, rate limiting, input validation |
| [DataSource Adapters](official_document/datasource-adapters.md) | StarRocks/Prometheus details and extension guide |
| [Observability](official_document/observability.md) | Prometheus metrics, OpenTelemetry tracing, structured logging |
| [Deployment Guide](official_document/deployment.md) | Docker, Kubernetes, CI/CD, production checklist |
| [Performance Tuning](official_document/performance.md) | Caching, connection pools, circuit breaker, benchmarks |
| [Developer Guide](official_document/developer-guide.md) | Project structure, code standards, testing, contribution |
| [Error Code Reference](official_document/error-reference.md) | Complete error codes, HTTP status mapping, client handling |
| [Troubleshooting](official_document/troubleshooting.md) | Common issues diagnosis and solutions |
| [FAQ](official_document/faq.md) | Frequently asked questions |

See also: [CONTRIBUTING.md](CONTRIBUTING.md) · [CHANGELOG.md](CHANGELOG.md)

## Code Generation

GraphQL code is generated with [gqlgen](https://gqlgen.com/):

```bash
go run github.com/99designs/gqlgen generate
```

## Testing

```bash
# Unit tests
go test ./...

# With race detection and coverage
go test -race -coverprofile=coverage.out ./...

# Benchmarks
go test -bench=. -benchmem ./internal/server/
```

Property-based tests use [rapid](https://pkg.go.dev/pgregory.net/rapid) with 100+ iterations per property.

## Docker

```bash
# Build
docker build -f deploy/Dockerfile \
  --build-arg VERSION=$(git describe --tags) \
  --build-arg BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  -t graphql-api:latest .

# Run
docker run -p 8080:8080 -v $(pwd)/config.yaml:/config.yaml graphql-api:latest
```

## Kubernetes

```bash
kubectl apply -f deploy/k8s/
```

Includes Deployment (with startup/liveness/readiness probes), Service, ConfigMap, and HPA (based on `graphql_requests_in_flight` custom metric).

## License

See [LICENSE](LICENSE) for details.
