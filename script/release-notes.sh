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
# A release body is limited to 125000 characters (the GitHub API answers
# 422 "body is too long"; v0.23.0's section was about 380000). Above
# RELEASE_NOTES_MAX (default 120000, room for the sponsoring note
# release.yml prepends) the body is the short form: the section's intro,
# a link to the section at the tag, and its subsections with the number
# of their entries — the form the v0.23.0 release was created with by hand
# (owner decision 267 of review II). Relative links of the intro point at
# the tag's tree. The repository is GITHUB_SERVER_URL/GITHUB_REPOSITORY,
# which a workflow run sets.
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
max="${RELEASE_NOTES_MAX:-120000}"
size=$(wc -c <"$out" | tr -d ' ')
if [ "$size" -gt "$max" ]; then
	base="${GITHUB_SERVER_URL:-https://github.com}/${GITHUB_REPOSITORY:?release-notes: GITHUB_REPOSITORY is needed for the short form}"
	header=$(grep -m1 -F "## [${version}]" CHANGELOG.md)
	anchor=$(printf '%s' "${header#\#\# }" | tr '[:upper:]' '[:lower:]' | LC_ALL=C sed -e 's/[^a-z0-9 -]//g' -e 's/ /-/g')
	short="$out.short"
	{
		awk '/^### /{exit} {print}' "$out" |
			sed -E "s#\]\((\./)?([^)#:]+)\)#](${base}/blob/${tag}/\2)#g"
		printf 'The full notes are the CHANGELOG section, longer than a release body may be (%s characters): [CHANGELOG.md § %s](%s/blob/%s/CHANGELOG.md#%s).\n\n' "$size" "$version" "$base" "$tag" "$anchor"
		printf '| Section | Entries |\n| --- | --- |\n'
		awk '/^### /{if (name != "") printf "| %s | %d |\n", name, n; name=substr($0, 5); n=0; next} /^- /{n++} END{if (name != "") printf "| %s | %d |\n", name, n}' "$out"
		breaking=$(grep -c '^- \*\*Breaking' "$out" || true)
		printf '\n%s entries are marked **Breaking**.\n' "$breaking"
	} >"$short"
	mv "$short" "$out"
	echo "release-notes: the section has ${size} characters, above ${max}: the short form" >&2
fi
echo "Release notes for ${version}:" >&2
cat "$out" >&2
