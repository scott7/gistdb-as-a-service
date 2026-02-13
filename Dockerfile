# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY gistdb/ ./gistdb/

# Build the application
WORKDIR /app/gistdb
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/gistdb-service .

# Run stage
FROM alpine:latest

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/gistdb-service .

# Expose the application port
EXPOSE 8080

# Run the application
CMD ["./gistdb-service"]
