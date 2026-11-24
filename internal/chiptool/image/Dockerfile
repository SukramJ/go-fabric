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
#     requirements and the hypercorn patch its README.md:19-25 applies;
#   - /bin/chip-tool-ble and /bin/all-clusters-app-ble, chip-tool and
#     the all-clusters app with BLE and Wi-Fi (milestone 7, wave 28c): the
#     base image builds every binary without them (matter.js
#     support/chip/support/build-one:23, :60-63), so the BLE-Wi-Fi
#     commissioning leg and its control get their own builds, the base
#     image's binaries unchanged; with them bluezoo==1.0.2, chip's BlueZ
#     mock (connectedhomeip scripts/setup/constraints.txt:25 at the pin),
#     which chip's matter.testing.linux.BluetoothMock starts.
#   - /opt/matter-ble, a second Python controller built with BLE (a venv
#     of the CHIP wheels over the base image's site-packages): the base
#     image's controller is built without BLE (matter.js
#     support/chip/support/install-chip-python at 862ad75f), so the cases
#     whose TH commissions over BLE onto Wi-Fi (`make chiptool-ble-cases`)
#     run with /opt/matter-ble/bin/python3, every other family with the
#     base image's python3, unchanged.
#   - /opt/matter-webrtc, a third Python controller, built with chip's
#     WebRTC bindings (matter.webrtc), and /lib/test-descriptor-webrtc.json,
#     the base image's generate-test-descriptor run with it: the base
#     image's controller has no matter.webrtc, so its descriptor skips the
#     case files that import it (review II, decisions 642, 644, 647). The
#     family runner runs a case only that descriptor files with
#     /opt/matter-webrtc/bin/python3, every other case as before.
#
# The CHIP source and build stages are matter.js's own (support/chip/
# Dockerfile at matter.js 862ad75f, stages base, build and source; its
# clone-chip, bootstrap-chip and build-one scripts are fetched at that
# commit), so the apps are built the way the image's other apps were.
# Two inputs differ from matter.js's stages, both pins of what that
# Dockerfile takes floating (review II, R2-P5-sup-07): ubuntu:24.04 by its
# digest of 2026-11-15 (Dependabot's docker entry proposes newer ones), and
# crudini at the commit its default branch pointed to on 2026-11-15
# (a6b83377, committed 2025-06-03) rather than at whatever HEAD is when
# the image is rebuilt.
# Build it with .github/workflows/chip-image.yml or, locally:
#
#   docker buildx build --load -t go-fabric-chip:local internal/chiptool/image

ARG CHIP_TEST_IMAGE=ghcr.io/matter-js/chip@sha256:d6f1de89d714309beb621543d451a98996690eea82fa74d1563abd7d2b3cb326
ARG MATTERJS_COMMIT=862ad75f4d3c7ab528b8a3954168146082cad8a6

