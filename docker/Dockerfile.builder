# syntax=docker/dockerfile:1.7

FROM ubuntu:22.04

ARG DEBIAN_FRONTEND=noninteractive
ARG TARGETARCH
ARG APT_PRIMARY_MIRROR=http://mirrors.tencent.com/ubuntu
ARG APT_SECURITY_MIRROR=http://mirrors.tencent.com/ubuntu
ARG GO_VERSION=1.24.8
ARG GO_MODULE_PROXY=https://goproxy.cn,direct
ARG PROTOC_VERSION=28.3
ARG LIBSECCOMP_VERSION=2.5.5
ARG RUST_TOOLCHAIN_DEFAULT=1.89
ARG RUST_TOOLCHAIN_HYPERVISOR=1.77.2
ARG RUST_TOOLCHAIN_E2BAPI=1.85
ARG RUST_TOOLCHAIN_AGENT=1.89
ARG GITHUB_ACTIONS=false
ARG RUSTUP_DIST_SERVER=https://rsproxy.cn
ARG RUSTUP_UPDATE_ROOT=https://rsproxy.cn/rustup

ENV LANG=C.UTF-8 \
    LC_ALL=C.UTF-8 \
    GOPATH=/go \
    RUSTUP_HOME=/usr/local/rustup \
    CARGO_HOME=/usr/local/cargo \
    PATH=/usr/local/go/bin:/go/bin:/usr/local/cargo/bin:${PATH} \
    CARGO_NET_GIT_FETCH_WITH_CLI=true \
    OPENSSL_INCLUDE_DIR=/usr/include \
    X86_64_UNKNOWN_LINUX_GNU_OPENSSL_LIB_DIR=/usr/lib/x86_64-linux-gnu \
    X86_64_UNKNOWN_LINUX_MUSL_OPENSSL_LIB_DIR=/usr/lib/x86_64-linux-gnu \
    AARCH64_UNKNOWN_LINUX_GNU_OPENSSL_LIB_DIR=/usr/lib/aarch64-linux-gnu \
    AARCH64_UNKNOWN_LINUX_MUSL_OPENSSL_LIB_DIR=/usr/lib/aarch64-linux-gnu \
    CARGO_TARGET_X86_64_UNKNOWN_LINUX_MUSL_LINKER=x86_64-linux-musl-gcc \
    CARGO_TARGET_AARCH64_UNKNOWN_LINUX_MUSL_LINKER=aarch64-linux-musl-gcc \
    LIBSECCOMP_LINK_TYPE=static \
    LIBSECCOMP_LIB_PATH=/usr/local/lib64/libseccomp/lib

RUN if [ "${GITHUB_ACTIONS}" != "true" ]; then \
        sed -i "s|http://archive.ubuntu.com/ubuntu|${APT_PRIMARY_MIRROR}|g; \
                s|http://security.ubuntu.com/ubuntu|${APT_SECURITY_MIRROR}|g" \
            /etc/apt/sources.list; \
    fi

