# Minimal container for sandboxed JavaScript expression evaluation
# This container is designed to be as restricted as possible

FROM golang:1.21-alpine AS builder

WORKDIR /build

# Copy only what's needed for the sandbox worker
COPY go.mod go.sum ./
RUN go mod download

COPY internal/cwl/sandbox/ ./internal/cwl/sandbox/
COPY cmd/sandbox-worker/ ./cmd/sandbox-worker/

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /sandbox-eval ./cmd/sandbox-worker

# Minimal runtime image
FROM scratch

# Copy the binary
COPY --from=builder /sandbox-eval /sandbox-eval

# Run as non-root (nobody)
USER 65534:65534

# No shell, no package manager, no utilities
# The only thing this container can do is evaluate expressions

ENTRYPOINT ["/sandbox-eval"]
