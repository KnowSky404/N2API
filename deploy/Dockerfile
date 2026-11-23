FROM --platform=$BUILDPLATFORM oven/bun:1.4.5@sha256:3aa040e5445efa09d75bb428a64ba50db13e2b3ac9b6b0c7f145615f62b2ee18 AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/bun.lock* ./
RUN bun install --frozen-lockfile
COPY frontend/ ./
RUN bun run build

FROM golang:1.27.4-alpine3.23@sha256:f70850ef6ce341e8b6427f677049145d63c0b7ce002d4d7e3d464828b1b96dcd AS backend
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

FROM alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
ARG N2API_VERSION=dev
ARG N2API_COMMIT=unknown
ARG N2API_BUILD_TIME=1970-01-01T00:00:00Z
ARG N2API_SOURCE=https://github.com/KnowSky404/N2API
LABEL org.opencontainers.image.source="${N2API_SOURCE}" \
      org.opencontainers.image.revision="${N2API_COMMIT}" \
      org.opencontainers.image.version="${N2API_VERSION}" \
      org.opencontainers.image.created="${N2API_BUILD_TIME}"
RUN addgroup -S -g 10001 n2api \
    && apk add --no-cache --upgrade libcrypto3 libssl3 \
    && adduser -S -D -H -h /nonexistent -u 10001 -G n2api -s /sbin/nologin n2api
WORKDIR /app
COPY --from=backend --chown=0:10001 /out/n2api /app/n2api
COPY --from=backend --chown=0:10001 /src/frontend/build /app/frontend/build
RUN chmod -R a-w /app
USER 10001:10001
EXPOSE 3000
ENTRYPOINT ["/app/n2api"]
