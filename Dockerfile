# Build Stage
FROM golang:1.27-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /simulator ./cmd/simulator

# Final Minimal Stage
FROM alpine:3.24

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /simulator /app/simulator

# Default configuration template
RUN /app/simulator --init /app/config.yaml

# Expose ONVIF HTTP (8000), RTSP (8554), UDP Multicast WS-Discovery (3702)
EXPOSE 8000/tcp 8554/tcp 8554/udp 3702/udp

ENTRYPOINT ["/app/simulator"]
CMD ["--no-tui", "--config", "/app/config.yaml"]
