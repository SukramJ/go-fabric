#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (C) 2026 SukramJ.
#
# Prepares the inputs the chip-tool suite's YAML leg needs, on a developer
# machine, the way .github/workflows/chiptool.yml prepares them in CI:
#
#   1. a connectedhomeip checkout AT THE MAKEFILE PIN (the commit the
#      chip-cert-bins chip-tool is built from), sparse and shallow;
#   2. a Python venv with matter-yamltests and matter-idl installed FROM THAT
#      CHECKOUT, plus click and diskcache.
#
# `make chiptool-test` picks both up by default when they exist, so after one
# `make chiptool-setup` the conformance test runs instead of skipping.
#
# The sparse set is CI's five directories plus the sources CLAUDE.md names as
# the authority on controller behaviour (examples/chip-tool, src/). It is
# never a full checkout and never pulls submodules: nothing here builds CHIP.
#
# An existing checkout is never clobbered. One at the pin is reused (its
# sparse set widened if needed); one at another commit is reported and left
# alone — it may be somebody's working tree.
#
# Usage: script/chiptool-setup.sh <pin> <chip-root> <venv-dir>

set -euo pipefail

pin=${1:?usage: chiptool-setup.sh <pin> <chip-root> <venv-dir>}
chip_root=${2:?usage: chiptool-setup.sh <pin> <chip-root> <venv-dir>}
venv=${3:?usage: chiptool-setup.sh <pin> <chip-root> <venv-dir>}

sparse=(
	# CI's set (.github/workflows/chiptool.yml, chiptool-conformance job).
	scripts/py_matter_idl
	scripts/py_matter_yamltests
	scripts/tests
	src/app/tests/suites
	src/app/zap-templates/zcl/data-model/chip
	# The controller-behaviour sources this module cites.
	examples/chip-tool
	src
)

if [ -e "$chip_root" ] && [ ! -d "$chip_root/.git" ]; then
	echo "chiptool-setup: $chip_root exists but is not a git checkout; move it aside or set CHIP_ROOT" >&2
	exit 1
fi

if [ ! -d "$chip_root/.git" ]; then
	echo "chiptool-setup: cloning connectedhomeip@$pin into $chip_root (sparse, depth 1)"
	mkdir -p "$chip_root"
	git -C "$chip_root" init -q
	git -C "$chip_root" remote add origin https://github.com/project-chip/connectedhomeip.git
	git -C "$chip_root" sparse-checkout set --cone "${sparse[@]}"
	# GitHub serves a commit by id, so the pin needs no branch or tag.
	git -C "$chip_root" fetch -q --depth 1 --filter=blob:none origin "$pin"
	git -C "$chip_root" -c advice.detachedHead=false checkout -q FETCH_HEAD
else
	head=$(git -C "$chip_root" rev-parse HEAD 2>/dev/null || echo none)
	if [ "$head" != "$pin" ]; then
		echo "chiptool-setup: $chip_root is at $head, not at the Makefile pin $pin." >&2
		echo "  The YAML cases, the cluster XML and chip-tool must come from one commit." >&2
		echo "  Check out the pin there yourself (git -C $chip_root fetch --depth 1 origin $pin &&" >&2
		echo "  git -C $chip_root checkout FETCH_HEAD), or point CHIP_ROOT at another directory." >&2
		exit 1
	fi
	if git -C "$chip_root" config --get core.sparseCheckout >/dev/null 2>&1; then
		git -C "$chip_root" sparse-checkout add "${sparse[@]}"
	fi
	echo "chiptool-setup: reusing $chip_root at the pin"
fi

if [ ! -x "$venv/bin/python" ]; then
	echo "chiptool-setup: creating the YAML-runner venv at $venv"
	python3 -m venv "$venv"
fi
# From the checkout, never from PyPI: a released wheel floats against the pin
# and would parse the cases with a different vocabulary. click and diskcache
# are the runner's only further imports outside a full CHIP bootstrap.
"$venv/bin/pip" install -q --disable-pip-version-check \
	"$chip_root/scripts/py_matter_idl" "$chip_root/scripts/py_matter_yamltests" click diskcache
"$venv/bin/python" -c "import matter.yamltests, matter.idl, click, diskcache"

echo "chiptool-setup: ready"
echo "  GOFABRIC_CHIP_ROOT=$chip_root"
echo "  GOFABRIC_CHIPYAML_PYTHON=$venv/bin/python"
echo "  (both are the defaults \`make chiptool-test\` uses; no export needed)"
