# Brief phase 33 — new test-parity register roots (M8)

Date 2026-11-04. Roadmap `docs/roadmap.md` phase 33: the roots
`packages/types/test/tlv`, `packages/general/test/net`,
`packages/general/test/codec`, `packages/model/test/aspects`,
`packages/types/test/schema`, `packages/general/test/crypto` (52 test files,
782 static `it` sites) enter the test-parity register, ported by meaning;
"the rest of general, model and nodejs excluded with a reason". **Model:**
one Opus agent; the crypto root touches `secure/`, so every cited line of
that root is read by Fable at the pin before the merge (owner decision
2026-11-04). Branch `m8/phase-33` from main 19588e9b, worktree
`../go-fabric-p33`, logs and `summary.log` in
`~/.cache/go-fabric-scratch/p33/`. Pin matter.js 862ad75f
(`git -C ../matter.js show 862ad75f:<path>`; never the working tree).
Map: `~/.cache/go-fabric-scratch/read-phase33.md` (Sonnet; §A the tool's
mechanics, §B the "rest", §C–H per root with COV/PORT/JS/NOSUBJ/CHK per
describe group). **A map, never a citation**: verify every line you build
on; every `// Mirrors …` and every register `reason` names lines you read
at 862ad75f.

## 0 Working rules

`export PATH=$HOME/go/bin:$PATH GOTMPDIR=$HOME/.cache/gotmp`. Commits as
`SukramJ <sukramj@icloud.com>` (`git var GIT_AUTHOR_IDENT` first),
`<package>: <what> (phase 33)`, no attribution lines, one commit per root
at least; stage named paths, never `git add -A` after a `go build` of a
main package (a tool binary at the root was committed once). Guards under
`flock ~/.cache/go-fabric-scratch/guards.lock`: `make testparity-check`,
`make cite-check`, `make reachability-check`, `make apidiff-check`,
`make ahead-check`, `make ci`, `make race`, `make cover-check`
(`make cover-check` and `make reachability-check` are ratchets — regenerate
and commit in the same commit as the change: **every new test citation
needs `make cite-report && make reachability` regenerated in the same
commit** or CI fails). No chip-tool run (the daemon's composition and
FeatureMaps do not move; say so). Pull request against main at the end,
body = report, auto-merge off, hand back with SubagentHandback.

Port by meaning, as phases 31 and 32: one Go test may cover a group of
matter.js cases (`go[]` names it; `missing[]`/`left[]` name the `it`s not
covered, free text or `"<describe> > <it>"`). A case whose behaviour the
module does not have is `left` with class `by-design` (a `by_design.md`
row, one paragraph, citations, one sentence of reasoning) or `missing`
(a findings row in `notes/parity/matter_behaviour_findings.md`) — never
silently dropped. The citecheck identifier heuristic: a sentence that
cites `File.ts:NN-MM` names an identifier on those lines and no other.

## 1 Owner decisions 2026-11-04 (wizards)

1. **Register rows for every file.** Besides the six roots, the whole roots
   `packages/general/test`, `packages/model/test`, `packages/nodejs/test`
   and `packages/types/test` enter `snapshotRoots`
   (`script/testparity/main.go:120`; the six are inside them). Every file
   not ported gets an `excluded` row with class and reason (≈140, see
   map §B: general 67, model 37, nodejs 21, types 15). Eight files of the
   "rest" are **ported, not excluded**: `general/test/math/VerhoeffTest.ts`
   (Go `secure/setup/setup.go:316` verhoeffCheck), `general/test/util/IpTest.ts`,
   `BytesTest.ts`, `NumberTest.ts` (Go net/netip, `client/discovery/service.go:58`,
   the byte/number helpers the codec uses — port what has a Go subject,
   `left` the rest), `model/test/logic/EffectiveAccessTest.ts`
   (`cluster/spec/model.go:8-60`), `DefaultValueTest.ts`,
   `RequirementApplicabilityTest.ts` (`cluster/spec/conformance.go`),
   `nodejs/test/crypto/NodeJsCryptoTest.ts` (`secure/sigma/ipk_test.go:25`
   cites it). `packages/matter.js`, `nodejs-ble`, `nodejs-ws`, `testing`,
   `support/codegen` stay out of the roots, named with their reason in the
   intro of `notes/parity/matterjs_tests.md` (`render.go:29-30`).
2. **mdns.** The relevance pre-filter (`MdnsRelevanceFilterTest.ts`, 36;
   the relevance cases of `MdnsSocketTest.ts`, 12) is `left: by-design`:
   a performance pre-filter of matter.js's socket layer, no wire
   behaviour; `mdns/query_conn.go:24-25` keeps it out. Query/answer
   splitting (`MdnsSocketTest.ts` L290-425, 5): read `MdnsSocket.ts` at the
   pin — if matter.js splits a query or answer that exceeds the MTU on the
   wire, port it into `mdns/query_conn.go` `PackDNSQuery` (:146) /
   `mdns/answer.go` with a parity test; else `left` with the line that
   shows it is not wire behaviour.
