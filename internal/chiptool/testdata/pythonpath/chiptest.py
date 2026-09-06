# SPDX-License-Identifier: MIT
# Copyright (C) 2026 SukramJ.

"""A deliberate stand-in for connectedhomeip's scripts/tests/chiptest.

The chip YAML runner is invoked through its own entry point,
scripts/tests/chipyaml/chiptool.py, unmodified -- how a certification case is
driven is not something this module gets to reinterpret. That script imports
`chiptest` at module scope, but uses it in exactly one place: the `tests list`
branch, which enumerates every case in the SDK.

The real package is not importable outside a full CHIP bootstrap. Its import
chain, read at the pinned commit:

    scripts/tests/chiptest/__init__.py   ->  from . import runner
    scripts/tests/chiptest/runner.py:28  ->  import python_path
    scripts/tests/chiptest/runner.py:35  ->  PythonPath(
                                             '.../src/python_testing/matter_testing_infrastructure')

`python_path` is supplied by the pigweed environment that scripts/bootstrap.sh
builds, not by the connectedhomeip tree; the checkout this harness uses does
not have it, and bootstrapping pigweed to run one YAML case would cost more
than the case is worth.

Placing this module ahead of the SDK on PYTHONPATH satisfies the import
without pulling any of that in. It is not a reimplementation: every attribute
access raises, so if a future pin makes chiptool.py depend on chiptest for
real, the run stops here with this text rather than quietly doing something
else.
"""


def __getattr__(name):
    raise RuntimeError(
        "chiptest.%s was used by the chip YAML runner. This harness substitutes "
        "a stub for chiptest because the real one needs a pigweed bootstrap; "
        "that substitution is no longer safe. See the module docstring." % name)
