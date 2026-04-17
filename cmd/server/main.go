package main

import (
	"fmt"

	_ "github.com/cespare/xxhash/v2"
	_ "github.com/fsnotify/fsnotify"
	_ "github.com/go-chi/chi/v5"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/golang-jwt/jwt/v5"
	_ "github.com/hashicorp/golang-lru/v2"
	_ "github.com/prometheus/client_golang/prometheus"
	_ "github.com/redis/go-redis/v9"
	_ "github.com/spf13/viper"
	_ "go.opentelemetry.io/otel"
	_ "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	_ "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	_ "go.opentelemetry.io/otel/sdk/trace"
	_ "go.uber.org/zap"
	_ "golang.org/x/sync/singleflight"
	_ "golang.org/x/time/rate"
)

func main() {
	fmt.Println("GraphQL Multi-DataSource API Server")
}
