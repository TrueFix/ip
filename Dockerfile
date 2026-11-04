FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG REVISION=unknown
ARG BUILD_DATE=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN FLAGS="-s -w -X github.com/TrueFix/myip/internal/buildinfo.Version=${VERSION} -X github.com/TrueFix/myip/internal/buildinfo.Revision=${REVISION} -X github.com/TrueFix/myip/internal/buildinfo.BuildDate=${BUILD_DATE}" \
    && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="$FLAGS" -o /out/myip ./cmd/myip \
    && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="$FLAGS" -o /out/updater ./cmd/updater \
    && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="$FLAGS" -o /out/bench ./cmd/bench

FROM build AS updater-build
RUN go mod download github.com/maxmind/geoipupdate/v8@v8.0.0 \
    && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -C "$(go env GOMODCACHE)/github.com/maxmind/geoipupdate/v8@v8.0.0" -trimpath -o /out/geoipupdate ./cmd/geoipupdate

FROM alpine:3.24 AS base
ENV DATA_DIR=/data
ENV METRICS_ADDR=:9090
ARG VERSION=dev
ARG REVISION=unknown
ARG BUILD_DATE=unknown
LABEL org.opencontainers.image.title="myip" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.created="${BUILD_DATE}"
RUN apk add --no-cache ca-certificates \
    && mkdir -p /data && chown 65532:65532 /data
USER 65532:65532
WORKDIR /data
COPY LICENSE THIRD_PARTY.md /usr/share/licenses/myip/
COPY licenses/ /usr/share/licenses/myip/data/

FROM base AS api
COPY --from=build /out/myip /usr/local/bin/myip
COPY --from=build /out/bench /usr/local/bin/bench
EXPOSE 8080 9090
HEALTHCHECK --interval=30s --timeout=3s --start-period=60s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/myip"]

FROM base AS updater
COPY --from=updater-build /out/geoipupdate /usr/local/bin/geoipupdate
COPY --from=build /out/updater /usr/local/bin/updater
ENTRYPOINT ["/usr/local/bin/updater"]

# One published image supports both roles; Compose overrides the updater's
# entrypoint and disables the API-only healthcheck on that container.
FROM api AS production
COPY --from=build /out/updater /usr/local/bin/updater
COPY --from=updater-build /out/geoipupdate /usr/local/bin/geoipupdate
