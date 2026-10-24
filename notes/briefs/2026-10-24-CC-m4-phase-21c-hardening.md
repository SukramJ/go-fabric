# Brief CC — M4 phase 21c: the limits page, `node.Config.Limits`, fuzz targets for the controller's decoders

Date 2026-10-24. Concept v7 (`docs/concept-v7-operation-and-packaging.md`
A.4, A.5 row CC), the read map `notes/briefs/reads/read-m4-phase-21-22.md`
§3 (3.1 every limit with constant and file:line, 3.2/3.3 PASE handling
here and in matter.js, 3.4 the 24 fuzz targets and the decoders without
one — a map, verify before acting), ADR 0025 P5 (spec-near names),
ADR 0026 P1, `docs/security.md:24-25` (the PASE paragraph). Brief CB's
report `notes/briefs/reports/CB.md` (read Differences and Left out; its
`observability.md` is the sibling page — do not duplicate). matter.js
pin `862ad75f`, read only via `git -C $MATTERJS_DIR show 862ad75f:<path>`;
cite lines. go-fabric `main` after CB has merged (one implementing
agent at a time — owner 2026-10-23: start when `gh pr list --state open`
shows no `m4/phase-21b` pull request and `git log origin/main --oneline -5`
shows CB's squash). Branch `m4/phase-21c`, commits `m4 CC: <step>`, one
pull request, no auto-merge, no chip-tool label (no composition change;
`Config.Limits` defaults equal today's constants, so the reference
bridge's behaviour does not move). The removal rule of 2026-10-21
applies. Version: a `v0.10.x` patch. The owner merges.

**Model:** Opus for §1–§3. **Owner paths stay the owner's:** no fuzz
target and no change in `client/handshake`, `secure/sigma`,
`secure/spake2`, `secure/channel`, `secure/operational`,
`cluster/core/{access_control,operational_credentials,
general_commissioning,group_key_management}.go`; where §2 threads a
limit through one of them, the diff there is the constant's read
replaced by the configured value and nothing else — list every such
line in the report. The PASE limit changes nothing (concept A.4: only
the table; read map 3.2/3.3 say the behaviour already mirrors
`PaseServer.ts:33-34, :106-110`).

## 1 The limits page

`docs/guides/limits.md` (new page, `mkdocs.yml` nav next to
`observability.md`): one table, one row per limit of read map §3.1 —
what, value, the constant and file (an absolute GitHub URL, the strict
build rejects relative ones), the spec or matter.js reference (cite the
matter.js line where the value is matter.js's, e.g. `MAX_SESSIONS_PER_PEER`
`SessionManager.ts:185-189`, `PASE_COMMISSIONING_MAX_ERRORS`
`PaseServer.ts:34`; "module's own" where matter.js has none, with the
reason from the constant's comment), what happens at the limit (the
status or error the peer sees, the log event), and whether a host sets
it (§2's answer). Rows at least: sessions per peer, subscriptions per
fabric, interval ceiling, paths per invoke, PASE errors and lockout,
pairing timeout, fabrics (254 / facade default 5), ACL entries per
fabric, groups and group keys per fabric, TCP connections per peer and
in-flight per connection, unsecured duplicate windows, the session-miss
burst table, controller exchanges per session, controller subscription
ceiling, MRP backoff (`transport/mrp`: base intervals, retransmissions,
cite `MessageReceptionState`/`MRP` constants of matter.js). Every row's
value is checked against the code by a test: `docs/guides/limits_test.go`
is not possible — write `script/docscheck`-style assertions instead
(read how `script/docscheck` verifies a cited line; a table cell that
names `Name = value` is compared with the Go constant by a test in the
constant's package, or by one test in `node` that imports the exported
ones and reads the page; say which, prefer the one that fails when a
constant moves).

## 2 `node.Config.Limits`

- Rule (concept A.4): where matter.js keeps a value configurable, the
  host gets it; where matter.js has a constant, it stays a constant
  here. Read, per row of §1, whether matter.js configures it — the
  likely places: `packages/protocol/src/session/SessionManager.ts`
  (sessions), `packages/protocol/src/interaction/ServerSubscription*.ts`
  and `packages/node/src/node/server/ServerNode*.ts` or
  `ServerEnvironment.ts` (`subscriptionOptions`: max interval,
  min/max subscriptions), `packages/protocol/src/protocol/MessageExchange.ts`
  / `MessageReceptionState.ts` (MRP), `packages/node/src/behaviors/network/`
  or `commissioning/` (the fabric count `SupportedFabrics` — already
  the facade's default 5, `node/config.go:428-431`), the ACL limit
  (`AccessControlServer.ts`), groups (`GroupKeyManagementServer.ts`,
  `maxGroupsPerFabric`/`maxGroupKeysPerFabric` attributes). Cite each
  finding in the report's table; the ones matter.js configures become
  fields of `node.Limits{…}` on `node.Config.Limits` (zero = today's
  default, documented on the field with the default and the bound);
  the rest stay constants and the page says "constant".
- Plumbing: `node` passes the value where the constant is read today
  (`im/subscription.Config.MaxSubscriptionsPerFabric` exists already;
  `bridge.Config.MaxPathsPerInvoke` exists). A new knob below `node`
  is a config field of the component, not a package variable. Validate
  at `node.New` (a value out of the spec's bound → `ErrConfiguration`
  naming the field and bound, the pattern of `node/config.go`).
- Tests: per field, a node built with a non-default value shows it
  (the subscription quota refused at N+1 with `ResourceExhausted`; the
  attribute `MaxGroupsPerFabric` read through `fabrictest`; …), and
  the zero value equals the constant (one table test over the fields).
  CHANGELOG `### Added`.

## 3 Fuzz targets for what reads foreign bytes on the controller side

Read map §3.4 names the decoders without a target. Add, in the
existing pattern (`im/fuzz_invoke_test.go`, `secure/setup/fuzz_setup_test.go`:
seed corpus from the package's fixtures, the invariant stated in a
comment, no panic, bounded allocations):
- `client/interaction`: `FuzzDecodeReport` over `decodeReport`
  (report.go:427) and the subscribe-response path — the invariant: an
  arbitrary ReportData either decodes or returns an error, never
  panics, and the decoded attribute count is bounded by the input
  length.
- `client/discovery`: `FuzzParseTXTData` (txt_data.go:101),
  `FuzzParseTxtDecimal` (:71), `FuzzDecodeSupportedTransports` (:30);
  `mdns`: `FuzzParseDNSMessage` (query_conn.go:125).
- `client/exchange`: `FuzzDecodeStatusReport` (errors.go:142).
- `controller/dcl`: `FuzzParseRevocationSet` (revocation.go:812),
  `FuzzParseCRL` (crl.go:324) — the DCL answers are foreign JSON and
  DER.
- `controller/attestation`: `FuzzDecodeCDContent` (cd.go:151),
  `FuzzDecodeAttestationElements` (:195), `FuzzParseAttCert`
  (cert.go:77) — these read a peer's bytes; they are the controller's
  attestation path, Opus writes the harness only, no change to the
  functions (any defect found is reported, not fixed, in the report's
  Questions).
- `controller/commissioner`: `FuzzDecodeCertificateChainResponse`,
  `FuzzDecodeNOCResponse`, `FuzzDecodeCertSigningRequest` (wire.go),
  `FuzzDecodeNetworks` (network.go:129) — same rule: harness only.
- `bdx/otaimage`: the image header reader (`ReadOTAImage`'s header
  parse; read `bdx/otaimage` for the function) — `FuzzReadHeader`.
- Not in this wave (owner's): `client/handshake`, `secure/sigma`,
  `secure/spake2`. Say so in the report.
`make fuzz` discovers the targets (Makefile:115-124; no list to
update); run it once with the default `FUZZTIME` and once with
`FUZZTIME=1000000x` for the new targets only, report runtime. A finding
(a panic, an unbounded allocation) is a defect: fix it when the
function is on an Opus path (`client/*`, `mdns`, `controller/dcl`,
`bdx`), report it unfixed with the reproducer when it is on an owner
path. CHANGELOG `### Added` names the targets in one line; a fixed
defect gets a `### Fixed` line.

## Guards and report

`make ci`, `make race` (once; no new concurrency unless §2 adds a
lock — then twice), `make fuzz`, `make archcheck`, `make reachability`
then `-check`, `make cite-check` (`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`),
`make testparity-check`, `go test ./script/...`, `mkdocs build
--strict`. Report `notes/briefs/reports/CC.md`: **Limits** (the table
of §1 with the matter.js verdict per row), **Config.Limits** (fields,
defaults, bounds, plumbing sites incl. every owner-path line),
**Fuzz** (targets, runtimes, findings with reproducers), **Guards**
with HEAD, **Differences**, **Decisions**, **Left out**, **Questions**,
`by_design` candidates with citations. Copy this brief to
`notes/briefs/2026-10-24-CC-m4-phase-21c-hardening.md` in the first
commit. Never add attribution lines to commits or the PR body.
