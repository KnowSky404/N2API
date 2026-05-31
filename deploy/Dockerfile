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
WORKDIR /app
COPY --from=backend /out/n2api /app/n2api
COPY --from=backend /src/frontend/build /app/frontend/build
EXPOSE 3000
ENTRYPOINT ["/app/n2api"]
