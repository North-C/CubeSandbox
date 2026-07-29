# syntax=docker/dockerfile:1.7

ARG GO_IMAGE=docker.io/library/golang:1.26.2-bookworm
ARG BASE_IMAGE=docker.io/cubesandbox-bench/sandbox-code-envd-ci@sha256:21255c98dfb3863d153f2b83122a90b04d099d545fad16add5841dacb4fcdbbb

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS envd-builder
ARG ENVD_REPOSITORY=https://github.com/e2b-dev/infra.git
ARG ENVD_COMMIT=b8ca332f435370397bf42be614b2a5b620d65d39
ARG TARGETOS=linux
ARG TARGETARCH=arm64
RUN apt-get update \
    && apt-get install -y --no-install-recommends git ca-certificates \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src/infra
RUN git init \
    && git remote add origin "${ENVD_REPOSITORY}" \
    && git fetch --depth=1 origin "${ENVD_COMMIT}" \
    && git checkout --detach FETCH_HEAD
COPY envd/0001-add-snapshot-mmds-prime-window.patch /tmp/envd-mmds-prime.patch
RUN git apply /tmp/envd-mmds-prime.patch
RUN CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -buildvcs=false -trimpath \
    -ldflags "-X=main.commitSHA=${ENVD_COMMIT} -s -w" \
    -o /out/envd ./packages/envd

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS code-server-builder
ARG TARGETOS=linux
ARG TARGETARCH=arm64
WORKDIR /src/native-code-server
COPY native-code-server/ ./
RUN go test ./... && go vet ./...
RUN CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -buildvcs=false -trimpath -ldflags "-s -w" \
    -o /out/cube-native-code-server .

FROM ${BASE_IMAGE}
COPY --from=envd-builder --chmod=0755 /out/envd /usr/bin/envd
COPY --from=code-server-builder --chmod=0755 /out/cube-native-code-server /usr/local/bin/cube-native-code-server
COPY --chmod=0755 start-envd-code-interpreter.sh /usr/local/bin/start-envd-code-interpreter.sh
