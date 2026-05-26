# syntax=docker/dockerfile:1.7

FROM ubuntu:22.04

ARG DEBIAN_FRONTEND=noninteractive
ARG APT_PRIMARY_MIRROR=http://mirrors.tencent.com/ubuntu
ARG APT_SECURITY_MIRROR=http://mirrors.tencent.com/ubuntu
ARG GO_VERSION=1.24.8
ARG PROTOC_VERSION=28.3
ARG LIBSECCOMP_VERSION=2.5.5
ARG RUST_TOOLCHAIN_DEFAULT=1.89
ARG RUST_TOOLCHAIN_HYPERVISOR=1.77.2
ARG RUST_TOOLCHAIN_E2BAPI=1.85
ARG RUST_TOOLCHAIN_AGENT=1.89
ARG GITHUB_ACTIONS=false
ARG RUSTUP_DIST_SERVER=https://rsproxy.cn
ARG RUSTUP_UPDATE_ROOT=https://rsproxy.cn/rustup
ARG GOPROXY=https://goproxy.cn,direct
ARG GOSUMDB=sum.golang.google.cn

ENV LANG=C.UTF-8 \
    LC_ALL=C.UTF-8 \
    GOPROXY="${GOPROXY}" \
    GOSUMDB="${GOSUMDB}" \
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
    LIBSECCOMP_LINK_TYPE=static \
    LIBSECCOMP_LIB_PATH=/usr/local/lib64/libseccomp/lib

RUN apt-get update -o Acquire::Retries=3 \
    && apt install -y ca-certificates --no-install-recommends
    
RUN if [ "${GITHUB_ACTIONS}" != "true" ]; then \
        sed -i "s|http://archive.ubuntu.com/ubuntu|${APT_PRIMARY_MIRROR}|g; \
                s|http://security.ubuntu.com/ubuntu|${APT_SECURITY_MIRROR}|g" \
            /etc/apt/sources.list; \
    fi

