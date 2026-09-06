#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (C) 2026 SukramJ.
#
# How far behind matter.js has parity/schema.json's pin fallen?
#
# parity/schema.json is an extract from a matter.js checkout and records the
# commit it came from (script/extract-from-matter-js.ts writes
# `git rev-parse HEAD` into matter.sourceCommit). Every parity test and the
# generated schema/ package read that extract, so the pin is the module's
# single statement of which upstream revision its Matter data model is true
# against. Nothing in the repository can notice when that statement expires:
# the schema parity test compares two in-repo copies of the same extract.
#
# Usage: matterjs-pin-distance.sh <matter.js checkout> [schema.json]
#
# Exits 0 when the pin is within the threshold below, 1 when it is not, and 2
# when the measurement could not be performed (a missing checkout, a pin that
# is not on the upstream default branch). A measurement that could not run is
# reported as such rather than as a pass — an unperformable check that exits 0
# is indistinguishable from a green one.

set -euo pipefail

# --- what is measured -------------------------------------------------------
#
# Not "commits behind HEAD". matter.js merges on the order of 160 commits a
# month across its whole tree; nearly all of them are protocol, tooling,
# examples or tests that this extract does not read, so a total-commit gate
# would be red within days of any refresh and would be muted within a month of
# being introduced.
#
# What is counted instead is upstream commits since the pin that touch the
# files the extract is actually derived from. Those are, from
# script/extract-from-matter-js.ts:
#
#   packages/model/src/standard/          — MatterDefinition and the 365
#       generated *.element.ts files that register into it. Every deviceType
#       and cluster row in the extract, with its revision, featureMap,
#       attributes, commands, events and features, comes from here.
#   packages/model/src/common/Specification.ts — the four `matter.*` fields at
#       the head of the extract: REVISION, SPECIFICATION_VERSION,
#       INTERACTION_MODEL_REVISION, DATA_MODEL_REVISION, read by name.
#   packages/model/src/elements/          — the element factories whose output
#       shape the extractor walks (`tag`, `id`, `element`, `direction`,
#       `conformance`). A change to that shape changes the extract without
#       any element file moving.
#
# Measured over the twelve months before this file was written, those paths
# took 34 commits — about three a month, busiest single month six — against
# 1954 across the whole repository. The scoped number is therefore both small
# enough to act on and large enough to move.
#
# The deliberate limit of this measurement: the pin governs the *extract*, and
# the remedy this script prints regenerates the extract. It does not govern
# the hand-written port of matter.js's behaviour — packages/protocol/src/
# session, packages/node/src/behaviors and the rest — which carries no
# recorded pin at all. Commits there are reported below as context and are
# never counted toward the threshold, because failing on them would demand an
# action that does not address them.
EXTRACT_INPUTS=(
    "packages/model/src/standard/"
    "packages/model/src/common/Specification.ts"
    "packages/model/src/elements/"
    # The extractor does not read this directory, it REIMPLEMENTS it:
    # script/extract-from-matter-js.ts:132,135,137 hand-copy matter.js's
    # member-identity, response-naming and requirement-keying rules and cite
    # models/Model.ts:195, models/CommandModel.ts:64 and
    # models/RequirementModel.ts:24 for them. A hand-copied rule is exactly
    # the kind that drifts without anything failing: upstream changes how a
    # command's direction is decided, the extract keeps deriving the old
    # answer, and every downstream parity test agrees with itself. Counted
    # for the same reason as the files that are read.
    "packages/model/src/models/"
)

# The declared threshold, in commits touching EXTRACT_INPUTS since the pin.
#
# Ten is roughly a quarter of ordinary drift at the measured ~3 commits a
# month, and sits above the busiest single month observed (six) so that one
# active month upstream cannot trip it on its own. It protects against the
# failure this whole job exists for: the extract silently ageing past the
# point where a refresh is a small reviewable diff. Refreshing is a deliberate
# act with a diff to read, so the gate is tuned to fire quarterly, not weekly.
#
# Raise it only with a reason written here; lowering it is free.
MAX_SCOPED_COMMITS=10

# Paths this module ports by hand. Counted and printed for context only —
# see the limit note above. Never compared against a threshold.
PORTED_SURFACE=(
    "packages/protocol/src/"
    "packages/node/src/behaviors/"
    "packages/node/src/devices/"
    "packages/types/src/"
)

die() {
    echo "::error::$*" >&2
    echo "matterjs-pin-distance: $*" >&2
    exit 2
}

MATTERJS_DIR="${1:-}"
[ -n "$MATTERJS_DIR" ] || die "usage: matterjs-pin-distance.sh <matter.js checkout> [schema.json]"
[ -d "$MATTERJS_DIR/.git" ] || die "not a git checkout: $MATTERJS_DIR"

SCHEMA="${2:-$(cd "$(dirname "$0")/.." && pwd)/parity/schema.json}"
[ -f "$SCHEMA" ] || die "no schema extract at $SCHEMA"

