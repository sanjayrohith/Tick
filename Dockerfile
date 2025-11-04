# syntax=docker/dockerfile:1

# One Dockerfile builds all three binaries. Pick which with --build-arg CMD_NAME.
ARG GO_VERSION=1.27

# ---------- build ----------
FROM golang:${GO_VERSION} AS build

ARG CMD_NAME=tick-api
ARG VERSION=dev
ARG COMMIT=none

WORKDIR /src

# Dependencies resolve in their own layer so a source-only change does not
# re-download the module graph.
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# CGO off and a static build, so the result runs on a distroless base with no
# libc present. -trimpath keeps build paths out of the binary.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/app ./cmd/${CMD_NAME}

# ---------- runtime ----------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

ARG CMD_NAME=tick-api
ARG VERSION=dev
ARG COMMIT=none

LABEL org.opencontainers.image.title="${CMD_NAME}" \
      org.opencontainers.image.source="https://github.com/sanjayrohith/tick" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.licenses="MIT"

COPY --from=build /out/app /app

# Distroless nonroot is uid 65532. Nothing here writes to disk.
USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/app"]
