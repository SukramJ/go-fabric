#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (C) 2026 SukramJ.
#
# Makes the connectedhomeip source CLAUDE.md sends you to when chip-tool or
# a CHIP test case behaves unexpectedly: a sparse, shallow checkout AT THE
# COMMIT THE CHIP HARNESS IMAGE WAS BUILT FROM (the Makefile's
# CHIP_TEST_IMAGE; its CHIP commit is CHIP_TEST_IMAGE_COMMIT), so the source
# you read is the code that ran.
#
# The sparse set is the controller and harness sources (examples/chip-tool,
# src/, the YAML and Python cases and their runners). It is never a full
# checkout and never pulls submodules: nothing here builds CHIP — the
# harness image carries the binaries.
#
# An existing checkout is never clobbered. One at the pin is reused (its
# sparse set widened if needed); one at another commit is reported and left
# alone — it may be somebody's working tree.
#
# Usage: script/chiptool-setup.sh <pin> <chip-root>

set -euo pipefail

pin=${1:?usage: chiptool-setup.sh <pin> <chip-root>}
chip_root=${2:?usage: chiptool-setup.sh <pin> <chip-root>}

sparse=(
	scripts/py_matter_yamltests
	scripts/tests
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
		echo "  The source you read should be the code the harness image runs." >&2
		echo "  Check out the pin there yourself (git -C $chip_root fetch --depth 1 origin $pin &&" >&2
		echo "  git -C $chip_root checkout FETCH_HEAD), or point CHIP_ROOT at another directory." >&2
		exit 1
	fi
	if git -C "$chip_root" config --get core.sparseCheckout >/dev/null 2>&1; then
		git -C "$chip_root" sparse-checkout add "${sparse[@]}"
	fi
	echo "chiptool-setup: reusing $chip_root at the pin"
fi

echo "chiptool-setup: ready — $chip_root at $pin"
