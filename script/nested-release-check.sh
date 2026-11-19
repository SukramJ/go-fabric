#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
#
# The release shape of the nested modules (review II, R2-P5-sup-01). In
# this tree every nested module builds through a replace of go-fabric to
# ../.. (examples/ble-wifi-light through the replaces of ble/bluez and
# wifi/wpasupplicant as well), so it always compiles against the code
# beside it. A consumer's build has no replace: it resolves the require
# from the module proxy. A nested module tagged while its require names a
# go-fabric version its code has outgrown compiles here and fails for
# every consumer, and a published module version cannot be changed.
#
# For each module directory named (default: every nested module), in a
# scratch copy of it:
#
#   - every require of a module of this repository must name a version
#     this repository has tagged (<directory>/<version> for a nested
#     module, <version> for the root) — the tags are read from the local
#     clone, so fetch them first;
#   - its replace is dropped, so the build resolves the require the way a
#     consumer's build does, through GOPROXY;
#   - no replace to a local path may remain;
#   - go mod download first: a module that cannot be fetched (GOPROXY,
#     the checksum database, the network — or a require of a version the
#     proxy does not serve, a go.sum hash it does not match) exits 2, red
#     wherever the check runs (review II, DR5a-ci-08, RR5a-04);
#   - go build ./... and go vet ./... with GOWORK=off and GOFLAGS=-mod=mod.
#
# With --tag <dir>/vX.Y.Z (a pushed nested tag) it checks that one module
# and first that the tag names it: a directory with a go.mod whose module
# path is the root's path plus the directory, and a Go module version.
#
# With --released <dir>/vX.Y.Z (repeatable) a nested release pull request
# names a tag its merge commit will carry (owner decision 266): a require of
# that sibling at that version, untagged until the merge, is not an error
# when the sibling was checked earlier in the same run and is in release
# shape — the module builds against the sibling's checked copy, its own
# replaces dropped, which is the code the tag will name. A require of any
# other untagged version stays an error. chiptool.yml's nested-release job
# derives the tags from the pull request (its head branch
# release/nested-vX.Y.Z and the nested go.mod files it changes).
#
# Usage: script/nested-release-check.sh [--tag <tag> | [--released <tag>]... <module-dir> ...]
# Exit 1: a module fails the release shape; 2: the environment or the
# arguments (no git, no go, no jq, a directory without a go.mod, a module
# that cannot be downloaded).
set -euo pipefail

for tool in git go jq; do
	command -v "$tool" >/dev/null || { echo "nested-release-check: $tool not found"; exit 2; }
done

root=$(git rev-parse --show-toplevel) || exit 2
cd "$root"
rootmod=$(GOWORK=off go list -m) || exit 2

# nested_tag checks that $1, given to option $2, names a nested module of
# this repository with a Go module version; on failure it says why and
# returns 1, or 2 when the argument is not of the form <dir>/vX.Y.Z at all.
nested_tag() {
	local tag="$1" opt="$2" dir version path
	dir="${tag%/*}"
	version="${tag##*/}"
	if [ -z "$tag" ] || [ "$dir" = "$tag" ]; then
		echo "nested-release-check: $opt wants <dir>/vX.Y.Z, got '$tag'"
		return 2
	fi
	if ! grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$' <<<"$version"; then
		echo "nested-release-check: tag '$tag': '$version' is not a Go module version"
		return 1
	fi
	if [ ! -f "$dir/go.mod" ]; then
		echo "nested-release-check: tag '$tag' names '$dir', which holds no go.mod"
		return 1
	fi
	path=$(cd "$dir" && GOWORK=off go list -m) || return 2
	if [ "$path" != "$rootmod/$dir" ]; then
		echo "nested-release-check: tag '$tag': the module in $dir is '$path', not '$rootmod/$dir'"
		return 1
	fi
}

