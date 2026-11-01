# Whole-codebase review, November 2026

Owner decision 2026-11-01: a layered review of the whole module, waves along
the request path, every theme (security and crypto, concurrency and
resources, wire and IM correctness, API and architecture, cluster servers,
controller and client, tests and guards, docs and consistency). Findings go
into a register per wave, clear fixes into one pull request per wave,
decisions to the owner.

Pins: matter.js `862ad75f` (`parity/schema.json` → `matter.sourceCommit`),
connectedhomeip `6170af84` (`CHIP_TEST_IMAGE_COMMIT`). Starting HEAD `769ea3c2`.

## Waves

| Wave | Packages | Themes |
| --- | --- | --- |
| W1 | `transport/` (udp, tcp, btp, message, mrp, conn), `secure/`, `tlv/` | security, concurrency, wire |
| W2 | `bridge/`, `im/`, `endpoint/`, `mdns/`, `commissioning/`, `store/`, `groups/`, `icd/`, `clock/`, `bootid/`, `diagevent/` | ACL and IM gates, subscriptions, port contracts |
| W3 | `cluster/` (core first, then generated tranches), `schema/`, `parity/`, `conformance/`, `surface/` | cluster semantics, generated-vs-hand-written drift |
| W4 | `client/`, `controller/`, `binding/`, `thread/`, `bdx/`, BLE | state machines, timeouts, peer set |
| W5 | `node/`, `device/`, `contract/`, bridge facade, `examples/`, `fabrictest/`, `eligibility/` | API design, package cuts, apidiff surface |
| W6 | `script/`, `internal/`, `docs/`, `notes/parity/` | tests and guards, docs against code, register currency; uses W1–W5 findings |

## Method per wave

1. **Map.** Sonnet reads the wave's packages and writes an inventory
   (`notes/reads/read-review-w<N>-<pkg>.md`) with `file:line` for every
   fact: entry points, goroutines, locks, timers, crypto call sites,
   bounds checks, error paths, public API. A map, never a citation.
2. **Review.** One reviewer per theme, briefed with the map, the file
   list and the pinned sources. Model (owner 2026-11-01, token cost):
   **Fable** only on the security paths (`secure/`, `commissioning/`,
   ACL, the four `cluster/core` security servers) and for the
   verification step; **Opus** for every other review theme; **Sonnet**
   for the inventory maps. Findings into `wave-<N>-<theme>.md` here:
   ID, severity (high / medium / low / note), `file:line`, the claim, the
   matter.js or chip citation, the proposed fix.
3. **Verify.** Every high and medium finding is re-read against the code
   and the pin before it stands; false positives are struck with a reason.
4. **Fix.** Clear fixes on branch `review/w<N>`, Opus implements, guards in
   the background, one pull request per wave with auto-merge. Decision
   cases go to the owner as a wizard. What does not fit the wave stays
   open in the register.

## Finding IDs

`W<N>-<theme>-<nn>`, themes: `sec`, `conc`, `wire`, `api`, `clu`, `ctl`,
`test`, `doc`. Status: open / fixed (#PR) / struck (reason) / deferred (wave).
