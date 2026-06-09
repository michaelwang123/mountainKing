# Multi-stage Dockerfile for GraphQL Multi-DataSource API
# Requirements: 18.1 (multi-stage, scratch/distroless), 18.2 (non-root), 18.3 (build-arg version/buildTime)

# --- Build stage ---
FROM golang:1.25.11-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" \
    -o /bin/graphql-api ./cmd/server

# --- Final stage ---
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /bin/graphql-api /graphql-api
COPY --from=builder /src/config.yaml /config.yaml

USER 65534

EXPOSE 8080

ENTRYPOINT ["/graphql-api"]
