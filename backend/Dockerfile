FROM golang:1.26-alpine AS builder

WORKDIR /src
ARG GOPROXY=https://proxy.golang.org,direct
ARG TOKENHUB_VERSION=0.3.0
ENV GOPROXY=$GOPROXY
RUN apk add --no-cache gcc musl-dev
COPY backend/go.mod ./backend/go.mod
COPY backend/go.sum ./backend/go.sum
COPY backend/cmd ./backend/cmd
COPY backend/internal ./backend/internal
COPY data/model-catalog.yaml ./catalog/model-catalog.yaml
COPY data/provider-catalog.json ./catalog/provider-catalog.json
RUN cd backend && CGO_ENABLED=1 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.buildVersion=${TOKENHUB_VERSION} -X main.buildType=release -X main.deploymentType=container" \
    -o /out/tokenhub ./cmd/tokenhub

FROM alpine:3.20

ARG TOKENHUB_VERSION=0.3.0
LABEL org.opencontainers.image.version="${TOKENHUB_VERSION}"
RUN apk add --no-cache sqlite-libs && adduser -D -H tokenhub && mkdir -p /app/data /app/catalog && chown -R tokenhub:tokenhub /app
USER tokenhub
WORKDIR /app
COPY --from=builder /out/tokenhub /app/tokenhub
COPY --from=builder --chown=tokenhub:tokenhub /src/catalog/model-catalog.yaml /app/catalog/model-catalog.yaml
COPY --from=builder --chown=tokenhub:tokenhub /src/catalog/provider-catalog.json /app/catalog/provider-catalog.json
EXPOSE 8080
ENTRYPOINT ["/app/tokenhub"]
