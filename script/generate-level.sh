#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
#
# Extracts one specification level from one matter.js release tag into
# parity/levels/<level>.json (concept v11 §C.5, brief S3 part 1).
#
#   script/generate-level.sh <tag> <matterjs-clone> <out-dir>
#
# The tag is checked out as a detached worktree of the clone (the clone's
# own checkout is never touched), its dependencies installed with the tag's
# lockfile (npm ci, no install scripts), and @matter/model built with the
# tag's own build tool (matter-build up to v0.16, nacho-build from v0.17;
# `npm run build` in packages/model resolves which) — ESM only, with its
# workspace dependencies, which is all the extractor imports. The slim
# extractor (script/extract-levels-from-matter-js.ts) runs inside the
# worktree so its bare @matter/model import resolves; the worktree is
# removed again on every exit path. The level id is the tag's
# Specification.REVISION, as the extractor reports it.
#
# LEVELS_WORKDIR picks where the worktree goes (default: $TMPDIR or /tmp).
set -euo pipefail

tag=${1:?usage: generate-level.sh <tag> <matterjs-clone> <out-dir>}
matterjs=$(cd "${2:?matter.js clone}" && pwd)
out=$(cd "${3:?out dir}" && pwd)
here=$(cd "$(dirname "$0")" && pwd)
work=${LEVELS_WORKDIR:-${TMPDIR:-/tmp}}
wt="${work%/}/matterjs-$tag"

git -C "$matterjs" rev-parse --verify --quiet "refs/tags/$tag" >/dev/null ||
	{ echo "generate-level: no tag $tag in $matterjs" >&2; exit 2; }
if [ -e "$wt" ]; then
	echo "generate-level: $wt exists; remove it first (git -C $matterjs worktree remove --force $wt)" >&2
	exit 2
fi

git -C "$matterjs" worktree add --quiet --detach "$wt" "$tag"
cleanup() { git -C "$matterjs" worktree remove --force "$wt" || true; }
trap cleanup EXIT

(cd "$wt" && npm ci --ignore-scripts --no-audit --no-fund --loglevel=error)
# ESM only first: matter-build's full build of the dependencies stops at
# @matter/testing's test-type validation (measured at v0.13.0). nacho-build
# (v0.17 on) emits no JavaScript for `esm` alone (measured at v0.17.0), so
# its full build runs when the ESM entry point is still missing.
(cd "$wt/packages/model" && npm run --silent build -- -d esm)
if [ ! -f "$wt/packages/model/dist/esm/index.js" ]; then
	(cd "$wt/packages/model" && npm run --silent build -- -d)
fi

cp "$here/extract-levels-from-matter-js.ts" "$wt/.gofabric-levels.mts"
tmp="$out/.level-$tag.json.tmp"
# A node that cannot strip TypeScript types itself gets the extractor
# transpiled by the tag's own esbuild first (as generate-matter-schema does).
if (cd "$wt" && node .gofabric-levels.mts >"$tmp" 2>"$tmp.err"); then
	:
elif [ -x "$wt/node_modules/.bin/esbuild" ]; then
	(cd "$wt" && node_modules/.bin/esbuild --log-level=warning --format=esm --platform=node \
		.gofabric-levels.mts --outfile=.gofabric-levels.mjs && node .gofabric-levels.mjs >"$tmp") ||
		{ cat "$tmp.err" >&2; rm -f "$tmp" "$tmp.err"; exit 1; }
else
	cat "$tmp.err" >&2; rm -f "$tmp" "$tmp.err"; exit 1
fi
rm -f "$tmp.err"

level=$(node -e 'const m=JSON.parse(require("fs").readFileSync(process.argv[1],"utf8")).matter; if(m.tag!==process.argv[2]) {console.error(`tag ${m.tag} != ${process.argv[2]}`); process.exit(1)} process.stdout.write(m.revision)' "$tmp" "$tag")
mv "$tmp" "$out/$level.json"
echo "generate-level: $tag -> parity/levels/$level.json"
