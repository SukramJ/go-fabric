#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
#
# The virtual OTBR of ADR 0027 decision 7: an otbr-agent with its REST API
# on the simulation RCP, built as connectedhomeip's CI builds it
# (.github/workflows/tests.yaml:327-359 at 6170af84, plus -DOTBR_REST=ON),
# then thread/otbr's live test against it. UNVERIFIED until a dispatched
# run has passed: that the simulation RCP runs on the ubuntu-24.04 runner,
# and the agent's REST flags at OTBR_REF.
#
# The summary separates the ENVIRONMENT (the clone, the bootstrap, the
# builds, the agent coming up) from a DEFECT (the live test), as
# script/ble-virtual-test.sh does; both fail.
set -euo pipefail

OTBR_REF="${OTBR_REF:-main}"
WORK="${OTBR_WORK:-${RUNNER_TEMP:-/tmp}/otbr-virtual}"
LOG_DIR="${OTBR_LOG_DIR:-$WORK/logs}"
REST_PORT="${OTBR_REST_PORT:-8081}"
mkdir -p "$WORK" "$LOG_DIR"

environment() { echo "otbr-virtual: ENVIRONMENT: $*" >&2; exit 2; }
defect() { echo "otbr-virtual: DEFECT: $*" >&2; exit 1; }

repo="$WORK/ot-br-posix"
if [ ! -d "$repo" ]; then
	git clone --quiet https://github.com/openthread/ot-br-posix "$repo" || environment "clone"
fi
git -C "$repo" checkout --quiet "$OTBR_REF" || environment "checkout $OTBR_REF"
git -C "$repo" submodule update --init --recursive --depth 1 >"$LOG_DIR/submodules.log" 2>&1 || environment "submodules"

(cd "$repo" && ./script/bootstrap) >"$LOG_DIR/bootstrap.log" 2>&1 || environment "bootstrap"
(cd "$repo" && PLATFORM=Linux ./script/cmake-build \
	-DBUILD_TESTING=OFF -DCMAKE_BUILD_TYPE=Debug \
	-DOTBR_REST=ON -DOTBR_WEB=OFF -DOTBR_BORDER_ROUTING=ON -DOTBR_MDNS=openthread \
	-DOT_POSIX_SETTINGS_PATH='"tmp"' -DOT_FIREWALL=OFF -DOT_LOG_LEVEL=INFO -DOT_TREL=OFF) \
	>"$LOG_DIR/agent-build.log" 2>&1 || environment "otbr-agent build"
(cd "$repo/third_party/openthread/repo" && ./script/cmake-build simulation \
	-DBUILD_TESTING=OFF -DOT_BUILD_GTEST=OFF -DOT_FTD=OFF -DOT_MTD=OFF -DOT_RCP=ON \
	-DOT_APP_CLI=OFF -DOT_APP_NCP=OFF -DOT_APP_RCP=ON) \
	>"$LOG_DIR/rcp-build.log" 2>&1 || environment "simulation RCP build"

agent="$repo/build/otbr/src/agent/otbr-agent"
rcp="$repo/third_party/openthread/repo/build/simulation/examples/apps/ncp/ot-rcp"
[ -x "$agent" ] && [ -x "$rcp" ] || environment "binaries missing"

sudo "$agent" -d 7 -v -I wpan0 -B lo \
	--rest-listen-address 127.0.0.1 --rest-listen-port "$REST_PORT" \
	"spinel+hdlc+forkpty://$rcp?forkpty-arg=1" >"$LOG_DIR/otbr-agent.log" 2>&1 &
agent_pid=$!
trap 'sudo kill "$agent_pid" 2>/dev/null || true' EXIT

for _ in $(seq 1 60); do
	if curl -fsS "http://127.0.0.1:$REST_PORT/node/state" >/dev/null 2>&1; then
		break
	fi
	sleep 1
done
curl -fsS "http://127.0.0.1:$REST_PORT/node/state" >/dev/null 2>&1 || environment "the agent's REST API did not come up"

GOFABRIC_OTBR_URL="http://127.0.0.1:$REST_PORT" go test -count=1 -run TestLiveOTBR -v ./thread/otbr/ \
	| tee "$LOG_DIR/live-test.log" || defect "the live test failed"
grep -q -- '--- PASS: TestLiveOTBR' "$LOG_DIR/live-test.log" || defect "the live test did not pass"
echo "otbr-virtual: PASS"
