# Third-Party Notices — `ble/esphome`

`github.com/SukramJ/go-fabric/ble/esphome` is licensed under the Apache License
2.0 (see [`LICENSE`](./LICENSE) and [`NOTICE`](./NOTICE)). It requires the main
module `github.com/SukramJ/go-fabric`, whose own
[`THIRD-PARTY-NOTICES.md`](../../THIRD-PARTY-NOTICES.md) covers everything that
module links. This file records only what this module adds to a build.

No third-party source is copied into this tree. The ESPHome native API is
encoded by hand from its message definitions in aioesphomeapi
(https://github.com/esphome/aioesphomeapi, `aioesphomeapi/api.proto`, MIT),
cited by file and line; ESPHome's firmware
(https://github.com/esphome/esphome) is read for its behaviour and cited, not
copied. Neither is linked: a proxy is a device reached over TCP.

## Go module dependencies

The license family below was read from the module's own `LICENSE` file in the
module cache, and the copyright line is quoted verbatim from it.

| Module | License | Why |
| --- | --- | --- |
| `github.com/flynn/noise` | BSD-3-Clause | the Noise handshake (`Noise_NNpsk0_25519_ChaChaPoly_SHA256`) of the API's encrypted transport — the one Noise library of this module |
| `golang.org/x/crypto` | BSD-3-Clause | required by `flynn/noise` (Curve25519, ChaCha20-Poly1305, BLAKE2); not linked by the main module |
| `golang.org/x/sys` | BSD-3-Clause | required by `golang.org/x/crypto`; already linked by the main module and listed in its notices |

The package `esphome` links only those beside the main module.
`cmd/reference-controller` is the reference controller through ESPHome
proxies and links, besides, what `examples/reference-controller` links in the
main module — the SQLite driver `modernc.org/sqlite` and its dependencies,
all MIT or BSD-3-Clause and recorded with their copyright lines in the main
module's notices.

### Verbatim copyright notices

```
github.com/flynn/noise             Copyright (c) 2015 Prime Directive, Inc. All rights reserved.
golang.org/x/crypto                Copyright 2009 The Go Authors.
golang.org/x/sys                   Copyright 2009 The Go Authors.
```

## github.com/flynn/noise — BSD-3-Clause

- Source: https://github.com/flynn/noise
- License: BSD-3-Clause — the upstream text is reproduced verbatim at
  [`licenses/LICENSE-flynn-noise.txt`](./licenses/LICENSE-flynn-noise.txt),
  as its clause 2 requires for a binary redistribution.

## golang.org/x/crypto — BSD-3-Clause

- Source: https://go.googlesource.com/crypto
- License: BSD-3-Clause — the upstream text is reproduced verbatim at
  [`licenses/LICENSE-golang-x-crypto.txt`](./licenses/LICENSE-golang-x-crypto.txt).
