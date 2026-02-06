# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git make

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binaries
RUN make build

# Runtime stage - Server
FROM alpine:3.19 AS server

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/bin/cwe-server /app/
COPY --from=builder /app/configs/config.yaml /app/configs/

EXPOSE 8080

ENTRYPOINT ["/app/cwe-server"]
CMD ["-config", "/app/configs/config.yaml"]

# Runtime stage - Scheduler
FROM alpine:3.19 AS scheduler

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/bin/cwe-scheduler /app/
COPY --from=builder /app/configs/config.yaml /app/configs/

ENTRYPOINT ["/app/cwe-scheduler"]
CMD ["-config", "/app/configs/config.yaml"]

# Runtime stage - CLI
FROM alpine:3.19 AS cli

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /app/bin/cwe-cli /usr/local/bin/

ENTRYPOINT ["cwe-cli"]
