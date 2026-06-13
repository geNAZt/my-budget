# Stage 1: Builder stage with compilation tools and dependencies
FROM golang:1.26-alpine AS builder

# Install build tools and Node.js (for execution engine deps)
RUN apk add --no-cache gcc musl-dev git curl nodejs npm

WORKDIR /app

# Ensure data directory exists in the builder
RUN mkdir -p /app/data

# Install buf
RUN BIN="/usr/local/bin" && \
    VERSION="1.32.1" && \
    curl -sSL "https://github.com/bufbuild/buf/releases/download/v${VERSION}/buf-$(uname -s)-$(uname -m)" -o "${BIN}/buf" && \
    chmod +x "${BIN}/buf"

# Pre-install Go tools (do this early as they change rarely)
RUN go install github.com/deepmap/oapi-codegen/v2/cmd/oapi-codegen@v2.1.0 && \
    go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.1

# Copy go.mod and go.sum and download modules
COPY backend/go.mod backend/go.sum ./backend/
WORKDIR /app/backend
RUN go mod download

# Pre-install execution engine dependencies
COPY backend/execution_engine/package*.json ./execution_engine/
RUN cd ./execution_engine && npm ci --omit=dev

# Copy only necessary files for generation and build
# Using selective copy to avoid cache invalidation when frontend changes
WORKDIR /app
COPY proto/ ./proto/
COPY backend/ ./backend/

# Run generation from backend folder
WORKDIR /app/backend
RUN go generate ./...

# Build the binary
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go build -o ../main ./cmd/api

# Stage 2: Clean, minimal production runtime with Node.js sandbox support
FROM alpine:latest AS runner

# Install Node.js, ca-certificates, tzdata, and postgresql-client (required for backups)
# We don't need npm in the runner stage as we copy node_modules from builder
RUN apk add --no-cache nodejs ca-certificates tzdata postgresql-client

WORKDIR /app

# Copy the compiled binary, persistent directories, and execution engine
COPY --from=builder /app/main .
COPY --from=builder /app/data ./data
COPY --from=builder /app/backend/execution_engine ./execution_engine

EXPOSE 8080

CMD ["./main"]
