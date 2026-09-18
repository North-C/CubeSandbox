SHELL := /bin/sh

.PHONY: verify test test-full build-envd build-envd-amd64 build-envd-arm64 images image-amd64 image-arm64

verify:
	./scripts/verify-source-and-patch.sh

test:
	./scripts/test-envd.sh

test-full:
	./scripts/test-envd-full.sh

build-envd: build-envd-amd64 build-envd-arm64

build-envd-amd64:
	./scripts/build-envd.sh amd64

build-envd-arm64:
	./scripts/build-envd.sh arm64

images: image-amd64 image-arm64

image-amd64:
	./scripts/build-image.sh amd64

image-arm64:
	./scripts/build-image.sh arm64