FROM ubuntu:24.04@sha256:2198e4c65076e9cb0b3d695219df667c0e3b0072da64ca3c2648aca17dcecaf5 AS base
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
RUN pip install --break-system-packages git+https://github.com/pixelb/crudini.git@a6b833779bbf01356a9ee8d3256a6a39979af1ad#egg=crudini

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
# Warnings are not errors here, as matter.js builds its camera-app (its
# Dockerfile at 862ad75f): chip's CI builds these apps with its own
# toolchain, and Ubuntu's clang reports pigweed's socket_stream.h under
# -Wthread-safety-analysis. The code is upstream's.
RUN build-one jf-admin-app/linux jfa-app jfa-app treat_warnings_as_errors=false
RUN build-one jf-control-app jfc-app jfc-app treat_warnings_as_errors=false
# chip-tool and the all-clusters app with BLE and Wi-Fi, as chip's CI builds
# them for its ble-wifi run (.github/workflows/tests.yaml:301-302, :486-488
# at the pin: linux-x64-chip-tool and linux-x64-all-clusters with no
# "no-ble", "no-wifi" or "platform-mdns" modifier). build-one fixes BLE,
# Wi-Fi, IPv4 and the platform mDNS off in GN_ARGS and appends the caller's
# arguments after them (build-one:58-71); whether gn accepts a second
# assignment of the same argument was not established, so these two are
# generated here with build-one's steps (:56-97: gn gen with its flags,
# ninja, the executable by name, strip, /dist/bin) and its arguments less
# the five network ones (chip_inet_config_enable_ipv4, the two BLE
# switches, chip_enable_wifi, chip_mdns), which keep chip's defaults.
# Thread stays off as in build-one. Warnings are not errors, as above.
RUN set -e; \
    ble_one() { \
      root=$1; target=$2; out_name=$3; \
      rm -rf out; mkdir out; \
      gn gen out --fail-on-unused-args --add-export-compile-commands=* --root="examples/$root" \
        --args='chip_enable_thread=false chip_build_libshell=false is_clang=true matter_enable_tracing_support=false matter_trace_config="///src/tracing/none" matter_commandline_enable_perfetto_tracing=false treat_warnings_as_errors=false'; \
      ninja -C out "$target"; \
      bin="$(find out -type f -executable -name "$target")"; \
      [ -x "$bin" ] || { echo "no executable $target" >&2; exit 1; }; \
      strip "$bin"; mkdir -p /dist/bin; mv "$bin" "/dist/bin/$out_name"; rm -rf out; \
    }; \
    ble_one chip-tool chip-tool chip-tool-ble; \
    ble_one all-clusters-app/linux chip-all-clusters-app all-clusters-app-ble

# The Python controller with BLE, for the certification cases whose TH
# commissions over BLE onto Wi-Fi in the test (`make chiptool-ble-cases`).
# The base image's controller is matter.js's install-chip-python (862ad75f),
# which builds it with chip_config_network_layer_ble=false and
# chip_enable_ble=false (its _ChipDeviceCtrl.so has no BlueZ code and no
# pychip_ble_adapter_list_new, src/controller/python/BUILD.gn:147-150 at
# the pin). This one is install-chip-python's gn arguments and BUILD.gn
# edits less those two switches, so BLE and Wi-Fi keep chip's defaults, as
# chip's scripts/build_python.sh builds it (enable_ble=true, :39, :300-301),
# and with chip's own mDNS, build_python.sh's default chip_mdns=minimal
# (:44): the cases run in chip's network namespaces on a private system
# bus, where no avahi-daemon answers. Only the CHIP wheels go into the venv
# /opt/matter-ble, --no-deps over the system site-packages: their
# dependencies are the base image's, and the base image's controller in
# /usr/local stays the one every other family runs.
FROM source AS pyble
RUN set -e; \
    gn gen out --fail-on-unused-args \
      --args='matter_log_json_payload_hex=true matter_log_json_payload_decode_full=true chip_detail_logging=true chip_project_config_include_dirs=["//config/python"] chip_mdns="minimal" matter_enable_tracing_support=false matter_trace_config="///src/tracing/none" matter_commandline_enable_perfetto_tracing=false'; \
    gn args out --list --short | grep -qx 'chip_enable_ble = true' || { echo "the controller build has no BLE" >&2; exit 1; }; \
    sed -i -e /ipython/d -e /ipykernel/d -e /ipdb/d -e /pygobject/d -e s/dbus-python==1.2.18/dbus-python==1.4.0/ src/controller/python/BUILD.gn; \
    ninja -C out python_wheels; \
    find out -name '*.whl' | grep -v matter_yamltests-0.0.1 | grep -v matter-controller-wheels > /tmp/wheels; \
    PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin python3 -m venv --system-site-packages /opt/matter-ble; \
    /opt/matter-ble/bin/pip install --no-deps --no-cache-dir $(cat /tmp/wheels); \
    /opt/matter-ble/bin/pip uninstall -q -y pip; \
    find /opt/matter-ble -name '*.so' | xargs strip; \
    rm -rf out

