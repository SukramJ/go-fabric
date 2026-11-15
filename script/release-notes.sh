#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
#
# The release body of version X.Y.Z: the curated CHANGELOG section, not a
# commit dump. The tag is vX.Y.Z and the changelog headers are Keep a
# Changelog "## [X.Y.Z] …", so the leading v is stripped and the section
# sliced from that header to the next "## [". A missing section fails: a
# version whose changelog nobody wrote does not ship. release.yml runs it
# twice — the verify job fails on a missing section before anything is
# published, the release job writes the body.
#
# Usage: script/release-notes.sh <tag> <out-file>
set -euo pipefail

tag="${1:-}"
out="${2:-}"
if [ -z "$tag" ] || [ -z "$out" ]; then
	echo "release-notes: usage: $0 <tag> <out-file>"
	exit 2
fi
version="${tag#v}"
awk -v ver="$version" '
	/^## \[/ {
		if (insec) exit
		if (index($0, "## [" ver "]") == 1) { insec=1; next }
	}
	insec { print }
' CHANGELOG.md >"$out"
# Trim leading blank lines.
sed -i.bak -e '/./,$!d' "$out" && rm -f -- "$out.bak"
if [ ! -s "$out" ]; then
	echo "::error::No '## [${version}]' section in CHANGELOG.md — add it in the release commit."
	exit 1
fi
echo "Release notes for ${version}:" >&2
cat "$out" >&2
