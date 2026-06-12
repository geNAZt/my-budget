# Stage 1: Builder stage with compilation tools and dependencies
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache gcc musl-dev git curl

WORKDIR /app

# Ensure data directory exists in the builder
RUN mkdir -p /app/data

# Install buf
RUN BIN="/usr/local/bin" && \
    VERSION="1.32.1" && \
    curl -sSL "https://github.com/bufbuild/buf/releases/download/v${VERSION}/buf-$(uname -s)-$(uname -m)" -o "${BIN}/buf" && \
    chmod +x "${BIN}/buf"

# Copy go.mod and go.sum from backend
COPY backend/go.mod backend/go.sum ./backend/
WORKDIR /app/backend
RUN go mod download

# Install oapi-codegen
RUN go install github.com/deepmap/oapi-codegen/v2/cmd/oapi-codegen@v2.1.0

# Install protoc-gen-go
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.1

# Copy the rest of the workspace (needed for proto)
WORKDIR /app
COPY . .

# Run generation from backend folder
WORKDIR /app/backend
RUN go generate ./...

# Build the binary
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go build -o ../main ./cmd/api

# Stage 2: Clean, minimal production runtime with Node.js sandbox support
FROM alpine:latest AS runner

# Install Node.js, npm, ca-certificates, tzdata, and postgresql-client (required for backups)
RUN apk add --no-cache nodejs npm ca-certificates tzdata postgresql-client

WORKDIR /app

# Copy the compiled binary, persistent directories, and execution engine
COPY --from=builder /app/main .
COPY --from=builder /app/data ./data
COPY --from=builder /app/backend/execution_engine ./execution_engine

# Install production-only npm dependencies for the isolated execution engine daemon
RUN cd /app/execution_engine && npm ci --omit=dev

EXPOSE 8080

CMD ["./main"]
