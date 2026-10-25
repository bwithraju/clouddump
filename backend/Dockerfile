# Multi-stage Dockerfile for CloudDump Backend

FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /clouddump-server ./cmd/server

FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /clouddump-server /app/clouddump-server

# Create storage mount directory
RUN mkdir -p /app/storage

EXPOSE 8080
ENTRYPOINT ["/app/clouddump-server"]
