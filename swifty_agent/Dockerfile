# Copyright (c) 2026 hangtiancheng
#
# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions:
#
# The above copyright notice and this permission notice shall be included in
# all copies or substantial portions of the Software.
#
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
# AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
# OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
# SOFTWARE.

# Swifty Agent multi-stage build
# Build: docker build -t swifty-agent .
# Run:   docker run -p 8123:8123 -v $(pwd)/config.json:/app/config.json swifty-agent

# ---- Build stage ----
FROM golang:1.25-alpine AS builder

# Install git (required by go mod) and ca-certificates
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy go.mod/go.sum first to leverage Docker layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Static compilation (go work requires separate handling inside the container;
# this assumes the swifty_agent module can be built independently)
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/swifty-agent .

# ---- Runtime stage ----
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /out/swifty-agent /app/swifty-agent

# Default configuration (can be overridden via volume mount)
COPY config.json /app/config.json

EXPOSE 8123

ENTRYPOINT ["/app/swifty-agent"]
