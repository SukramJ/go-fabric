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
#   - have a successful run of the ci workflow (.github/workflows/ci.yml)
#     on that very commit — the push run on main, which also ran the race
#     detector (job `test (linux, race)`). A merge whose push run was
#     cancelled by the next merge, or a merge that started none (a merge by
#     the GitHub Actions app starts no workflow), has none: re-run ci on
#     the commit, or tag a commit that has one.
#
# Usage: script/release-provenance.sh <tag>
# Needs git, gh with a token that may read the repository's workflow runs
# (GH_TOKEN), and GITHUB_REPOSITORY (owner/name). Exit 1: the tag fails a
# check; 2: the environment.
set -euo pipefail

tag="${1:-}"
repo="${GITHUB_REPOSITORY:-}"
if [ -z "$tag" ] || [ -z "$repo" ]; then
	echo "release-provenance: usage: GITHUB_REPOSITORY=owner/name $0 <tag>"
	exit 2
fi
for tool in git gh; do
	command -v "$tool" >/dev/null || { echo "release-provenance: $tool not found"; exit 2; }
done

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

runs=$(gh api "repos/$repo/actions/workflows/ci.yml/runs?head_sha=$commit&status=success&per_page=1" --jq '.total_count') || {
	echo "release-provenance: cannot list the ci runs of $commit"
	exit 2
}
if [ "${runs:-0}" -lt 1 ]; then
	echo "::error::tag $tag: no successful run of the ci workflow on $commit; re-run ci on it, or tag a commit that has one"
	exit 1
fi
echo "tag $tag: $commit has a successful ci run"
