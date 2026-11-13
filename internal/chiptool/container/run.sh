#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
#
# The entry point of `make chiptool-container`: a system bus and avahi of
# the container's own (chip-tool resolves through avahi over D-Bus), the
# reference daemon built for this container, then the suite through its
# host fallback. Arguments are passed to `go test` after the package.
set -eu
mkdir -p /run/dbus /tmp/kvs
dbus-daemon --system --fork
avahi-daemon -D --no-chroot --no-drop-root
i=0
until avahi-daemon --check 2>/dev/null; do
	i=$((i + 1))
	if [ "$i" -gt 50 ]; then
		echo "chiptool-container: avahi-daemon did not come up" >&2
		exit 1
	fi
	sleep 0.1
done
cd /src
go build -o /tmp/reference-bridge ./examples/reference-bridge
export GOFABRIC_CHIP_HARNESS=host
export GOFABRIC_CHIPTOOL_BIN=/root/apps/chip-tool
export GOFABRIC_CHIPTOOL_BRIDGE=/tmp/reference-bridge
export GOFABRIC_CHIPTOOL_KVS_BASE=/tmp/kvs
exec go test -tags=chiptool -count=1 -v ./internal/chiptool/... "$@"
