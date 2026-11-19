#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
#
# Where a release tag points (review II, R2-P5-sup-04). A tag can be pushed
# at any commit; a published module version cannot be taken back. Before
# release.yml builds anything, the tagged commit must
#
#   - be on main: an ancestor of origin/main, so it went through the branch
#     protection's required checks as a pull request;
#   - have a run of the ci workflow (.github/workflows/ci.yml) on that
#     very commit in which every required check that ci.yml defines passed
#     — the push run on main, which also ran the race detector (job
#     `test (linux, race)`). The required checks are the branch
#     protection's copy, script/requiredchecks/required.txt, read from the
#     checkout this script is in (the tag's, in release.yml), intersected
#     with ci.yml's job names there; the required checks of other
#     workflows are named in one line and not checked here
#     (TestProvenanceRequiredCI holds that list). A ci job that is not a required check
#     (a guard still pending its promotion) does not block a release, as it
#     does not block a merge (review II, DR5a-ci-04). A merge whose push
#     run was cancelled by the next merge, or a merge that started none (a
#     merge by the GitHub Actions app starts no workflow), has none: re-run
#     ci on the commit, or tag a commit that has one.
#
# Usage: script/release-provenance.sh <tag>
#        script/release-provenance.sh --required-ci   (print the required
#        checks ci.yml defines, one per line; no network)
# Needs git, gh with a token that may read the repository's workflow runs
# (GH_TOKEN), and GITHUB_REPOSITORY (owner/name). Exit 1: the tag fails a
# check; 2: the environment, an unreadable answer included — never a pass.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)

# required_ci prints the contexts of required.txt that are job names of
# ci.yml: the `name:` of a job sits at four spaces under `jobs:` (the
# shape script/requiredchecks reads, TestProvenanceRequiredCI holds this
# reader to it).
#
# Every membership test in this script reads a here-string, never a pipe
# into `grep -q`: grep -q exits at its first match, bash's printf writes a
# multi-line value one line per write(2), and a write after grep has gone
# is SIGPIPE — under pipefail the pipeline's status is 141, so a context
# that matched counted as absent, once in a few hundred runs (review II
# phase 7, fl-ci; TestNoQuietGrepAfterPipeUnderPipefail).
required_ci() {
	local required="$root/script/requiredchecks/required.txt" ci="$root/.github/workflows/ci.yml"
	[ -r "$required" ] && [ -r "$ci" ] || { echo "release-provenance: cannot read $required or $ci" >&2; return 2; }
	local names
	names=$(sed -n -e '/^jobs:/,$ s/^    name: *//p' "$ci" | sed -e 's/ *#.*$//' -e 's/^"\(.*\)"$/\1/' -e "s/^'\(.*\)'\$/\1/")
	local ctx found=0
	while IFS= read -r ctx || [ -n "$ctx" ]; do
		[ -n "$ctx" ] || continue
		if grep -Fxq -- "$ctx" <<<"$names"; then
			printf '%s\n' "$ctx"
			found=1
		fi
	done <"$required"
	[ "$found" -eq 1 ] || { echo "release-provenance: no context of required.txt is a job of ci.yml" >&2; return 2; }
}

if [ "${1:-}" = "--required-ci" ]; then
	required_ci || exit 2
	exit 0
fi

tag="${1:-}"
repo="${GITHUB_REPOSITORY:-}"
if [ -z "$tag" ] || [ -z "$repo" ]; then
	echo "release-provenance: usage: GITHUB_REPOSITORY=owner/name $0 <tag>"
	exit 2
fi
for tool in git gh; do
	command -v "$tool" >/dev/null || { echo "release-provenance: $tool not found"; exit 2; }
done
required=$(required_ci) || exit 2
# The required checks no ci.yml job carries are not checked here: say so,
# so a check that left ci.yml is seen rather than dropped
# (TestProvenanceRequiredCI names each with its workflow; RR5a-01).
outside=""
while IFS= read -r ctx || [ -n "$ctx" ]; do
	[ -n "$ctx" ] || continue
	grep -Fxq -- "$ctx" <<<"$required" || outside="$outside${outside:+, }$ctx"
