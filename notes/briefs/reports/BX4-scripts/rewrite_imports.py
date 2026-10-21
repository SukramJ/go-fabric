#!/usr/bin/env python3
"""BX4: rewrite import paths controller/<moved> -> client/<moved> in every .go file."""
import os
import re
import sys

root = sys.argv[1]
pat = re.compile(r'"github\.com/SukramJ/go-fabric/controller/(discovery|exchange|handshake|interaction|peer)"')
changed = 0
sites = 0
for d, dirs, files in os.walk(root):
    dirs[:] = [x for x in dirs if x not in ('.git', '.claude', 'node_modules')]
    for f in files:
        if not f.endswith('.go'):
            continue
        p = os.path.join(d, f)
        with open(p, encoding='utf-8') as fh:
            s = fh.read()
        n, k = pat.subn(r'"github.com/SukramJ/go-fabric/client/\1"', s)
        if k:
            with open(p, 'w', encoding='utf-8') as fh:
                fh.write(n)
            changed += 1
            sites += k
print(f"files={changed} sites={sites}")
