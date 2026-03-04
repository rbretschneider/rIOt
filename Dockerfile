# Stage 1: Build frontend
FROM node:20-alpine AS frontend
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: Build Go binary
FROM golang:1.22-alpine AS builder
RUN apk add --no-cache git
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Copy built frontend into cmd/riot-server for embedding
COPY --from=frontend /app/web/dist ./cmd/riot-server/dist
RUN CGO_ENABLED=0 go build -tags embed_frontend -ldflags "-s -w" -o /riot-server ./cmd/riot-server

# Stage 3: Minimal runtime
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /riot-server /usr/local/bin/riot-server
EXPOSE 7331
ENTRYPOINT ["riot-server"]
