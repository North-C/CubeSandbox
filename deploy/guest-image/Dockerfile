ARG ONE_CLICK_GUEST_IMAGE_BASE_REF=cube-sandbox-image.tencentcloudcr.com/opensource/tencentos4-minimal:4.4-v20250331
FROM ${ONE_CLICK_GUEST_IMAGE_BASE_REF}

WORKDIR /work

RUN set -eux; \
    if command -v dnf >/dev/null 2>&1; then \
      dnf install -y \
        util-linux \
        busybox \
        gawk \
        gcc \
        which \
        vim-minimal \
        sudo \
        lsof \
        procps-ng \
        iproute \
        iputils; \
      dnf clean all; \
    else \
      yum install -y \
        util-linux \
        busybox \
        gawk \
        gcc \
        util-linux \
        which \
        vim \
        sudo \
        lsof; \
      yum clean all; \
    fi

CMD ["bash"]
