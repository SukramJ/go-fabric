# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
"""MkDocs hook: repository links in the published site.

A page under docs/ links to files outside docs/ (CLAUDE.md, notes/,
the Go packages) by relative path, which GitHub renders and MkDocs
cannot serve. This hook rewrites each such link whose target exists in
the repository to its GitHub URL, so the published page links to the
same file. A target that does not exist is left alone, and strict mode
still reports it as a broken link.
"""

import os
import posixpath
import re

_INLINE = re.compile(r"(\]\()([^)\s]+)(\))")
_REFERENCE = re.compile(r"^(\s*\[[^\]]+\]:\s*)(\S+)", re.MULTILINE)


def _rewrite(target, page_dir, docs_rel, repo_root, repo_url):
    """The GitHub URL of a link that leaves docs/, or None to keep it."""
    if re.match(r"^[a-zA-Z][a-zA-Z0-9+.-]*:", target) or target.startswith(("#", "/")):
        return None
    path, sep, anchor = target.partition("#")
    if not path:
        return None
    resolved = posixpath.normpath(posixpath.join(page_dir, path))
    if not resolved.startswith("../"):
        return None  # inside docs/: MkDocs validates it
    rel = posixpath.normpath(posixpath.join(docs_rel, resolved))
    if rel.startswith("../"):
        return None  # outside the repository
    full = os.path.join(repo_root, rel)
    if os.path.isdir(full):
        kind = "tree"
    elif os.path.isfile(full):
        kind = "blob"
    else:
        return None
    return f"{repo_url.rstrip('/')}/{kind}/main/{rel}{sep}{anchor}"


def on_page_markdown(markdown, page, config, files):
    repo_root = os.path.dirname(os.path.abspath(config["config_file_path"]))
    docs_rel = os.path.relpath(config["docs_dir"], repo_root).replace(os.sep, "/")
    page_dir = posixpath.dirname(page.file.src_uri)
    repo_url = config["repo_url"]

    def fix(match):
        new = _rewrite(match.group(2), page_dir, docs_rel, repo_root, repo_url)
        if new is None:
            return match.group(0)
        if match.re is _INLINE:
            return match.group(1) + new + match.group(3)
        return match.group(1) + new

    return _REFERENCE.sub(fix, _INLINE.sub(fix, markdown))
