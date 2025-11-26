# --- Stage 1: Build Frontend ---
FROM node:22-alpine AS node-builder

WORKDIR /app/web

# Install dependencies and build
COPY web/package.json ./
RUN npm install

COPY web/ ./
RUN npm run build

# --- Stage 2: Build Backend ---
# Use Go 1.25.4 (Specified version)
FROM golang:1.25.4-alpine AS go-builder

# Install build dependencies (git required for go mod)
RUN apk add --no-cache git

WORKDIR /app

# Copy source code
COPY go.mod ./
COPY cmd/ cmd/
COPY internal/ internal/
COPY schema.sql ./

# Resolve dependencies
RUN go mod tidy

# Build the binary
ENV CGO_ENABLED=0
RUN go build -o nwdevice-viz ./cmd/server

# --- Stage 3: Final Image ---
FROM alpine:3.20

WORKDIR /app

# Create a non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# Copy binary and schema
COPY --from=go-builder /app/nwdevice-viz .
COPY --from=go-builder /app/schema.sql .

# Copy built frontend assets
COPY --from=node-builder /app/web/dist ./web/dist

# Create directories and set permissions
RUN mkdir -p /app/data /app/certs && \
    chown -R appuser:appgroup /app/data /app/certs /app

# Switch to non-root user
USER appuser

# Create DB directory
VOLUME /app/data

EXPOSE 8080

CMD ["./nwdevice-viz"]