# Read the pin the same way nightly.yml does, off the JSON rather than by
# pattern-matching the file.
PINNED="$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['matter']['sourceCommit'])" "$SCHEMA")"
REVISION="$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['matter']['revision'])" "$SCHEMA")"
[ -n "$PINNED" ] || die "parity/schema.json carries no matter.sourceCommit"

# Resolve upstream's default-branch tip. A fresh clone sets
# refs/remotes/origin/HEAD; prefer it over hard-coding a branch name, and fall
# back to the checkout's own HEAD (which is what a --no-checkout clone leaves
# pointing at the default branch) so a developer can run this against the
# checkout they already have.
if UPSTREAM_REF="$(git -C "$MATTERJS_DIR" symbolic-ref --quiet refs/remotes/origin/HEAD 2>/dev/null)"; then
    :
else
    UPSTREAM_REF="HEAD"
fi
HEAD_SHA="$(git -C "$MATTERJS_DIR" rev-parse "$UPSTREAM_REF")"

git -C "$MATTERJS_DIR" cat-file -e "${PINNED}^{commit}" 2>/dev/null ||
    die "the pinned commit ${PINNED} does not exist in this matter.js checkout — the distance is not measurable, not zero"

# A pin that is not an ancestor of the tip is a different situation from a
# stale one: upstream rewrote history, or the extract was taken off a branch
# that never landed. Counting commits across that gap would produce a number
# that means nothing.
git -C "$MATTERJS_DIR" merge-base --is-ancestor "$PINNED" "$HEAD_SHA" 2>/dev/null ||
    die "the pinned commit ${PINNED} is not an ancestor of ${UPSTREAM_REF} (${HEAD_SHA}) — refresh the extract and record a pin that is on the default branch"

count_since() {
    git -C "$MATTERJS_DIR" rev-list --count "${PINNED}..${HEAD_SHA}" -- "$@"
}

SCOPED="$(count_since "${EXTRACT_INPUTS[@]}")"
TOTAL="$(git -C "$MATTERJS_DIR" rev-list --count "${PINNED}..${HEAD_SHA}")"
PORTED="$(count_since "${PORTED_SURFACE[@]}")"
PIN_DATE="$(git -C "$MATTERJS_DIR" log -1 --format=%cs "$PINNED")"
HEAD_DATE="$(git -C "$MATTERJS_DIR" log -1 --format=%cs "$HEAD_SHA")"

report() {
    echo "### matter.js pin distance"
    echo
    echo "| | |"
    echo "|---|---|"
    echo "| Matter revision in the extract | \`${REVISION}\` |"
    echo "| Pinned matter.js commit | \`${PINNED}\` (${PIN_DATE}) |"
    echo "| matter.js default-branch head | \`${HEAD_SHA}\` (${HEAD_DATE}) |"
    echo "| **Commits touching the extract's inputs** | **${SCOPED}** (threshold ${MAX_SCOPED_COMMITS}) |"
    echo "| Commits anywhere in matter.js | ${TOTAL} (not gated) |"
    echo "| Commits in the hand-ported surface | ${PORTED} (not gated, no pin governs it) |"
    echo
    echo "The extract's inputs are:"
    for p in "${EXTRACT_INPUTS[@]}"; do echo "- \`${p}\`"; done
    if [ "$SCOPED" -gt 0 ]; then
        echo
        echo "Commits since the pin that touch them:"
        echo
        # -n rather than a `head` pipe: under `set -o pipefail` a closed pipe
        # would turn a long list into a failed measurement.
        git -C "$MATTERJS_DIR" log --no-merges -n 40 --format='- `%h` %s' \
            "${PINNED}..${HEAD_SHA}" -- "${EXTRACT_INPUTS[@]}"
    fi
}

SUMMARY="$(report)"
echo "$SUMMARY"
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    printf '%s\n' "$SUMMARY" >>"$GITHUB_STEP_SUMMARY"
fi

if [ "$SCOPED" -le "$MAX_SCOPED_COMMITS" ]; then
    echo
    echo "Within threshold: ${SCOPED} of ${MAX_SCOPED_COMMITS} commits on the extract's inputs."
    exit 0
fi

# The failure text carries the whole story, because a scheduled red job that
# says only "threshold exceeded" is a job somebody turns off.
FAIL="parity/schema.json is pinned to matter.js ${PINNED} (${PIN_DATE}); the default branch is at ${HEAD_SHA} (${HEAD_DATE}). ${SCOPED} commits since the pin touch the files the extract is generated from, over the declared threshold of ${MAX_SCOPED_COMMITS} (see script/matterjs-pin-distance.sh). Refresh with: make generate-matter-schema MATTERJS_DIR=<checkout> (default ../matter.js), then read the diff to parity/schema.json and schema/ — that diff is the point of this job. If the drift is known and accepted, raise MAX_SCOPED_COMMITS with the reason written next to it."
echo "::error::${FAIL}"
{
    echo
    echo "**Over threshold.** ${FAIL}"
} >>"${GITHUB_STEP_SUMMARY:-/dev/null}"
echo
echo "$FAIL" >&2
exit 1
