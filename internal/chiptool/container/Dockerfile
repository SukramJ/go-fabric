# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
#
# One container that plays "the Linux host" of the chip-tool suite's host
# fallback (internal/chiptool/doc.go): the pinned chip-tool with the
# libraries it was built against, Go, and an avahi of its own. `make
# chiptool-container` builds and runs it on a user-defined bridge network —
# never the host network — so a machine that cannot or may not run the CHIP
# harness image on its host network (a Mac under Docker Desktop) still has a
# local chip-tool loop.
ARG CHIP_CERT_BINS_IMAGE
ARG GO_IMAGE
FROM ${GO_IMAGE} AS go
FROM ${CHIP_CERT_BINS_IMAGE}
# The suite logs this sidecar as the binary's identity in every run, as it
# does for `make chiptool-extract`.
ARG CHIP_CERT_BINS_IMAGE
RUN echo "${CHIP_CERT_BINS_IMAGE}" > /root/apps/chip-tool.source
COPY --from=go /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:/root/apps:$PATH \
    GOFLAGS=-buildvcs=false \
    GOMODCACHE=/go/pkg/mod
RUN apt-get update -qq \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends \
      avahi-daemon avahi-utils dbus ca-certificates git gcc libc6-dev >/dev/null \
 && apt-get clean
COPY run.sh /usr/local/bin/chiptool-container-run
RUN chmod +x /usr/local/bin/chiptool-container-run
ENTRYPOINT ["/usr/local/bin/chiptool-container-run"]
