# 项目变量
APP_NAME    := mountainKing
BIN_DIR     := bin
BINARY      := $(BIN_DIR)/mountainking
COVER_FILE  := coverage.out
FUZZ_TIME   := 30s
DOCKER_TAG  := $(APP_NAME):latest

.DEFAULT_GOAL := help

.PHONY: dev build test lint vet generate docker run fuzz clean help coverage

# Development mode: zero-config startup with mock datasource
dev:             ## 开发模式运行（使用 mock 数据源）
	go run ./cmd/server -config config.dev.yaml

# Build production binary
build:           ## 编译项目（生产优化）
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY) ./cmd/server

# Run all tests
test:            ## 运行测试（竞态检测 + 覆盖率）
	go test -race -count=1 -coverprofile=$(COVER_FILE) ./...

# Run golangci-lint
lint:            ## 运行 golangci-lint
	golangci-lint run ./...

vet:             ## 运行 go vet
	go vet ./...

generate:        ## 运行 go generate（gqlgen 代码生成）
	go generate ./...

docker:          ## 构建 Docker 镜像
	docker build -t $(DOCKER_TAG) -f deploy/Dockerfile .

run:             ## 本地运行服务
	go run cmd/server/main.go

fuzz:            ## 运行 fuzz 测试（默认 30s，逐个运行）
	go test -fuzz=FuzzSafeString -fuzztime=$(FUZZ_TIME) ./internal/template/
	go test -fuzz=FuzzSanitizeSQL -fuzztime=$(FUZZ_TIME) ./internal/template/
	go test -fuzz=FuzzSanitize -fuzztime=$(FUZZ_TIME) ./internal/sanitize/

# Clean build artifacts
clean:           ## 清理构建产物
	rm -rf $(BIN_DIR) dist/ $(COVER_FILE)

coverage: test   ## 生成覆盖率报告并输出总覆盖率
	go tool cover -func=$(COVER_FILE) | tail -1

help:            ## 列出所有可用目标
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'
