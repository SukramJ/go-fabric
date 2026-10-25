# Pin lifts — what moved upstream, and what it means here

One section per lift of the matter.js pin (`parity/schema.json` →
`matter.sourceCommit`). Every upstream fix between the two commits is
classified against this module: **have it** (by construction or already
ported), **gap** (a register entry in `matter_behaviour_findings.md`, closed by
a later wave), **not applicable** (JS-runtime concern, harness, a role or
feature this module does not have yet — with the milestone that will have
it), **caught up** (matter.js now implements behaviour this module already
had ahead of it — a row of the *Ahead of matter.js* register in
[`by_design.md`](./by_design.md); the two implementations are reconciled onto
matter.js and the row closes, see `CLAUDE.md` workflow step 5), or **open**
(to be settled by the test port named). Independently of the commit list,
every lift re-reads each marker of that register at the new pin: an upstream
change can close a gap without a commit message that says so. Owner's work (Fable
row); the reading map behind each section is a Sonnet `read-*.md` under
`notes/reference/`, verified line by line before anything landed here.

## 0ceb202d → 862ad75f (2026-10-18, milestone 2 phase 14)

57 commits; 16 skipped (7 `[CHIP BUILD]`, 7 version bumps, `ci:` #4787,
`docs:` #4782). Map: `notes/reference/read-matterjs-delta-862ad75f.md`.
Model: one element change, `bridged-node.element.ts:17` — BridgedNode
`composition: "full-family"` (#4735); no cluster, attribute, command, event or
revision moved. Snapshot regenerated in commit `9a6c2fb` (brief BD's branch).

### Device side

| PR | Fix | Verdict | Where |
| --- | --- | --- | --- |
| #4735 | BridgedNode PartsList lists every descendant (`DescriptorServer.ts:227-270`) | **gap → brief BD item 1** | `endpoint/assembler.go:230-238` lists direct parts |
| #4766 | keep-alive DataReport with `suppressResponse: false` so INVALID_SUBSCRIPTION is read on every transport (`ServerSubscription.ts:913-914`) | **gap → register** | `bridge/subscribe.go:447-453` sets `SuppressResponse = true` (the rule this commit reverses) |
| #4742 | a subscription that ends while the node runs is dropped from the store; only `closeForShutdown` keeps it (`Subscription.ts:21-40`, `ServerSubscription.ts:766-812`, SubscriptionsServerTest) | **gap → register** | `im/subscription/manager.go:128-149` keeps the former rule: closed on session close = re-established after restart |
| #4768 | server: another exchange's write of an attribute a chunked write holds is answered BUSY (`InteractionServer.ts:507,526`) | **gap → register** | `bridge/exchange_routing.go:62` tracks the chunked write; no BUSY path |
| #4775 | KeypadLockout stays as configured or stored; NoLockout only when undefined (`ThermostatUserInterfaceConfigurationServer.ts:30-32`) | **gap → register** | `cluster/thermo/thermostatuserinterfaceconfiguration.go:31-32` resets on every start |
| #4736 | OTA provider: `NewVersion 0` under a known token applies the token's version; another version than the token's is refused Cancelled (`OtaSoftwareUpdateProviderServer.ts:419-458`) | **gap → register** | `cluster/core/ota_provider.go:558-563` checks consent on the raw `NewVersion` only |
| #4745 | a utility component the composing endpoint lists itself counts that endpoint as a candidate (`DeviceTypeConformance.ts:596-614`, Core § 9.2.1) | **gap → register** | `endpoint/devicetype_conformance.go:842-850` iterates the composition scope only |
| #4746 | a condition true only by a `Root` assertion never refuses; the violation is marked `assertedAtRoot` and logged (`DeviceTypeConformance.ts:65-76`, `ConditionAssertions.ts:63-68`) | **gap → register** | `endpoint/devicetype_conformance.go:503-526` adds Root-asserted conditions; `devicetype_validation.go:333-339` refuses the resulting violations like any other |
| #4779 | a replaced Fabric shares groups, ACL and ICD state with its replacement | **have it** (by construction) | `handleUpdateNOC` updates the NOC of the session's fabric index in place; ACL, groups and ICD state are keyed by fabric index and untouched |
| #4778 | only a change of the fabric the armed fail-safe owns is deferred; UpdateFabricLabel / SetVIDVerificationStatement under a fail-safe without AddNOC/UpdateNOC are final | **open** — no deferred commissioned-state update exists here, so a label change takes effect at once; whether a `SetVIDVerificationStatement` under an ownerless fail-safe is kept (ours drops it with the pending update, `operational_credentials.go:522-528`) is settled by the OperationalCredentialsServerTest port (phase 16) |
| #4777 | client answers the priming report of a cancelled subscribe with INVALID_SUBSCRIPTION; the server drops it | **open** (server side) — `bridge/subscribe.go:721-780` waits for the peer's status per chunk; that a non-success status on the priming report ends the subscription is settled by the ServerSubscriptionTest port (phase 16) |
| #4765 | an out-of-range vendor ID in a manual code is an invalid payload | **have it** — `secure/setup/decode.go:96-107` validates through `assertValidPayloadIdentity`, which returns the invalid-payload error |
| #4759 | AtomicRequest: request-wide checks before per-attribute statuses | **not applicable** — no atomic-write handler here (thermostat presets, milestone 3; chipdm acknowledges the command) |
| #4737 | WebRTC requestor sessions keyed per peer | **not applicable** — no WebRTC server (milestone 6 stubs) |
| #4734 | ML-DSA attestation certificates skipped at debug | **not applicable** — ML-DSA is not admitted here (`controller/dcl/revocation.go:389-391`); no fetch or store path for such a PAA |
| #4758 | IPv4 multicast membership on the configured interface | **not applicable** — IPv6 only (`transport/udp/multicast.go`) |
| #4731 #4732 #4748 #4749 #4762 #4771 | reference lifetimes, migration hook, WebSocket, console format, transaction lock, cache lifetime | **not applicable** — JS runtime |
| #4738 #4772 #4774 #4776 #4786 #4717 #4788 (harness part) | matter.js test harness | **not applicable** |

### Controller side

| PR | Fix | Verdict | Where |
| --- | --- | --- | --- |
| #4741 | DNS-SD TXT keys match regardless of case, first wins (`DnssdParameters.ts:15-49`, RFC 6763 § 6.4) | **gap → register** | `controller/discovery/name.go:154-162` `Parameters` is a case-sensitive map |
| #4756 | known answers carry their remaining TTL and are left out past half TTL (`DnssdSolicitor.ts:280-291`, RFC 6762 § 7.1) | **gap → register** | `controller/discovery/solicitor.go:368-375` copies records unchanged |
| #4770 | a client node is seeded once BasicInformation and every endpoint of the root's PartsList are in; a root-only peer qualifies (`ClientNodeLifecycle.ts:52-56`) | **gap → register** | `controller/client/config.go:116-118`, `node.go` `evaluateSeeded` keep "more than the root endpoint" |
| #4747 | discovery error kinds (`DeviceNotDiscoveredError`, `DiscoveryIncompleteError`, `NoDiscoveryTransportError`) and an abort that stops every attempt | **gap → register** (API) | `controller/discovery`, `controller/commissioner`: one error, no abort option |
| #4750 | a status the peer sent is told from one produced locally (`ResultStatus.received`) | **gap → register** (API, low) | `controller/interaction/interaction.go:56` `StatusError` has no origin |
| #4752 | a closed sustained subscription never reports itself active | **open** — `controller/interaction/sustained.go:122-225` gates `setActive` on close in three places; the in-flight-then-success case is settled by the SustainedSubscriptionTest port (phase 16) |
| #4751 | one owner for a client node's event number and data versions | **open** — `controller/client/network.go:78-259`; settled by the ClientEventNotificationTest port (phase 16) |
| #4760 | every ClientNode write reaches the device, the cache follows the device | **have it** — no local cache write exists; the image changes when the peer reports (`controller/client/interact.go:100-110`) |
| #4767 | invoke batching per request options | **not ported** (documented, `controller/interaction/interaction.go:12`); unchanged |
| #4769 | ICD Check-In key refresh kept in step | **not applicable** until milestone 5 (no ICD client) |
| #4733 | controller-side OTA: an update applied without restart fails | **not applicable** until milestone 5 (no controller OTA manager) |
| #4788 | `caseConnectionTimeout` commissioning budget | **open** — no such budget in `controller/commissioner`; settled by the CaseConnectionTimeoutTest port (phase 16) |
| #4743 | `PeerDescriptor.dataModelRevision` removed | **not applicable** (type only) |
