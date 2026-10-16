# Brief AX — open findings: codec, IM, controller, tooling

Owner: Fable. Implementer: Opus. Part of one findings wave with brief AY
(app clusters, harness — another Opus) and the owner (security paths:
`bridge/`, `node/`, `transport/`, `secure/` except `secure/setup`,
`cluster/core/{operational_credentials,general_commissioning,access_control}.go`).
Stay out of their paths; `CHANGELOG.md` and `notes/parity/` are the
owner's (put your CHANGELOG lines and by_design candidates in the
report). Branch `findings/ax` from `planning/briefs-findings`, pushed,
**no PR**. Report `notes/briefs/reports/AX.md`. Never run chip-tool.

Read `CLAUDE.md` (incl. Token use), the register
`notes/parity/matter_behaviour_findings.md` at the lines below, and the
owner's table `notes/briefs/reads/open-findings.md`. For every item:
read the cited matter.js (`git -C ../matter.js show 0ceb202d:<path>`) or
chip (`git -C ../connectedhomeip show 6170af8461b10b1766044122ac83332c6d00ab20:<path>`)
source, mirror it, cite it, add a test that fails before the fix. If the
register's description no longer matches the code (fixed meanwhile, or
the path is the host's, not this module's), say so and close it without
a change. One commit per item group below; `make reachability` in each.

## Items (register line → decision)

1. **TLV (437):** fixed-width integer encodes beyond the sanctioned
   SubscriptionID/DataVersion family — narrow to matter.js's encodings,
   fix the provenance comments (`tlv/encode.go`); invalid UTF-8 on decode —
   mirror matter.js's string decode (reject or replace — read it).
2. **IM invoke (437):** enforce `maxPathsPerInvoke` and honour Invoke
   `SuppressResponse` as matter.js/chip do (`im/invoke.go`).
3. **Mandatory command without a handler (433):** `spec.Server`
   construction refuses a mandatory command with no handler; check every
   mounted server still constructs (a test over `device/` defaults).
4. **Negative-write guards (437):** the parity guards pin implementations
   that are not mounted — point them at the mounted servers.
5. **Controller registry (443):** record follows the descriptor on every
   save (`peer.Observer.Updated`); `ForDevice` stores the record TTL (the
   scanner exposes it); a `FabricRemovedError` counterpart in the
   decommission confirmation classes. Mirror `Peers.ts`/`CommissioningClient.ts`.
6. **Group sender (445):** group writes list-chunked as matter.js does.
   The `epochStartTime` comparison stays mirrored and the ephemeral send
   socket is already `BD-Matter-GroupSendFromEphemeralPort` — report
   both as by_design candidates, no change.
7. **Binding target (447):** `core.TargetStruct`'s sentinel makes
   endpoint 0 impossible as a target — encode absence explicitly.
8. **OTA (435, 449, 518):** `queryRunning` cleared with `defer`;
   **decision:** the provider's Operate/CASE ACL entry goes on **every**
   fabric a requestor queries from, not the first only (a divergence —
   by_design candidate citing `OtaSoftwareUpdateProviderServer.ts:118`);
   consent reset and `userConsentNeeded` stay mirrored (by_design
   candidates); TC-SU-2.7's two asserts — follow matter.js, record why.
9. **setup codes (451):** `QRCode` refuses what matter.js's encoder
   refuses (`PairingCodeSchema.ts:277-330`, `INVALID_PASSCODES`,
   `assertValidPayloadIdentity`); add the 21-digit manual-code encoder.
10. **DCL (455):** fold `controller/dcl/cert.go` into exported helpers of
    `controller/attestation`; the expired-CRL behaviour, the unported
    parts and the context-less seams are by_design candidates (no API
    break before the freeze).
11. **Codegen (441, 450):** clustergen resolves cross-cluster datatypes
    (Channel `ExternalIdList` → `ContentLauncher.AdditionalInfoStruct`);
    generate definitions for the snapshot clusters `controller/client`
    lacks where clustergen can (not the hand-written AccessControl,
    OperationalCredentials, GroupKeyManagement; not camera, Joint Fabric,
    TLS, Thread) — list what stays and why.
12. **Store hygiene (454):** `matter_diagnostics` table and API: mark the
    API Deprecated (README §API stability) and stop creating the table in
    a new migration only if no reader exists — read first; `endpoint/sqlitestore`
    gains `Migrations()` and `SchemaFingerprint()` like `store`.
13. **Wording (454):** host-specific words in public docs and logs
    (`north.matter.mdns_advertise`, "matter.enabled", "v1.1/v1.2" in
    `store`, loom terms in `contract`) — module terms only; a log key
    rename is a CHANGELOG line.
14. **Tooling (447):** `make chipdm-check` from a worktree without an
    absolute `CHIP_ROOT`.
15. **Flakes (452, 453):** `TestReactionLeave` waits on `NodeIDs`;
    `TestStartReaper_ActiveSessionSurvivesSweeps` on a fake clock.

Guards: `make ci`, `make race`, `make reachability-check`,
`make chipdm-check` if a definition was added (export
`PATH=$HOME/go/bin:$PATH`; usual rerun rules). Report per item: done /
closed-stale / by_design candidate, with sources; CHANGELOG lines.
