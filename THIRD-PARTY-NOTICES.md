# Third-Party Notices

go-fabric is MIT-licensed (see [`LICENSE`](./LICENSE)). It is a semantic port
of `matter.js` for the Matter side and depends on a small set of Go modules.
No third-party source is copied verbatim into this tree; the Go code is
written from scratch and cites the upstream file + function it mirrors in a
file-top or inline comment, so the lineage stays auditable.

This file records the upstream projects, their licenses, and the verbatim
copyright notices they carry, so that the people behind them are credited and
the license obligations travel with any redistribution.

Full license texts referenced below live under [`licenses/`](./licenses/):
[`MIT.txt`](./licenses/MIT.txt), [`Apache-2.0.txt`](./licenses/Apache-2.0.txt)
and [`LICENSE-filippo.io-nistec.txt`](./licenses/LICENSE-filippo.io-nistec.txt),
alongside the reproduced upstream
[`NOTICE-matter.js.txt`](./licenses/NOTICE-matter.js.txt).

---

## The Matter-side gold standard

### matter.js — Apache-2.0

Everything in this module is a semantic port of matter.js HEAD: cluster IDs,
revisions, attribute IDs, constraints, defaults, and wire shape are mirrored
from it. Well over a hundred Go files cite the matter.js `path:function` they
mirror.

- Source: <https://github.com/matter-js/matter.js>
- Copyright: Copyright 2022-2026 Matter.js Authors (`LICENSE:189`).
  The project began under the Connectivity Standards Alliance organisation
  (`project-chip`, which still redirects) and now lives in its own
  `matter-js` organisation, self-described as an unofficial implementation
  of the Matter protocol. It is not a CSA project and is not certified.
- License: Apache License 2.0 — see [`licenses/Apache-2.0.txt`](./licenses/Apache-2.0.txt).
- Upstream `NOTICE`, reproduced verbatim as required by its own final line:
  [`licenses/NOTICE-matter.js.txt`](./licenses/NOTICE-matter.js.txt).
- No matter.js source code is reproduced verbatim. What does ship inside every
  binary built from this module is the parity schema snapshot — the matter.js
  element model extracted from HEAD — embedded from `parity/schema.json`
  (516,843 bytes) by the `//go:embed` in `parity/parity.go`.

---

## Go module dependencies

The dependency graph is entirely permissive (MIT / BSD-3-Clause); there is no
copyleft (GPL / LGPL / MPL / AGPL) module in the build. Each license family
below was read from the module's own `LICENSE` file in the module cache, and
each copyright line is quoted verbatim from it.

### Linked into a build that imports this module

| Module | License |
| --- | --- |
| `filippo.io/nistec` | BSD-3-Clause |
| `github.com/cenkalti/backoff` | MIT |
| `github.com/grandcat/zeroconf` | MIT |
| `github.com/miekg/dns` | BSD-3-Clause |
| `golang.org/x/net` | BSD-3-Clause |
| `golang.org/x/sys` | BSD-3-Clause |

### Test-only (reached by `go test ./...`, never by a consumer's build)

`modernc.org/sqlite` is the pure-Go SQLite driver this module's `store`
package tests open a scratch database with. The package itself takes an
already-migrated `*sql.DB` and imports no driver, so nothing in a consumer's
build links it.

| Module | License |
| --- | --- |
| `modernc.org/sqlite` | BSD-3-Clause |
| `modernc.org/libc` | BSD-3-Clause |
| `modernc.org/mathutil` | BSD-3-Clause |
| `modernc.org/memory` | BSD-3-Clause |
| `github.com/dustin/go-humanize` | MIT |
| `github.com/google/uuid` | BSD-3-Clause |
| `github.com/mattn/go-isatty` | MIT |
| `github.com/ncruces/go-strftime` | MIT |
| `github.com/remyoudompheng/bigfft` | BSD-3-Clause |

To regenerate an exhaustive, machine-verified list, run a tool such as
`go-licenses report ./...` against the module graph.

### Verbatim copyright notices

```
filippo.io/nistec                  Copyright 2009 The Go Authors.
github.com/cenkalti/backoff        Copyright (c) 2014 Cenk Altı
github.com/dustin/go-humanize      Copyright (c) 2005-2008  Dustin Sallings <dustin@spy.net>
github.com/google/uuid             Copyright (c) 2009,2014 Google Inc. All rights reserved.
github.com/grandcat/zeroconf       Copyright (c) 2016 Stefan Smarzly
github.com/mattn/go-isatty         Copyright (c) Yasuhiro MATSUMOTO <mattn.jp@gmail.com>
github.com/miekg/dns               Copyright (c) 2009, The Go Authors. Extensions copyright (c) 2011, Miek Gieben.
github.com/ncruces/go-strftime     Copyright (c) 2022 Nuno Cruces
github.com/remyoudompheng/bigfft   Copyright (c) 2012 The Go Authors. All rights reserved.
golang.org/x/net                   Copyright 2009 The Go Authors.
golang.org/x/sys                   Copyright 2009 The Go Authors.
modernc.org/libc                   Copyright (c) 2017 The Libc Authors. All rights reserved.
modernc.org/mathutil               Copyright (c) 2014 The mathutil Authors. All rights reserved.
modernc.org/memory                 Copyright (c) 2017 The Memory Authors. All rights reserved.
modernc.org/sqlite                 Copyright (c) 2017 The Sqlite Authors. All rights reserved.
```

One of these carries a per-module notice obligation and is therefore recorded
in full below.

### filippo.io/nistec — BSD-3-Clause

An exported build of the standard library's internal NIST P-curve
implementation. Linked into the binary and used for the P-256 point arithmetic
of the Matter SPAKE2+ handshake (`secure/spake2/spake2.go`); the stdlib
exposes no public API for the operations that handshake needs.

- Source: <https://filippo.io/nistec>
- Copyright notice (verbatim from upstream `LICENSE`):

  ```
  Copyright 2009 The Go Authors.
  ```

- License: BSD-3-Clause — the upstream text is reproduced at
  [`licenses/LICENSE-filippo.io-nistec.txt`](./licenses/LICENSE-filippo.io-nistec.txt),
  and its third clause names Google LLC and its contributors.
