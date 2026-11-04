# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
#
# go-fabric's certification harness image: matter.js's CHIP image (the
# Makefile's CHIP_TEST_IMAGE, ghcr.io/matter-js/chip) with what the Joint
# Fabric and Push AV families need on top, built from the same CHIP commit:
#
#   - /bin/jfa-app and /bin/jfc-app, chip's examples/jf-admin-app and
#     examples/jf-control-app (the JFADMIN and JFDS cases' ${JF_ADMIN_APP}
#     and ${JF_CONTROL_APP}, CHIP's .github/workflows/tests.yaml:1192-1193 at
#     the pin), with the pigweed RPC server their args.gni enable
#     (with_pw_rpc.gni);
#   - /src/tools/push_av_server, chip's Push AV TH server (the PAVST and
#     PAVSTI cases' ${PUSH_AV_SERVER}, tests.yaml:1199), with its Python
#     requirements and the hypercorn patch its README.md:19-25 applies.
#
# The CHIP source and build stages are matter.js's own (support/chip/
# Dockerfile at matter.js 862ad75f, stages base, build and source; its
# clone-chip, bootstrap-chip and build-one scripts are fetched at that
# commit), so the apps are built the way the image's other apps were.
# Build it with .github/workflows/chip-image.yml or, locally:
#
#   docker buildx build --load -t go-fabric-chip:local internal/chiptool/image

ARG CHIP_TEST_IMAGE=ghcr.io/matter-js/chip@sha256:d6f1de89d714309beb621543d451a98996690eea82fa74d1563abd7d2b3cb326
ARG MATTERJS_COMMIT=862ad75f4d3c7ab528b8a3954168146082cad8a6

FROM ubuntu:24.04 AS base
SHELL [ "/bin/bash", "-c" ]
ENV TZ=Etc/UTC
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone
RUN --mount=type=cache,sharing=locked,target=/var/cache/apt \
    --mount=type=cache,sharing=locked,target=/var/lib/apt \
    apt-get -qq update && \
    apt-get -qq -y install python3 python-is-python3 libavahi-client3 libglib2.0-0 libpcsclite1 libevent-dev

FROM --platform=$BUILDPLATFORM base AS build
ARG TARGETPLATFORM
RUN --mount=type=cache,sharing=locked,target=/var/cache/apt \
    --mount=type=cache,sharing=locked,target=/var/lib/apt \
    apt-get -qq update && \
    apt-get -qq -y install --no-install-recommends \
        git curl clang pkg-config libssl-dev libdbus-1-dev libavahi-client-dev \
        ninja-build cmake python3-venv python3-dev unzip libreadline-dev python3-pip \
        g++ libglib2.0-dev libglib2.0-dev-bin libpcsclite-dev libevent-dev ca-certificates
RUN pip install --break-system-packages git+https://github.com/pixelb/crudini.git#egg=crudini

FROM --platform=$BUILDPLATFORM build AS source
ARG MATTERJS_COMMIT
ARG CHIP_COMMIT=6170af8461b10b1766044122ac83332c6d00ab20
ADD --chmod=755 https://raw.githubusercontent.com/matter-js/matter.js/${MATTERJS_COMMIT}/support/chip/support/clone-chip /bin/clone-chip
ADD --chmod=755 https://raw.githubusercontent.com/matter-js/matter.js/${MATTERJS_COMMIT}/support/chip/support/bootstrap-chip /bin/bootstrap-chip
ADD --chmod=755 https://raw.githubusercontent.com/matter-js/matter.js/${MATTERJS_COMMIT}/support/chip/support/build-one /bin/build-one
RUN bash /bin/clone-chip
WORKDIR /connectedhomeip
RUN bash /bin/bootstrap-chip
ENV PW_PROJECT_ROOT=/connectedhomeip
ENV PATH=/connectedhomeip/.environment/pigweed-venv/bin:/connectedhomeip/.environment/cipd/packages/pigweed:/connectedhomeip/.environment/cipd/packages/zap:$PATH
RUN sed -i 's/"-g\${symbol_level}"/""/' build/config/compiler/BUILD.gn

FROM source AS bins
ARG TARGETPLATFORM
# The JF apps' pigweed RPC services are generated with protoc, which
# matter.js's bootstrap (CIPD packages only, fewer of them) does not
# install and none of its apps needs. Pigweed's protos use editions, so
# Ubuntu's protoc 3.21 is too old: protobuf's own release, by checksum.
ARG PROTOC_VERSION=29.3
RUN set -e; \
    case "$TARGETPLATFORM" in \
      linux/amd64) arch=x86_64; sum=3e866620c5be27664f3d2fa2d656b5f3e09b5152b42f1bedbf427b333e90021a ;; \
      linux/arm64) arch=aarch_64; sum=6427349140e01f06e049e707a58709a4f221ae73ab9a0425bc4a00c8d0e1ab32 ;; \
      *) echo "unsupported $TARGETPLATFORM"; exit 1 ;; \
    esac; \
    curl -fsSL -o /tmp/protoc.zip "https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-${arch}.zip"; \
    echo "$sum  /tmp/protoc.zip" | sha256sum -c -; \
    unzip -q -o /tmp/protoc.zip -d /usr/local bin/protoc 'include/*'; \
    rm /tmp/protoc.zip; protoc --version
RUN build-one jf-admin-app/linux jfa-app
RUN build-one jf-control-app jfc-app

FROM ${CHIP_TEST_IMAGE} AS chip
ARG CHIP_COMMIT=6170af8461b10b1766044122ac83332c6d00ab20
LABEL org.opencontainers.image.title="go-fabric CHIP certification harness"
LABEL org.opencontainers.image.source=https://github.com/SukramJ/go-fabric/tree/main/internal/chiptool/image
LABEL org.opencontainers.image.revision=${CHIP_COMMIT}
COPY --from=bins /dist/bin/jfa-app /dist/bin/jfc-app /bin/
COPY --from=source /connectedhomeip/src/tools/push_av_server /src/tools/push_av_server
RUN --mount=type=cache,sharing=locked,target=/var/cache/apt \
    --mount=type=cache,sharing=locked,target=/var/lib/apt \
    apt-get -qq update && apt-get -qq -y install --no-install-recommends python3-pip patch && \
    pip install --break-system-packages --no-cache-dir -r /src/tools/push_av_server/requirements.txt && \
    patch -d "$(pip show hypercorn | sed -n 's/^Location: //p')" < /src/tools/push_av_server/hypercorn.patch && \
    apt-get -qq -y purge python3-pip && apt-get -qq -y autoremove
