# GraphQL Multi-DataSource API

A production-grade GraphQL API server in Go that provides a unified query interface across multiple data sources — currently StarRocks (OLAP) and Prometheus (metrics).

## Features

- Unified GraphQL API for querying StarRocks and Prometheus
- Relay-style cursor pagination for StarRocks queries
- Instant and range queries for Prometheus
- JWT and API Key authentication
- Per-datasource authorization and permissions
- Circuit breaker and retry with exponential backoff
- Request batching with configurable limits
- DataLoader-based N+1 query prevention
- In-memory or Redis-backed caching with TTL jitter
- Local or distributed rate limiting
- OpenTelemetry tracing (OTLP export)
- Prometheus metrics endpoint
- Structured logging (zap, JSON format)
- Hot-reloadable YAML configuration with env var overrides
- Graceful shutdown with ordered teardown
- Kubernetes-ready (health/readiness probes)

## Prerequisites

- Go 1.25+
- (Optional) StarRocks instance
- (Optional) Prometheus instance
- (Optional) Redis — for distributed caching/rate limiting

## Getting Started

```bash
# Clone
git clone https://github.com/example/graphql-api.git
cd graphql-api

# Install dependencies
go mod download

# Run the server
go run cmd/server/main.go
```

The server listens on `:8080` by default.

## Configuration


Configuration is loaded from a YAML file with environment variable overrides using the `GRAPHQL_` prefix (12-Factor style).

```bash
# Example env overrides
export GRAPHQL_SERVER_PORT=9090
export GRAPHQL_LOGGING_LEVEL=debug
export GRAPHQL_GRAPHQL_INTROSPECTION_ENABLED=true
```

Key config sections:

| Section | Description |
|---------|-------------|
| `server` | Port, mode, timeouts, batch limits |
| `graphql` | Introspection, complexity/depth limits, APQ |
| `datasources` | StarRocks/Prometheus connection configs |
| `auth` | JWT or API Key authentication |
| `cache` | Memory/Redis backend, TTL, jitter |
| `rate_limit` | Local or distributed rate limiting |
| `tracing` | OpenTelemetry OTLP export |
| `circuit_breaker` | Failure threshold, open duration |
| `retry` | Max retries, backoff strategy |

## GraphQL Schema

### Queries

```graphql
# StarRocks OLAP query with filtering, sorting, and pagination
starrocks(table: String!, fields: [String!], filters: [StarRocksFilter!],
          orderBy: [StarRocksOrderBy!], first: Int, after: String,
          offset: Int, limit: Int): StarRocksConnection!

# Prometheus instant query
prometheusInstant(query: String!, time: DateTime,
                  filters: [PrometheusLabelFilter!]): PrometheusInstantResult!

# Prometheus range query
prometheusRange(query: String!, startTime: DateTime!, endTime: DateTime!,
                step: String!, filters: [PrometheusLabelFilter!]): PrometheusRangeResult!
```

### Mutations

```graphql
# Clear cache (all or per-datasource)
clearCache(datasource: String): Boolean!
```

### Endpoints

| Path | Method | Description |
|------|--------|-------------|
| `/graphql` | POST | GraphQL endpoint |
| `/graphql` | GET | GraphQL (if `allow_get_queries` enabled) |
| `/playground` | GET | GraphiQL (development mode only) |
| `/health` | GET | Health check |
| `/ready` | GET | Readiness probe |
| `/metrics` | GET | Prometheus metrics |

## Project Structure

```
cmd/server/          Entry point
internal/
  adapter/
    prometheus/      Prometheus adapter, query builder, type mapper
    starrocks/       StarRocks adapter, query builder, whitelist
  config/            YAML config loading, validation, hot-reload
  datasource/        DataSource interface, manager, circuit breaker, registry
  errors/            Typed error definitions
  graphql/
    dataloader/      DataLoader for batched fetching
    generated/       gqlgen generated code
    resolver/        GraphQL resolvers
    scalar/          Custom scalars (DateTime, JSON)
    schema/          GraphQL schema files (.graphql)
  middleware/        HTTP middleware (request ID, etc.)
  observability/     Structured logging setup
  server/            HTTP server, routing, graceful shutdown, batching
pkg/
  retry/             Retry logic with classifier and backoff
deploy/k8s/          Kubernetes manifests
```

## Code Generation

GraphQL code is generated with [gqlgen](https://gqlgen.com/):

```bash
go run github.com/99designs/gqlgen generate
```

## Testing

```bash
go test ./...
```

## License

See [LICENSE](LICENSE) for details.