3. **TLV.** (a) The decoder **rejects implicit-profile tags** as matter.js
   does (`TlvCodecTest.ts:60-68`, readTag throws UnexpectedDataError;
   chip's TLVReader errors without an ImplicitProfileId): change
   `tlv/decode.go:103-112`, retire the "by design (L3-D2)" note, port
   the three cases, a fuzz-corpus check, CHANGELOG `### Changed`. (b) The
   TlvObject schema-layer cases (inject/remove field, ValidationError,
   protocol-specific tags, chunked arrays via TlvArray — ≈35, map §C) are
   `left: by-design` (structs and the generated `cluster/spec` codecs are
   the module's schema layer; the chunk assembly lives in
   `client/interaction/report.go:285-370` and is covered there) and the
   **five FixMe skips go** (`tlv/object_parity_test.go:694-740`, rows in
   `register.json` `skips`). (c) `relaxNumberTypeChecks` (8) and
   zero-length-as-null (7) are `left` with the reason (Go is strict as
   matter.js's default; the wire null element).
4. **crypto.** Opus ports `packages/general/test/crypto`; Fable reads
   every cited line at the pin before merge, so cite precisely. AES-CCM
   with tag 8 / L=3 and the CMAC cases are `left: by-design`: Matter uses
   AES-CCM with nonce 13, tag 16, L=2 only (`secure/aesccm/aesccm.go:19-25`)
   and no CMAC. The pure-JS AES/hash/PEM/key-format cases are
   `excluded`/`left` js-runtime-only (Go stdlib); `MlDsaTest.ts` and
   `Spake2pTest.ts` are already ported by meaning
   (`controller/attestation/pqc_parity_matterjs_test.go`,
   `secure/spake2/parity_matterjs_test.go`) — register them, refresh their
   cites to 862ad75f, port the cases they miss (map §H).

## 2 Per root (map §C–H; the map's PORT/COV/JS/NOSUBJ are hints)

- **tlv** (13 files, 218): COV groups → register the existing tests
  (`tlv/codec_parity_test.go`, `tlv/object_parity_test.go`,
  `tlv/parity_matterjs_test.go`, `tlv/tlv_test.go`, `tlv/is1_truncation_test.go`)
  with their cites refreshed to 862ad75f; PORT groups (generic decoding,
  nullable bitmap MSB, bound ranges, string bounds, byte sizes, fabricIndex
  on write) get Go tests in `tlv/` or `cluster/spec/`; JS groups `left`.
- **net** (16 files, 228): COV groups → register `client/discovery/*_test.go`
  and `mdns/*_test.go`; PORT: ServiceDiscovery (browse), UdpMulticastServer
  per-family send (`mdns/socket.go:217`), socket close behaviour; the
  RetrySchedule cases against `client/discovery` and `client/interaction`;
  ServerAddressSet NOSUBJ → `left`; http/* and mock/* `excluded`
  (js-runtime-only / test-infrastructure).
- **codec** (3, 34): Base64 → `left` (stdlib); DerCodec decode refusals →
  COV `secure/mattercert/der_edges_test.go`; DnsCodec → PORT what
  `mdns/query_conn.go` ParseDNSMessage/escape functions cover (TXT edge
  cases, bad pointers, unreadable record keeps the readable ones).
- **model/aspects** (4, 125): text parser/serializer cases are JS (`left`:
  the AST arrives pre-parsed in `parity/schema.json`); conformance
  parsing has a Go subject in `internal/chipdm/conf.go:445` (register
  `conf_test.go`; port the invalid-form cases); applicability →
  `cluster/spec/conformance_test.go`; constantOf → `cluster/spec/bound_expr.go`.
- **schema** (5, 78): PairingCodeSchemaTest (61) against `secure/setup`
  (`decode_test.go` cites 0ceb202d — refresh, port what the grown file
  adds: PBKDF both-or-neither, manufacturer tag range, vendor/product
  validation, commissioning-flow conditional); Base38 → `secure/setup`;
  Bitmap/SpecificationVersion/QrCode → `left`/`excluded` with reason.
- **crypto** (11, 99): §1.4.

## 3 Register mechanics (map §A)

`snapshotRoots` (`main.go:120`) + the package doc (`main.go:8-9`) + the
render intro (`render.go:29-30`); `make testparity-snapshot` regenerates
`matterjs_tests.json`; `TestTestparitySnapshotIsThePin`
(`register_test.go:102-129`) holds it. One `Entry` per snapshot file
(`register.go:84-95`; `go[]` refs resolve to top-level Test funcs;
`snapshotSha` = blob id). Widen the `js-runtime-only` doc (`main.go:60-64`,
`register.go:27-41`) to name platform, util, storage, time and codec
runtime tests. Ratchets in `register_test.go` (planned `:41` stays 2 —
every new file is decided in this PR; ported `:68`, missing `:69`, left
`:84` move; extend each doc comment with "phase 33"). `make testparity`
re-renders `notes/parity/matterjs_tests.md`. `docs/matter-parity-contract.md:143`
names the register — add the roots sentence. CHANGELOG `### Added`
(the roots, the new parity tests) and `### Changed` (the TLV rejection);
`docs/roadmap.md` phase-33 status line.

## 4 Report shape

Per root: files ported / partial / excluded / left with counts; the Go
tests added (file:func); differences from this brief; decisions; left
out; open questions; by_design rows written (name + marker lines);
findings rows written; every matter.js line cited, re-read at 862ad75f;
guards table (guard, HEAD, exit) with the ratchet values before/after.
