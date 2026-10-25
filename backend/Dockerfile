# Multi-stage Dockerfile for CloudDump Backend (Production-Ready)

FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /clouddump-server ./cmd/server

FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Create non-root user and group
RUN addgroup -g 10001 -S appgroup && \
    adduser -u 10001 -S appuser -G appgroup

# Create storage mount directory and assign permissions
RUN mkdir -p /app/storage && chown -R appuser:appgroup /app

COPY --from=builder /clouddump-server /app/clouddump-server

USER appuser

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/clouddump-server"]
