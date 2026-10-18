# Third-Party Notices — `ble/bluez`

`github.com/SukramJ/go-fabric/ble/bluez` is licensed under the Apache License
2.0 (see [`LICENSE`](./LICENSE) and [`NOTICE`](./NOTICE)). It requires the main
module `github.com/SukramJ/go-fabric`, whose own
[`THIRD-PARTY-NOTICES.md`](../../THIRD-PARTY-NOTICES.md) covers everything that
module links. This file records only what this module adds to a build.

No third-party source is copied into this tree. The D-Bus mapping is written
from the BlueZ API documents (https://github.com/bluez/bluez/tree/master/doc)
and cites them by file and section; BlueZ itself is a system service reached
over the bus, not linked.

## Go module dependencies

The license family below was read from the module's own `LICENSE` file in the
module cache, and the copyright line is quoted verbatim from it.

| Module | License | Why |
| --- | --- | --- |
| `github.com/godbus/dbus/v5` | BSD-2-Clause | the system-bus client this module speaks BlueZ through (ADR 0024 decision 3) |
| `golang.org/x/sys` | BSD-3-Clause | required by `godbus/dbus/v5`; already linked by the main module and listed in its notices |

### Verbatim copyright notices

```
github.com/godbus/dbus/v5          Copyright (c) 2013, Georg Reinke (<guelfey at gmail dot com>), Google
golang.org/x/sys                   Copyright 2009 The Go Authors.
```

## github.com/godbus/dbus/v5 — BSD-2-Clause

- Source: https://github.com/godbus/dbus
- License: BSD-2-Clause — the upstream text is reproduced verbatim at
  [`licenses/LICENSE-godbus-dbus.txt`](./licenses/LICENSE-godbus-dbus.txt),
  as its clause 2 requires for a binary redistribution.
