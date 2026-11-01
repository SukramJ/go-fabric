# W5 verdict (2026-11-01, HEAD 769ea3c2)

Theme files: `wave-5-ex.md` (Opus, done), `wave-5-api.md` (Opus, done).

## Verified against the code (Fable re-read)

| ID | Severity | Verdict |
| --- | --- | --- |
| W5-ex-01 | **high** (fabrictest / host paths with `#` or `?`) | confirmed: `node/store_path.go:30`, `controller/open.go:36`, `fabrictest/device.go:159`, `examples/quickstart-device/main.go:72` build `"file:"+path+"?…"` without escaping; a `#`/`?` in the path opens another file and drops the pragmas (foreign keys off). Go subtests named `iter#02` hit it in fabrictest. Fix: one `node`-level opener with `url.PathEscape`/`(&url.URL{Scheme:"file", Path: path, RawQuery: …}).String()`, used by all four sites. |
| W5-ex-05 | medium | confirmed: no `Chmod`/`0600` anywhere in node/, controller/, store/, examples/ — the SQLite file holding operational keys (and the controller's CA root key) is created under the umask. `docs/guides/operations.md:37` says to protect it like a key. Fix: create with 0600 (and the WAL/SHM siblings), document. |
| W5-ex-02, 03, 04 | medium | reviewer-confirmed (quickstart-device bypasses the facade; quickstart-controller uses `Nodes()[0]` instead of the paired node; reference-bridge README stale) — doc/example fixes, no API change. |
| W5-api-01 | medium (API) | confirmed: `node/commissioning.go:483` OpenBasicWindow, no Close on the Node; `node.go:977` hands out the bridge's `CommissioningWindow`. matter.js pairs allow/end (`DeviceCommissioner.ts:130, :319`). Owner decision: add `CloseWindow(ctx)` and stop exposing the bridge type (API break for callers of `CommissioningWindow()`). |
| W5-api-02 | medium (API) | confirmed: `device/options.go:40-44` probe and `device/lifecycle.go:112` SetReachable; only the latter emits ReachableChanged. matter.js has one `reachable` state (`BridgedDeviceBasicInformationServer.ts:37`). Owner decision: one input (SetReachable, probe removed or probe drives SetReachable). |
| W5-api-03 | medium (API) | confirmed: `node/node.go:164` `New(cfg)` without ctx, `:182` `context.Background()` for store open/migrate; goroutines until Stop. Owner decision: `New(ctx, cfg)` (API break) or defer the store work to Start. |
| W5-api-04, 05 | medium (API) | reviewer-confirmed: four callback shapes against ADR 0025 principle 8; `device.Logger` package global ignores `node.Config.Logger` (ADR 0026 principle 9). |

## Counts (from the reviewers' reports)

| Theme | high | medium | low | note |
| --- | --- | --- | --- | --- |
| ex (Opus) | 1 | 4 | 6 | 4 |
| api (Opus) | 0 | 5 | 12 | 3 |
| **W5** | **1** | **9** | **18** | **7** |

## Owner decisions pending (W5)

W5-api-01/02/03 are API breaks (README §API stability; owner rule: remove, do not deprecate, while loom is the only consumer). W5-api lows list five duplicate-shape pairs for removal.
