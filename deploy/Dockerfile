FROM oven/bun:1 AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/bun.lock* ./
RUN bun install --frozen-lockfile
COPY frontend/ ./
RUN bun run build

FROM golang:1.26-alpine AS backend
WORKDIR /src
COPY backend/go.mod ./backend/
COPY backend/ ./backend/
COPY --from=frontend /src/frontend/build ./frontend/build
WORKDIR /src/backend
RUN go build -o /out/n2api ./cmd/n2api

FROM alpine:3.23
RUN addgroup -S -g 10001 n2api \
    && adduser -S -D -H -h /nonexistent -u 10001 -G n2api -s /sbin/nologin n2api
WORKDIR /app
COPY --from=backend --chown=0:10001 /out/n2api /app/n2api
COPY --from=backend --chown=0:10001 /src/frontend/build /app/frontend/build
RUN chmod -R a-w /app
USER 10001:10001
EXPOSE 3000
ENTRYPOINT ["/app/n2api"]
