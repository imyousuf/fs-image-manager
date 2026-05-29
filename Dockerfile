# syntax=docker/dockerfile:1

# ---- Stage 1: build the frontend bundle (embedded into the Go binary) ----
FROM node:20-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# ---- Stage 2: build the CGO-free Go binary with the embedded frontend ----
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Bring in the built frontend so the //go:embed of web/dist has content.
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/fs-image-manager .

# ---- Stage 3: minimal runtime image ----
FROM alpine:3.20
# ca-certificates for outbound HTTPS (self-update, Ollama/Rekognition over TLS).
# The optional external media tools (ffmpeg, exiftool, dcraw, darktable) can be
# layered on top of this image as needed; they are detected at runtime.
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/fs-image-manager /usr/local/bin/fs-image-manager
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/fs-image-manager"]
CMD ["serve", "-config", "/etc/fs-image-manager/image-manager.cfg"]