modules=()
# released: the tags this change makes at its merge (--released), one per
# line; read with a here-string, never a pipe into grep -q (see
# script/release-provenance.sh, TestNoQuietGrepAfterPipeUnderPipefail).
released=""
if [ "${1:-}" = "--tag" ]; then
	if [ $# -gt 2 ]; then
		echo "nested-release-check: --tag checks one module and takes no further arguments"
		exit 2
	fi
	rc=0
	nested_tag "${2:-}" --tag || rc=$?
	if [ "$rc" -ne 0 ]; then exit "$rc"; fi
	modules=("${2%/*}")
else
	while [ $# -gt 0 ]; do
		case "$1" in
		--released)
			if [ $# -lt 2 ]; then
				echo "nested-release-check: --released wants <dir>/vX.Y.Z"
				exit 2
			fi
			# A wrong --released is the arguments, never the module: exit 2.
			nested_tag "$2" --released || exit 2
			released="$released$2"$'\n'
			shift 2
			;;
		--tag)
			echo "nested-release-check: --tag is the first and only argument"
			exit 2
			;;
		*)
			modules+=("$1")
			shift
			;;
		esac
	done
fi
if [ ${#modules[@]} -eq 0 ]; then
	modules=(ble/bluez ble/esphome wifi/wpasupplicant examples/ble-wifi-light)
fi

scratch=$(mktemp -d)
trap 'rm -rf -- "${scratch:?}"' EXIT

failed=()
# passed: the modules this run found in release shape, one per line.
passed=""
for dir in "${modules[@]}"; do
	if [ ! -f "$dir/go.mod" ]; then
		echo "nested-release-check: no go.mod in '$dir'"
		exit 2
	fi
	echo "-> $dir"
	work="$scratch/$dir"
	mkdir -p "$work"
	cp -R "$dir/." "$work/"
	ok=true
	# siblings: the requires built against a sibling's checked copy, as
	# released by this change; their replace stays, to that copy.
	siblings=""
	while read -r path version; do
		[ -n "$path" ] || continue
		rel="${path#"$rootmod"}"
		rel="${rel#/}"
		want="$version"
		if [ -n "$rel" ]; then want="$rel/$version"; fi
		if ! git rev-parse -q --verify "refs/tags/$want" >/dev/null; then
			if [ -z "$rel" ] || ! grep -Fxq -- "$want" <<<"$released"; then
				echo "   requires $path $version, but this repository has no tag $want, and this change does not release it (--released)"
				ok=false
				continue
			fi
			if ! grep -Fxq -- "$rel" <<<"$passed"; then
				echo "   requires $path $version, released by this change (tag $want), but $rel is not checked before $dir in this run, or is not in release shape"
				ok=false
				continue
			fi
			echo "   requires $path $version, released by this change (tag $want at its merge): built against $rel as checked above"
			(cd "$work" && go mod edit -replace="$path=$scratch/$rel")
			siblings="$siblings$path"$'\n'
			continue
		fi
		echo "   requires $path $version (tag $want): resolved without the replace"
		(cd "$work" && go mod edit -dropreplace="$path")
	done < <(cd "$work" && go mod edit -json |
		jq -r --arg root "$rootmod" '.Require[]? | select(.Path == $root or (.Path | startswith($root + "/"))) | .Path + " " + .Version')
	if [ "$ok" = true ]; then
		local_replaces=$(cd "$work" && go mod edit -json | jq -r --arg keep "$siblings" '($keep | split("\n")) as $k | .Replace[]? | select(.New.Path | startswith(".") or startswith("/")) | select(.Old.Path as $o | ($k | index([$o])) == null) | .Old.Path + " => " + .New.Path')
		if [ -n "$local_replaces" ]; then
			echo "   a replace to a local path remains, which a consumer's build ignores:"
			printf '     %s\n' "$local_replaces"
			ok=false
		fi
	fi
	if [ "$ok" = true ] && ! (cd "$work" && GOWORK=off GOFLAGS=-mod=mod go mod download); then
		echo "nested-release-check: $dir: the modules it requires cannot be downloaded (above): the environment, or a require or go.sum line the proxy does not serve; either way not released"
		exit 2
	fi
	if [ "$ok" = true ]; then
		if ! (cd "$work" && GOWORK=off GOFLAGS=-mod=mod go build ./... && GOWORK=off GOFLAGS=-mod=mod go vet ./...); then
			echo "   does not build against the versions it requires"
			ok=false
		fi
	fi
	if [ "$ok" = true ]; then
		echo "   ok"
		passed="$passed$dir"$'\n'
	else
		failed+=("$dir")
	fi
done

if [ ${#failed[@]} -ne 0 ]; then
	echo "nested-release-check: not in release shape: ${failed[*]}"
	echo "A release of such a module bumps its require of go-fabric (and of the"
	echo "nested modules it uses) to tags whose code it compiles against."
	exit 1
fi
echo "nested-release-check: every module builds against the versions it requires"