# The Python controller with chip's WebRTC bindings, for the certification
# cases whose file imports matter.webrtc (TC_WEBRTCP_2_11, 2_12, 2_17 …
# 2_25, 2_33, TC_AVSM_2_16, 2_17 at the pin). chip declares
# chip_support_webrtc_python_bindings = false (src/controller/flags.gni:25)
# and matter.js's install-chip-python (862ad75f, the same at 6e8eff403, the
# commit the base image was built from) does not set it, so the base
# image's controller has no matter.webrtc; chip's own scripts/build_python.sh
# builds it on Linux (enable_webrtc=true, :56, passed as
# chip_support_webrtc_python_bindings, :306). The switch adds the
# matter/webrtc sources and the libdatachannel dependency
# (src/controller/python/BUILD.gn:122-129, :183-192, :286-300), whose
# submodule clone-chip keeps (862ad75f clone-chip:34-35) and which
# third_party/libdatachannel/BUILD.gn builds with CMake against OpenSSL —
# the controller's crypto already, chip_crypto defaulting to openssl on
# Linux (src/crypto/crypto.gni:51-61), which build_python.sh selects for
# WebRTC explicitly (:285-289). This one is install-chip-python's gn
# arguments (:24-39) and BUILD.gn edits (:44-60) unchanged plus that one
# switch, so it is the base image's controller in everything else (BLE off,
# platform mDNS); the wheels go into the venv /opt/matter-webrtc as
# /opt/matter-ble's do. matter.webrtc imports websockets, which the base
# image carries (17.2, /usr/local/lib/python3.12/dist-packages).
FROM source AS pywebrtc
RUN set -e; \
    gn gen out --fail-on-unused-args \
      --args='matter_log_json_payload_hex=true matter_log_json_payload_decode_full=true chip_detail_logging=true chip_project_config_include_dirs=["//config/python"] chip_mdns="platform" chip_config_network_layer_ble=false chip_enable_ble=false matter_enable_tracing_support=false matter_trace_config="///src/tracing/none" matter_commandline_enable_perfetto_tracing=false chip_support_webrtc_python_bindings=true'; \
    gn args out --list --short | grep -qx 'chip_support_webrtc_python_bindings = true' || { echo "the controller build has no WebRTC bindings" >&2; exit 1; }; \
    sed -i -e /ipython/d -e /ipykernel/d -e /ipdb/d -e /pygobject/d -e s/dbus-python==1.2.18/dbus-python==1.4.0/ src/controller/python/BUILD.gn; \
    ninja -C out python_wheels; \
    find out -name '*.whl' | grep -v matter_yamltests-0.0.1 | grep -v matter-controller-wheels > /tmp/wheels; \
    PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin python3 -m venv --system-site-packages /opt/matter-webrtc; \
    /opt/matter-webrtc/bin/pip install --no-deps --no-cache-dir $(cat /tmp/wheels); \
    /opt/matter-webrtc/bin/pip uninstall -q -y pip; \
    find /opt/matter-webrtc -name '*.so' | xargs strip; \
    rm -rf out