done <"$root/script/requiredchecks/required.txt"
[ -z "$outside" ] || echo "release-provenance: required checks outside ci.yml, not checked on the tag here: $outside"

commit=$(git rev-parse --verify -q "refs/tags/$tag^{commit}") || {
	echo "release-provenance: no tag $tag in this clone"
	exit 2
}

if ! git rev-parse -q --verify refs/remotes/origin/main >/dev/null; then
	git fetch --quiet --no-tags origin +refs/heads/main:refs/remotes/origin/main || {
		echo "release-provenance: cannot fetch main"
		exit 2
	}
fi
if ! git merge-base --is-ancestor "$commit" refs/remotes/origin/main; then
	echo "::error::tag $tag points at $commit, which is not on main"
	exit 1
fi
echo "tag $tag: $commit is on main"

# is_count: a non-negative integer, as the API's counts are. Anything else
# (null, empty, a message) is the environment, never a pass (DR5a-ci-05).
is_count() {
	case "$1" in '' | *[!0-9]*) return 1 ;; esac
}

listing=$(gh api "repos/$repo/actions/workflows/ci.yml/runs?head_sha=$commit&per_page=100" \
	--jq '.total_count, (.workflow_runs[] | .id)') || {
	echo "release-provenance: cannot list the ci runs of $commit"
	exit 2
}
total=$(printf '%s\n' "$listing" | sed -n 1p)
ids=$(printf '%s\n' "$listing" | sed 1d)
is_count "$total" || { echo "release-provenance: unexpected total_count '$total' for the ci runs of $commit"; exit 2; }
if [ "$total" -gt 100 ]; then
	echo "release-provenance: $total ci runs on $commit; this check reads one page of 100"
	exit 2
fi
n=0
for id in $ids; do
	is_count "$id" || { echo "release-provenance: unexpected run id '$id'"; exit 2; }
	n=$((n + 1))
done
if [ "$n" -ne "$total" ]; then
	echo "release-provenance: the listing names $n runs, its total_count $total"
	exit 2
fi
if [ "$total" -eq 0 ]; then
	echo "::error::tag $tag: no run of the ci workflow on $commit; re-run ci on it, or tag a commit that has one"
	exit 1
fi

# A run proves the commit when each required ci check is one of its jobs
# (the latest attempt) with conclusion success. The runs are listed newest
# first; the first that proves it is enough.
for id in $ids; do
	jobs=$(gh api "repos/$repo/actions/runs/$id/jobs?filter=latest&per_page=100" \
		--jq '.total_count, (.jobs[] | "\(.conclusion // "none")\t\(.name)")') || {
		echo "release-provenance: cannot list the jobs of ci run $id"
		exit 2
	}
	jtotal=$(printf '%s\n' "$jobs" | sed -n 1p)
	is_count "$jtotal" || { echo "release-provenance: unexpected total_count '$jtotal' for the jobs of ci run $id"; exit 2; }
	if [ "$jtotal" -gt 100 ]; then
		echo "release-provenance: ci run $id has $jtotal jobs; this check reads one page of 100"
		exit 2
	fi
	jlines=$(printf '%s\n' "$jobs" | sed 1d)
	missing=""
	while IFS= read -r ctx; do
		if ! grep -Fxq -- "$(printf 'success\t%s' "$ctx")" <<<"$jlines"; then
			missing="$missing${missing:+, }$ctx"
		fi
	done <<<"$required"
	if [ -z "$missing" ]; then
		echo "tag $tag: $commit has ci run $id with every required ci check passed ($(printf '%s\n' "$required" | wc -l | tr -d ' ') checks)"
		exit 0
	fi
	echo "ci run $id on $commit: not passed: $missing"
done
echo "::error::tag $tag: no ci run on $commit passed every required check of ci.yml; re-run ci on it, or tag a commit that has one"
exit 1
