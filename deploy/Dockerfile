FROM oven/bun:1.3.14@sha256:e10577f0db68676a7024391c6e5cb4b879ebd17188ab750cf10024a6d700e5c4 AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/bun.lock* ./
RUN bun install --frozen-lockfile
COPY frontend/ ./
RUN bun run build

FROM golang:1.26.5-alpine3.23@sha256:622e56dbc11a8cfe87cafa2331e9a201877271cbff918af53d3be315f3da88cc AS backend
ARG N2API_VERSION=dev
ARG N2API_COMMIT=unknown
ARG N2API_BUILD_TIME=1970-01-01T00:00:00Z
WORKDIR /src
COPY backend/go.mod ./backend/
COPY backend/ ./backend/
COPY --from=frontend /src/frontend/build ./frontend/build
WORKDIR /src/backend
RUN go build -trimpath \
    -ldflags="-s -w -X=github.com/KnowSky404/N2API/backend/internal/buildinfo.Version=${N2API_VERSION} -X=github.com/KnowSky404/N2API/backend/internal/buildinfo.Commit=${N2API_COMMIT} -X=github.com/KnowSky404/N2API/backend/internal/buildinfo.BuildTime=${N2API_BUILD_TIME}" \
    -o /out/n2api ./cmd/n2api

FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
ARG N2API_VERSION=dev
ARG N2API_COMMIT=unknown
ARG N2API_BUILD_TIME=1970-01-01T00:00:00Z
ARG N2API_SOURCE=https://github.com/KnowSky404/N2API
LABEL org.opencontainers.image.source="${N2API_SOURCE}" \
      org.opencontainers.image.revision="${N2API_COMMIT}" \
      org.opencontainers.image.version="${N2API_VERSION}" \
      org.opencontainers.image.created="${N2API_BUILD_TIME}"
RUN addgroup -S -g 10001 n2api \
    && adduser -S -D -H -h /nonexistent -u 10001 -G n2api -s /sbin/nologin n2api
WORKDIR /app
COPY --from=backend --chown=0:10001 /out/n2api /app/n2api
COPY --from=backend --chown=0:10001 /src/frontend/build /app/frontend/build
RUN chmod -R a-w /app
USER 10001:10001
EXPOSE 3000
ENTRYPOINT ["/app/n2api"]
