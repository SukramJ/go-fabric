#!/usr/bin/env python3
"""BX3 sweep 1: error message prefixes = package name (G1-9, ADR 0026 P8).

Idempotent. Run from the repository root; prints one diff line per changed
message. Rules, for the 23 package directories of inventory 5a:

  * an errors.New / fmt.Errorf literal starting with a known sub-package
    prefix (matter:, matter store:, receive:, doorlock:, ...) gets the
    package name instead;
  * an errors.New literal without a `word: ` prefix gets `<pkg>: ` in front;
  * an errors.New literal with another word prefix (conformance:, ota:,
    hex:) keeps it behind `<pkg>: `.

fmt.Errorf without a known sub-package prefix is left as it is (it wraps).
Messages a test asserts on are listed by the test run, not guessed here.
"""
import os
import re
import sys

DIRS = {
    "bdx/otaimage": [],
    "bridge": ["receive", "reply", "privacy", "securechannel"],
    "cluster/appliance": ["microwaveovencontrol"],
    "cluster/closure": ["closurecontrol", "closuredimension"],
    "cluster/core": ["matter", "identify"],
    "cluster/cover": ["windowcovering"],
    "cluster/light": ["colorcontrol"],
    "cluster/lock": ["doorlock"],
    "cluster/spec": [],
    "cluster/thermo": ["thermostat", "temperaturecontrol"],
    "cluster/wire": ["matter"],
    "controller/attestation": [],
    "controller/commissioner": [],
    "controller/dcl": [],
    "im": ["matter"],
    "internal/chipdm": [],
    "mdns": [],
    "node": [],
    "secure/attestation": [],
    "secure/sigma": [],
    "secure/spake2": [],
    "store": ["matter store", "matter"],
    "transport/btp": [],
}

CALL = re.compile(r'(errors\.New|fmt\.Errorf)\((\s*)"((?:[^"\\]|\\.)*)"')
WORD_PREFIX = re.compile(r"^([A-Za-z][A-Za-z0-9_]*): ")


def pkg_name(path):
    for line in open(path):
        m = re.match(r"package (\w+)", line)
        if m:
            return m.group(1)
    raise SystemExit("no package clause in " + path)


def main():
    changed = 0
    for d, subs in DIRS.items():
        for f in sorted(os.listdir(d)):
            if not f.endswith(".go") or f.endswith("_test.go"):
                continue
            path = os.path.join(d, f)
            src = open(path).read()
            pkg = pkg_name(path)
            sub_re = re.compile(r"^(" + "|".join(re.escape(s) for s in subs) + r"): ") if subs else None

            def fix(m):
                nonlocal changed
                fn, ws, msg = m.group(1), m.group(2), m.group(3)
                if msg.startswith(pkg + ": "):
                    return m.group(0)
                new = None
                if sub_re and sub_re.match(msg):
                    new = pkg + ": " + sub_re.sub("", msg, count=1)
                elif fn == "errors.New":
                    new = pkg + ": " + msg
                if new is None:
                    return m.group(0)
                line = src.count("\n", 0, m.start()) + 1
                print(f"{path}:{line}: {msg[:60]!r} -> {new[:60]!r}")
                changed += 1
                return f'{fn}({ws}"{new}"'

            out = CALL.sub(fix, src)
            # error texts built outside errors.New/fmt.Errorf (typed errors,
            # im.UnsupportedCommandf, ...): only the leading `matter: ` of a
            # string literal, which is the old package-wide prefix.
            for old in ("matter store", "matter"):
                if old in subs:
                    pat = re.compile(r'"' + re.escape(old) + r': ')

                    def lit(m):
                        nonlocal changed
                        line = out.count("\n", 0, m.start()) + 1
                        print(f"{path}:{line}: literal {old!r} -> {pkg!r}")
                        changed += 1
                        return '"' + pkg + ': '

                    out = pat.sub(lit, out)
            if out != src:
                open(path, "w").write(out)
    print(f"prefixes: {changed} messages changed")


if __name__ == "__main__":
    main()