RUN apt-get update -o Acquire::Retries=3 \
    && apt install -y ca-certificates \
    && apt-get install -y --no-install-recommends \
        bash \
        bc \
        binutils-dev \
        build-essential \
        ca-certificates \
        clang \
        cpio \
        curl \
        dmsetup \
        dnsmasq \
        dosfstools \
        file \
        flex \
        bison \
        gperf \
        git \
        git-lfs \
        jq \
        libcap-dev \
        libcap-ng-dev \
        libelf-dev \
        libglib2.0-dev \
        libiberty-dev \
        libpixman-1-dev \
        libseccomp-dev \
        libssl-dev \
        libtool \
        llvm \
        make \
        mtools \
        musl-tools \
        docker.io \
        ntfs-3g \
        pkg-config \
        python-is-python3 \
        python3 \
        python3-distutils \
        python3-pip \
        python3-setuptools \
        qemu-utils \
        socat \
        sudo \
        unzip \
        uuid-dev \
        wget \
        xz-utils \
        zip \
        zlib1g-dev \
    && if [ "$(dpkg --print-architecture)" = "amd64" ]; then apt-get install -y --no-install-recommends gcc-multilib; fi \
    && rm -rf /var/lib/apt/lists/*

RUN set -eux; \
    if [ -x /usr/bin/llvm-strip-14 ] && [ ! -e /usr/local/bin/llvm-strip ]; then ln -s /usr/bin/llvm-strip-14 /usr/local/bin/llvm-strip; fi; \
    if [ ! -e /usr/bin/musl-g++ ]; then ln -s /usr/bin/g++ /usr/bin/musl-g++; fi; \
    case "$(dpkg --print-architecture)" in \
        amd64) musl_cc=x86_64-linux-musl-gcc; musl_cxx=x86_64-linux-musl-g++ ;; \
        arm64) musl_cc=aarch64-linux-musl-gcc; musl_cxx=aarch64-linux-musl-g++ ;; \
        *) musl_cc=; musl_cxx= ;; \
    esac; \
    if [ -n "${musl_cc}" ] && ! command -v "${musl_cc}" >/dev/null 2>&1; then ln -s /usr/bin/musl-gcc "/usr/local/bin/${musl_cc}"; fi; \
    if [ -n "${musl_cxx}" ] && ! command -v "${musl_cxx}" >/dev/null 2>&1; then ln -s /usr/bin/musl-g++ "/usr/local/bin/${musl_cxx}"; fi

RUN set -eux; \
    case "${TARGETARCH:-$(dpkg --print-architecture)}" in \
        amd64) go_arch=amd64 ;; \
        arm64) go_arch=arm64 ;; \
        *) echo "unsupported Go target arch: ${TARGETARCH:-$(dpkg --print-architecture)}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL --retry 5 --retry-delay 5 --retry-all-errors "https://go.dev/dl/go${GO_VERSION}.linux-${go_arch}.tar.gz" -o /tmp/go.tgz \
    && rm -rf /usr/local/go \
    && tar -C /usr/local -xzf /tmp/go.tgz \
    && rm -f /tmp/go.tgz

RUN set -eux; \
    case "${TARGETARCH:-$(dpkg --print-architecture)}" in \
        amd64) protoc_arch=x86_64 ;; \
        arm64) protoc_arch=aarch_64 ;; \
        *) echo "unsupported protoc target arch: ${TARGETARCH:-$(dpkg --print-architecture)}" >&2; exit 1 ;; \
    esac; \
    wget -q --tries=5 --waitretry=5 --timeout=30 --read-timeout=30 "https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-${protoc_arch}.zip" -O /tmp/protoc.zip \
    && unzip -q /tmp/protoc.zip -d /tmp/protoc \
    && install -m 0755 /tmp/protoc/bin/protoc /usr/local/bin/protoc \
    && cp -r /tmp/protoc/include/* /usr/local/include/ \
    && rm -rf /tmp/protoc /tmp/protoc.zip

RUN set -eux; \
    go_install_retry() { \
        attempt=1; \
        while true; do \
            if GOPROXY="${GO_MODULE_PROXY}" go install "$@"; then \
                return 0; \
            fi; \
            if [ "${attempt}" -ge 3 ]; then \
                return 1; \
            fi; \
            sleep "$((attempt * 10))"; \
            attempt="$((attempt + 1))"; \
        done; \
    }; \
    go_install_retry google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11; \
    go_install_retry google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.1; \
    go_install_retry github.com/pseudomuto/protoc-gen-doc/cmd/protoc-gen-doc@v1.5.1

RUN curl --proto '=https' --tlsv1.2 -sSf --retry 5 --retry-delay 5 --retry-all-errors https://sh.rustup.rs \
    | sh -s -- -y --profile minimal --default-toolchain none

ENV RUSTUP_DIST_SERVER="${RUSTUP_DIST_SERVER}"
ENV RUSTUP_UPDATE_ROOT="${RUSTUP_UPDATE_ROOT}"

RUN set -eux; \
    rustup_retry() { \
        attempt=1; \
        while true; do \
            if rustup "$@"; then \
                return 0; \
            fi; \
            if [ "${attempt}" -ge 3 ]; then \
                return 1; \
            fi; \
            sleep "$((attempt * 10))"; \
            attempt="$((attempt + 1))"; \
        done; \
    }; \
    case "${TARGETARCH:-$(dpkg --print-architecture)}" in \
        amd64) rust_musl_target=x86_64-unknown-linux-musl ;; \
        arm64) rust_musl_target=aarch64-unknown-linux-musl ;; \
        *) echo "unsupported Rust musl target arch: ${TARGETARCH:-$(dpkg --print-architecture)}" >&2; exit 1 ;; \
    esac; \
    for toolchain in "${RUST_TOOLCHAIN_HYPERVISOR}" "${RUST_TOOLCHAIN_E2BAPI}" "${RUST_TOOLCHAIN_AGENT}"; do \
        rustup_retry toolchain install "${toolchain}" --profile minimal; \
        rustup_retry component add rust-src clippy rustfmt rust-analyzer llvm-tools-preview --toolchain "${toolchain}"; \
        rustup_retry target add "${rust_musl_target}" --toolchain "${toolchain}"; \
    done; \
    rustup_retry default "${RUST_TOOLCHAIN_DEFAULT}"

RUN mkdir -p "${CARGO_HOME}" /root/.cargo \
    && printf '[registries.crates-io]\nprotocol = "sparse"\n\n[net]\ngit-fetch-with-cli = true\n' > "${CARGO_HOME}/config.toml" \
    && ln -sf "${CARGO_HOME}/config.toml" /root/.cargo/config.toml \
    && ln -sf "${CARGO_HOME}/env" /root/.cargo/env

RUN set -eux; \
    case "${TARGETARCH:-$(dpkg --print-architecture)}" in \
        amd64) seccomp_host=x86_64-linux-musl; seccomp_multiarch=x86_64-linux-gnu ;; \
        arm64) seccomp_host=aarch64-linux-musl; seccomp_multiarch=aarch64-linux-gnu ;; \
        *) echo "unsupported libseccomp target arch: ${TARGETARCH:-$(dpkg --print-architecture)}" >&2; exit 1 ;; \
    esac; \
    tmp_dir="$(mktemp -d)" \
    && wget -q --tries=5 --waitretry=5 --timeout=30 --read-timeout=30 "https://github.com/seccomp/libseccomp/releases/download/v${LIBSECCOMP_VERSION}/libseccomp-${LIBSECCOMP_VERSION}.tar.gz" -O "${tmp_dir}/libseccomp.tgz" \
    && tar -xzf "${tmp_dir}/libseccomp.tgz" -C "${tmp_dir}" --strip-components=1 \
    && cd "${tmp_dir}" \
    && CC=musl-gcc ./configure --host="${seccomp_host}" CPPFLAGS="-I/usr/include/${seccomp_host} -idirafter /usr/include -idirafter /usr/include/${seccomp_multiarch}" CFLAGS="-O2 -I/usr/include/${seccomp_host} -idirafter /usr/include -idirafter /usr/include/${seccomp_multiarch}" --disable-shared --enable-static --prefix=/usr/local/lib64/libseccomp \
    && make -j"$(nproc)" \
    && make install \
    && rm -rf "${tmp_dir}"

RUN arch="$(dpkg --print-architecture)" \
    && case "${arch}" in \
        amd64) openssl_dir=/usr/include/x86_64-linux-gnu/openssl ;; \
        arm64) openssl_dir=/usr/include/aarch64-linux-gnu/openssl ;; \
        *) openssl_dir='' ;; \
    esac \
    && if [ -n "${openssl_dir}" ] && [ -f "${openssl_dir}/opensslconf.h" ] && [ ! -f /usr/include/openssl/opensslconf.h ]; then \
        cp "${openssl_dir}/opensslconf.h" /usr/include/openssl/opensslconf.h; \
    fi

WORKDIR /workspace

CMD ["/bin/bash"]
