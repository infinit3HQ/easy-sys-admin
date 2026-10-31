# syntax=docker/dockerfile:1
# Control plane image. Build from the repository root:
#   docker build -f apps/esa/Dockerfile .
FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build
ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=none
WORKDIR /src/apps/esa
ENV GOWORK=off CGO_ENABLED=0 GOFLAGS=-trimpath

COPY apps/esa/go.mod apps/esa/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY apps/esa/ ./
ARG LDFLAGS="-s -w -X github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/version.Version=${VERSION} -X github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/version.Commit=${COMMIT}"
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags "$LDFLAGS" -o /out/esa ./cmd/esa && \
    for arch in amd64 arm64; do \
      GOOS=linux GOARCH=$arch go build -ldflags "$LDFLAGS" -o /out/helpers/esa-helper-linux-$arch ./cmd/esa-helper; \
    done

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/esa /usr/local/bin/esa
# The helper binaries the control plane uploads to servers.
COPY --from=build /out/helpers/ /usr/local/share/esa/helpers/
ENV ESA_LISTEN=0.0.0.0:8080 \
    ESA_HELPER_DIR=/usr/local/share/esa/helpers \
    ESA_LOG_FORMAT=json
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 CMD ["/usr/local/bin/esa", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/esa"]
CMD ["serve"]
