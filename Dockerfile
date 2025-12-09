# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy go module files
COPY go.mod go.sum ./

# Download dependencies with verification
RUN go mod download
RUN go mod verify

# Copy ALL source code
COPY . .

# List files to verify everything is copied (debugging)
RUN echo "=== Verifying directory structure ===" && \
    ls -la && \
    echo "=== cmd/ directory ===" && \
    ls -la cmd/ && \
    echo "=== internal/ directory ===" && \
    ls -la internal/ && \
    echo "=== pkg/ directory ===" && \
    ls -la pkg/

# Build with explicit module mode and verbose output
RUN CGO_ENABLED=0 GOOS=linux GO111MODULE=on \
    go build -v -ldflags="-w -s" -o bot ./cmd/bot

# Verify binary was created
RUN ls -lh bot

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# Copy binary and config
COPY --from=builder /app/bot .
COPY --from=builder /app/config ./config

# Make binary executable
RUN chmod +x ./bot

CMD ["./bot"]