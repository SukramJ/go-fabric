#!/usr/bin/env python3
"""BX4: rewrite module-relative references controller/<moved> -> client/<moved>.

A reference preceded by '/' or a word character (a matter.js path such as
behavior/system/controller/discovery) is left alone. Usage:
rewrite_refs.py <root> <ext>[,<ext>...] [file ...]
"""
import os
import re
import sys

root = sys.argv[1]
exts = tuple(sys.argv[2].split(','))
only = sys.argv[3:]
pat = re.compile(r'(?<![/\w.-])controller/(discovery|exchange|handshake|interaction|peer)\b')


def files():
    if only:
        for f in only:
            yield os.path.join(root, f)
        return
    for d, dirs, fs in os.walk(root):
        dirs[:] = [x for x in dirs if x not in ('.git', '.claude', 'node_modules', 'site')]
        for f in fs:
            if f.endswith(exts):
                yield os.path.join(d, f)


total = 0
for p in files():
    with open(p, encoding='utf-8') as fh:
        s = fh.read()
    n, k = pat.subn(r'client/\1', s)
    if k:
        with open(p, 'w', encoding='utf-8') as fh:
            fh.write(n)
        total += k
        print(f"{os.path.relpath(p, root)} {k}")
print(f"sites={total}")
