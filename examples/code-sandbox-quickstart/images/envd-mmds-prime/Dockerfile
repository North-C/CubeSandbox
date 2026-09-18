ARG GO_IMAGE=docker.io/library/golang:1.25.4-bookworm@sha256:e17419604b6d1f9bc245694425f0ec9b1b53685c80850900a376fb10cb0f70cb
ARG BASE_IMAGE=cube-sandbox-int.tencentcloudcr.com/cube-sandbox/sandbox-code@sha256:467494c38f3c335e42d590e23d5cbb00dc15c2627c9da4f7e7f3bd9f4ec18c5e
ARG SOURCE_DATE_EPOCH=1776266298

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS envd-source
ARG ENVD_COMMIT=b8ca332f435370397bf42be614b2a5b620d65d39
ARG GOFLAGS
WORKDIR /src/infra
COPY source/packages/envd/ packages/envd/
COPY source/packages/shared/ packages/shared/
COPY patches/0001-envd-bound-mmds-prime-window.patch /tmp/envd-mmds-prime.patch
COPY --from=envd-deps / /tmp/envd-deps/
ENV GOWORK=off
ENV GOFLAGS=${GOFLAGS}
RUN git apply --check /tmp/envd-mmds-prime.patch \
    && git apply /tmp/envd-mmds-prime.patch \
    && if [ -f /tmp/envd-deps/modules.txt ]; then \
         mkdir -p packages/envd/vendor \
         && cp -a /tmp/envd-deps/. packages/envd/vendor/; \
       fi
WORKDIR /src/infra/packages/envd
RUN test "$(go env GOWORK)" = "off" \
    && if [ -f vendor/modules.txt ]; then \
         go list -mod=vendor -deps ./... >/dev/null; \
       else \
         go mod download \
         && go mod vendor \
         && go list -mod=vendor -deps ./... >/dev/null; \
       fi

FROM envd-source AS envd-test
RUN go test -run '^$' ./... \
    && go test ./internal/api -run '^(TestValidateInitAccessToken|TestCheckMMDSHash)$' \
    && go test ./internal/host ./internal/services/legacy ./internal/utils

FROM envd-source AS envd-test-full
RUN go test ./...

FROM envd-source AS envd-builder
ARG TARGETOS
ARG TARGETARCH
ARG ENVD_COMMIT=b8ca332f435370397bf42be614b2a5b620d65d39
ARG SOURCE_DATE_EPOCH=1776266298
COPY --chmod=0755 start-lightweight-code-interpreter.sh /out/start-lightweight-code-interpreter.sh
RUN go test -run '^$' ./... \
    && CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
       go build -buildvcs=false -trimpath \
       -ldflags "-buildid= -s -w -X=main.commitSHA=${ENVD_COMMIT}" \
       -o /out/envd . \
    && touch --date="@${SOURCE_DATE_EPOCH}" /out/envd /out/start-lightweight-code-interpreter.sh

FROM scratch AS envd-artifact
COPY --from=envd-builder /out/envd /envd

FROM ${BASE_IMAGE} AS sandbox-code
ARG ENVD_COMMIT=b8ca332f435370397bf42be614b2a5b620d65d39
ARG ENVD_PATCH_SHA256=b7957a5433715f0dde3d80fea56c7dd3cc0d05e06fc3d45fc5efdf9bc90e2ec3
LABEL io.cubesandbox.envd.upstream="https://github.com/e2b-dev/infra.git" \
      io.cubesandbox.envd.commit="${ENVD_COMMIT}" \
      io.cubesandbox.envd.patch.sha256="${ENVD_PATCH_SHA256}"
COPY --from=envd-builder --chmod=0755 /out/envd /usr/bin/envd
COPY --from=envd-builder --chmod=0755 /out/start-lightweight-code-interpreter.sh /usr/local/bin/start-lightweight-code-interpreter.sh
