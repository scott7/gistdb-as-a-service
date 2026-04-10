# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY client/ ./client/

WORKDIR /app/client
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/gistdb-client .

# Run stage
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/gistdb-client .
COPY client/index.html .

EXPOSE 8081

CMD ["./gistdb-client", "--credentials", "/data/credentials.json"]