FROM ${CHIP_TEST_IMAGE} AS chip
ARG CHIP_COMMIT=6170af8461b10b1766044122ac83332c6d00ab20
LABEL org.opencontainers.image.title="go-fabric CHIP certification harness"
LABEL org.opencontainers.image.source=https://github.com/thingfabric/go-fabric/tree/main/internal/chiptool/image
LABEL org.opencontainers.image.revision=${CHIP_COMMIT}
COPY --from=bins /dist/bin/jfa-app /dist/bin/jfc-app /dist/bin/chip-tool-ble /dist/bin/all-clusters-app-ble /bin/
COPY --from=pyble /opt/matter-ble /opt/matter-ble
COPY --from=pywebrtc /opt/matter-webrtc /opt/matter-webrtc
COPY --from=source /connectedhomeip/src/tools/push_av_server /src/tools/push_av_server
# The base image installs libcurl4 for chip-camera-app (matter.js
# support/chip/Dockerfile:28-33 at 862ad75f); its apt state does not survive
# the base's cache mounts, so an autoremove takes libcurl4t64 with pip's
# dependencies unless it is marked manual first, as below — the e443ea0c
# image was built without the mark and its chip-camera-app did not load
# ("libcurl.so.4 => not found"); since 482f8caf it loads (issue 415).
RUN --mount=type=cache,sharing=locked,target=/var/cache/apt \
    --mount=type=cache,sharing=locked,target=/var/lib/apt \
    apt-get -qq update && apt-get -qq -y install --no-install-recommends python3-pip patch libglib2.0-0t64 && \
    pip install --break-system-packages --no-cache-dir -r /src/tools/push_av_server/requirements.txt && \
    patch -d "$(python3 -c 'import hypercorn, os; print(os.path.dirname(os.path.dirname(hypercorn.__file__)))')" -p0 < /src/tools/push_av_server/hypercorn.patch && \
    pip install --break-system-packages --no-cache-dir bluezoo==1.0.2 && \
    apt-mark manual libcurl4t64 libglib2.0-0t64 && apt-get -qq -y purge python3-pip && apt-get -qq -y autoremove && \
    bluezoo --help >/dev/null && \
    for b in /bin/chip-tool-ble /bin/all-clusters-app-ble; do \
      ! ldd "$b" | grep 'not found' || { echo "$b misses a library" >&2; exit 1; }; \
      grep -q org.bluez "$b" || { echo "$b has no BlueZ support" >&2; exit 1; }; \
      grep -q fi.w1.wpa_supplicant1 "$b" || { echo "$b has no wpa_supplicant support" >&2; exit 1; }; \
    done && \
    lib=/opt/matter-ble/lib/python3.12/site-packages/matter/_ChipDeviceCtrl.so && \
    grep -q org.bluez "$lib" || { echo "/opt/matter-ble's controller has no BlueZ support" >&2; exit 1; }; \
    /opt/matter-ble/bin/python3 -c 'import ctypes, os, matter, matter.testing.linux; d = os.path.dirname(matter.__file__); assert d.startswith("/opt/matter-ble/"), d; ctypes.CDLL(d + "/_ChipDeviceCtrl.so").pychip_ble_adapter_list_new' && \
    python3 -c 'import matter; assert matter.__file__.startswith("/usr/local/"), matter.__file__'
# The WebRTC controller loads, is the venv's, and carries the bindings; the
# base image's generate-test-descriptor, run with it, files the case files
# the base image's descriptor skips for want of matter.webrtc. The base
# image's /lib/test-descriptor.json stays as it is.
RUN set -e; \
    /opt/matter-webrtc/bin/python3 -c 'import ctypes, os, matter, matter.webrtc; d = os.path.dirname(matter.__file__); assert d.startswith("/opt/matter-webrtc/"), d; ctypes.CDLL(d + "/_ChipDeviceCtrl.so").pychip_webrtc_provider_client_create'; \
    /opt/matter-webrtc/bin/python3 /bin/generate-test-descriptor > /lib/test-descriptor-webrtc.json 2> /tmp/webrtc-descriptor.err; \
    if grep "No module named 'matter.webrtc'" /tmp/webrtc-descriptor.err; then echo "the WebRTC descriptor still skips matter.webrtc files" >&2; exit 1; fi; \
    for f in TC_WEBRTCP_2_12.py TC_AVSM_2_16.py; do grep -q "/src/python_testing/$f" /lib/test-descriptor-webrtc.json || { echo "the WebRTC descriptor files no $f" >&2; exit 1; }; done; \
    rm /tmp/webrtc-descriptor.err; \
    python3 -c 'import matter; assert matter.__file__.startswith("/usr/local/"), matter.__file__'
