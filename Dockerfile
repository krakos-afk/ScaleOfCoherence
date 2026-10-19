# syntax=docker/dockerfile:1

# --- Frontend build ---------------------------------------------------------
FROM node:24-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# --- Backend build ----------------------------------------------------------
FROM golang:1.27-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY embed.go ./
COPY migrations/ migrations/
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# --- Runtime ----------------------------------------------------------------
FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
    && adduser -D -H -u 10001 app
WORKDIR /app
COPY --from=backend /out/server ./server
COPY --from=frontend /src/frontend/dist/ ./web/
ENV PORT=8080 \
    SPA_DIR=/app/web
EXPOSE 8080
USER app
ENTRYPOINT ["/app/server"]