RUN set -eux; \
    arch="$(dpkg --print-architecture)"; \
    extra_packages=""; \
    case "${arch}" in \
        amd64) extra_packages="gcc-multilib" ;; \
        arm64) extra_packages="" ;; \
        *) echo "unsupported builder architecture: ${arch}" >&2; exit 1 ;; \
    esac; \
    apt-get update -o Acquire::Retries=3 \
    && apt-get install -y ca-certificates \
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
        ${extra_packages} \
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
    && rm -rf /var/lib/apt/lists/*

RUN if [ -x /usr/bin/llvm-strip-14 ] && [ ! -e /usr/local/bin/llvm-strip ]; then ln -s /usr/bin/llvm-strip-14 /usr/local/bin/llvm-strip; fi \
    && if [ ! -e /usr/bin/musl-g++ ]; then ln -s /usr/bin/g++ /usr/bin/musl-g++; fi

ARG GO_DOWNLOAD_BASE_URL=https://mirrors.aliyun.com/golang
RUN set -eux; \
    arch="$(dpkg --print-architecture)"; \
    case "${arch}" in \
        amd64) go_arch=amd64 ;; \
        arm64) go_arch=arm64 ;; \
        *) echo "unsupported builder architecture: ${arch}" >&2; exit 1 ;; \
    esac; \
    for attempt in 1 2 3; do \
        if curl --retry 5 --retry-delay 2 --connect-timeout 20 -fsSL "${GO_DOWNLOAD_BASE_URL}/go${GO_VERSION}.linux-${go_arch}.tar.gz" -o /tmp/go.tgz; then \
            break; \
        fi; \
        rm -f /tmp/go.tgz; \
        if [ "${attempt}" = "3" ]; then \
            exit 1; \
        fi; \
        sleep "$((attempt * 5))"; \
    done \
    && rm -rf /usr/local/go \
    && tar -C /usr/local -xzf /tmp/go.tgz \
    && rm -f /tmp/go.tgz

ARG PROTOC_DOWNLOAD_BASE_URL=https://github.com/protocolbuffers/protobuf/releases/download
RUN set -eux; \
    arch="$(dpkg --print-architecture)"; \
    case "${arch}" in \
        amd64) protoc_arch=x86_64 ;; \
        arm64) protoc_arch=aarch_64 ;; \
        *) echo "unsupported builder architecture: ${arch}" >&2; exit 1 ;; \
    esac; \
    for attempt in 1 2 3; do \
        if curl --retry 5 --retry-delay 2 --connect-timeout 20 -fL "${PROTOC_DOWNLOAD_BASE_URL}/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-${protoc_arch}.zip" -o /tmp/protoc.zip; then \
            break; \
        fi; \
        rm -f /tmp/protoc.zip; \
        if [ "${attempt}" = "3" ]; then \
            exit 1; \
        fi; \
        sleep "$((attempt * 5))"; \
    done \
    && unzip -q /tmp/protoc.zip -d /tmp/protoc \
    && install -m 0755 /tmp/protoc/bin/protoc /usr/local/bin/protoc \
    && cp -r /tmp/protoc/include/* /usr/local/include/ \
    && rm -rf /tmp/protoc /tmp/protoc.zip

RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11 \
    && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.1 \
    && go install github.com/pseudomuto/protoc-gen-doc/cmd/protoc-gen-doc@v1.5.1

RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs \
    | sh -s -- -y --profile minimal --default-toolchain none

ENV RUSTUP_DIST_SERVER="${RUSTUP_DIST_SERVER}"
ENV RUSTUP_UPDATE_ROOT="${RUSTUP_UPDATE_ROOT}"

RUN set -eux; \
    for toolchain in "${RUST_TOOLCHAIN_HYPERVISOR}" "${RUST_TOOLCHAIN_E2BAPI}" "${RUST_TOOLCHAIN_AGENT}"; do \
        rustup toolchain install "${toolchain}" --profile minimal; \
        rustup component add rust-src clippy rustfmt rust-analyzer llvm-tools-preview --toolchain "${toolchain}"; \
        rustup target add x86_64-unknown-linux-musl --toolchain "${toolchain}"; \
        rustup target add aarch64-unknown-linux-musl --toolchain "${toolchain}"; \
    done; \
    rustup default "${RUST_TOOLCHAIN_DEFAULT}"

RUN mkdir -p "${CARGO_HOME}" /root/.cargo \
    && printf '[registries.crates-io]\nprotocol = "sparse"\n\n[net]\ngit-fetch-with-cli = true\n' > "${CARGO_HOME}/config.toml" \
    && ln -sf "${CARGO_HOME}/config.toml" /root/.cargo/config.toml \
    && ln -sf "${CARGO_HOME}/env" /root/.cargo/env

ARG LIBSECCOMP_DOWNLOAD_BASE_URL=https://github.com/seccomp/libseccomp/releases/download
RUN tmp_dir="$(mktemp -d)" \
    && arch="$(dpkg --print-architecture)" \
    && case "${arch}" in \
        amd64) musl_host=x86_64-linux-musl; musl_include=/usr/include/x86_64-linux-musl; gnu_include=/usr/include/x86_64-linux-gnu ;; \
        arm64) musl_host=aarch64-linux-musl; musl_include=/usr/include/aarch64-linux-musl; gnu_include=/usr/include/aarch64-linux-gnu ;; \
        *) echo "unsupported builder architecture: ${arch}" >&2; exit 1 ;; \
    esac \
    && for attempt in 1 2 3; do \
        if wget -q "${LIBSECCOMP_DOWNLOAD_BASE_URL}/v${LIBSECCOMP_VERSION}/libseccomp-${LIBSECCOMP_VERSION}.tar.gz" -O "${tmp_dir}/libseccomp.tgz"; then \
            break; \
        fi; \
        rm -f "${tmp_dir}/libseccomp.tgz"; \
        if [ "${attempt}" = "3" ]; then \
            exit 1; \
        fi; \
        sleep "$((attempt * 5))"; \
    done \
    && tar -xzf "${tmp_dir}/libseccomp.tgz" -C "${tmp_dir}" --strip-components=1 \
    && cd "${tmp_dir}" \
    && CC=musl-gcc ./configure --host="${musl_host}" CPPFLAGS="-I${musl_include} -idirafter /usr/include -idirafter ${gnu_include}" CFLAGS="-O2 -I${musl_include} -idirafter /usr/include -idirafter ${gnu_include}" --disable-shared --enable-static --prefix=/usr/local/lib64/libseccomp \
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
