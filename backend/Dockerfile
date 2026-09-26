FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod ./
# If go.sum exists, copy it
COPY go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /clouddump-server ./cmd/server

FROM alpine:3.20
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /clouddump-server /app/clouddump-server

EXPOSE 8080
ENTRYPOINT ["/app/clouddump-server"]
