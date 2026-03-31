# Build Go binaries
FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS builder
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /rana ./cmd/rana
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /rana-api ./cmd/rana-api

# Build Web assets
FROM node:22-alpine AS web-builder
WORKDIR /web
COPY web/package*.json ./
RUN if [ -f package.json ]; then npm ci; else echo '{"name":"rana-web","version":"0.0.0","scripts":{"build":"mkdir -p dist && cp -r public/* dist/ 2>/dev/null || true"}}' > package.json && npm ci; fi
COPY web/ .
RUN npm run build

# Runtime
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata bash
WORKDIR /opt/rana
COPY --from=builder /rana /usr/local/bin/rana
COPY --from=builder /rana-api /usr/local/bin/rana-api
COPY --from=web-builder /web/dist /opt/rana/web/dist
COPY config.example.yaml /opt/rana/config.example.yaml
ENTRYPOINT ["rana-api"]
