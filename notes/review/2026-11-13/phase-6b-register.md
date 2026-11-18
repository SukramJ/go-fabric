# Phase 6b — the dispositions

The commit ids below are those of the branch of this phase's second pull
request — "the phase 6b pull request" where a number would stand
(`review2/p6b`, `dd17ab06..361102dd`, 552 commits). If that pull request
merges squashed, as the earlier phases' did, its single commits stay
retrievable through the pull request's reference. Items that describe a
weakness a party on the network could use and that stay open after this branch
are held back with their text until the pull request that fixes them merges,
or the release decision 126 names (owner decisions 25, 126); the table carries
their id and phase only, and "Counts" says how many. What this branch fixed is
public as what the code does now, the rows held in earlier phases among them.

Phase 6 is the full pass over every hand-written package, cut in two halves
with a pull request each (owner decision 120). This is 6b: waves W4 to W6 of
the plan — the client stack, the controller, the facade and the guards
(decisions 185 to 187 set its shape). Two review workflows read the tree at
`dd17ab06`, each code unit followed in the same workflow by a completion stage
on exactly the slices it left unread (decision 185): review A — `chs`, `dcl`,
`radio`, `cpeer`, `cam`, `ctl` with the reference overlay `par-node`; review B
— `facade`, `device`, `examples`, `gen`, `guards` and the bounded units
`docs`, `tests` and `cite`. One verify judged every row in eight bins: the
high and medium rows and the held verdicts, the reference and security
questions, the lows and notes of each review, the documentation's reference
claims, the citations, and a follow-up read of the referrals. The owner
answered the decision brief's step 0 and 21 steps (decisions 188 to 209) and,
during the fixes, decisions 210 to 257. Four fix waves carried the fixes,
everything confirmed being fixed (decisions 122, 188): wave 1 `chs`, `dcl`,
`fx-csql`, `radio-esp`, `radio-nix`, `cpeer` under a delta of the fix rules;
wave 2 `cam`, `facade`, `device`, `ctl`, `dev6a`, `gen`; wave 3 `examples`,
`guards` (with `fx-sup` and `fx-lint`), `docs`, `tests`, `register`, and the
6a rows still open split by path into `fxo-net`, `fxo-node` and `fxo-ctl`
(decision 209), then a post-integration pass; wave 4 `cite`, in the path units
`sec`, `cli`, `ctl`, `node` and `clu`, then `w4fix`, which applied the
reference reads of their citations, the boundary reads (decisions 255, 256)
and the DAV alias (decision 257). A reference read took each unit's first
round and every later round that fixed a medium or high or touched a security
path; fact notes were read before the waves' code. Which model did which part
is the coordinator's account (CLAUDE.md, "Which model does which work"); this
file names none.

One row per finding of the review (152 of review A, 181 of review B, the
completion stages' rows among them, and 17 the verify raised), one per item of
the reference reads (518), one per item of earlier phases the plan placed in
this half (218), one per question, one per owner decision of this phase, one
per item the fix agents, the guard runs and the coordinator noticed and did
not fix where it was noticed, and one per unit for what it did not read.
Sources: the units' reports and their completion stages', the merged verify
and its bins, the decision brief, the fix notes of the four waves with their
rounds, the reference reads, the hand-over, the owner's decisions and `git log
dd17ab06..361102dd`. The notes under `.git/` are not part of this change. The
guards of the branch are in the pull request's body, not here.

How a row was placed — by rule:

- **fixed `<commit>`** — the commit is in `dd17ab06..361102dd` and its message
  names the ID (a range of ids such as "-3075 … -3082" counts each); a fix
  note's worktree commit was mapped to the branch by the ID. What a fixed row
  says holds now is the commit's subject or the fix note's line, written
  short, and was not checked against the code for this file. **fixed in part**
  names what stays after a colon or a semicolon. A notes commit (`findings`,
  `by_design`, `register`, `notes/parity`) is named as the record, never as
  the fix; `361102dd` is the release of the held parity texts, `2eeaa87b` of
  their CHANGELOG lines.
- **parity: a recorded divergence** — the behaviour is the module's by
  decision, and the row names the `by_design` entry and the notes commit that
  wrote or amended it.
- **decided** — the owner's or the coordinator's answer where no behaviour
  change was asked.
- **struck** — refuted by the verify or by the unit's own read; **not a
  finding** — the reference's shape, mirrored, or nothing to change; **closed
  before** — fixed before this branch; **not reproduced**, **not performable**
  — with the runs or the missing input the notes name.
- **open: findings row** — the gap is a row of
  `notes/parity/matter_behaviour_findings.md`, opened, amended or kept on this
  branch.
- **re-cited in the citation pass** — a carried citation row whose sites the
  wave-4 `cite` units took as rows of their patch plan; the fix notes count
  per action and do not name the row.
- **phase 7**, **the release after 6b** (decision 125) or **the pin lift after
  phase 7** (decision 187) — what this phase leaves. The placement is the
  decision's, the hand-over's or the fix note's where one of them names it,
  the register editor's otherwise, and then the row says so.
- **held back** — the item tells a party on the network what it could use and
  stays open after this branch, or is one of the three rows decision 126
  releases with the release that carries their fix.

Severity is the verify's final one, with the reviewer's in brackets where it
differs (350 rows judged; lowered 8 times, raised 1 times); a struck row or a
duplicate carries "—" and the reviewer's; a row the verify raised carries its
own. The reference reads had no verifier; their rows carry the read's
severity, and "—" where it gives none. The reference reads' ids are this
register's: `6b-<read> <item>`, the item as the read labels it (a line number
is added where a label repeats). A reference statement in a row is its
source's — a reviewer's, the verify's, a commit subject's or a decision's —
and was not re-read for this file.

## The findings

### Review A: the client stack and the controller

The units of review A and their completion stages read the tree at `dd17ab06`.
Blocks: `18` chs, `19` dcl, `20` radio, `21` cpeer, `22` cam, `23` ctl, `29`
par-node; a completion-stage row has a number from its block's 50 range.

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P6-sec-1801 | medium | fixed `d5ed345f` | `client/exchange/manager.go` — refuse peer-opened handshakes on an unsecured session this node opened |
| R2-P6-bound-1802 | medium | fixed `d5ed345f` | `client/exchange/manager.go` — refuse peer-opened handshakes on an unsecured session this node opened |
| R2-P6-sec-1803 | low (medium) | fixed `08aca44f` | `controller/commissioner/flow.go` — the attested pair needs a production root and a CD not signed by the test signer |
| R2-P6-par-1804 | low | fixed `2cf8ccf6`, `b5e5fc2a` | `controller/attestation/validate.go` — chip's stricter device-certificate checks as error findings |
| R2-P6-par-1805 | low | fixed `2cf8ccf6`, `e355f45b` | `controller/attestation/validate.go` — chip's stricter device-certificate checks as error findings |
| R2-P6-fsm-1806 | low | fixed `89501926`, `0d8bd65b` | `controller/commissioner/connection.go` — a wrong passcode ends one address, not the device; a held responder holds the next address for at most the delay |
| R2-P6-bound-1807 | medium | fixed `b73efc62` | `client/exchange/exchange.go` — bound an exchange's delivery queue |
| R2-P6-conc-1809 | low | fixed `71cd1152`, `2a08923f` | `client/exchange/manager.go` — register no exchange on a closed session |
| R2-P6-conc-1810 | low | fixed `93575eb8` | `controller/commissioner/commissioner.go` — hold a commissioning's node id until it returns; ctx bounds Commission |
| R2-P6-fsm-1811 | note | fixed `2cf8ccf6` | `controller/commissioner/flow.go` — chip's stricter device-certificate checks as error findings |
| R2-P6-bound-1812 | note | fixed `93575eb8` | `controller/commissioner/flow.go` — hold a commissioning's node id until it returns; ctx bounds Commission |
| R2-P6-bound-1815 | note | fixed `d5ed345f` | `client/handshake/server.go` — refuse peer-opened handshakes on an unsecured session this node opened |
| R2-P6-sec-1850 | medium | fixed `c4945e30`, `2a08923f` | `controller/commissioner/jcm.go` — JCM without TrustAnchors fails closed; jcm.SelfAnchored is the opt-in |
| R2-P6-store-1851 | medium | fixed `8fbbb027`, `3c1a22e0` | `controller/ca/ca.go` — never replace a stored record; refuse an incomplete or inconsistent one |
| R2-P6-store-1852 | low | fixed `8fbbb027` | `controller/ca/ca.go` — never replace a stored record; refuse an incomplete or inconsistent one |
| R2-P6-store-1853 | low | fixed `8fbbb027`, `b644944f`, `92382640`, `95c9f8de` | `controller/ca/ca.go` — never replace a stored record; refuse an incomplete or inconsistent one |
| R2-P6-par-1854 | low | fixed `92382640`, `0d8bd65b` | `controller/commissioner/fabric.go` — hold the stored fabric to its NOC, bound the label in bytes, never replace a fabric |
| R2-P6-err-1855 | low | fixed `8cdb4c59` | `controller/ca/encode.go` — verify a signer's signature before the certificate leaves the CA |
| R2-P6-store-1856 | medium | fixed `c44412ab` | `controller/ca/keyfile/keyfile.go` — sync the key file and its directory before Create returns |
| R2-P6-doc-1857 | note | fixed `7099456b`, `052d3560`, `0d8bd65b` | `controller/commissioner/doc.go` — OperationalCredentials onto the generated definition; the package doc names what is ported |
| R2-P6-conc-1858 | note | fixed `92382640` | `controller/commissioner/fabric.go` — hold the stored fabric to its NOC, bound the label in bytes, never replace a fabric |
| R2-P6-api-1859 | low | fixed `92382640`, `e355f45b` | `controller/commissioner/fabricauthority.go` — hold the stored fabric to its NOC, bound the label in bytes, never replace a fabric |
| R2-P6-fsm-1860 | note | fixed `e0efc3e5`, `22e348a3` | `controller/commissioner/jcm.go` — Consent takes Commission's context and may fail; the fail-safe stays armed while it runs |
| R2-P6-sec-1861 | note | fixed `ef33b08c`, `e355f45b` | `controller/commissioner/network.go` — a device's DebugText reaches an error cut and without control bytes |
| R2-P6-conc-1901 | medium | fixed `9d8805f5`, `28b0a9d7`, `21ec388c` | `controller/softwareupdate/images.go` — one writer per image file name |
| R2-P6-sec-1902 | low (medium) | fixed `3d0a2aec`, `90c6d817`, `d46c0d43`, `4bc10f0f`, `09db6115`, `79a05ce1` | `controller/dcl/certstore.go` — a promotion to production takes the production ledger's record; a test source never replaces a production entry |
| R2-P6-bound-1903 | low | fixed `315546c6` | `controller/softwareupdate/images.go` — UpdateInfoFromURL reads the header only |
| R2-P6-bound-1904 | low | fixed `b46d88af` | `controller/dcl/revocation.go` — the revocation cache sweeps every expired lookup; CRL points per issuer bounded and counted per vendor once |
| R2-P6-bound-1905 | low | fixed `b46d88af`, `90c6d817`, `fe2cff99`, `09db6115` | `controller/dcl/revocation.go` — the revocation cache sweeps every expired lookup; CRL points per issuer bounded and counted per vendor once |
| R2-P6-conc-1906 | low | fixed `e42eb17b`, `05d3ccc2` | `controller/dcl/vendorinfo.go` — Close cancels the periodic vendor refresh; every context.Background() of dcl and softwareupdate named |
| R2-P6-conc-1907 | low | fixed `d22013f7`, `e42eb17b`, `05d3ccc2` | `controller/softwareupdate/manager.go` — the timers' checks run under a life Close cancels and waits for, report their failures, read the provider fields under the lock, forget nodes that left |
| R2-P6-bound-1908 | low | fixed `eda34248`, `09db6115` | `bdx/serve.go` — a served SendInit holds its definite length; ScopeConfig.MaxLength caps a scope's received transfers; the storage name documented |
| R2-P6-wire-1909 | low | fixed `589bdaf2`, `09db6115` | `bdx/messages.go` — a range value above 2^32-1 takes the wide form, as connectedhomeip chooses it by value |
| R2-P6-store-1910 | low | fixed `9f69cf02`, `33350487` | `controller/dcl/store.go` — FileStore syncs before its rename; a store blob that does not parse is read as empty and refetched |
| R2-P6-bound-1911 | low | fixed `c5a7cef2`, `33350487`, `05d3ccc2`, `fe2cff99`, `5b6ebc44`, `09db6115` | `controller/dcl/certstore.go` — an update fetches at most 1024 roots and 1024 GitHub files; the waiters on a running update leave with their context |
| R2-P6-err-1912 | note | fixed `d22013f7`, `9d8639fb`, `ccd2f31a` | `controller/softwareupdate/manager.go` — the timers' checks run under a life Close cancels and waits for, report their failures, read the provider fields under the lock, forget nodes that left |
| R2-P6-conc-1913 | note | fixed `d22013f7` | `controller/softwareupdate/manager.go` — the timers' checks run under a life Close cancels and waits for, report their failures, read the provider fields under the lock, forget nodes that left |
| R2-P6-bound-1914 | note | fixed `d22013f7`, `f943a7e3`, `837883f2`, `ad757629`, `eee16e82` | `controller/softwareupdate/manager.go` — the timers' checks run under a life Close cancels and waits for, report their failures, read the provider fields under the lock, forget nodes that left |
| R2-P6-fsm-1915 | note | open: findings row `R2-P6-fsm-1915` `b8514237`; no code (the coordinator, dcl Q2: "as matter.js" unverified and not written) | `controller/softwareupdate/manager.go` — One update is in progress at a time, and any change of the transfer counter is liveness |
| R2-P6-conc-1916 | note | fixed `49c8bf36` | `controller/softwareupdate/images.go` — MemoryBlobs.Get's slice is read-only |
| R2-P6-wire-1917 | note | fixed `6a6c0ff7` | `controller/dcl/crl.go` — a DER long-form length is bounded before it becomes an int |
| R2-P6-sec-2001 | medium | fixed `8389d542` | `ble/esphome/discover.go` — dial a discovered proxy only with a key unless AllowPlaintext |
| R2-P6-bound-2002 | medium | fixed `dd8357f1` | `ble/esphome/gatt.go` — bound what a proxy or a LAN host makes the central hold |
| R2-P6-bound-2003 | medium | fixed `dd8357f1` | `ble/esphome/central.go` — bound what a proxy or a LAN host makes the central hold |
| R2-P6-bound-2004 | low | fixed `dd8357f1` | `ble/esphome/gatt.go` — bound what a proxy or a LAN host makes the central hold |
| R2-P6-bound-2005 | low | fixed `8eb2b9a7` | `ble/bluez/central_linux.go` — bound the central's C2 queue, reserve a connecting path, drop an unanswered Connect |
| R2-P6-err-2006 | low | fixed `060c87ff` | `ble/esphome/proxy.go` — skip a discovered instance without an address |
| R2-P6-conc-2007 | low | fixed `7adc4869` | `ble/esphome/gatt.go` — publish the Matter handles under the lock the reader takes |
| R2-P6-fsm-2008 | low | fixed `6d1b6254` | `ble/esphome/proxy.go` — keep the reconnect backoff growing while connections fail at once |
| R2-P6-bound-2009 | low | fixed `dd8357f1` | `ble/esphome/discover.go` — bound what a proxy or a LAN host makes the central hold |
| R2-P6-err-2010 | low | fixed `138d550a` | `ble/esphome/api.go` — bound each API write and keep the silence check beside a stuck ping |
| R2-P6-conc-2011 | low | fixed `3966ad8a` | `ble/esphome/api.go` — Close waits for the API readers and the C2 deliveries |
| R2-P6-err-2012 | medium | fixed `bdd91cf4` | `ble/bluez/peripheral_linux.go` — the peripheral's teardown, ordering, callers and confirmations |
| R2-P6-conc-2013 | low | fixed `bdd91cf4` | `ble/bluez/peripheral_linux.go` — the peripheral's teardown, ordering, callers and confirmations |
| R2-P6-sec-2014 | low | fixed `bdd91cf4`, `e294316c`, `de8d00ed` | `ble/bluez/peripheral_linux.go` — the peripheral's teardown, ordering, callers and confirmations |
| R2-P6-fsm-2015 | low | fixed `bdd91cf4` | `ble/bluez/peripheral_linux.go` — the peripheral's teardown, ordering, callers and confirmations |
| R2-P6-conc-2016 | low | fixed `8eb2b9a7` | `ble/bluez/central_linux.go` — bound the central's C2 queue, reserve a connecting path, drop an unanswered Connect |
| R2-P6-err-2017 | low | fixed `8eb2b9a7` | `ble/bluez/central_linux.go` — bound the central's C2 queue, reserve a connecting path, drop an unanswered Connect |
| R2-P6-test-2018 | low (medium) | fixed `c700c60f`, `33e8eba2` | `wifi/wpasupplicant/live_linux_test.go` — the live test expects the network restore keeps |
| R2-P6-fsm-2019 | low | fixed `21376b24` | `wifi/wpasupplicant/driver.go` — end a join with the bus, watch the interface only, remove networks left behind |
| R2-P6-bound-2020 | low | fixed `21376b24` | `wifi/wpasupplicant/bus_linux.go` — end a join with the bus, watch the interface only, remove networks left behind |
| R2-P6-store-2021 | low | fixed `21376b24`, `e294316c`, `185cb545` | `wifi/wpasupplicant/driver.go` — end a join with the bus, watch the interface only, remove networks left behind |
| R2-P6-sec-2022 | medium | fixed `15b7d978`, `4a69b172` | `thread/dataset.go` — OperationalDataset.Redact drops Raw as well |
| R2-P6-bound-2023 | low | fixed `26a960b5`, `b38fe78d` | `thread/otbr/client.go` — a 1 MiB answer bound, range-checked router counts, re-enable after a failed activation, no redirects by default |
| R2-P6-wire-2024 | low | fixed `26a960b5`, `6f301d5f` | `thread/otbr/client.go` — a 1 MiB answer bound, range-checked router counts, re-enable after a failed activation, no redirects by default |
| R2-P6-err-2025 | low | fixed `26a960b5`, `4a69b172` | `thread/otbr/borderrouter.go` — a 1 MiB answer bound, range-checked router counts, re-enable after a failed activation, no redirects by default |
| R2-P6-bound-2026 | low | fixed `0bc1a2e1`, `b38fe78d`, `ea61416c`, `a8cad215` | `thread/brdiscovery/registry.go` — cap the watched instances, coalesce queued updates |
| R2-P6-bound-2027 | low | fixed `0bc1a2e1` | `thread/brdiscovery/registry.go` — cap the watched instances, coalesce queued updates |
| R2-P6-sec-2028 | low | fixed `26a960b5`, `e294316c` | `thread/otbr/client.go` — a 1 MiB answer bound, range-checked router counts, re-enable after a failed activation, no redirects by default |
| R2-P6-bound-2029 | low | fixed `b38fe78d`, `4f2973a0`, `d84891fa` | `controller/ble/scanner.go` — bound a continuous discovery's reported peripherals by the records |
| R2-P6-api-2030 | — (note) | struck: the type's doc states that a view shares its bytes with its blob (verify) | `thread/view.go` — `NewDatasetView` keeps the caller's slice (no copy) while `Bytes`/`Locate` hand out sub-slices of it: a caller that reuses the buffer changes the view |
| R2-P6-conc-2050 | low | fixed `a5a20c8f` | `ble/esphome/esphometest/esphometest.go` — read a subscriber's raw flag under the lock; drop clients in the handshake |
| R2-P6-conc-2051 | low | fixed `308ffbe9` | `controller/ble/bletest/radio.go` — the radio's accept calls do not overlap, nor outlive Serve |
| R2-P6-err-2052 | low | fixed `a5a20c8f` | `ble/esphome/esphometest/esphometest.go` — read a subscriber's raw flag under the lock; drop clients in the handshake |
| R2-P6-conc-2053 | low | not reproduced at the head; a defensive check kept `14289ff6` | `controller/ble/bletest/bletest.go` — the test kit starts no session on a connection dropped while it starts |
| R2-P6-wire-2054 | note | fixed `bd5d86d4`, `c469cabf` | `ble/esphome/internal/wire/proto.go` — skip a field that arrives in another wire type |
| R2-P6-test-2055 | note | fixed `89fa511f`: the kit's side; the firmware's behaviour is a sentence of the ESPHome entry (the register unit) | `ble/esphome/esphometest/esphometest.go` — a second connect to an address replaces its slot silently |
| R2-P6-doc-2056 | note | fixed `f00ee423` | `ble/esphome/doc.go` — say every case NewCentral returns ErrNoProxies for |
| R2-P6-bound-2101 | medium | fixed `4f4e094a`, `7b7bfbdf` | `client/discovery/name.go` — bound the cached A/AAAA records of one host at the staging's number |
| R2-P6-bound-2102 | medium | fixed `da22d710`, `f2aad79c`, `de92d2e5`, `14faaf2d`, `e4a4780f` | `client/interaction/report.go` — charge a report or invoke chunk its decoded elements against the report bound |
| R2-P6-conc-2103 | low | fixed `289f2fc9` | `client/peer/semaphore.go` — a slot reads its timeout timer under the semaphore's lock |
| R2-P6-bound-2104 | low | fixed `cf30ba2c`, `ef064f8a`, `3b2d7c1d`, `a732ad22` | `binding/resolve.go` — release the peer the manager created once no entry names its node |
| R2-P6-fsm-2105 | low | fixed `bc3da668` | `client/interaction/subscriptions.go` — a subscription displaced by one granted its id is closed |
| R2-P6-err-2106 | note | fixed `9e4bd72d`, `dc4b4787` | `client/peer/set.go` — Disconnect's doc says what a later Connect returns |
| R2-P6-bound-2107 | note | fixed `b89f408b` | `client/peer/hint.go` — say why a discovered or hinted loopback address is admitted |
| R2-P6-acl-2108 | note | fixed `5a73d321`, `e48c0a79` | `client/interaction/handler.go` — a served request on a session that is not CASE is refused UnsupportedAccess |
| R2-P6-fsm-2109 | note | open: findings row `R2-P6-fsm-2109`, not performable — missing input the peer's IdleModeDuration (decision 211) `b8514237` | `client/interaction/subscribe.go` — The granted `MaxInterval` is taken as the device states it (up to 65 535 s), though the request's ceiling defaults to 60 min |
| R2-P6-fsm-2110 | note | fixed `6af0b5e6`, `385bde02`, `9e62fe9b`, `6f212ad0`, `9c3066da` | `client/interaction/sustained.go` — a sustained subscription's retry schedule spans establishments |
| R2-P6-conc-2111 | note | fixed `422c3b70` | `client/peer/monitor.go` — a session-suspect check never takes an address-change run's result |
| R2-P6-conc-2112 | note | fixed `e1c10fe5` | `client/peer/peer.go` — a session reads the discovered addresses before it takes the peer's lock |
| R2-P6-bound-2150 | — (medium) | as its twin R2-P6-bound-2102 (verify: a duplicate): fixed `da22d710`, `f2aad79c`, `de92d2e5`, `14faaf2d`, `e4a4780f` | `client/interaction/invoke.go` — The invoke path has the amplification of R2-P6-bound-2102 at a second holder: a chunked InvokeResponse is held to the per-session report bound by payload bytes … |
| R2-P6-bound-2151 | note | fixed `808b7f9e` | `client/discovery/browse.go` — a browse drops an instance once its PTR goes, and the SRV targets only it named |
| R2-P6-par-2152 | note | fixed `c4114186` | `client/discovery/scanner.go` — ByInstanceID matches the instance label without regard to case |
| R2-P6-wire-2153 | note | fixed `0893ffc6`, `4445eea9` | `client/interaction/invoke.go` — an invoke Result carries the path of the entry that answered |
| R2-P6-err-2154 | note | fixed `270167b9` | `client/interaction/invoke.go` — refuse a negative TimedTimeout instead of wrapping it |
| R2-P6-api-2155 | note | fixed `fcfd9f35` | `client/peer/timing.go` — a negative timing parameter is unset, ProvenAddressRetention excepted |
| R2-P6-par-2156 | note | open: findings row `R2-P6-par-2156`, not performable — missing input the device's endpoint count (decision 212) `b8514237`; the budget's doc names its scope `dc4b4787` | `client/interaction/write_chunk.go` — The WriteResponse budget counts one status per AttributeDataIB, sized from the request path |
| R2-P6-doc-2157 | note | fixed `d50fcf9c`, `99998acf` | `client/discovery/doc.go` — the package doc names its clock import |
| R2-P6-conc-2201 | low | fixed `fec1af8d` | `controller/camera/provider_sessions.go` — Close ends the raw route's sessions; the closed check and the requestor upsert are one step |
| R2-P6-fsm-2202 | medium | fixed `fec1af8d`, `bb9b3431` | `controller/camera/service.go` — Close ends the raw route's sessions; the closed check and the requestor upsert are one step |
| R2-P6-store-2203 | low | fixed `85953a2b`, `65352afe` | `controller/camera/service.go` — a report drops a listed raw-route session the camera no longer lists |
| R2-P6-wire-2204 | low | fixed `093318d3` | `controller/camera/provider_sessions.go` — a withdrawn re-offer restores the session's earlier offer |
| R2-P6-test-2205 | low | fixed `51d079ca` | `controller/camera/cameratest/cameratest.go` — a re-offer keeps its session's streams; Offers names what the port answered |
| R2-P6-bound-2206 | low | fixed `5657b2a3`, `bb9b3431`, `2362c8d4` | `controller/camera/session.go` — an ended session's box discards its unread signals after a grace and its pump ends |
| R2-P6-api-2207 | note | fixed `4671fc83` | `controller/camera/service.go` — every camera-acting method answers ErrClosed after Close, Release excepted |
| R2-P6-api-2208 | low | fixed `5fe94f41`, `a1a28f3b` | `controller/camera/reports.go` — New refuses a Reports a live Service holds; Install wraps a configuration once; Deleted forgets the node's id |
| R2-P6-bound-2209 | note | fixed `5fe94f41` | `controller/camera/reports.go` — New refuses a Reports a live Service holds; Install wraps a configuration once; Deleted forgets the node's id |
| R2-P6-conc-2210 | note | fixed `3ecbd2da`, `2362c8d4`, `8b64d262` | `controller/camera/manager.go` — the give-back chain and the sweep's EndSession run within the cleanup budget |
| R2-P6-bound-2250 | medium | fixed `8f53e828` | `controller/camera/policy_stream_search.go` — 32-bit builds — camera uint32 values saturate, the bit rate halving terminates, pixel arithmetic is int64 |
| R2-P6-bound-2251 | low | fixed `8f53e828`, `bb9b3431` | `controller/camera/types.go` — 32-bit builds — camera uint32 values saturate, the bit rate halving terminates, pixel arithmetic is int64 |
| R2-P6-doc-2252 | note | fixed `27071a61` | `controller/camera/policy_snapshot.go` — requestedResolution's comment states the floor of 1 on each side |
| R2-P6-conc-2301 | medium | fixed `11f418e5` | `controller/groups.go` — SetGroupKey reloads the group manager outside the closed gate |
| R2-P6-store-2302 | low (medium) | fixed `e066d2e0`, `01631318`, `bfd40f85`, `af33dd53` | `controller/client/sqlstore/sqlstore.go` — Reseal for a controller's database |
| R2-P6-store-2303 | medium | fixed `a48d419a`, `01631318`, `bfd40f85`, `984a6e7d` | `controller/client/sqlstore/sqlstore.go` — an unfinished owed scrub opens the registry, ScrubOwed |
| R2-P6-store-2304 | low | fixed `deaca6d4`, `01631318`, `bfd40f85`, `e977f3cc` | `controller/client/sqlstore/sqlstore.go` — the node registry's own seal canary, schema v3 |
| R2-P6-conc-2305 | low | fixed `2df5f4a4`, `7d166b1f` | `controller/ota.go` — background callbacks read the components through live pointers Close clears |
| R2-P6-store-2307 | low | fixed `ec3f9640` | `controller/client/cache.go` — a flush erases again a cluster erased while it wrote |
| R2-P6-conc-2308 | low | fixed `7449a29a` | `controller/client/icd.go` — the ICD client refuses work once closed, removes a peer fed during a decommission and admits Register again before it starts |
| R2-P6-err-2309 | low | fixed `37f27e3b` | `controller/client/registry.go` — Registry.Close ends its context before it drains the fabric operations |
| R2-P6-fsm-2310 | low | fixed `87d55df6`, `6f64382b` | `controller/client/complete.go` — CompleteCommissioning refuses a node committed meanwhile inside its fabric operation |
| R2-P6-sec-2311 | low | fixed `525dadcb` | `controller/diagnostics.go` — the trace and the topology escape control characters in strings LAN hosts and devices control |
| R2-P6-api-2312 | low | fixed `c41d780e`, `7d166b1f` | `controller/client/icd.go` — ICDRegistration and CommissioningWindow redact their secrets under fmt, slog and JSON |
| R2-P6-doc-2313 | low | fixed `9d3e7ee7` | `controller/controller.go` — a Pair race calls the Progress hook one report at a time |
| R2-P6-fsm-2314 | low | fixed `87499794`, `2062406c` | `controller/topology/service.go` — a topology debounce burst rebuilds at the latest five debounces after its first change |
| R2-P6-doc-2315 | note | fixed `984a6e7d`, `ffd6b186` | `controller/open.go` — New logs a sealing's owed scrub once, and the Config.Sealer doc states it |
| R2-P6-store-2316 | note | fixed `017f8a4c` | `controller/ota_provider.go` — a sealed controller scrubs after it retired the own node's record |
| R2-P6-err-2317 | note | fixed `a449a6b9` | `controller/vendorinfo.go` — VendorInfo builds its service without the lock Close takes |
| R2-P6-err-2318 | note | fixed `478490f7` | `controller/client/fabricops.go` — commission records the peer address before it looks the peer up, as matter.js |
| R2-P6-fsm-2350 | medium | fixed `70d7ec5a`, `6f64382b` | `controller/client/node.go` — Stop announces a reported default subscription ended |
| R2-P6-bound-2351 | medium | fixed `dc1875b7`, `7d166b1f`, `2062406c` | `controller/client/image_bounds.go` — the image quota charges the clusters and endpoints a reported ServerList or PartsList creates |
| R2-P6-bound-2352 | low | fixed `fcc6cc0b` | `controller/client/properties.go` — ICD timings wider than their attribute's type are absent |
| R2-P6-conc-2353 | low | fixed `7449a29a` | `controller/client/icd.go` — the ICD client refuses work once closed, removes a peer fed during a decommission and admits Register again before it starts |
| R2-P6-store-2354 | low | fixed `1d840584`, `6f64382b` | `controller/client/node.go` — a node deleted without a decommission leaves the fabric's ICD table |
| R2-P6-fsm-2355 | low | fixed `7449a29a` | `controller/client/icd.go` — the ICD client refuses work once closed, removes a peer fed during a decommission and admits Register again before it starts |
| R2-P6-api-2356 | low | fixed `21a2789d` | `controller/client/settings.go` — SubscribeOptions clone copies the interval bounds |
| R2-P6-bound-2357 | low | fixed `07018e11`, `2062406c` | `controller/client/network.go` — a node logs at most 256 unknown clusters and events, then says further ones are not |
| R2-P6-bound-2358 | low | fixed `726a4d9d` | `controller/client/conformance.go` — rawIDList dedupes with a set |
| R2-P6-doc-2359 | note | fixed `9d70c0f5` | `controller/client/doc.go` — the client package doc names the ICD client |
| R2-P6-par-2360 | note | fixed `8a209102` | `controller/client/opcreds.go` — the client's OperationalCredentials, diagnostics and NetworkCommissioning ids from the generated definitions |
| R2-P6-sec-2361 | note | fixed `b283c476`, `0e89c40a` | `controller/client/ping.go` — PingAddress states that the host passes only the record's addresses |
| R2-P6-bound-2362 | note | fixed `b4b25a37`, `2062406c`, `159b9616`: a colliding extended address or RLOC16 resolves to neither node, an RLOC16 counted per ExtendedPanId, logged once through an optional `topology.Config.Logger` (decisions 222, 237); the divergence paragraph in `BD-matterjs-server-NetworkTopology` `2bc73568` | `controller/topology/thread.go` — an extended address or RLOC16 two Thread nodes claim resolves to neither in the topology |
| R2-P6-err-2363 | note | fixed `a6ae476f`, `6f64382b` | `controller/client/network.go` — a failed newly-commissioned read is made again by the next sync |
| R2-P6-doc-2901 | note | fixed `9a331c53` | `contract/matter.go` — citations as read at the pins |
| R2-P6-par-2902 | note | fixed `1534d894`, `485aa6a4`, `c12f72af`, `3d26f26f` | `notes/parity/by_design.md` — a Sigma1 that meets a running handshake is answered Busy with a minimum wait time, as connectedhomeip |
| R2-P6-par-2903 | note | parity: a recorded divergence `BD-Matter-NonVolatileWrite-StoredBeforeApplied` amended (decision 206) `2bc73568`; the per-path FAILURE pinned by a test `958f4f07` | `endpoint/dispatcher.go` — 5b-F Q5, traced |
| R2-P6-par-2904 | note | fixed `6842ca1f`, `3d26f26f` | `internal/attestids/attestids.go` — read a DN value from its content bytes whatever its string tag, as matter.js |
| R2-P6-par-2905 | low | fixed `6003d30f` | `secure/sessionparams/sessionparams.go` — state SUPPORTED_TRANSPORTS always, 0 without a TCP role, against a matter.js fixture |
| R2-P6-doc-2906 | note | fixed in part `3cdfe141`, `c2827afe`, `887d2c99`: the eight stale examples moved to their `db021da0` lines and two doc comments rewritten; the 14 script-flagged ranges were left to the citation pass of wave 4 (the fix note) and are not traced there (the register editor's judgement) | `binding/manager.go` — the eight stale unlabelled citations moved to their lines at db021da0 |
| R2-P6-doc-2907 | note | open: findings rows `F-db021da0-15` and `-53` amended with the two answers `b8514237` | `notes/parity/matter_behaviour_findings.md` — Two "unverified" sentences of open rows have answers, and one line moved |
| R2-P6-doc-2908 | note | fixed `9a331c53` | `client/interaction/report.go` — citations as read at the pins |
| R2-P6-doc-2909 | note | phase 7 (the register editor's judgement): `script/testparity/register.json`'s reason is not a notes file; the register unit left it to the tests unit or the coordinator, and no fix note names it | `script/testparity/register.json` — The test register's reason for the two missing reboot-armer cases says the controller does not "publish an operational record a device could re-establish its subscription to" |
| R2-P6-doc-2910 | note | fixed `9a331c53` | `node/client.go` — citations as read at the pins |

### Review B: the facade, the device layer, the examples, the generators and the guards

The units of review B and their completion stages, at the same head. Blocks:
`24` facade, `25` device, `26` examples, `27` gen, `28` guards, `30` docs,
`31` tests, `32` cite.

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P6-sec-2401 | medium | fixed in part `af3b6c48`: both ACL writes take the AccessControl write lock; the limit half is kept as matter.js (decision 228), a findings-register row (`361102dd`) | `node/webrtc_requestor_acl.go` — the node's own Operate entry and AddNOC's default entry write under the AccessControl write lock |
| R2-P6-err-2402 | medium | fixed `46b367dc` | `node/node.go` — the callback queue runs before the owed fabric events are delivered |
| R2-P6-store-2403 | medium | fixed `2e08ba8b` | `node/persist.go` — a failed restore read fails New or the assembly, never serves a default over the stored value |
| R2-P6-store-2404 | medium | fixed `2e08ba8b` | `node/client.go` — a failed restore read fails New or the assembly, never serves a default over the stored value |
| R2-P6-conc-2405 | medium | fixed `fd208a9b` | `node/aggregator.go` — a Forget batch reassembles and unbinds before it erases; a key added again in the batch is served after the erasure |
| R2-P6-store-2406 | low | fixed `b5653f2c`, `5c87a22c`, `e06e3509` | `node/reserve.go` — a Forget is recorded until all its writes landed; New finishes one left half done |
| R2-P6-api-2407 | medium | fixed `ad83dbb8`, `2bced2d3` | `node/config.go` — Config.Validate refuses a BasicInformation string beyond its schema constraint, naming the attribute |
| R2-P6-api-2408 | low | fixed `41900634` | `node/root.go` — Config.AggregatorServers refuses a cluster the Aggregator serves already, as RootServers |
| R2-P6-api-2409 | low | fixed `ca868d80`, `3316e132`, `2bced2d3` | `node/config.go` — Config.Validate bounds the periods, OTA limits, device type, mDNS host name, the EndpointState pair and the TLS capacities |
| R2-P6-err-2410 | low | fixed `5b799067` | `node/diagnostics.go` — OnDiagnosticEventAsync and ICD.OnModeChangeAsync contain a host panic, as every queued callback |
| R2-P6-fsm-2411 | medium | fixed `9a5a4da1`, `3316e132`, `2bced2d3` | `node/node.go` — Stop expires an armed fail-safe and rolls back; OnFabricRemoved runs before Stop returns |
| R2-P6-sec-2412 | low | fixed `315e6aa6`, `f0012f7d`, `08a84db9`, `b15bd140`, `d4e1423f`, `e836a1ce`, `3a273328`, `45498238` | `node/security.go` — a CASE handshake admits its session only under the fabric identity it began with |
| R2-P6-conc-2413 | low | fixed `d9f708f1` | `node/commissioning.go` — a basic window is refused for an administrator's window under the install lock |
| R2-P6-store-2414 | low | fixed `58284b1a` | `node/aggregator.go` — a refused Add or Replace leaves a served key's surface record; the OTA provider's key is reserved |
| R2-P6-api-2415 | low | fixed `58284b1a` | `node/ota_provider.go` — a refused Add or Replace leaves a served key's surface record; the OTA provider's key is reserved |
| R2-P6-store-2416 | low | fixed `427f6b60` | `node/user_labels.go` — a bridged UserLabel change is stored under its key's lock and owed when refused |
| R2-P6-test-2417 | note | fixed `fe2a84d5` | `internal/sessionseam/sessionseam_test.go` — doc rows of block 24 and the sessionseam guard's dot import |
| R2-P6-doc-2418 | note | fixed `fe2a84d5` | `internal/nodeseam/nodeseam.go` — doc rows of block 24 and the sessionseam guard's dot import |
| R2-P6-doc-2419 | note | fixed `fe2a84d5` | `node/limits.go` — doc rows of block 24 and the sessionseam guard's dot import |
| R2-P6-conc-2420 | note | fixed `fe2a84d5` | `node/matter_time.go` — doc rows of block 24 and the sessionseam guard's dot import |
| R2-P6-api-2450 | medium | fixed `c47aa787` | `node/wifi_driver.go` — the host's Wi-Fi driver goes to the server unwrapped; the re-announce rides the server's OnConnected hook |
| R2-P6-conc-2451 | low | fixed `dee9ea69` | `node/commissioner_reach.go` — SignVIDVerification invokes over one session and returns that session's challenge |
| R2-P6-api-2452 | low | fixed `ca868d80`, `2bced2d3` | `node/tls.go` — Config.Validate bounds the periods, OTA limits, device type, mDNS host name, the EndpointState pair and the TLS capacities |
| R2-P6-bound-2453 | note | fixed `40cf4e29`, `a9a3f1c3` | `node/root.go` — the SoftwareDiagnostics heap attributes share one heap reading per second |
| R2-P6-doc-2454 | note | fixed `fe2a84d5` | `node/served.go` — doc rows of block 24 and the sessionseam guard's dot import |
| R2-P6-doc-2455 | note | fixed `fe2a84d5` | `contract/matter.go` — doc rows of block 24 and the sessionseam guard's dot import |
| R2-P6-fsm-2501 | medium | fixed `a7391365`, `31dfeb31`, `39e9d10d` | `device/lifecycle.go` — SetReachable, Rename and Update return an error (ErrNotBridged on a part, Rename the store's refusal); AddPart moves a part as matter.js's owner setter, an attach binds a whole tree or nothing; Update's fn runs unlocked; port subscriptions end at detach |
| R2-P6-wire-2502 | medium | fixed `a9df5b54`, `cf223097`, `d516f10d`, `24455b36` | `device/bridged_basic_information.go` — New refuses a declared BridgedDeviceBasicInformation value or an Options.Vendor beyond its constraint, naming the attribute; the assembler serves a stated VendorName as stated |
| R2-P6-api-2503 | low | fixed `a7391365` | `device/lifecycle.go` — SetReachable, Rename and Update return an error (ErrNotBridged on a part, Rename the store's refusal); AddPart moves a part as matter.js's owner setter, an attach binds a whole tree or nothing; Update's fn runs unlocked; port subscriptions end at detach |
| R2-P6-conc-2504 | low | fixed `a7391365`, `99905f39` | `device/lifecycle.go` — SetReachable, Rename and Update return an error (ErrNotBridged on a part, Rename the store's refusal); AddPart moves a part as matter.js's owner setter, an attach binds a whole tree or nothing; Update's fn runs unlocked; port subscriptions end at detach |
| R2-P6-err-2505 | low | fixed `4f3fb044` | `device/options.go` — a typed nil Declare, a typed HostServer of another cluster or with an illegal ID, and a second undeclared HostServer are refused |
| R2-P6-err-2506 | low | fixed `8f21ffcf`, `bdb8d58d` | `device/defaults_ports.go` — a declared Initial value the kit's server does not take stays for the definition's check, not dropped |
| R2-P6-err-2507 | low | fixed `4f3fb044` | `device/options.go` — a typed nil Declare, a typed HostServer of another cluster or with an illegal ID, and a second undeclared HostServer are refused |
| R2-P6-err-2508 | low | fixed `4f3fb044` | `device/options.go` — a typed nil Declare, a typed HostServer of another cluster or with an illegal ID, and a second undeclared HostServer are refused |
| R2-P6-conc-2509 | low | fixed `ee0ea2c7` | `fabrictest/controller.go` — Controller.Addr, Commission and PASE read the stack under the lock Restart takes; a PASE pair's interaction runs on the controller's counted host clock |
| R2-P6-err-2510 | low | fixed `54da6440` | `fabrictest/device.go` — a Start that fails after building its node stops that node and closes its socket |
| R2-P6-test-2511 | low | fixed `1d08f36a`, `99905f39` | `fabrictest/site.go` — Site.Run without a context deadline ends with ErrWaited after Options.Wait, and the fn it gave up on sees its context end |
| R2-P6-test-2512 | low | fixed `b4900983` | `internal/paritytest/paritytest.go` — Conformance cuts a choice suffix before the bracket test and models only "Rev >= vN"; any other revision form fails the caller |
| R2-P6-conc-2513 | low | fixed `ee0ea2c7` | `fabrictest/pase.go` — Controller.Addr, Commission and PASE read the stack under the lock Restart takes; a PASE pair's interaction runs on the controller's counted host clock |
| R2-P6-test-2514 | low | fixed `248c9df3` | `fabrictest/access.go` — Pair.Grant picks the pair's fabric by its compressed fabric id as well as the node id |
| R2-P6-store-2515 | note | fixed `cd8ba64c`, `a7391365`, `bdb8d58d` | `device/defaults_declared.go` — a self-held server's subscription on a host port lives while the device is added; the tariff's SyncTime error is logged |
| R2-P6-err-2516 | note | fixed `c5f4dd69` | `device/defaults_sensing.go` — a sensing server's refused Initial value is a configuration error, only a refused selection is ErrSelectionNotServed |
| R2-P6-api-2517 | note | fixed `d23e2ee4` | `device/initial.go` — New's documentation says it reads a declared host port |
| R2-P6-err-2518 | note | fixed `fee35e98` | `device/defaults.go` — the dead defaultBuild.cfg is gone; OnOff's Initial is applyInitial's |
| R2-P6-test-2519 | note | fixed `eefd474a`, `99905f39` | `fabrictest/device.go` — teardown errors and a controller serve that ends early reach the test's log; the package documentation says what the site leaves to the test |
| R2-P6-conc-2520 | note | fixed `a87d7dcc`, `99905f39` | `fabrictest/net.go` — a Network filter runs outside the network's lock; Act refuses a device that is not running and names the calls fn must not make |
| R2-P6-conc-2521 | note | fixed `a7391365` | `device/device.go` — SetReachable, Rename and Update return an error (ErrNotBridged on a part, Rename the store's refusal); AddPart moves a part as matter.js's owner setter, an attach binds a whole tree or nothing; Update's fn runs unlocked; port subscriptions end at detach |
| R2-P6-par-2601 | medium | fixed `a800c5c7` | `examples/reference-bridge/fleet_energy_market.go` — one EPM Accuracy entry per type the fake readings set, count = len |
| R2-P6-doc-2602 | low | fixed `9edfd296` | `examples/reference-controller/README.md` — --seal-key file:<path> on the daemon and the reference controller; README database tables |
| R2-P6-store-2603 | note (low) | fixed `056c43e9`, `9edfd296` | `examples/reference-controller/cli/group.go` — the group record keeps no epoch key; bind --group reads it from the key set |
| R2-P6-sec-2604 | note | fixed `9edfd296` | `examples/reference-bridge/main.go` — --seal-key file:<path> on the daemon and the reference controller; README database tables |
| R2-P6-doc-2605 | low | fixed `2a8251f2` | `examples/reference-bridge/README.md` — --ota-harness fails when the seam refuses; the nodeseam reach named in README and package doc |
| R2-P6-doc-2606 | note | fixed `2dc68178` | `control_actions.go` — four stale comments on the app-pipe and trigger dispatch |
| R2-P6-conc-2607 | low | fixed `39ea7fa6` | `examples/reference-bridge/fleet.go` — stale timer callbacks act only for the latest arming (latestTimer); push transport events in transition order |
| R2-P6-conc-2608 | low | fixed `39ea7fa6` | `examples/reference-bridge/camera.go` — stale timer callbacks act only for the latest arming (latestTimer); push transport events in transition order |
| R2-P6-wire-2609 | low | fixed `77b11c99` | `examples/reference-controller/cli/group.go` — AddGroupResponse decoded by tag, command and group checked |
| R2-P6-err-2610 | low | fixed `8e4d5834` | `examples/reference-bridge/control.go` — the app pipe skips an overlong line and logs a read error |
| R2-P6-err-2611 | low | fixed `32ec113f` | `examples/reference-bridge/ota.go` — the OTA applier removes an image it cannot keep; the harness restart ends with the daemon |
| R2-P6-conc-2612 | note | fixed `32ec113f` | `examples/reference-bridge/ota.go` — the OTA applier removes an image it cannot keep; the harness restart ends with the daemon |
| R2-P6-sec-2613 | note | fixed `681c13c8` | `examples/reference-bridge/metrics.go` — --metrics-expvar refuses a non-loopback address unless --metrics-expvar-any-address |
| R2-P6-sec-2614 | note | fixed `fa758ac5` | `examples/reference-bridge/fifo_unix.go` — the app pipe is checked, chmod-ed and read through one descriptor (O_NOFOLLOW, fstat, fchmod) |
| R2-P6-sec-2615 | note | fixed `4b737031` | `examples/reference-bridge/main.go` — a random SPAKE2+ salt per start instead of a shared constant; the daemon's passcode from @file or $GOFABRIC_PASSCODE, the enable key from @file |
| R2-P6-sec-2616 | note | decided: a doc sentence on `ApplyUpdate` and in the README's OTA part, no behaviour (decision 238) `93e0b635` | `examples/reference-bridge/ota.go` — The applier's verification is the image's own digest, size, version and vendor/product |
| R2-P6-err-2617 | note | fixed `3b3188d7` | `examples/reference-bridge/fleet.go` — the fleet's add sees a typed-nil device |
| R2-P6-err-2650 | low | fixed `299eb0d8` | `examples/reference-bridge/fleet_closure.go` — a trigger for a device the spec level withheld is not handled, no nil server reached; every trigger swept at every level |
| R2-P6-conc-2651 | low | fixed `39ea7fa6` | `fleet_closure.go` — stale timer callbacks act only for the latest arming (latestTimer); push transport events in transition order |
| R2-P6-store-2652 | low | fixed `b6b4495c` | `examples/reference-bridge/camera.go` — the camera logs a kept state it cannot read or decode and persists nothing over it |
| R2-P6-doc-2653 | note | fixed `ef7e0960` | `examples/reference-bridge/media.go` — the state-forecast ESA takes no PowerAdjustmentCapability; the media doc names --media-messages and the app's PIN |
| R2-P6-wire-2654 | note | fixed `93e0b635`: `SkipForward` saturates instead of wrapping (decision 207); a clause in the example's entry `6006fe32` | `examples/reference-bridge/media.go` — SkipForward saturates instead of wrapping; the OTA applier states that a product verifies its vendor signature |
| R2-P6-err-2655 | note | fixed `ef7e0960` | `examples/reference-bridge/fleet_evse.go` — the state-forecast ESA takes no PowerAdjustmentCapability; the media doc names --media-messages and the app's PIN |
| R2-P6-sec-2701 | low | fixed `4a7493ba`, `da3699e6` | `script/generate_matter_schema.go` — request commands and changesOmitted from the effective layer |
| R2-P6-par-2702 | low | fixed `4a7493ba` | `script/generate_matter_schema.go` — request commands and changesOmitted from the effective layer |
| R2-P6-par-2703 | low | fixed `ce2ccb19`, `3fe5f5d4` | `script/clustergen/gen.go` — refuse a form the generator does not render |
| R2-P6-acl-2704 | note | fixed `ce2ccb19` | `script/clustergen/gen.go` — refuse a form the generator does not render |
| R2-P6-par-2705 | note | fixed `ce2ccb19` | `script/clustergen/gen.go` — refuse a form the generator does not render |
| R2-P6-par-2706 | note | fixed `ce2ccb19` | `script/clustergen/gen.go` — refuse a form the generator does not render |
| R2-P6-par-2707 | note | fixed `747aef1a`, `eafe808c` | `script/devicegen/conformance.go` — refuse a conformance node or constraint form it does not evaluate |
| R2-P6-par-2708 | note | fixed `747aef1a`, `3fe5f5d4` | `script/devicegen/gen.go` — refuse a conformance node or constraint form it does not evaluate |
| R2-P6-err-2709 | low | fixed `23c082b8`, `eafe808c` | `script/clustergen/main.go` — script/{generate_matter_schema,clustergen,devicegen,levelgen}: a failed run writes nothing |
| R2-P6-sup-2710 | low | fixed `3e115b21` | `script/generate-level.sh` — the level extract cleans up on refusal, requires its provenance and checks command directions |
| R2-P6-err-2711 | note | fixed `76e9def8` | `script/profilegen/main.go` — a row after the register table ended is an error naming its line |
| R2-P6-err-2712 | note | fixed `89d2f796` | `script/levelgen/leveltable/render.go` — Block bounds the end marker's newline as Splice does |
| R2-P6-par-2713 | note | fixed `3e115b21`, `b180c311`, `080b7c60` | `script/extract-levels-from-matter-js.ts` — the level extract cleans up on refusal, requires its provenance and checks command directions |
| R2-P6-par-2714 | note | fixed `2f039d54`, `ce2ccb19` | `script/generate_matter_schema.go` — script/{generate_matter_schema,devicegen,levelgen}: refuse a feature without a usable bit |
| R2-P6-par-2715 | note | fixed `ce2ccb19` | `script/clustergen/hull.go` — refuse a form the generator does not render |
| R2-P6-err-2716 | note | fixed `9d6e196c` | `script/clustergen/ports_device.go` — a cluster named in two port or host tables panics at start |
| R2-P6-err-2717 | note | fixed `55045ea1` | `script/levelgen/main.go` — deadlines on the daemon build and its dumps |
| R2-P6-doc-2718 | note | fixed `3ca20082`, `3df9cac6` | `script/extract-from-matter-js.ts` — make generate-matter-schema builds packages/model first; the usage block names all three generators |
| R2-P6-doc-2750 | note | fixed `4f819eaf`, `52d4fd3c`, `e73e277c` | `script/clustergen/ports_device.go` — device port docs state the status rule |
| R2-P6-api-2751 | note | fixed `52d4fd3c`, `aa3678f8` | `script/clustergen/declare.go` — the generated DoorLock StateSource doc carries the lock's contract |
| R2-P6-doc-2752 | note | fixed `3df9cac6` | `script/clustergen/clusters.go` — numericBounds' and committed's docs say what the code does |
| R2-P6-doc-2753 | — (note) | struck: every range of the tables holds at the pins (verify) | `script/clustergen/declare.go` — These tables carry matter.js and connectedhomeip citations and behaviour statements, for example: "matter.js has no counterpart" (`declare.go:206`, `ports_device.go:296-297`), "Other than chip … |
| R2-P6-sup-2801 | medium | fixed `1aaea07c` | `script/apidiff/main.go` — the base is the last v* tag and must have its CHANGELOG heading; a symbol is named with its package; the nested modules are compared against their own tags |
| R2-P6-sup-2802 | low | fixed `1aaea07c` | `script/apidiff/check.go` — the base is the last v* tag and must have its CHANGELOG heading; a symbol is named with its package; the nested modules are compared against their own tags |
| R2-P6-sup-2803 | low | fixed `1aaea07c` | the base is the last v* tag and must have its CHANGELOG heading; a symbol is named with its package; the nested modules are compared against their own tags |
| R2-P6-test-2804 | low | fixed `6e897861` | `script/reachability/main.go` — package initializers seeded, exported methods in the population, the exemptions on an exact ratchet; the manual tools search tracked files only |
| R2-P6-test-2805 | low | fixed `6e897861` | `script/reachability/main.go` — package initializers seeded, exported methods in the population, the exemptions on an exact ratchet; the manual tools search tracked files only |
| R2-P6-test-2806 | low | fixed `6e897861` | `script/reachability/main.go` — package initializers seeded, exported methods in the population, the exemptions on an exact ratchet; the manual tools search tracked files only |
| R2-P6-test-2807 | low | fixed `6e897861` | `script/reachability/whitelist_audit.go` — package initializers seeded, exported methods in the population, the exemptions on an exact ratchet; the manual tools search tracked files only |
| R2-P6-test-2808 | low | fixed `a87e0920` | `script/archcheck/archcheck_test.go` — the nested modules are loaded and held to the layers; wifi/... in the support layer |
| R2-P6-test-2809 | low | fixed `138205df`, `0b52e5dd` | `script/testparity/register_test.go` — make ci-net is three guards — cite-check, testparity-snapshot-check, chipdm-check |
| R2-P6-test-2810 | note | fixed `138205df` | `script/testparity/register_test.go` — a register reference into a file go test does not build fails; a test file not named *Test.ts fails the snapshot; ci-net and the cite-check job regenerate both snapshots |
| R2-P6-test-2811 | note | fixed `138205df` | `script/testparity/main.go` — a register reference into a file go test does not build fails; a test file not named *Test.ts fails the snapshot; ci-net and the cite-check job regenerate both snapshots |
| R2-P6-err-2812 | low | fixed `e5c630ba`, `eae67538` | `script/aheadcheck/main.go` — a clone read that fails is exit 2, not a finding; existence is the tree's listing |
| R2-P6-test-2813 | note | fixed `8cc7bbe0` | `.github/workflows/matterjs-pin.yml` — ahead-check names the ref it reads and says a local run reads the clone's HEAD |
| R2-P6-test-2814 | note | phase 7 (the register editor's judgement): reported back by the guards unit (register content, not its path); the per-row re-marking of the presence markers was not done | `script/aheadcheck/register.go` — The guard can only see catch-up as a change of the marker text |
| R2-P6-test-2815 | note | not a finding: by design (concept v4 §A.2), recorded by the verify and reported back by the guards unit | `script/citecheck/check.go` — Drift class: a citation whose lines moved |
| R2-P6-test-2816 | note | fixed `3030d9fc` | `internal/chipdm/source.go` — the composition of composed device types is compared; unknown input fails; a duplicate key and an ambiguous scoped type are reported; unread files are counted |
| R2-P6-doc-2817 | low | fixed `048e6e05`, `64988804`, `f3e33959` | `notes/parity/matter_behaviour_findings.md` — a test ties the findings register's class (ii) section to acknowledged.go |
| R2-P6-test-2818 | note | fixed `3030d9fc` | `internal/chipdm/compare.go` — the composition of composed device types is compared; unknown input fails; a duplicate key and an ambiguous scoped type are reported; unread files are counted |
| R2-P6-sup-2819 | note | fixed `4727ba4b` | each snapshot guard runs its own test, compares with HEAD and refuses an untracked .go file |
| R2-P6-doc-2820 | note | fixed `9811d6a2` | `internal/chiptool/doc.go` — the root package list names client/ and narrows controller/ to the role; the chip-tool harness has six entry points |
| R2-P6-doc-2821 | note | fixed `138205df` | `script/testparity/main.go` — a register reference into a file go test does not build fails; a test file not named *Test.ts fails the snapshot; ci-net and the cite-check job regenerate both snapshots |
| R2-P6-doc-2822 | note | fixed `9811d6a2` | `doc.go` — the root package list names client/ and narrows controller/ to the role; the chip-tool harness has six entry points |
| R2-P6-err-2823 | note | fixed `63b73943` | `notes/briefs/reports/BX3-scripts/bx3-mutex.go` — a walk or write error ends the run; dot-directories are skipped |
| R2-P6-test-2824 | note | fixed `4727ba4b` | `script/citecheck/collect.go` — each snapshot guard runs its own test, compares with HEAD and refuses an untracked .go file |
| R2-P6-test-2850 | low | fixed `3030d9fc`, `a0b67f18` | `internal/chipdm/snapshot.go` — the composition of composed device types is compared; unknown input fails; a duplicate key and an ambiguous scoped type are reported; unread files are counted |
| R2-P6-test-2851 | note (low) | fixed `465466a7`, `a0b67f18` | `internal/chipdm/conf.go` — a second "or" in conformance text is an error |
| R2-P6-test-2852 | note | fixed `3030d9fc` | `internal/chipdm/chipxml.go` — the composition of composed device types is compared; unknown input fails; a duplicate key and an ambiguous scoped type are reported; unread files are counted |
| R2-P6-test-2853 | note | fixed `0803ee58`, `07a5e2b5` | `internal/chipdm/levels.go` — the level cross-check compares request and response command ids apart |
| R2-P6-test-2854 | note | fixed `3030d9fc` | `internal/chipdm/compare.go` — the composition of composed device types is compared; unknown input fails; a duplicate key and an ambiguous scoped type are reported; unread files are counted |
| R2-P6-err-2855 | note | fixed `6e897861` | `script/reachability/main.go` — package initializers seeded, exported methods in the population, the exemptions on an exact ratchet; the manual tools search tracked files only |
| R2-P6-doc-3001 | low | fixed `4d9125de`, `59c95a14`, `5ec77d44` | `docs/guides/persistence.md` — the family table and golden carry the symbol citations docs/certifiability.md's generated block now shows |
| R2-P6-doc-3002 | low | fixed `dd21bdb2` | `docs/guides/persistence.md` — backup, ACL, fabric-scoped invoke, Reseal journal, encryption scope, pin wording, RI citation |
| R2-P6-doc-3003 | note | fixed `4d9125de` | `docs/security.md` — cite the repository by symbol, not by line; docscheck holds the form |
| R2-P6-doc-3004 | note | fixed `4d9125de` | `docs/guides/operations.md` — cite the repository by symbol, not by line; docscheck holds the form |
| R2-P6-doc-3005 | note | fixed `dd21bdb2` | `docs/guides/ports.md` — backup, ACL, fabric-scoped invoke, Reseal journal, encryption scope, pin wording, RI citation |
| R2-P6-doc-3006 | note | fixed `dd21bdb2` | `docs/security.md` — backup, ACL, fabric-scoped invoke, Reseal journal, encryption scope, pin wording, RI citation |
| R2-P6-doc-3007 | note | fixed `dd21bdb2` | `docs/security.md` — backup, ACL, fabric-scoped invoke, Reseal journal, encryption scope, pin wording, RI citation |
| R2-P6-doc-3008 | note | closed before: fixed in wave 1 with R2-P6-store-2302 (`docs/security.md` states `controller.Reseal` and `sqlstore.Reseal`), as the docs unit reported | `docs/security.md` — The page presents `store.Reseal` as the key rotation and says it moves "the device store's rows only, not the controller's node registry" (`:167-168`) … |
| R2-P6-doc-3009 | note | fixed `dd21bdb2` | `docs/security.md` — backup, ACL, fabric-scoped invoke, Reseal journal, encryption scope, pin wording, RI citation |
| R2-P6-doc-3010 | note | fixed `dd21bdb2` | `docs/matterjs-comparison.md` — backup, ACL, fabric-scoped invoke, Reseal journal, encryption scope, pin wording, RI citation |
| R2-P6-doc-3011 | note | fixed `3e7680a2` | `docs/adr/README.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-doc-3012 | note | fixed `3e7680a2` | `docs/adr/0026-architecture-principles.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-doc-3013 | note | fixed `b3cb03cf` | `docs/guides/embed-in-a-daemon.md` — guides and comments brought to the code; docscheck holds the Host table and the schema version |
| R2-P6-doc-3014 | note | fixed `305a379e` | `cluster/wire/schedules.go` — Drop the v1.1 label from non-test Go; docscheck refuses it |
| R2-P6-doc-3015 | note | fixed `dd21bdb2` | `bridge/bridge.go` — backup, ACL, fabric-scoped invoke, Reseal journal, encryption scope, pin wording, RI citation |
| R2-P6-doc-3050 | note | fixed `b3cb03cf` | `docs/guides/ports.md` — guides and comments brought to the code; docscheck holds the Host table and the schema version |
| R2-P6-doc-3051 | note | fixed `b3cb03cf` | `docs/guides/limits.md` — guides and comments brought to the code; docscheck holds the Host table and the schema version |
| R2-P6-doc-3052 | note | fixed `b3cb03cf` | `cluster/core/network_commissioning_wifi.go` — guides and comments brought to the code; docscheck holds the Host table and the schema version |
| R2-P6-doc-3053 | low | fixed `b3cb03cf` | `docs/guides/advanced-seams.md` — guides and comments brought to the code; docscheck holds the Host table and the schema version |
| R2-P6-doc-3054 | note | fixed `b3cb03cf` | `docs/guides/camera.md` — guides and comments brought to the code; docscheck holds the Host table and the schema version |
| R2-P6-doc-3055 | note | fixed `b3cb03cf` | `docs/certifiability.md` — guides and comments brought to the code; docscheck holds the Host table and the schema version |
| R2-P6-doc-3075 | note | fixed `3e7680a2` | `docs/adr/0001-matter-commissioning-bring-up.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-doc-3076 | note | fixed `3e7680a2` | `docs/adr/0008-subscription-resumption.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-doc-3077 | note | fixed `3e7680a2` | `docs/adr/0008-subscription-resumption.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-doc-3078 | note | fixed `3e7680a2` | `docs/adr/0009-groups-and-group-messaging.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-doc-3079 | note | fixed `3e7680a2` | `docs/adr/0010-groupcast-and-auxiliary-acl.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-doc-3080 | note | fixed `3e7680a2` | `docs/adr/0011-certifiability-is-a-goal.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-doc-3081 | note | fixed `3e7680a2` | `docs/adr/0013-generated-cluster-definitions.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-doc-3082 | note | fixed `3e7680a2` | `docs/adr/0016-device-type-validation.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-doc-3083 | note | fixed `4d9125de` | `docs/adr/0022-media-and-content-clusters.md` — cite the repository by symbol, not by line; docscheck holds the form |
| R2-P6-doc-3084 | note | fixed `3e7680a2`: ADR 0026's note for principles 7 and 9; its two file-based residues are two findings-register rows, fixed in phase 7 (decision 249) | `docs/adr/0026-architecture-principles.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-doc-3085 | note | fixed `3e7680a2` | `docs/adr/README.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-doc-3086 | note | fixed `3e7680a2`, `59c95a14` | `docs/adr/0020-commissioner-and-controller-role.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-doc-3087 | note | fixed `3e7680a2` | `docs/adr/0002-im-opcode-dispatch-seam.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-test-3101 | low | fixed `da4b8591`, `e779c6c9` | `fabrictest/silent_restart_test.go` — the silent-restart subscription case is two subtests ordered against the device's persistence: controller recovers (nothing persisted) and device resumes (persisted first) |
| R2-P6-par-3102 | note (low) | decided: no code (decision 239); the 3101 subtest asserts the old session neither lost nor closed `531c0767`, `b301c305`; a "Verified sound" line `fd633cc8` | `client/exchange` — In the device-resumed path of 3101 the controller keeps the session from before the device's restart open and not lost beside the session the device opened: probe 2 extended … |
| R2-P6-test-3103 | low | fixed `72af5e9a` | `controller/ble/scanner_parity_matterjs_test.go` — positive candidate reads wait on the offer, not on settle; end-of-run reads after the discovery returned |
| R2-P6-test-3104 | low | fixed `96b112fa`, `e779c6c9` | `node/stop_done_context_test.go` — the stop tests bind no probe port; the first node takes an assigned port, the test proves each release by its own bind, a foreign EADDRINUSE is retried once and logged |
| R2-P6-test-3105 | low | fixed `265d6dcf` | `node/commission_test.go` — RemoveFabric's session teardown test waits on the commissioner's closed session and the device's session table, not on wall-clock windows |
| R2-P6-test-3106 | note | fixed `4d555317` | `node/client_session_command_test.go` — the client-session setup also waits until the controller holds no session the device closed |
| R2-P6-test-3107 | note | fixed `8c24def6` | `secure/sigma/sigma_accessor_test.go` — the SetSessionParameters tests assert the stored parameters through SessionParameters; cannotFailLeft empty |
| R2-P6-test-3108 | note | fixed `388c373f` | `secure/mattercert/verify_test.go` — drop the always-skipped WrongSigLength test; der_edges_test.go's "signature short" case holds the check; preconditionSkipsLeft empty |
| R2-P6-test-3109 | note | fixed `a8576767`, `e779c6c9` | `clock/sleep_ratchet_test.go` — the sleep ratchet walks the four nested modules, one exact pair per module |
| R2-P6-test-3110 | note | decided: recorded, no test seam for one test (the coordinator, tests Q3 rule) `fd633cc8` | `client/exchange/rollover_close_test.go` — N-rw2-3 still holds: the rollover tests register a hand-made secure session with `channel.Config.InitialCounter` and assert its close and farewell |
| R2-P6-test-3111 | note | fixed `409f6f04`, `2e08700a`: with the wrong-Go-type class of R2-P6-err-3112 (decision 234); `securityLeft` is empty | `cluster/core/operational_credentials_test.go` — The six security-path refusals left on `securityLeft` (`script/refusalcheck/refusalcheck_test.go:87-91`), listed by a probe of the guard with `securityLeft` emptied |
| R2-P6-err-3112 | note | fixed `409f6f04`, `2e08700a` | `cluster/core/operational_credentials.go` — one status per Go-only refusal class on the security-path servers; the ratchet at 0 |
| R2-P6-test-3113 | note | fixed in part `8c5b09e3`: the peers case; the rest recorded without a red test (the coordinator, tests Q3) | `cluster/spec/selfheld.go` — InvokeHeld admits a request field bounded by a sibling attribute through the server's peers, on the host-handler and the server-logic path |
| R2-P6-test-3114 | note | fixed `44eb35c7` | `secure/sigma/decoder_lengths_test.go` — the decoder-lengths comment names the tests that hold Sigma1's missing fields |
| R2-P6-test-3201 | low | fixed `c5f6818a`: line citations off the pin counted per commit and held by an exact ratchet (decision 123) | `script/citecheck/main.go` — line citations off the pin counted per commit and held exactly (citecheckOffPinRatchet); the collector reads the bare parts after a file named without a line and the "(<sha>," and "matter.js <sha>" forms; file-only, bare and chip counts printed, not ratcheted |
| R2-P6-test-3202 | low | fixed `c5f6818a`: the collector reads the bare parts after a file name and a sha in the two missed forms (decision 123); `transport/message/packet.go:29-40`'s list paragraph stays outside the grammar (the fix note) | `script/citecheck/collect.go` — line citations off the pin counted per commit and held exactly (citecheckOffPinRatchet); the collector reads the bare parts after a file named without a line and the "(<sha>," and "matter.js <sha>" forms; file-only, bare and chip counts printed, not ratcheted |
| R2-P6-doc-3203 | note (low) | fixed `43a13b73` | `mdns/records.go` — mdns — empty TXT record rule at DnsCodec.ts:477-483 at db021da0 |
| R2-P6-doc-3204 | note | fixed `3b467d23` | `client/handshake/resumption.go` — client/handshake — SessionManager.ts:880-886 lookup at db021da0 |
| R2-P6-doc-3205 | note | fixed `f4b8ce8a` | `im/subscription/manager.go` — im/subscription — SubscriptionsServer record, internal.subscriptions, SubscriptionStore at db021da0 |
| R2-P6-doc-3206 | note | fixed `adf9e8b7`: the range kept, its sentence labelled `at db021da0` | `controller/client/interact.go` — controller — R2-P6-doc-3206, R2-P6-doc-3207, R2-P6-par-3353 at db021da0 |
| R2-P6-doc-3207 | note | fixed `c2c7b4b1`, `abc85505`, `adf9e8b7` | `controller/client/interact.go` — the DN schema at the pin is TlvDistinguishedName, its type refusal DistinguishedName.ts:636-638; Noc.ts asSignedTlv at :58-60 |
| R2-P6-par-3208 | low (note) | fixed `2ebf9633`, `c12f72af` | `secure/mattercert/der.go` — DecodeDER refuses a malformed version, as matter.js at the pin |
| R2-P6-par-3209 | note | fixed `689c720e`, `7d166b1f`, `6f64382b` | `controller/tcp.go` — TCPConfig.MaxMessageSize, resolved and asserted as matter.js resolveTcpConfig |
| R2-P6-par-3210 | — (note) | struck: the client chunks a write that does not fit one message, as the pin; the reviewer's search missed it (verify); the stale header citation went with the wave-4 citation pass | `client/interaction/write.go` — The cited `sendWriteCommand` (`InteractionMessenger.ts:1110-1162 at 0ceb202d`) sends several WriteRequestMessages for a write that does not fit one at the pin (`:1269-1317` … |
| R2-P6-par-3211 | note | not a finding: a list of changed reference bodies; no gap in the rows read; the one divergence is R2-P6-par-3351 (verify) | `see for-fable.tsv rows 109, 141, 221, 228, 257, 302, 836, 1351, 1378, 1386, 1535` — Constructs a Go mirror cites at `862ad75f` / `0ceb202d` whose body changed by the pin, where the Go side may follow either version: `MessageExchange.send` (ack-wait abort, `PeerAckMissingError`) … |

### Rows the verify raised

Rows the verify raised itself (block `33`): the documentation's reference
claims found stale (`docs` and the follow-up read), the citation bin's new
rows, and R2-P6-sec-3320, which the follow-up read found beside a held row and
which was held until this branch fixed it. Their severity is the verifier's.

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P6-doc-3301 | low | fixed `b3cb03cf` | `docs/guides/advanced-seams.md` — guides and comments brought to the code; docscheck holds the Host table and the schema version |
| R2-P6-doc-3302 | low | fixed `3e7680a2` | `docs/adr/0016-device-type-validation.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-doc-3303 | note | fixed `b3cb03cf` | `docs/guides/persistence.md` — guides and comments brought to the code; docscheck holds the Host table and the schema version |
| R2-P6-doc-3310 | note | fixed `b3cb03cf` | `docs/certifiability.md` — guides and comments brought to the code; docscheck holds the Host table and the schema version |
| R2-P6-doc-3311 | note | fixed `b3cb03cf` | `docs/certifiability.md` — guides and comments brought to the code; docscheck holds the Host table and the schema version |
| R2-P6-doc-3312 | note | fixed `3e7680a2` | `docs/adr/0001-matter-commissioning-bring-up.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-doc-3313 | note | fixed `3e7680a2`, `77f5f886` | `docs/adr/0008-subscription-resumption.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-doc-3314 | note | fixed `3e7680a2` | `docs/adr/0013-generated-cluster-definitions.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-doc-3315 | note | fixed `3e7680a2` | `docs/adr/0013-generated-cluster-definitions.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-doc-3316 | note | fixed `3e7680a2` | `docs/adr/0016-device-type-validation.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-doc-3317 | note | fixed `3e7680a2` | `docs/adr/0018-device-layer-and-node-facade.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-doc-3318 | note | fixed `3e7680a2` | `docs/adr/0019-tcp-bdx-and-the-ota-requestor.md` — dated notes for reversed decisions, the index complete; docscheck holds both |
| R2-P6-sec-3320 | low | fixed `1f17987e`, `2590b624`, `27544f41`, `3d26f26f`, `d5c58bc9`, `53c3dbc6`, `b2e7ff67`: decisions 198, 231, 232, 243; `BD-Matter-CASEServer-ResumptionRecordSingleUse` (`361102dd`) | `bridge/handlers.go` — the responder claims a resumption id at lookup and releases the claim when no session is installed; a resumed session whose Sigma2_Resume is not acknowledged is closed; an initiator's failure StatusReport keeps the installed session, as matter.js |
| R2-P6-par-3351 | note | fixed `90eddef7` | `client/peer/descriptor.go` — deprecate Descriptor.DataModelRevision, absent from PeerDescriptor at the pin |
| R2-P6-par-3352 | note | fixed `afb08e9d` | `secure/mattercert/certificates_parity_matterjs_test.go` — matter.js builds new Rcac and Icac objects in the fabric-id cases |
| R2-P6-par-3353 | note | fixed `c2c7b4b1`, `adf9e8b7`, `c8f87a5f`: the DN schema sentence at every site, `controller/ca/encode.go` included; behaviour unchanged | `secure/mattercert/decode.go` — the DN schema at the pin is TlvDistinguishedName, its type refusal DistinguishedName.ts:636-638; Noc.ts asSignedTlv at :58-60 |
| R2-P6-par-3354 | note | fixed `abc85505`: the four conditions cited; the code already implements all four | `im/invoke.go` — im — #wildcardTargetOf's four conditions at db021da0 |

### The reference reads

Reference reads at the pins (matter.js `db021da0`, connectedhomeip `6170af84`,
read through `git show` because the neighbouring checkouts are at other
commits). The fact notes were written before a wave's code (`6b-w1`, `6b-w2`,
`6b-w3`, `6b-w3-items`, `6b-w4`, `6b-1857b`, `6b-2412i`, `6b-fxo-net`,
`6b-fxo-node`); the other reads took a fix unit's diff and its later rounds
(`6b-chs`, `-dcl`, `-cpeer`, `-radio-nix`, `-w1a`, `-guardfix`, `-cam`,
`-ctl`, `-gen`, `-facade`, `-device`, `-dev6a`), the third wave before and
after its merge (`6b-w3`, `6b-w3r2`), the register's text (`6b-register-w3`)
and the citations of wave 4 (`6b-w4-a`, `-w4-b`, `-w4-ctl86`, `-w4-s3a`,
`-w4-s3b`, `-w4-final`). A row is an item that asked for a change, recorded
something, found a defect, or is a fact a fix used; the items a read found to
hold are not rows. The item list is a map of the reads made for this file;
every row's line was checked to hold the item's label, a verdict word or an
item's start, and "where" carries a short summary of the item only when it
states no reference fact and concerns no held row — otherwise it points to the
read's line. **taken** names the fix note's line that took the item (for the
reads after the third wave's merge, the hand-over's line on the
post-integration pass); **recorded** the line that kept it as a record, a
question or a phase-7 carry; **not taken up** an item no note names (see "Not
placed, not verified").

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| 6b-w1 G1 1806 | — | fact note, used: `fix-chs.md:15`, `fix-chs.md:89` | `fable-6b-w1.md:17` (1806) — a reference statement of the read, not restated here |
| 6b-w1 G1 1909 | — | fact note, used: `fix-dcl.md:18`, `fix-dcl.md:74` | `fable-6b-w1.md:22` (1909) — a reference statement of the read, not restated here |
| 6b-w1 G1 2104 | — | fact note, used: `fix-cpeer.md:13`, `fix-cpeer.md:146` | `fable-6b-w1.md:27` (2104) — a reference statement of the read, not restated here |
| 6b-w1 G1 2105 | — | fact note, used: `fix-cpeer.md:14` | `fable-6b-w1.md:32` (2105) — a reference statement of the read, not restated here |
| 6b-w1 G1 2106 | — | fact note, used: `fix-cpeer.md:15` | `fable-6b-w1.md:37` (2106) — a reference statement of the read, not restated here |
| 6b-w1 G1 2109 | — | fact note, used: `fix-cpeer.md:18` | `fable-6b-w1.md:42` (2109) — a reference statement of the read, not restated here |
| 6b-w1 G1 2110 | — | fact note, used: `fix-cpeer.md:19`, `fix-cpeer.md:161` | `fable-6b-w1.md:47` (2110) — a reference statement of the read, not restated here |
| 6b-w1 G1 2111 | — | fact note, used: `fix-cpeer.md:20` | `fable-6b-w1.md:52` (2111) — a reference statement of the read, not restated here |
| 6b-w1 G1 2152 | — | fact note, used: `fix-cpeer.md:23` | `fable-6b-w1.md:57` (2152) — a reference statement of the read, not restated here |
| 6b-w1 G1 2153 | — | fact note, used: `fix-cpeer.md:24` | `fable-6b-w1.md:62` (2153) — a reference statement of the read, not restated here |
| 6b-w1 G1 2156 | — | fact note, used: `fix-cpeer.md:27` | `fable-6b-w1.md:67` (2156) — a reference statement of the read, not restated here |
| 6b-w1 G2 1801/1802 | — | fact note, used: `fix-chs.md:84` | `fable-6b-w1.md:74` (1801; 1802) — a reference statement of the read, not restated here |
| 6b-w1 G2 1807 | — | fact note, used: `fix-chs.md:85` | `fable-6b-w1.md:75` (1807) — a reference statement of the read, not restated here |
| 6b-w1 G2 1851 | — | fact note, used: `fix-chs.md:86` | `fable-6b-w1.md:76` (1851) — a reference statement of the read, not restated here |
| 6b-w1 G2 1804 | — | fact note, used: `fix-chs.md:87` | `fable-6b-w1.md:77` (1804) — a reference statement of the read, not restated here |
| 6b-w1 G2 1805 | — | fact note, used: `fix-chs.md:88` | `fable-6b-w1.md:78` (1805) — a reference statement of the read, not restated here |
| 6b-w1 G2 1902 | — | fact note, used: `fix-dcl.md:73` | `fable-6b-w1.md:79` (1902) — a reference statement of the read, not restated here |
| 6b-w1 G2 2101 | — | fact note, used: `fix-cpeer.md:79` | `fable-6b-w1.md:80` (2101) — a reference statement of the read, not restated here |
| 6b-w2 Register form (2203, 2210, 2362) | — | fact note, used: `fix-cam.md:126`, `fix-cam.md:130`, `fix-ctl.md:183` | `fable-6b-w2.md:16` (2203; 2210; 2362) — a reference statement of the read, not restated here |
| 6b-w2 G1 2203 | — | fact note, used: `fix-cam.md:21` | `fable-6b-w2.md:27` (2203) — a reference statement of the read, not restated here |
| 6b-w2 G1 2210 | — | fact note, used: `fix-cam.md:22`, `fix-cam.md:120` | `fable-6b-w2.md:48` (2210) — a reference statement of the read, not restated here |
| 6b-w2 G1 2362 | — | fact note, used: `fix-ctl.md:46`, `fix-ctl.md:160` | `fable-6b-w2.md:67` (2362) — a reference statement of the read, not restated here |
| 6b-w2 G1 2307 | — | fact note, used: `fix-ctl.md:41` | `fable-6b-w2.md:83` (2307) — a reference statement of the read, not restated here |
| 6b-w2 G1 2310 | — | fact note, used: `fix-ctl.md:42` | `fable-6b-w2.md:99` (2310) — a reference statement of the read, not restated here |
| 6b-w2 G1 2354 | — | fact note, used: `fix-ctl.md:43` | `fable-6b-w2.md:116` (2354) — a reference statement of the read, not restated here |
| 6b-w2 G1 2357 | — | fact note, used: `fix-ctl.md:44` | `fable-6b-w2.md:135` (2357) — a reference statement of the read, not restated here |
| 6b-w2 G1 2363 | — | fact note, used: `fix-ctl.md:45` | `fable-6b-w2.md:147` (2363) — a reference statement of the read, not restated here |
| 6b-w2 G2 2412 | — | fact note, used: `fix-dev6a.md:71`, `fix-facade.md:22`, `fix-facade.md:179` | `fable-6b-w2.md:163` (2412) — a reference statement of the read, not restated here |
| 6b-w2 G2 2902 | — | fact note, used: `fix-dev6a.md:72`, `fix-dev6a.md:206` | `fable-6b-w2.md:171` (2902) — a reference statement of the read, not restated here |
| 6b-1857b Schema | — | fact note, used: `fix-chs.md:158` | `fable-6b-1857b.md:5` (1857) — a reference statement of the read, not restated here |
| 6b-1857b matter.js decoder | — | fact note, used: `fix-chs.md:158` | `fable-6b-1857b.md:10` (1857) — a reference statement of the read, not restated here |
| 6b-1857b connectedhomeip decoder | — | fact note, used: `fix-chs.md:158` | `fable-6b-1857b.md:17` (1857) — a reference statement of the read, not restated here |
| 6b-1857b go-fabric today | — | fact note, used: `fix-chs.md:158` | `fable-6b-1857b.md:24` (1857) — a reference statement of the read, not restated here |
| 6b-1857b Verdict | — | fact note, used: `fix-chs.md:158` | `fable-6b-1857b.md:28` (1857) — a reference statement of the read, not restated here |
| 6b-2412i Verdict | — | fact note, used: `fix-dev6a.md:208`, `fix-dev6a.md:191` | `fable-6b-2412i.md:9` (2412; decision 233) — a reference statement of the read, not restated here |
| 6b-2412i matter.js measured | — | fact note, used: `fix-dev6a.md:191` | `fable-6b-2412i.md:12` (2412) — a reference statement of the read, not restated here |
| 6b-2412i matter.js inferred | — | fact note: no fix note names its use | `fable-6b-2412i.md:44` (2412) — a reference statement of the read, not restated here |
| 6b-2412i Not read | — | fact note: no fix note names its use | `fable-6b-2412i.md:56` (2412) — a reference statement of the read, not restated here |
| 6b-2412i Marker for the Ahead row | — | fact note, used: `fix-dev6a.md:208` | `fable-6b-2412i.md:61` (2412) — a reference statement of the read, not restated here |
| 6b-2412i go-fabric initiator paths | — | fact note, used: `fix-dev6a.md:191`, `fix-facade.md:22` | `fable-6b-2412i.md:71` (2412) — a reference statement of the read, not restated here |
| 6b-2412i Seen on the way | — | not taken up: no fix note names it — see "Not placed, not verified" | `fable-6b-2412i.md:95` — a reference statement of the read, not restated here |
| 6b-w1a F1 | low | taken: `fix-fx-csql.md:106` | `fable-read-6b-w1a.md:99` — resealTx runs the schema check before the write lock in both stores; the UPDATE should come first (inferred from SQLite snapshot rules) |
| 6b-w1a F2 | low | taken: `fix-ctl.md:30` | `fable-read-6b-w1a.md:111` (decision 132) — Decision 132 one warning line for an owed scrub is not on the controller path; hunk handed to wave 2 ctl must land before 6b closes |
| 6b-w1a F3 | note | recorded: `fix-fx-csql.md:109` | `fable-read-6b-w1a.md:117` (decision 131) — controller.Reseal against a running controller has no guard; a later secret is sealed under the old key; same contract as store.Reseal |
| 6b-w1a F4 | note | recorded: `fix-fx-csql.md:109` | `fable-read-6b-w1a.md:123` (decision 195) — Decision 195 both canaries last differs from the per-store order; a note for the decision text |
| 6b-w1a F5 | note | taken: `fix-fx-csql.md:107` | `fable-read-6b-w1a.md:125` — v3 Down is unguarded and removes the mark from a registry with sealed rows; operator action |
| 6b-w1a F6 | note | recorded: `fix-fx-csql.md:92` | `fable-read-6b-w1a.md:129` — Integration items: README API stability must name internal/sealseam (docscheck red) and controller/erase.go is outside the brief path list |
| 6b-guardfix F1 | — | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6b-guardfix.md:13` — A peripheral reported in the first loop and renewed before the drop is reported again next round, only past the bound |
| 6b-guardfix F2 | low | taken: `fix-fxo-ctl.md:15` | `fable-read-6b-guardfix.md:14` — The scanner.go:591-593 sentence once per peripheral while the scanner holds its record does not hold below the bound; suggest once per peripheral (address) |
| 6b-guardfix F3 | low | taken: `fix-fxo-ctl.md:15` | `fable-read-6b-guardfix.md:15` — a reference statement of the read, not restated here |
| 6b-guardfix §2 wire.go:42 | — | taken: `fix-guards.md:40` | `fable-read-6b-guardfix.md:18` — a reference statement of the read, not restated here |
| 6b-chs 2 — 1850 | — | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6b-chs.md:40` (1850) — Nil TrustAnchors fails closed in controller/commissioner, but the claim that SelfAnchored is the only self-anchored path does not hold module-wide (see F1) |
| 6b-chs 9 — held rows | — | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6b-chs.md:132` (R2-P3-rr-ctl-02; N3-rwrace-1; N3-ctl-3; U3-ctl-dev-1) — an item on a held row, not restated here |
| 6b-chs F1 | medium | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6b-chs.md:139` (1850) — A nil TrustAnchors on the jointfabric commissionee side is still the other side own RCAC implicitly; outside the unit paths and needs a row for its owner |
| 6b-chs F2 | low | taken: `fix-chs.md:188` | `fable-read-6b-chs.md:140` (1854) — a reference statement of the read, not restated here |
| 6b-chs F3 | low | taken: `fix-chs.md:191` | `fable-read-6b-chs.md:141` (1851; 1852; 1853; 1859; decision 191) — Two fabric load paths still save over a stored record (key without NOC, and a signer not matching the stored NOC); coordinator decides whether decision 191 covers them |
| 6b-chs F4 | low | taken: `fix-chs.md:192` | `fable-read-6b-chs.md:142` (1851) — A CA record that does not parse is refused but its error is not ErrConfig |
| 6b-chs F5 | low | taken: `fix-chs.md:193` | `fable-read-6b-chs.md:143` (1860) — The 25 s re-arm period lets the fail-safe lapse for devices with a fail-safe under about 25 s; fails closed later |
| 6b-chs F6 | low | taken: `fix-chs.md:194` | `fable-read-6b-chs.md:144` (1860) — The periodic ArmFailSafe runs on a context without cancel and can overlap the next step or the abort disarm on IP |
| 6b-chs F7 | note | taken: `fix-chs.md:195` | `fable-read-6b-chs.md:145` (1860) — The three consent tests use one helper and no BLE case is named |
| 6b-chs F8 | note | taken: `fix-chs.md:189` | `fable-read-6b-chs.md:146` (1806) — Parity test comment cites :167-170 for both the drop and the late-session close; the close is :140-157 |
| 6b-chs F9 | note | taken: `fix-chs.md:190` | `fable-read-6b-chs.md:147` (1857) — The wire.go sentence about not validating bounds should say the signature, since the 900-byte element bound is kept |
| 6b-chs F10 | note | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6b-chs.md:148` (R2-P3-rr-ctl-02; N3-rwrace-1; N3-ctl-3; U3-ctl-dev-1) — an item on a held row, not restated here |
| 6b-chs F11 | note | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6b-chs.md:149` (1807; TM R5 5a.5) — 1807 bounds an exchange queue by count (16) but adds no size check; the row stays open for message size |
| 6b-chs F12 | note | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6b-chs.md:150` — Pin tags 0ceb202d and 862ad75f sit beside line numbers measured at db021da0; whether they hold at the older commits was not read |
| 6b-dcl D1 | low | taken: `fix-dcl.md:156` | `fable-read-6b-dcl.md:35` (1902) — A store written before the fix can hold entries promoted by the old rule; no migration and no doc sentence |
| 6b-dcl D2 | — | taken: `fix-dcl.md:157` | `fable-read-6b-dcl.md:36` (1902) — AddCertificate looks up then stores, so a production entry arriving between drops the test add silently while AddCertificate still returns true |
| 6b-dcl D3 | low | taken: `fix-dcl.md:158` | `fable-read-6b-dcl.md:43` (5b-dcl F6) — an item on a held row, not restated here |
| 6b-dcl More than the rule | — | taken: `fix-dcl.md:155` | `fable-read-6b-dcl.md:55` (5b-dcl F6; 5b-dcl R9) — an item on a held row, not restated here |
| 6b-dcl 3 — 1908 ScopeConfig.MaxLength | — | fact note, used: `fix-dcl.md:167` | `fable-read-6b-dcl.md:64` (1908) — a reference statement of the read, not restated here |
| 6b-dcl 3 — 1903 | — | taken: `fix-dcl.md:171` | `fable-read-6b-dcl.md:67` (1903) — a reference statement of the read, not restated here |
| 6b-dcl 3 — 1904 | — | taken: `fix-dcl.md:172` | `fable-read-6b-dcl.md:68` (1904) — a reference statement of the read, not restated here |
| 6b-dcl D5 | low | taken: `fix-dcl.md:162`, `fix-dcl.md:170` | `fable-read-6b-dcl.md:82` (1909) — a reference statement of the read, not restated here |
| 6b-dcl D6 | low | taken: `fix-dcl.md:163` | `fable-read-6b-dcl.md:83` (1905; 1911) — a reference statement of the read, not restated here |
| 6b-dcl D7 | — | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6b-dcl.md:90` (1909) — a reference statement of the read, not restated here |
| 6b-dcl 6 — other defects | — | taken: `fix-dcl.md:159` | `fable-read-6b-dcl.md:92` — No high or medium; low D3, D4, D8, D9, D10; caps documented as Ahead rows but not as judgement values (D11) |
| 6b-dcl D8 | low | taken: `fix-dcl.md:160` | `fable-read-6b-dcl.md:94` — NodeDeleted versus an in-flight Applying status can arm a deleted peer after the disarm (inferred) |
| 6b-dcl D9 | low | taken: `fix-dcl.md:160` | `fable-read-6b-dcl.md:95` — NodeDeleted without a queued update returns before disabling the BDX scope and disarming; disable and disarm unconditionally |
| 6b-dcl D10 | low | taken: `fix-dcl.md:161` | `fable-read-6b-dcl.md:97` (1911) — The GitHub cap counts files the loop later skips (dcld_mirror_ files); cap after the full filter |
| 6b-dcl D4 | low | taken: `fix-dcl.md:159` | `fable-read-6b-dcl.md:98` — The name lock waits with no context, so a second DownloadUpdate of a name waits whatever its context |
| 6b-dcl 1905 note | note | fact note: no fix note names its use | `fable-read-6b-dcl.md:99` (1905) — a reference statement of the read, not restated here |
| 6b-dcl D11 | — | taken: `fix-dcl.md:163` | `fable-read-6b-dcl.md:100` (1905; 1911) — The caps 64/1024/1024 and two unmeasured basis sentences read as the module choice but are not marked as judgement; reword both |
| 6b-dcl 1915 register note | — | taken: `fix-dcl.md:173` | `fable-read-6b-dcl.md:101` (1915) — a reference statement of the read, not restated here |
| 6b-cpeer 1 — 2104 | low | taken: `fix-cpeer.md:162`, `fix-cpeer.md:163` | `fable-read-6b-cpeer.md:106` (2104; decision 221) — 2104 follows G1 plus decision 221 as worded but F1 and F2 leave the stated effect unmet in two cases |
| 6b-cpeer 1 — 2110 | medium | taken: `fix-cpeer.md:161` | `fable-read-6b-cpeer.md:112` (2110) — 2110 is as G1 but the rule itself delays a re-subscribe by up to an hour after an outage (F3) |
| 6b-cpeer 1 — Ahead-row texts | — | taken: `fix-cpeer.md:171` | `fable-read-6b-cpeer.md:116` (2101; 2104; 2105; 2110; 2111; 2152) — Ahead texts for 2101, 2105, 2111, 2152 are right; 2104 body holds only once F1 and F2 close and 2110 body waits for F3 |
| 6b-cpeer 2 — 2102/2150 | low | taken: `fix-cpeer.md:164`, `fix-cpeer.md:165` | `fable-read-6b-cpeer.md:26` (2102; 2150) — a reference statement of the read, not restated here |
| 6b-cpeer 6 — TM R3.4 | — | taken: `fix-cpeer.md:31` | `fable-read-6b-cpeer.md:141` (TM R3.4) — an item on a held row, not restated here |
| 6b-cpeer 7 — on the way | — | taken: `fix-cpeer.md:161` | `fable-read-6b-cpeer.md:31` — Findings F1 to F5 below; no high |
| 6b-cpeer F1 | low | taken: `fix-cpeer.md:162` | `fable-read-6b-cpeer.md:35` (2104) — A second binding to the same node is never released because Has is true after the first release; keep the record after a release |
| 6b-cpeer F2 | low | taken: `fix-cpeer.md:163` | `fable-read-6b-cpeer.md:44` (2104) — The release can be overtaken by the entry own Connect, which restarts the process after Disconnect (measured by overlay test) |
| 6b-cpeer F3 | medium | taken: `fix-cpeer.md:161` | `fable-read-6b-cpeer.md:55` (2110) — a reference statement of the read, not restated here |
| 6b-cpeer F4 | low | taken: `fix-cpeer.md:164` | `fable-read-6b-cpeer.md:69` (2102) — The within-a-factor-of-two claim fails for status-only entries (about 3x measured); charge each entry its struct size too |
| 6b-cpeer F5 | note | taken: `fix-cpeer.md:165` | `fable-read-6b-cpeer.md:80` (2102) — a reference statement of the read, not restated here |
| 6b-cpeer Note 2104 | note | taken: `fix-cpeer.md:163` | `fable-read-6b-cpeer.md:89` (2104) — Has then For are two calls, so a host creating the peer in between has it recorded as the manager's (inferred, narrow) |
| 6b-cpeer Note 2108 | note | taken: `fix-cpeer.md:176` | `fable-read-6b-cpeer.md:91` (2108) — a reference statement of the read, not restated here |
| 6b-cpeer Note 2152 | note | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6b-cpeer.md:96` (2152) — strings.EqualFold folds Unicode while DNS folds ASCII; no effect on a hex instance id |
| 6b-cpeer Note 2102 texts | note | taken: `fix-cpeer.md:173` | `fable-read-6b-cpeer.md:97` (2102) — The 2102 texts say 112 bytes flat but decodedNodeBytes is smaller on 32-bit builds |
| 6b-cpeer Note TestOmap_N3Disc2 | note | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6b-cpeer.md:98` (N3-disc-2) — an item on a held row, not restated here |
| 6b-cpeer Note resolve.go cites | note | taken: `fix-cpeer.md:167` | `fable-read-6b-cpeer.md:99` (2104) — binding/resolve.go mixes line bases; several older cites are off the pin, for the citation unit |
| 6b-cpeer Proposed-text correction 1 | — | taken: `fix-cpeer.md:173` | `fable-read-6b-cpeer.md:147` (2102) — CHANGELOG 2102 should say a bound within about 3x for status entries until F4 closes and the 112 bytes is the 64-bit figure |
| 6b-cpeer Proposed-text correction 2 | — | taken: `fix-cpeer.md:171` | `fable-read-6b-cpeer.md:149` (2104; 2110) — CHANGELOG and Ahead texts for 2104 and 2110 should hold until F1 to F3 are decided |
| 6b-cpeer Re-check note F3 residual | note | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6b-cpeer.md:168` (2110) — losses resets only for a subscription that lasted its current interval; the pacing works as written and could be said in the Ahead row |
| 6b-cpeer Re-check note F1 record lifetime | note | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6b-cpeer.md:171` (2104) — a reference statement of the read, not restated here |
| 6b-cpeer Re-check note F1 replaced peer | note | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6b-cpeer.md:175` (2104) — A replaced peer record is deleted without cancelling its period; the context lives until m.life ends |
| 6b-cpeer Re-check note F2 stale release | note | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6b-cpeer.md:177` (2104) — A release goroutine may run its Disconnect after the next period began; it heals unless the Disconnect comes after the Connect call timed out (inferred) |
| 6b-radio-nix 4 — wire-2024 | — | taken: `fix-radio-nix.md:170` | `fable-read-6b-radio-nix.md:64` (2024) — a reference statement of the read, not restated here |
| 6b-radio-nix F1 | medium | taken: `fix-radio-nix.md:154` | `fable-read-6b-radio-nix.md:22` (2018) — The 2018 live test reads CurrentNetwork once right after ConnectNetwork and is probably racy; poll with a bound (inferred) |
| 6b-radio-nix F2 | low | taken: `fix-radio-nix.md:155`, `fix-radio-nix.md:170` | `fable-read-6b-radio-nix.md:23` (2024) — a reference statement of the read, not restated here |
| 6b-radio-nix F3 | low | taken: `fix-radio-nix.md:156` | `fable-read-6b-radio-nix.md:24` (2014) — The 2014 check guards C3 hand-out but C3 is also served unguarded via Properties.Get, GetAll and GetManagedObjects; guard them or soften the comment |
| 6b-radio-nix F4 | low | taken: `fix-radio-nix.md:157` | `fable-read-6b-radio-nix.md:25` (2021) — A path owed after a failed RemoveNetwork can later be reused by a new network and removed by the sweep; addNetwork should drop the returned path from owed |
| 6b-cam 2 — store-2203 reconcileUntracked | low | taken: `fix-cam.md:114` | `fable-read-6b-cam.md:29` (2203) — a reference statement of the read, not restated here |
| 6b-cam 3 — Note 1 | note | taken: `fix-cam.md:116` | `fable-read-6b-cam.md:85` (2210) — The claim that a cut invoke stops retransmitting holds only if every command of its exchange is abandoned; the sentence should carry the condition |
| 6b-cam 3 — Note 2 | note | taken: `fix-cam.md:117` | `fable-read-6b-cam.md:89` (2210) — a reference statement of the read, not restated here |
| 6b-cam 4 — bound-2206 grace | note | taken: `fix-cam.md:115` | `fable-read-6b-cam.md:93` (2206) — a reference statement of the read, not restated here |
| 6b-cam 5 — by_design 2203 section 1 | low | taken: `fix-cam.md:126` | `fable-read-6b-cam.md:108` (2203) — a reference statement of the read, not restated here |
| 6b-cam 5 — by_design 2210 section 2 | low | taken: `fix-cam.md:130` | `fable-read-6b-cam.md:116` (2210) — The paragraph overstates that reconcileLeases drops a lease a cut give-back left; a lease never named lives on; also cite invoke_batcher.go and the batch condition |
| 6b-ctl 1 — fsm-2350 | medium | taken: `fix-ctl.md:163` | `fable-read-6b-ctl.md:12` (2350) — Behaviour holds but the Mirrors path in the Stop comment is wrong; on stop the false is announced via the subscription own observer, not NetworkClient disposal |
| 6b-ctl 3 — par-3209 | low | taken: `fix-ctl.md:164` | `fable-read-6b-ctl.md:56` (3209) — a reference statement of the read, not restated here |
| 6b-ctl 4 — 2354 | medium | taken: `fix-ctl.md:162` | `fable-read-6b-ctl.md:92` (2354) — onDeleted does not set closed so an operation admitted before close can feed the table again; set c.closed in onDeleted (inferred) |
| 6b-ctl 4 — 2363 | low | taken: `fix-ctl.md:165`, `fix-ctl.md:177` | `fable-read-6b-ctl.md:105` (2363) — The check holds but next sync wording is needed (not same call again for Start) and a sync toward off also reads first and can fail |
| 6b-ctl 4 — 2362 | medium | taken: `fix-ctl.md:160`, `fix-ctl.md:183` | `fable-read-6b-ctl.md:119` (2362) — Built as decision 222, but an RLOC16 is unique only within one Thread network so nothing tells which claim is true is incomplete; owner question key by ExtendedPanId or state it |
| 6b-ctl 5 — decision 219 wiring | — | taken: `fix-ctl.md:158` | `fable-read-6b-ctl.md:136` (219) — a reference statement of the read, not restated here |
| 6b-ctl 6 — sec-2361 (Q1) facts | — | fact note, used: `fix-ctl.md:159` | `fable-read-6b-ctl.md:149` (2361) — a reference statement of the read, not restated here |
| 6b-gen N1 none | note | taken: `fix-gen.md:166` | `fable-read-6b-gen.md:39` (2703) — a reference statement of the read, not restated here |
| 6b-gen N3 latent silent pass | note | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6b-gen.md:41` (2703) — jsonable writes a bigint as a string so a bigint default would render as a Go string; not at the pin; for the open list |
| 6b-gen 4 — 2713 | low | taken: `fix-gen.md:164`, `fix-gen.md:165` | `fable-read-6b-gen.md:81` (2713) — effectiveDirection and the by_design line hold with reworks: name the six tags, say regex over element source, and do one real generate-level run at v0.13.0 |
| 6b-gen 5 — 2701/2702 | low | taken: `fix-gen.md:163` | `fable-read-6b-gen.md:109` (2701; 2702) — a reference statement of the read, not restated here |
| 6b-facade 1a | low | taken: `fix-facade.md:180` | `fable-read-6b-facade.md:31` (R2-P6-sec-2401; U4-fr-sec-2) — an item on a held row, not restated here |
| 6b-facade 1b | low | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6b-facade.md:32` (R2-P6-sec-2401; U4-fr-sec-2) — an item on a held row, not restated here |
| 6b-facade 1 — Q1 per-fabric limit | — | fact note, used: `fix-facade.md:172` | `fable-read-6b-facade.md:34` (2401) — a reference statement of the read, not restated here |
| 6b-facade 2a | low | taken: `fix-facade.md:184` | `fable-read-6b-facade.md:54` (2411) — Between ExpireFailSafe and GeneralCommissioning.Close an administrator can arm again and AddNOC; closing it means refusing an arm once stopping |
| 6b-facade 3a | medium | taken: `fix-dev6a.md:184` | `fable-read-6b-facade.md:64` (2412) — a reference statement of the read, not restated here |
| 6b-facade 3b | low | taken: `fix-facade.md:186` | `fable-read-6b-facade.md:70` (2412) — The resumption record is persisted after the admit in both roles, so an update can leave one old-identity record; re-check admissible after persist |
| 6b-facade 5 — api-2409 floors | low | taken: `fix-facade.md:149`, `fix-facade.md:150` | `fable-read-6b-facade.md:78` (2409) — a reference statement of the read, not restated here |
| 6b-facade 5a | low | taken: `fix-facade.md:150` | `fable-read-6b-facade.md:80` (2409) — a reference statement of the read, not restated here |
| 6b-facade 5b | low | taken: `fix-facade.md:149` | `fable-read-6b-facade.md:82` (2409) — a reference statement of the read, not restated here |
| 6b-device 1b | medium | taken: `fix-device.md:158`, `fix-device.md:172`, `fix-device.md:176` | `fable-read-6b-device.md:84` (decision 204; R2-P6-fsm-2501) — a reference statement of the read, not restated here |
| 6b-device 2 | low | taken: `fix-device.md:173` | `fable-read-6b-device.md:102` (BD-Matter-Device-PartBoundWithItsOwner) — a reference statement of the read, not restated here |
| 6b-device 3b | — | taken: `fix-device.md:174`, `fix-device.md:178` | `fable-read-6b-device.md:139` (R2-P6-wire-2502) — a reference statement of the read, not restated here |
| 6b-device 3c | low | taken: `fix-device.md:175` | `fable-read-6b-device.md:154` — The notes sentence that a move between nodes stops the part on the old node and starts it on the new is false; the code does neither and the sentence must stay out of the CHANGELOG |
| 6b-device R1 | low | recorded: `fix-device.md:217`, `fix-device.md:222`, `fix-device.md:241` | `fable-read-6b-device.md:215` (R2-P6-fsm-2501; decision 225) — Live-move refusal holds, but the start window is real and wider than stated: Running turns true only after the snapshot, so a racing AddPart is not refused |
| 6b-device R2 | low | taken: `fix-device.md:218` | `fable-read-6b-device.md:259` — The Mirrors comment and its citations hold; one optional wording nit to name the route (setter) for the leaves-ownerless sentence |
| 6b-dev6a R1 | low | taken: `fix-dev6a.md:185` | `fable-read-6b-dev6a.md:48` (R2-P6-sec-3320) — Entries dropped by AbandonUnsecuredExchange lose the tie and close nothing; close the installed session there or record which ends are left to the reaper |
| 6b-dev6a R2 | low | taken: `fix-dev6a.md:189`, `fix-dev6a.md:201` | `fable-read-6b-dev6a.md:118` (R2-P6-sec-3320) — a reference statement of the read, not restated here |
| 6b-dev6a Q1 | — | fact note, used: `fix-dev6a.md:187` | `fable-read-6b-dev6a.md:131` (R2-P6-sec-3320; decision 232) — a reference statement of the read, not restated here |
| 6b-dev6a R3 | low | taken: `fix-dev6a.md:189` | `fable-read-6b-dev6a.md:162` (R2-P6-par-2902) — a reference statement of the read, not restated here |
| 6b-dev6a R4 | note | taken: `fix-dev6a.md:189` | `fable-read-6b-dev6a.md:174` (R2-P6-par-2902) — Busy answers are reliable messages that take inboundUnsecuredAckWaitLimit slots; worth one clause in the row |
| 6b-dev6a Q2 | — | fact note, used: `fix-dev6a.md:191` | `fable-read-6b-dev6a.md:186` (R2-P6-sec-2412) — a reference statement of the read, not restated here |
| 6b-dev6a R5 | note | taken: `fix-dev6a.md:189` | `fable-read-6b-dev6a.md:221` (R2-P6-par-2904) — a reference statement of the read, not restated here |
| 6b-dev6a 7 W6-test-17 / 3112 / Q3 | low | taken: `fix-dev6a.md:188` | `fable-read-6b-dev6a.md:226` (W6-test-17; R2-P6-err-3112) — a reference statement of the read, not restated here |
| 6b-dev6a 8 Decision 190 | — | fact note, used: `fix-fxo-node.md:14` | `fable-read-6b-dev6a.md:250` (decision 190) — a reference statement of the read, not restated here |
| 6b-dev6a O1 | — | fact note, used: `fix-dev6a.md:186` | `fable-read-6b-dev6a.md:304` (R2-P6-sec-3320; decision 231) — a reference statement of the read, not restated here |
| 6b-dev6a Q4 | — | fact note, used: `fix-fxo-node.md:14` | `fable-read-6b-dev6a.md:316` (decision 190; decision 235) — a reference statement of the read, not restated here |
| 6b-dev6a Q5 | — | fact note, used: `fix-dev6a.md:190` | `fable-read-6b-dev6a.md:318` (N3-coord-1; R2-P6-sec-3320) — a reference statement of the read, not restated here |
| 6b-dev6a R6 | note | taken: `fix-fxo-net.md:252` | `fable-read-6b-dev6a.md:357` (R2-P6-sec-3320) — The resumeClaims doc says the set is bounded by the store's records but the bound is the persist failures |
| 6b-dev6a R7 | note | taken: `fix-fxo-net.md:253` | `fable-read-6b-dev6a.md:361` (R2-P6-sec-3320) — The claim is not released when the bridge skips the callback; fix or record it |
| 6b-dev6a Client stack / R8 | low | taken: `fix-fxo-node.md:16` | `fable-read-6b-dev6a.md:418` (R8) — A handshake adopting a session after an UpdateNOC context ends can outlive the update under the old NOC; wait with context.WithoutCancel at node/client.go:411 |
| 6b-w3 R2-P6-sec-2616 | — | taken: `fix-examples.md:35`, `fix-examples.md:73`, `fix-register-6b.md:140` | `fable-6b-w3.md:33` (R2-P6-sec-2616; decision 207) — a reference statement of the read, not restated here |
| 6b-w3 R2-P6-wire-2654 | — | fact note, used: `fix-examples.md:36`, `fix-register-6b.md:120` | `fable-6b-w3.md:54` (R2-P6-wire-2654; decision 207) — a reference statement of the read, not restated here |
| 6b-w3 R2-P6-test-2851 | — | fact note, used: `fix-guards.md:36`, `fix-guards.md:87` | `fable-6b-w3.md:71` (R2-P6-test-2851; decision 207) — a reference statement of the read, not restated here |
| 6b-w3 R2-P6-par-3102 | — | recorded: `fix-tests-6b.md:12`, `fix-tests-6b.md:150`, `fix-register-6b.md:136` | `fable-6b-w3.md:91` (R2-P6-par-3102; decision 207) — a reference statement of the read, not restated here |
| 6b-w3 F-db021da0-15 | — | fact note, used: `fix-fxo-ctl.md:33`, `fix-fxo-ctl.md:123` | `fable-6b-w3.md:116` (F-db021da0-15; decision 209) — a reference statement of the read, not restated here |
| 6b-w3 TM R5 5a.5 | — | fact note, used: `fix-fxo-ctl.md:34`, `fix-fxo-ctl.md:125`, `fix-fxo-net.md:259` | `fable-6b-w3.md:150` (TM R5 5a.5; decision 209) — a reference statement of the read, not restated here |
| 6b-w3 R2-P3-rr-ctl-02 | — | fact note, used: `fix-fxo-ctl.md:35` | `fable-6b-w3.md:176` (R2-P3-rr-ctl-02) — an item on a held row, not restated here |
| 6b-w3 N3-ctl-3 | — | fact note, used: `fix-fxo-ctl.md:36` | `fable-6b-w3.md:210` (N3-ctl-3) — an item on a held row, not restated here |
| 6b-w3 U3-ctl-lan-1 (1) | — | fact note, used: `fix-fxo-ctl.md:37` | `fable-6b-w3.md:227` (U3-ctl-lan-1; decision 209) — an item on a held row, not restated here |
| 6b-w3 U3-ctl-lan-1 (2) | — | fact note, used: `fix-fxo-ctl.md:38`, `fix-fxo-ctl.md:117` | `fable-6b-w3.md:240` (U3-ctl-lan-1; decision 45) — an item on a held row, not restated here |
| 6b-w3 U3-ctl-lan-1 (3) | — | fact note, used: `fix-fxo-ctl.md:39` | `fable-6b-w3.md:251` (U3-ctl-lan-1) — an item on a held row, not restated here |
| 6b-w3 U3-ctl-lan-1 (4)/(5) | — | fact note, used: `fix-fxo-ctl.md:40` | `fable-6b-w3.md:255` (U3-ctl-lan-1 part (4); U3-ctl-lan-1 part (5)) — an item on a held row, not restated here |
| 6b-w3 U3-net-1 | — | fact note, used: `fix-fxo-ctl.md:41` | `fable-6b-w3.md:261` (U3-net-1) — an item on a held row, not restated here |
| 6b-w3 N3-rwrace-2 BLE half | — | fact note, used: `fix-fxo-ctl.md:42` | `fable-6b-w3.md:289` (N3-rwrace-2; decision 55) — a reference statement of the read, not restated here |
| 6b-w3-items D1 | — | fact note, used: `fix-fxo-node.md:309` | `fable-6b-w3-items.md:12` (F-db021da0-20) — a reference statement of the read, not restated here |
| 6b-w3-items D2 | — | fact note, used: `fix-fxo-node.md:310`, `fix-register-6b.md:125` | `fable-6b-w3-items.md:93` (R2-P6-fsm-1593) — a reference statement of the read, not restated here |
| 6b-w3-items D3 | — | fact note, used: `fix-fxo-node.md:311`, `fix-fxo-node.md:388` | `fable-6b-w3-items.md:140` (decision 250) — a reference statement of the read, not restated here |
| 6b-w3-items D4 | — | fact note, used: `fix-guards.md:122` | `fable-6b-w3-items.md:192` (R2-P6-test-2853) — a reference statement of the read, not restated here |
| 6b-w3-items D5(a) | — | fact note, used: `fix-register-6b.md:140` | `fable-6b-w3-items.md:243` (R2-P6-sec-2615) — a reference statement of the read, not restated here |
| 6b-w3-items D5(b) | — | fact note, used: `fix-fxo-net.md:262`, `fix-register-6b.md:137` | `fable-6b-w3-items.md:261` (decision 251) — a reference statement of the read, not restated here |
| 6b-w3-items D6 | — | fact note, used: `fix-register-6b.md:142` | `fable-6b-w3-items.md:284` (TM R3.8) — an item on a held row, not restated here |
| 6b-w3-items D7 | — | fact note, used: `fix-fxo-ctl.md:13`, `fix-register-6b.md:130` | `fable-6b-w3-items.md:334` (dcl D7) — a reference statement of the read, not restated here |
| 6b-w3-items D8(a) | — | fact note, used: `fix-register-6b.md:121` | `fable-6b-w3-items.md:372` (R2-P6-fsm-1860) — a reference statement of the read, not restated here |
| 6b-w3-items D8(b) | — | fact note, used: `fix-register-6b.md:134` | `fable-6b-w3-items.md:394` (R2-P6-sec-2412) — a reference statement of the read, not restated here |
| 6b-w3-items D8(c) | — | fact note, used: `fix-guards.md:137`, `fix-register-6b.md:134` | `fable-6b-w3-items.md:418` (R2-P6-doc-2817) — a reference statement of the read, not restated here |
| 6b-w3 docs 3 | — | taken: `fix-docs.md:136` | `fable-read-6b-w3.md:16` — a reference statement of the read, not restated here |
| 6b-w3 R2-P6-test-2850 | — | taken: `fix-guards.md:116` | `fable-read-6b-w3.md:54` (R2-P6-test-2850) — Composed device type structure read: loader supplies the side, for-each-instance sentence is the module's reading, counts 7 and 18 and the listed-type conformance need rewording |
| 6b-w3 R2-P6-test-2851 (:72) | low | taken: `fix-guards.md:121` | `fable-read-6b-w3.md:72` (R2-P6-test-2851) — a reference statement of the read, not restated here |
| 6b-w3 F-db021da0-48 | — | taken: `fix-fxo-net.md:258` | `fable-read-6b-w3.md:118` (F-db021da0-48) — a reference statement of the read, not restated here |
| 6b-w3 R2-P6-fsm-1402 | — | taken: `fix-fxo-node.md:335` | `fable-read-6b-w3.md:174` (R2-P6-fsm-1402) — RemainingTime 0 is reportable only when the stopped transition lasted a second or more; replacement text given |
| 6b-w3 R2-P6-err-1415 | — | taken: `fix-fxo-node.md:337` | `fable-read-6b-w3.md:189` (R2-P6-err-1415) — a reference statement of the read, not restated here |
| 6b-w3 F-db021da0-20 test comments | — | taken: `fix-fxo-node.md:338` | `fable-read-6b-w3.md:260` (F-db021da0-20) — a reference statement of the read, not restated here |
| 6b-w3 F-db021da0-15 reported.go | low | taken: `fix-fxo-ctl.md:123` | `fable-read-6b-w3.md:280` (F-db021da0-15) — a reference statement of the read, not restated here |
| 6b-w3 TM R5 5a.5 maxInboundMessage doc | — | taken: `fix-fxo-ctl.md:125` | `fable-read-6b-w3.md:291` (TM R5 5a.5) — a reference statement of the read, not restated here |
| 6b-w3 BD-chip-BootReasonAfterUpdate | low | taken: `fix-register-6b.md:126` | `fable-read-6b-w3.md:326` (N4-coresys-1) — a reference statement of the read, not restated here |
| 6b-w3r2 TM R3.7 per-fabric share (245) | — | taken: `handover.md:405`, `fix-cite-node.md:35` | `fable-read-6b-w3r2.md:35` (TM R3.7; decision 245) — an item on a held row, not restated here |
| 6b-w3r2 TM R5 5a.5 device twin | — | taken: `handover.md:405` | `fable-read-6b-w3r2.md:69` (TM R5 5a.5) — a reference statement of the read, not restated here |
| 6b-w3r2 Decision 251 windowSalt | low | taken: `handover.md:405`, `handover.md:408` | `fable-read-6b-w3r2.md:79` (decision 251) — Fresh salt per window holds and the failing test is traced: the window is open before the acceptor is attached; the note and a comment name the wrong cause |
| 6b-w3r2 No WithRetransmitIntervalResolver in node | medium | taken: `handover.md:405`, `handover.md:408` | `fable-read-6b-w3r2.md:119` (F-db021da0-13) — Node never wires the retransmit interval resolver so every secure-session retransmission uses 500 ms instead of the peer's advertised interval, with no by_design row |
| 6b-w3r2 reportedValue signedness | low | taken: `handover.md:405` | `fable-read-6b-w3r2.md:155` (F-db021da0-15) — Behaviour holds; proposed register line cites :135 where it should cite :136 |
| 6b-w3r2 D3 withdraw on failed reload (250) | — | taken: `handover.md:405` | `fable-read-6b-w3r2.md:163` (decision 250) — a reference statement of the read, not restated here |
| 6b-w3r2 D2 SetSupportedMaps cascade | medium | taken: `handover.md:405` | `fable-read-6b-w3r2.md:188` (R2-P6-fsm-1593) — a reference statement of the read, not restated here |
| 6b-register-w3 1 | — | taken: `handover.md:405` | `fable-read-6b-register-w3.md:9` (F-db021da0-15) — Closed register row: bounds citation :135 should be :136 and an older sentence contradicts the closing line |
| 6b-register-w3 2 | — | taken: `handover.md:405` | `fable-read-6b-register-w3.md:19` (F-db021da0-51) — Split row cites ranges that do not show one server serving every session, and the rest-stays sentence drops four parts |
| 6b-register-w3 5 | — | taken: `handover.md:405` | `fable-read-6b-register-w3.md:35` (F-db021da0-44) — Both callback orders inferred from map iteration may stand, with one clause added that both callbacks registered was not asserted |
| 6b-register-w3 15 | — | taken: `handover.md:405` | `fable-read-6b-register-w3.md:68` (R2-P6-doc-2817) — Class (ii) lead sentence says each bullet states behaviour dependence which is not true of the three bullets; replacement given |
| 6b-register-w3 16 | — | taken: `handover.md:405` | `fable-read-6b-register-w3.md:73` (TM R3.7; decision 245) — an item on a held row, not restated here |
| 6b-fxo-net F-db021da0-11 | — | fact note, used: `fix-fxo-net.md:118` | `fable-6b-fxo-net.md:18` (F-db021da0-11; R2-P6-err-1027) — a reference statement of the read, not restated here |
| 6b-fxo-net F-db021da0-13 | — | fact note, used: `fix-fxo-net.md:115`, `fix-fxo-net.md:256` | `fable-6b-fxo-net.md:35` (F-db021da0-13) — a reference statement of the read, not restated here |
| 6b-fxo-net F-db021da0-35 | — | fact note, used: `fix-fxo-net.md:110` | `fable-6b-fxo-net.md:61` (F-db021da0-35) — a reference statement of the read, not restated here |
| 6b-fxo-net F-db021da0-42 | — | fact note, used: `fix-fxo-net.md:109` | `fable-6b-fxo-net.md:82` (F-db021da0-42) — a reference statement of the read, not restated here |
| 6b-fxo-net F-db021da0-48 | — | fact note, used: `fix-fxo-net.md:112`, `fix-fxo-net.md:258` | `fable-6b-fxo-net.md:103` (F-db021da0-48) — a reference statement of the read, not restated here |
| 6b-fxo-net F-db021da0-49 | — | fact note, used: `fix-fxo-net.md:119` | `fable-6b-fxo-net.md:129` (F-db021da0-49) — a reference statement of the read, not restated here |
| 6b-fxo-net F-db021da0-50 | — | fact note, used: `fix-fxo-net.md:120` | `fable-6b-fxo-net.md:141` (F-db021da0-50) — a reference statement of the read, not restated here |
| 6b-fxo-net F-db021da0-51 | — | fact note, used: `fix-fxo-net.md:117` | `fable-6b-fxo-net.md:158` (F-db021da0-51) — a reference statement of the read, not restated here |
| 6b-fxo-net F-db021da0-53 | — | fact note, used: `fix-fxo-net.md:121` | `fable-6b-fxo-net.md:180` (F-db021da0-53) — a reference statement of the read, not restated here |
| 6b-fxo-net F-db021da0-54 | — | fact note, used: `fix-fxo-net.md:121` | `fable-6b-fxo-net.md:187` (F-db021da0-54) — a reference statement of the read, not restated here |
| 6b-fxo-net F-db021da0-55 | — | fact note, used: `fix-fxo-net.md:116`, `fix-fxo-net.md:257` | `fable-6b-fxo-net.md:196` (F-db021da0-55) — a reference statement of the read, not restated here |
| 6b-fxo-net F-db021da0-59 | — | fact note, used: `fix-fxo-net.md:111` | `fable-6b-fxo-net.md:214` (F-db021da0-59) — a reference statement of the read, not restated here |
| 6b-fxo-net N-net-1 (b) | — | fact note, used: `fix-fxo-net.md:122` | `fable-6b-fxo-net.md:234` (N-net-1) — an item on a held row, not restated here |
| 6b-fxo-net N-rw3req-3 | — | fact note, used: `fix-fxo-net.md:122` | `fable-6b-fxo-net.md:243` (N-rw3req-3) — an item on a held row, not restated here |
| 6b-fxo-net N-bridge2-3 | — | fact note, used: `fix-fxo-net.md:122` | `fable-6b-fxo-net.md:258` (N-bridge2-3) — an item on a held row, not restated here |
| 6b-fxo-net TM R3b.4 | — | fact note, used: `fix-fxo-net.md:122` | `fable-6b-fxo-net.md:273` (TM R3b.4) — an item on a held row, not restated here |
| 6b-fxo-net TM R3b.2 | — | fact note, used: `fix-fxo-net.md:122` | `fable-6b-fxo-net.md:275` (TM R3b.2) — an item on a held row, not restated here |
| 6b-fxo-net TM R3.7 (slots) | — | fact note, used: `fix-fxo-net.md:114`, `fix-fxo-net.md:255` | `fable-6b-fxo-net.md:289` (TM R3.7) — an item on a held row, not restated here |
| 6b-fxo-net U3-lan-dev-1 | — | fact note, used: `fix-fxo-net.md:124` | `fable-6b-fxo-net.md:322` (U3-lan-dev-1) — an item on a held row, not restated here |
| 6b-fxo-net N4-f3-2 | — | fact note, used: `fix-fxo-net.md:122` | `fable-6b-fxo-net.md:342` (N4-f3-2) — an item on a held row, not restated here |
| 6b-fxo-net U3-admin-1 | — | fact note, used: `fix-fxo-net.md:124` | `fable-6b-fxo-net.md:359` (U3-admin-1) — an item on a held row, not restated here |
| 6b-fxo-net TM R3.2 | — | fact note, used: `fix-fxo-net.md:122` | `fable-6b-fxo-net.md:378` (TM R3.2) — an item on a held row, not restated here |
| 6b-fxo-net TM R3.6 | — | fact note, used: `fix-fxo-net.md:113` | `fable-6b-fxo-net.md:393` (TM R3.6) — an item on a held row, not restated here |
| 6b-fxo-net TM R3.3 | — | fact note, used: `fix-fxo-net.md:122` | `fable-6b-fxo-net.md:409` (TM R3.3) — an item on a held row, not restated here |
| 6b-fxo-net TM R3.5 first half | — | fact note, used: `fix-fxo-net.md:122` | `fable-6b-fxo-net.md:422` (TM R3.5) — an item on a held row, not restated here |
| 6b-fxo-net TM R3.8 rest | — | fact note, used: `fix-fxo-net.md:123` | `fable-6b-fxo-net.md:432` (TM R3.8) — an item on a held row, not restated here |
| 6b-fxo-net U3-net-1 (device part) | — | fact note, used: `fix-fxo-net.md:122` | `fable-6b-fxo-net.md:448` (U3-net-1) — an item on a held row, not restated here |
| 6b-fxo-node U-alloc-1 (device parts) | — | fact note, used: `fix-fxo-node.md:27` | `fable-6b-fxo-node.md:15` (U-alloc-1) — an item on a held row, not restated here |
| 6b-fxo-node F-db021da0-9 | — | fact note, used: `fix-fxo-node.md:28` | `fable-6b-fxo-node.md:47` (F-db021da0-9; R2-P0-rv-B04) — a reference statement of the read, not restated here |
| 6b-fxo-node F-db021da0-20 | — | fact note, used: `fix-fxo-node.md:29`, `fix-fxo-node.md:309` | `fable-6b-fxo-node.md:69` (F-db021da0-20) — a reference statement of the read, not restated here |
| 6b-fxo-node F-db021da0-43 | — | fact note, used: `fix-fxo-node.md:30`, `fix-fxo-node.md:306` | `fable-6b-fxo-node.md:96` (F-db021da0-43) — a reference statement of the read, not restated here |
| 6b-fxo-node F-db021da0-44 | — | fact note, used: `fix-fxo-node.md:31`, `fix-fxo-net.md:260` | `fable-6b-fxo-node.md:126` (F-db021da0-44) — a reference statement of the read, not restated here |
| 6b-fxo-node F-db021da0-47 | low | fact note, used: `fix-fxo-node.md:32` | `fable-6b-fxo-node.md:145` (F-db021da0-47) — a reference statement of the read, not restated here |
| 6b-fxo-node F-db021da0-57 | — | fact note, used: `fix-fxo-node.md:33`, `fix-fxo-node.md:307` | `fable-6b-fxo-node.md:164` (F-db021da0-57) — a reference statement of the read, not restated here |
| 6b-fxo-node N4-d4wifi-1 | — | fact note, used: `fix-fxo-node.md:34` | `fable-6b-fxo-node.md:192` (N4-d4wifi-1) — a reference statement of the read, not restated here |
| 6b-fxo-node R2-P6-par-1360 | — | fact note, used: `fix-fxo-node.md:35` | `fable-6b-fxo-node.md:224` (R2-P6-par-1360) — a reference statement of the read, not restated here |
| 6b-fxo-node R2-P6-fsm-1401 | — | fact note, used: `fix-fxo-node.md:36` | `fable-6b-fxo-node.md:240` (R2-P6-fsm-1401) — a reference statement of the read, not restated here |
| 6b-fxo-node R2-P6-fsm-1402 | — | fact note, used: `fix-fxo-node.md:37` | `fable-6b-fxo-node.md:264` (R2-P6-fsm-1402) — a reference statement of the read, not restated here |
| 6b-fxo-node R2-P6-fsm-1412 | — | fact note, used: `fix-fxo-node.md:38` | `fable-6b-fxo-node.md:289` (R2-P6-fsm-1412) — a reference statement of the read, not restated here |
| 6b-fxo-node R2-P6-err-1415 | — | fact note, used: `fix-fxo-node.md:39` | `fable-6b-fxo-node.md:312` (R2-P6-err-1415) — a reference statement of the read, not restated here |
| 6b-fxo-node R2-P6-sec-1513 | — | fact note, used: `fix-fxo-node.md:40` | `fable-6b-fxo-node.md:334` (R2-P6-sec-1513) — a reference statement of the read, not restated here |
| 6b-fxo-node R2-P6-fsm-1593 | — | fact note, used: `fix-fxo-node.md:41`, `fix-fxo-node.md:310` | `fable-6b-fxo-node.md:350` (R2-P6-fsm-1593) — a reference statement of the read, not restated here |
| 6b-fxo-node N-flake-1 | — | fact note, used: `fix-fxo-node.md:42` | `fable-6b-fxo-node.md:372` (N-flake-1) — a reference statement of the read, not restated here |
| 6b-w4 W1 client/exchange/margins.go:68-70 | — | taken: `fix-cite-cli.md:26` | `fable-6b-w4.md:42` — a reference statement of the read, not restated here |
| 6b-w4 W1 client/exchange/margins_parity_matterjs_test.go:190 | — | taken: `fix-cite-cli.md:26` | `fable-6b-w4.md:43` — a reference statement of the read, not restated here |
| 6b-w4 W1 secure/operational/manager.go:491 (beyond the brief) | — | taken: `fix-cite-sec.md:58` | `fable-6b-w4.md:44` — a reference statement of the read, not restated here |
| 6b-w4 W1 bridge/outbound_reliable_test.go:182 (beyond the brief) | — | taken: `fix-cite-node.md:44` | `fable-6b-w4.md:45` — a reference statement of the read, not restated here |
| 6b-w4 W1 transport/mrp/retransmit.go:58 (beyond the brief) | — | not taken up: no fix note names it — see "Not placed, not verified" | `fable-6b-w4.md:47` — a reference statement of the read, not restated here |
| 6b-w4 W2 (a) general.ts:33 | — | recorded: `fix-cite-cli.md:27` | `fable-6b-w4.md:60` — Both sites hold; connection.go:53 has a shortened unpinned path, optional text with pin offered |
| 6b-w4 W2 (c) ClusterRequirements.ts:169-187 | — | recorded: `fix-cite-sec.md:60` | `fable-6b-w4.md:77` — Both sites hold; note that gen.go:375-377 says min/max are tried when the value gives none, but they are tried only when numericValue is undefined |
| 6b-w4 W2 (d) Fabric.ts:408-415 | — | taken: `fix-cite-node.md:46` | `fable-6b-w4.md:86` — a reference statement of the read, not restated here |
| 6b-w4 W2 (e) DatasourceCache.ts:219-222 | — | taken: `fix-cite-ctl.md:38` | `fable-6b-w4.md:116` — a reference statement of the read, not restated here |
| 6b-w4 W2 (g) R2-P6-sec-2412 second replaced listener | — | taken: `fix-cite-node.md:48`, `fix-cite-clu.md:76` | `fable-6b-w4.md:132` (R2-P6-sec-2412) — a reference statement of the read, not restated here |
| 6b-w4 W3 R2-P6-doc-0343 (b) exchange_routing.go:26-30 | — | taken: `fix-cite-node.md:38` | `fable-6b-w4.md:168` (R2-P6-doc-0343) — a reference statement of the read, not restated here |
| 6b-w4-a cli 1a row 104 client/interaction/handler.go:28-52 | note | taken: `fix-cite-w4fix.md:14` | `fable-read-6b-w4-a.md:18` (row 104) — a reference statement of the read, not restated here |
| 6b-w4-a cli 1a row 110 interaction.go:141-146 | note | taken: `fix-cite-w4fix.md:14` | `fable-read-6b-w4-a.md:19` (row 110) — a reference statement of the read, not restated here |
| 6b-w4-a cli 1a row 114 invoke.go:47-49 | note | taken: `fix-cite-w4fix.md:14` | `fable-read-6b-w4-a.md:20` (row 114; row 115) — a reference statement of the read, not restated here |
| 6b-w4-a cli 1a row 117 invoke.go:119-136 | note | taken: `fix-cite-w4fix.md:14` | `fable-read-6b-w4-a.md:22` (row 117) — Head should cite ClientInteraction.#invokeSingle :487-527 with sendInvokeCommand :1206-1253 at db021da0 |
| 6b-w4-a cli 1a row 118 invoke.go:121 | note | taken: `fix-cite-w4fix.md:14` | `fable-read-6b-w4-a.md:23` (row 118) — Same label as row 117; the unlabelled neighbours hold |
| 6b-w4-a cli 1a row 150 server.go:23 | note | taken: `fix-cite-w4fix.md:14` | `fable-read-6b-w4-a.md:25` (row 150; rows 149, 151) — a reference statement of the read, not restated here |
| 6b-w4-a cli 1a row 167 sustained_eventnumber_parity test header | note | recorded: `fix-cite-w4fix.md:14`, `fix-cite-w4fix.md:92` (a phase-7 carry) | `fable-read-6b-w4-a.md:27` (row 167) — Header names groups that do not exist; correct to the event-numbers and when-a-report-fails cases, and three of the cited its have no Go subtest |
| 6b-w4-a cli 1a row 171 write.go:107-132 | note | taken: `fix-cite-w4fix.md:14` | `fable-read-6b-w4-a.md:28` (row 171) — a reference statement of the read, not restated here |
| 6b-w4-a cli 1a row 172 write.go:123-124 | note | taken: `fix-cite-w4fix.md:14` | `fable-read-6b-w4-a.md:29` (row 172) — :1269-1450 over-reaches into sendTimedRequest; use :1269-1317 and :1332-1438 and drop the inner pin |
| 6b-w4-a cli 1a row 71 exchange/manager.go:28-33 | note | taken: `fix-cite-w4fix.md:14` | `fable-read-6b-w4-a.md:30` (row 71) — a reference statement of the read, not restated here |
| 6b-w4-a cli 1a row 896 im/invoke.go:773-776 | note | taken: `fix-cite-w4fix.md:15` | `fable-read-6b-w4-a.md:31` (row 896) — a reference statement of the read, not restated here |
| 6b-w4-a cli 1a row 902 im/read.go:89-93 (+ more-than-a-citation, low) | low | recorded: `fix-cite-w4fix.md:15`, `fix-cite-w4fix.md:88` (a phase-7 carry) | `fable-read-6b-w4-a.md:33` (row 902; row 901; row 903) — a reference statement of the read, not restated here |
| 6b-w4-a cli 1a row 951 mdns/answer.go:499-502 | note | taken: `fix-cite-w4fix.md:15` | `fable-read-6b-w4-a.md:35` (row 951) — a reference statement of the read, not restated here |
| 6b-w4-a cli 1a R2-P6-doc-0746-b tlv/encode.go:531-534 (+ more-than-a-citation, low) | low | taken: `fix-cite-w4fix.md:15` | `fable-read-6b-w4-a.md:36` (R2-P6-doc-0746-b) — a reference statement of the read, not restated here |
| 6b-w4-a cli 2 row 199 peer/probe.go:44-48 | note | taken: `fix-cite-w4fix.md:14` | `fable-read-6b-w4-a.md:66` (row 199) — a reference statement of the read, not restated here |
| 6b-w4-a cli 2 row 208 peer/transport.go:16-18 | note | recorded: `fix-cite-w4fix.md:14`, `fix-cite-w4fix.md:64` (a phase-7 carry) | `fable-read-6b-w4-a.md:67` (row 208) — a reference statement of the read, not restated here |
| 6b-w4-a cli 2 row 911 subscription/manager.go:307-309 | note | taken: `fix-cite-w4fix.md:15` | `fable-read-6b-w4-a.md:72` (row 911) — a reference statement of the read, not restated here |
| 6b-w4-a cli 2 row 916 session_changed_test.go:15-16 | note | taken: `fix-cite-w4fix.md:15` | `fable-read-6b-w4-a.md:73` (row 916) — Same correction as row 911 |
| 6b-w4-a cli 2 row 1212 transport/udp/multicast.go:117-120 | note | taken: `fix-cite-w4fix.md:15`, `fix-cite-w4fix.md:73` | `fable-read-6b-w4-a.md:75` (row 1212) — a reference statement of the read, not restated here |
| 6b-w4-a cli 2 row 914 kept clause im/subscription/manager.go:1221-1226 | note | taken: `fix-cite-w4fix.md:15` | `fable-read-6b-w4-a.md:81` (row 914) — a reference statement of the read, not restated here |
| 6b-w4-a cli 2 row 70+f exchange/exchange.go:469-472 | note | taken: `fix-cite-w4fix.md:14` | `fable-read-6b-w4-a.md:82` (row 70+f) — The retransmission-until-ack part moved to #retransmitMessage :934-1018; cite (:784-799, :934-1018) instead of dropping it |
| 6b-w4-a cli 2 more-than-a-citation: rewrap flattened doc lists | low | taken: `fix-cite-w4fix.md:14` | `fable-read-6b-w4-a.md:87` — The unit's rewrap removed list indentation in exchange.go, case.go and packet.go so Go doc comments render as run-on paragraphs; restore indentation, keep new numbers |
| 6b-w4-a cli 3 row 1237 im/invoke.go:699-705 | note | taken: `fix-cite-w4fix.md:15` | `fable-read-6b-w4-a.md:113` (R2-P6-doc-3207; R2-P6-par-3354; row 1237) — Kept #processWildcard range ends at :173 not :170; replacement given |
| 6b-w4-a cli observation im/invoke.go:704-705 It answered UNSUPPORTED_ENDPOINT | note | recorded: `fix-cite-w4fix.md:65` (a phase-7 carry) | `fable-read-6b-w4-a.md:142` — A pre-existing sentence has no antecedent for It; wording note, no behaviour claim against the pin |
| 6b-w4-a ctl 1 row 459 attestation/pqc.go:18-20 | note | taken: `fix-cite-w4fix.md:23` | `fable-read-6b-w4-a.md:150` (row 459) — a reference statement of the read, not restated here |
| 6b-w4-a ctl 1 row 573 client/network.go:22-24 | note | taken: `fix-cite-w4fix.md:23` | `fable-read-6b-w4-a.md:151` (row 573) — The sentence describes #defaultSubscribe options branch :265-271, not the built-request branch |
| 6b-w4-a ctl 1 row 570 live_test.go:160-162 | note | taken: `fix-cite-w4fix.md:23` | `fable-read-6b-w4-a.md:152` (row 570) — a reference statement of the read, not restated here |
| 6b-w4-a ctl 1 row 571 live_test.go:163 | note | taken: `fix-cite-w4fix.md:23` | `fable-read-6b-w4-a.md:153` (row 571) — a reference statement of the read, not restated here |
| 6b-w4-a ctl 1 row 572 live_test.go:164-168 | note | taken: `fix-cite-w4fix.md:23` | `fable-read-6b-w4-a.md:154` (row 572) — One paragraph with new NetworkClient and ClientStructure ranges for rows 570-572 |
| 6b-w4-a ctl 1 row 608 record.go:142-144 | note | recorded: `fix-cite-w4fix.md:23`, `fix-cite-w4fix.md:58` (a phase-7 carry) | `fable-read-6b-w4-a.md:155` (row 608) — a reference statement of the read, not restated here |
| 6b-w4-a ctl 1 row 772 controller/tcp.go:22-25 | note | taken: `fix-cite-w4fix.md:23`, `fix-cite-w4fix.md:95` | `fable-read-6b-w4-a.md:156` (row 772; R2-P6-par-3209) — a reference statement of the read, not restated here |
| 6b-w4-a ctl 2 encode.go:130 (:123-153 under the label) | note | taken: `fix-cite-w4fix.md:23` | `fable-read-6b-w4-a.md:166` — a reference statement of the read, not restated here |
| 6b-w4-a ctl 2 network.go:74 (row 576) | note | taken: `fix-cite-w4fix.md:23` | `fable-read-6b-w4-a.md:169` (row 576) — a reference statement of the read, not restated here |
| 6b-w4-a ctl 2 network.go:134 (row 577) | note | taken: `fix-cite-w4fix.md:23` | `fable-read-6b-w4-a.md:170` (row 577) — Start's queue entry is :93-98 and the handlers :114-116 |
| 6b-w4-a ctl 2 network.go:362 (row 581) | note | taken: `fix-cite-w4fix.md:23` | `fable-read-6b-w4-a.md:171` (row 581) — a reference statement of the read, not restated here |
| 6b-w4-a ctl 2 node.go (row 588) | note | taken: `fix-cite-w4fix.md:23` | `fable-read-6b-w4-a.md:172` (row 588) — The function is :131-238, the old :130-196 end was a 0ceb202d end under an 862ad75f label |
| 6b-w4-a ctl 3 row 553 interact.go:92-93 | note | taken: `fix-cite-w4fix.md:23` | `fable-read-6b-w4-a.md:184` (row 553; R2-P6-doc-3207) — Bare parts of the sentence: applications are :81-84 (read) and :189-192 (write) |
| 6b-w4-a ctl 4 row 452 advertise.go:17-18 | note | taken: `fix-cite-w4fix.md:23` | `fable-read-6b-w4-a.md:192` (row 452) — a reference statement of the read, not restated here |
| 6b-w4-a ctl 4 row 582 network.go:770-778 | note | taken: `fix-cite-w4fix.md:23` | `fable-read-6b-w4-a.md:197` (row 582) — a reference statement of the read, not restated here |
| 6b-w4-a ctl 4 row 641 structure.go:1010-1016 | note | taken: `fix-cite-w4fix.md:23` | `fable-read-6b-w4-a.md:198` (row 641) — a reference statement of the read, not restated here |
| 6b-w4-b node D1+D2 device/defaults_fallback.go:20-25 | note | taken: `fix-cite-w4fix.md:29` | `fable-read-6b-w4-b.md:16` (D1; D2; rows 788; 789) — a reference statement of the read, not restated here |
| 6b-w4-b node D3 (a) status_response_decode_test.go:26-29 | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:17` (D3; row 1240) — a reference statement of the read, not restated here |
| 6b-w4-b node D3 (b) status_response_decode_test.go:121-123 | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:18` (D3) — a reference statement of the read, not restated here |
| 6b-w4-b node D3 (c) subscribe_dispatch.go:740-741 | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:19` (D3) — a reference statement of the read, not restated here |
| 6b-w4-b node D4 outbound_reliable.go:200 | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:20` (D4; row 1241) — a reference statement of the read, not restated here |
| 6b-w4-b node D5 conn_tcp_test.go:343 | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:21` (D5; row 1246) — a reference statement of the read, not restated here |
| 6b-w4-b node D6 bridge.go:1884-1898 | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:22` (D6; row 1251) — a reference statement of the read, not restated here |
| 6b-w4-b node D9 contract/matter.go:385 (CommandInvokeResponse.ts:292-295) | note | taken: `fix-cite-w4fix.md:29` | `fable-read-6b-w4-b.md:30` (R2-P6-doc-2906) — Holds; add at db021da0 label |
| 6b-w4-b node D9 contract/matter.go:944 (ThermostatServer.ts:451) | note | taken: `fix-cite-w4fix.md:29` | `fable-read-6b-w4-b.md:31` (R2-P6-doc-2906) — a reference statement of the read, not restated here |
| 6b-w4-b node D9 contract/matter.go:223, :974 (Datasource.ts) | note | recorded: `fix-cite-w4fix.md:50` (a phase-7 carry) | `fable-read-6b-w4-b.md:32` (R2-P6-doc-2906) — Not performable: already rewritten with at db021da0 and not re-read here; phase 7 |
| 6b-w4-b node D9 node/config.go:371 (GeneralDiagnosticsServer.ts:168-182) | note | taken: `fix-cite-w4fix.md:29` | `fable-read-6b-w4-b.md:34` (R2-P6-doc-2906) — a reference statement of the read, not restated here |
| 6b-w4-b node D9 node/config.go:808 (DeviceCertification.ts:181) | note | taken: `fix-cite-w4fix.md:29` | `fable-read-6b-w4-b.md:35` (R2-P6-doc-2906) — a reference statement of the read, not restated here |
| 6b-w4-b node D9 node/factory_reset.go:173-175 (ServerNodeStore.ts erase) | note | taken: `fix-cite-w4fix.md:29` | `fable-read-6b-w4-b.md:36` (R2-P6-doc-2906) — a reference statement of the read, not restated here |
| 6b-w4-b node D9 node/icd.go:95 (IcdManagementServer.ts:813-820) | note | taken: `fix-cite-w4fix.md:29` | `fable-read-6b-w4-b.md:37` (R2-P6-doc-2906) — Holds; add at db021da0 label |
| 6b-w4-b node D9 node/node.go:1273, :1277 (Node.ts:114-132) | note | taken: `fix-cite-w4fix.md:29` | `fable-read-6b-w4-b.md:38` (R2-P6-doc-2906) — a reference statement of the read, not restated here |
| 6b-w4-b node D9 node/node.go:1325 (GeneralDiagnosticsServer.ts #online) | note | taken: `fix-cite-w4fix.md:29` | `fable-read-6b-w4-b.md:39` (R2-P6-doc-2906) — #online is :414-415 and #assertDataModelTest :186-204 |
| 6b-w4-b node D9 node/observers.go:28-30 (FabricManager.ts:41-48, :404-410) | note | taken: `fix-cite-w4fix.md:29` | `fable-read-6b-w4-b.md:40` (R2-P6-doc-2906) — #events is :44-51 and unhandled :405-411 |
| 6b-w4-b node D9 node/observers.go:54 (FabricManager.ts:253) | note | taken: `fix-cite-w4fix.md:29` | `fable-read-6b-w4-b.md:42` (R2-P6-doc-2906) — a reference statement of the read, not restated here |
| 6b-w4-b node D9 node/observers.go:70-72 | note | taken: `fix-cite-w4fix.md:29` | `fable-read-6b-w4-b.md:43` (R2-P6-doc-2906) — a reference statement of the read, not restated here |
| 6b-w4-b node D9 node/observers.go:105, :109 (SessionsBehavior.ts) | note | taken: `fix-cite-w4fix.md:29` | `fable-read-6b-w4-b.md:44` (R2-P6-doc-2906) — Holds; add at db021da0 label |
| 6b-w4-b node D9 node/sessions.go:46, :109 (SessionManager.ts) | note | recorded: `fix-cite-w4fix.md:51` (a phase-7 carry) | `fable-read-6b-w4-b.md:46` (R2-P6-doc-2906) — Not performable, neither read; phase 7 |
| 6b-w4-b node D9 device/device.go:169-170 (Endpoint.ts:596-637) | note | taken: `fix-cite-w4fix.md:29`, `fix-cite-w4fix.md:56` | `fable-read-6b-w4-b.md:47` (R2-P6-doc-2906) — Holds; add at db021da0 label, optional :596-638 |
| 6b-w4-b node D10 MessageExchange.ts:428-433 (ackpump_test.go:534, receive.go:220) | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:56` (F1) — a reference statement of the read, not restated here |
| 6b-w4-b node D10 ExchangeManager.ts:407-440 (bdx_responder_test.go:91) | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:57` — a reference statement of the read, not restated here |
| 6b-w4-b node D10 ExchangeManager.ts:638-650 (conn.go:215) | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:62` — Re-range to #sessionsOnChannel :699-713 |
| 6b-w4-b node D10 ExchangeManager.ts:242-265 (conn.go:609) | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:63` — a reference statement of the read, not restated here |
| 6b-w4-b node D10 ExchangeManager.ts:674-681, :696-713 (conn_tcp_test.go:249) | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:64` — Re-range to :739-746 and #deleteSession :761-779 |
| 6b-w4-b node D10 EventReadResponse.ts (event_acl_test.go:86) | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:65` — Holds; add at db021da0 label |
| 6b-w4-b node D10 LevelControlServer.ts:596 (fields_reader_test.go:951) | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:70` — Re-range to :720 and :729-736 |
| 6b-w4-b node D10 InteractionProtocolTest.ts:2126-2138 | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:71` — Re-range to :2135-2147 |
| 6b-w4-b node D10 ExchangeManager.ts:554, SessionManager.ts:688 (ota_provider_link.go:377-378) | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:72` — a reference statement of the read, not restated here |
| 6b-w4-b node D10 MessageCodec.ts:377 (receive.go:276) | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:74` — a reference statement of the read, not restated here |
| 6b-w4-b node D10 MessageExchange.ts:602 (receive_dispatch.go:673-674) + F7 | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:76` (F7) — a reference statement of the read, not restated here |
| 6b-w4-b node D10 CommandInvokeResponse.ts:292 (receive_dispatch.go:726, timed_conformance_test.go:83) | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:77` — Holds; add label at timed_conformance_test.go:83 |
| 6b-w4-b node D10 ExchangeManager.ts:696-713 (securechannel.go:2593) | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:78` — Re-range to #deleteSession :761-779 |
| 6b-w4-b node D10 InteractionMessenger.ts:347 (subscribe.go:1682) | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:81` — Re-range to sendDataReport :470 |
| 6b-w4-b node D10 ThermostatServer.ts:451 (subscribe.go:2301, contract/matter.go:944) | note | taken: `fix-cite-w4fix.md:28`, `fix-cite-w4fix.md:29` | `fable-read-6b-w4-b.md:82` — a reference statement of the read, not restated here |
| 6b-w4-b node D10 Datasource.ts:776-796 (subscribe.go:2377) | note | recorded: `fix-cite-w4fix.md:51` (a phase-7 carry) | `fable-read-6b-w4-b.md:83` — Not performable: the emit was not read; phase 7 |
| 6b-w4-b node D10 AttributeReadResponse.ts:125-131 (subscribe_dispatch.go:93-95) + D11 | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:84` (D11) — a reference statement of the read, not restated here |
| 6b-w4-b node D10 ServerSubscription.ts:764 (subscribe_encryption_test.go:27) | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:85` — Re-range to :893 and :900 |
| 6b-w4-b node D10 ExchangeManager.ts:441-452 (subscribe_report_routing_test.go:104-106) | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:86` — a reference statement of the read, not restated here |
| 6b-w4-b node D10 bridged-node.ts:39-73 (device.go:762) | note | taken: `fix-cite-w4fix.md:29` | `fable-read-6b-w4-b.md:87` — Holds; add at db021da0 label |
| 6b-w4-b node D10 ClientStructure.ts:871 (device/type.go:196) | note | taken: `fix-cite-w4fix.md:29` | `fable-read-6b-w4-b.md:89` — a reference statement of the read, not restated here |
| 6b-w4-b node D10 OtaRequestorTestInstance.ts:166-168 (ota.go) | note | taken: `fix-cite-w4fix.md:29` | `fable-read-6b-w4-b.md:92` — Holds; add at db021da0 labels |
| 6b-w4-b node D10 ServerNode.ts:107-127 (node/config.go:47) | note | recorded: `fix-cite-w4fix.md:52` (a phase-7 carry) | `fable-read-6b-w4-b.md:93` — Not performable: the range is the create overloads, the behaviour ids are not in it; phase 7 |
| 6b-w4-b node D10 NetworkServer.ts:202-222 (config.go:51, :280) | note | taken: `fix-cite-w4fix.md:29`, `fix-cite-w4fix.md:53` | `fable-read-6b-w4-b.md:94` — a reference statement of the read, not restated here |
| 6b-w4-b clu row 244 port header general_diagnostics_features_parity_matterjs_test.go:4-9 | note | taken: `fix-cite-w4fix.md:34` | `fable-read-6b-w4-b.md:114` (row 244) — Header should describe the added upstream case and the DataModelTest feature check at db021da0; text given, bare parts hold |
| 6b-w4-b clu row 247 groupcast.go:61-62 | note | taken: `fix-cite-w4fix.md:56` | `fable-read-6b-w4-b.md:117` (row 247) — Holds; optional added label and joinGroup lines |
| 6b-w4-b clu row 346 spec/decode_value.go:26-28 | note | taken: `fix-cite-w4fix.md:34` | `fable-read-6b-w4-b.md:118` (row 346) — a reference statement of the read, not restated here |
| 6b-w4-b clu row 364 spec/ids.go:91-96 | note | taken: `fix-cite-w4fix.md:34` | `fable-read-6b-w4-b.md:119` (row 364) — Reporting together is report() :154-177 not #validateSchema body :191-213 |
| 6b-w4-b clu rows 443/444 wire/admincommissioning.go:218-219 | note | taken: `fix-cite-w4fix.md:56` | `fable-read-6b-w4-b.md:121` (rows 443; 444) — Holds; optional added clause about timeout bounds :226-236 |
| 6b-w4-b sec D1 mattercert/der.go:27-40 | note | taken: `fix-cite-w4fix.md:39` | `fable-read-6b-w4-b.md:127` (D1; row 1106) — Clause-by-clause text for parseAsn1Certificate size, algorithm and DN order at db021da0, replacement given |
| 6b-w4-b sec D2 der_edges_test.go:137-140 (+ F2) | low | recorded: `fix-cite-w4fix.md:39`, `fix-cite-w4fix.md:54` (a phase-7 carry) | `fable-read-6b-w4-b.md:128` (D2; F2; row 1110) — a reference statement of the read, not restated here |
| 6b-w4-b sec D3 spake2/parity_matterjs_test.go:12 (+ F6) | note | taken: `fix-cite-w4fix.md:39` | `fable-read-6b-w4-b.md:129` (D3; F6; row 1160) — a reference statement of the read, not restated here |
| 6b-w4-b sec D5 operational/manager.go:491-494 (W1) | note | taken: `fix-cite-w4fix.md:39` | `fable-read-6b-w4-b.md:131` (D5) — a reference statement of the read, not restated here |
| 6b-w4-b sec D6 operational/manager.go:1574-1577 | note | taken: `fix-cite-w4fix.md:39` | `fable-read-6b-w4-b.md:132` (D6) — a reference statement of the read, not restated here |
| 6b-w4-b sec D7 secure/channel/group.go:42-55, :31-33 | note | taken: `fix-cite-w4fix.md:39`, `fix-cite-w4fix.md:69` | `fable-read-6b-w4-b.md:133` (D7) — a reference statement of the read, not restated here |
| 6b-w4-b sec D9 rows 1087, 1135, 1136, 1137 | note | taken: `fix-cite-w4fix.md:57` | `fable-read-6b-w4-b.md:135` (D9; rows 1087; 1135; 1136; 1137) — Holds; optional (:155-165) tightening of one range |
| 6b-w4-b sec D10 mattercert/decode.go:80-82 | note | taken: `fix-cite-w4fix.md:39` | `fable-read-6b-w4-b.md:136` (D10) — a reference statement of the read, not restated here |
| 6b-w4-b sec D11 subscribe_dispatch.go:92-93 | note | taken: `fix-cite-w4fix.md:28` | `fable-read-6b-w4-b.md:137` (D11) — a reference statement of the read, not restated here |
| 6b-w4-b s2 clu row 367 markchanged.go:48-54 (+ F5) | note | taken: `fix-cite-w4fix.md:34` | `fable-read-6b-w4-b.md:148` (row 367; F5) — a reference statement of the read, not restated here |
| 6b-w4-b s3 clu by_design entry 5564-5644 (three notes) | note | taken: `fix-cite-w4fix.md:34` | `fable-read-6b-w4-b.md:170` — a reference statement of the read, not restated here |
| 6b-w4-b s4 node 1036 node/reserve.go:19-27 | note | taken: `fix-cite-w4fix.md:56` | `fable-read-6b-w4-b.md:187` (row 1036) — Holds; optional clause that a reservation of an uninstalled endpoint goes to another endpoint presetting the number |
| 6b-w4-b s5 collector misses three forms (+ F3) | low | taken: `fix-cite-w4fix.md:44` | `fable-read-6b-w4-b.md:231` (F3; R2-P6-test-3202) — Still unread forms: (..., sha), pin sha and HEAD sha, so the off-pin ratchet undercounts; add testdata per form or rewrite the sites |
| 6b-w4-b s5 off-pin floor not zero | note | taken: `fix-cite-w4fix.md:45` | `fable-read-6b-w4-b.md:245` — Notes for the integration count: levels_acknowledged cites two historical commits on purpose and two 9892fa39 rows are new-form rows in clu paths |
| 6b-w4-b F1 bridge/receive.go:216-222 expediteDuplicateAck | low | recorded: `fix-cite-w4fix.md:81` (a phase-7 carry) | `fable-read-6b-w4-b.md:261` (F1) — The pin's duplicate branch sends the standalone ack only if retransmission was not kicked; client side has the counterpart, bridge grep finds none; needs a phase-7 read |
| 6b-w4-b F4 client/jcm/doc.go:12 calls 862ad75f matter.js's pin | note | taken: `fix-cite-w4fix.md:39` | `fable-read-6b-w4-b.md:273` (F4) — Doc text names a non-pin commit as the pin; the wording fix lands in the cli path |
| 6b-w4-ctl86 ctl86 row 453 advertise.go:19-21 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:21` (row 453) — Class CommissionerMdnsAdvertisement is :21-76 not :20-71; the only-when-asked clause is not in the block (relabel) |
| 6b-w4-ctl86 ctl86 row 496 ca/ca.go:762-763 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:30` (row 496) — a reference statement of the read, not restated here |
| 6b-w4-ctl86 ctl86 row 500 ca/doc.go:26-28 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:33` (row 500) — a reference statement of the read, not restated here |
| 6b-w4-ctl86 ctl86 row 506a addresses.go:146 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:35` (row 506a) — a reference statement of the read, not restated here |
| 6b-w4-ctl86 ctl86 row 506b addresses.go:147 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:36` (row 506b) — a reference statement of the read, not restated here |
| 6b-w4-ctl86 ctl86 row 506c addresses.go:149 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:37` (row 506c) — Wait loop is :374-381, not :360-368 |
| 6b-w4-ctl86 ctl86 row 506d addresses.go:150 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:38` (row 506d) — Refresh of a match is :382-388, not :369-375 |
| 6b-w4-ctl86 ctl86 row 506e addresses.go:151 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:39` (row 506e) — return factory.create is :389, not :376 |
| 6b-w4-ctl86 ctl86 row 518 client_subscribe_cancelled test:21-23 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:43` (row 518) — ClientSubscribeTest describe ends :1333, not :1331 |
| 6b-w4-ctl86 ctl86 row 537 fabricops.go:173-175 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:46` (row 537) — a reference statement of the read, not restated here |
| 6b-w4-ctl86 ctl86 row 556 interact.go:138-142 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:48` (row 556) — a reference statement of the read, not restated here |
| 6b-w4-ctl86 ctl86 row 558a interact.go:269-270 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:50` (row 558a) — #awaitModeIcdRouting applications are :81-84 and :189-192 |
| 6b-w4-ctl86 ctl86 row 574 network.go:47-51 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:51` (row 574) — #endDefaultSubscription starts :243, not :239 |
| 6b-w4-ctl86 ctl86 row 578 network.go:223-226 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:52` (row 578) — #adoptFabricFilter ends :298, not :299 |
| 6b-w4-ctl86 ctl86 row 618 registry.go:112-113 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:58` (row 618) — a reference statement of the read, not restated here |
| 6b-w4-ctl86 ctl86 row 619 registry.go:113-114 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:59` (row 619) — forDescriptor ends :390, not :389 |
| 6b-w4-ctl86 ctl86 row 639 structure.go:217-224 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:65` (row 639) — injectVersionFilters ends :324, not :323 |
| 6b-w4-ctl86 ctl86 row 642 structure.go:1062-1067 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:66` (row 642) — a reference statement of the read, not restated here |
| 6b-w4-ctl86 ctl86 row 643 structure.go:1088-1091 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:67` (row 643) — a reference statement of the read, not restated here |
| 6b-w4-ctl86 ctl86 row 647 window.go:81-83 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:69` (row 647) — a reference statement of the read, not restated here |
| 6b-w4-ctl86 ctl86 row 661 commissioner.go:169-173 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:71` (row 661) — a reference statement of the read, not restated here |
| 6b-w4-ctl86 ctl86 row 693 progress.go:38-40 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:79` (row 693) — CommissioningStep type is :185-200, not :184-199 |
| 6b-w4-ctl86 ctl86 row 693a progress.go:51 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:80` (row 693a) — Quoted words are :444-445, not :444-446 |
| 6b-w4-ctl86 ctl86 row 693b progress.go:60 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:81` (row 693b) — Terminating branch starts :437, not :438 |
| 6b-w4-ctl86 ctl86 row 693c progress.go:63 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:82` (row 693c) — Failsafe bookkeeping if is :398-424, not :397-424 |
| 6b-w4-ctl86 ctl86 row 693d progress.go:64 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:83` (row 693d) — Recoverable branch is :431-436, not :432-437 |
| 6b-w4-ctl86 ctl86 row 693e progress.go:66 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:84` (row 693e) — Failure without fabric index is :1497-1503 in #updateFabricLabel, not :1480-1485 |
| 6b-w4-ctl86 ctl86 row 696 controller.go:95-97 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:86` (row 696) — a reference statement of the read, not restated here |
| 6b-w4-ctl86 ctl86 row 699 controller.go:136-138 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:88` (row 699) — PairedNode events literal is :277-290, not :277-283 |
| 6b-w4-ctl86 ctl86 row 707 controller.go:1099-1103 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:89` (row 707) — knownAddress branch ends :565, not :566 |
| 6b-w4-ctl86 ctl86 row 710 controller.go:1172-1175 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:91` (row 710) — Same range as row 696: :465-511 |
| 6b-w4-ctl86 ctl86 row 747 sessions.go:15-16 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:97` (row 747) — SessionIntervals defaults is :45-49, not :45-50 |
| 6b-w4-ctl86 ctl86 row 748 sessions.go:16-17 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:98` (row 748) — SessionManager doc starts :58, not :59 |
| 6b-w4-ctl86 ctl86 row 766a observe.go:22 | note | taken: `fix-cite-w4fix.md:126` | `fable-read-6b-w4-ctl86.md:102` (row 766a) — #queueUpdate is :892-921, not :892-920 |
| 6b-w4-ctl86 ctl86 row 727 controller/open.go:28-30 (+ F2) | note | recorded: `fix-cite-w4fix.md:176`, `fix-cite-w4fix.md:134`, `fix-cite-w4fix.md:210` (a phase-7 carry) | `fable-read-6b-w4-ctl86.md:94` (row 727; F2) — a reference statement of the read, not restated here |
| 6b-w4-ctl86 ctl86 row 661a commissioner.go:169 | note | taken: `fix-cite-w4fix.md:173`, `fix-cite-w4fix.md:137` | `fable-read-6b-w4-ctl86.md:72` (row 661a; F1) — a reference statement of the read, not restated here |
| 6b-w4-ctl86 ctl86 F1 DAV numbers of an older commit in attestation/validate.go | low | taken: `fix-cite-w4fix.md:172` | `fable-read-6b-w4-ctl86.md:140` (F1) — Unlabelled DAV:n numbers in step comments (32 lines plus commissioner.go:169) are from an older commit and mix with pin numbers; text defect, step order matches |
| 6b-w4-ctl86 ctl86 F2 controller/open.go:28-30 StorageService | note | recorded: `fix-cite-w4fix.md:210` (a phase-7 carry) | `fable-read-6b-w4-ctl86.md:148` (F2) — a reference statement of the read, not restated here |
| 6b-w4-ctl86 ctl86 F3 verify-6b-cite section 3 verified by overlap | note | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6b-w4-ctl86.md:152` (F3) — Process note: the section 3 confirmed verdicts checked overlap not boundaries, so 7 of 12 re-ranges taken from it were off at one end |
| 6b-w4-s3a s3a row 5 bdx/responder.go:92-93 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:29` (row 5) — a reference statement of the read, not restated here |
| 6b-w4-s3a s3a row 89 exchange/tcp_session_binding test:6-7 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:37` (row 89) — a reference statement of the read, not restated here |
| 6b-w4-s3a s3a row 155+a subscribe.go:147 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:42` (row 155+a) — The subscribe closure is :1056-1125, label (:1056-1125) |
| 6b-w4-s3a s3a row 155+b subscribe.go:147-148 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:43` (row 155+b) — sendSubscribeRequest call is :1074-1081 |
| 6b-w4-s3a s3a row 155+c subscribe.go:153-155 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:44` (row 155+c) — new PeerSubscription and addPeer are :1101-1110 |
| 6b-w4-s3a s3a row 155+d subscribe.go:160-161 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:45` (row 155+d) — Sustained branch is :1139-1162 |
| 6b-w4-s3a s3a row 179 peer/descriptor.go:16-17 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:51` (row 179) — a reference statement of the read, not restated here |
| 6b-w4-s3a s3a row 179+a descriptor.go:30-31 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:52` (row 179+a) — operationalAddress is :45-48 |
| 6b-w4-s3a s3a row 179+b descriptor.go:45-46 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:53` (row 179+b) — discoveryData is :50-53 |
| 6b-w4-s3a s3a row 179+c descriptor.go:48-50 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:54` (row 179+c) — reportedSessionParameters is :55-60 |
| 6b-w4-s3a s3a row 179+d descriptor.go:54-55 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:55` (row 179+d) — a reference statement of the read, not restated here |
| 6b-w4-s3a s3a row 207+a timing.go:21 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:57` (row 207+a) — maxDelayBetweenInitialContactRetries is :30-37 |
| 6b-w4-s3a s3a row 207+b timing.go:24 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:58` (row 207+b) — delayBeforeNextAddress is :39-44 |
| 6b-w4-s3a s3a row 207+c timing.go:26 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:59` (row 207+c) — delayAfterNetworkError is :46-54 |
| 6b-w4-s3a s3a row 207+d timing.go:29 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:60` (row 207+d) — delayAfterPeerError is :56-61 |
| 6b-w4-s3a s3a row 207+e timing.go:31 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:61` (row 207+e) — delayAfterUnhandledError is :63-68 |
| 6b-w4-s3a s3a row 207+f timing.go:34 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:62` (row 207+f) — kickThrottleInterval is :70-76 |
| 6b-w4-s3a s3a row 207+g timing.go:37 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:63` (row 207+g) — kickMinRetransmissions is :78-84 |
| 6b-w4-s3a s3a row 207+h timing.go:40 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:64` (row 207+h) — kickMinRestartSaving is :86-94 |
| 6b-w4-s3a s3a row 207+i timing.go:43 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:65` (row 207+i) — kickRestartCooldown is :96-108 |
| 6b-w4-s3a s3a row 207+j timing.go:47 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:66` (row 207+j) — addressChangeStabilizationDelay is :110-116 |
| 6b-w4-s3a s3a row 207+k timing.go:50 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:67` (row 207+k) — addressChangeProbeCooldown is :118-134 |
| 6b-w4-s3a s3a row 869 groups/reception_teardown test:10-12 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:69` (row 869) — a reference statement of the read, not restated here |
| 6b-w4-s3a s3a row 869+a reception_teardown test:8-9 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:70` (row 869+a) — a reference statement of the read, not restated here |
| 6b-w4-s3a s3a row 882 im/client.go:117 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:74` (row 882) — a reference statement of the read, not restated here |
| 6b-w4-s3a s3a row 894 im/invoke.go:393-395 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:75` (row 894) — a reference statement of the read, not restated here |
| 6b-w4-s3a s3a row 914 subscription/manager.go:1225-1226 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:76` (row 914) — State.subscriptions with its deprecated doc is :384-390, not the whole class |
| 6b-w4-s3a s3a row 953 mdns/answer.go:581-582 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:79` (row 953) — The answer-splitting part of MdnsSocket.send is :155-228 |
| 6b-w4-s3a s3a row 963+a mdns/records.go:85-86 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:82` (row 963+a) — a reference statement of the read, not restated here |
| 6b-w4-s3a s3a row 972 mdns/socket.go:26-27 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:84` (row 972) — a reference statement of the read, not restated here |
| 6b-w4-s3a s3a row 1183 tlv/encode.go:83-84 (eight places) | note | taken: `fix-cite-w4fix.md:177`, `fix-cite-w4fix.md:189` | `fable-read-6b-w4-s3a.md:86` (row 1183) — a reference statement of the read, not restated here |
| 6b-w4-s3a s3a row 1149 secure/setup/verhoeff test:8-10 | note | taken: `fix-cite-w4fix.md:178` | `fable-read-6b-w4-s3a.md:116` (row 1149) — a reference statement of the read, not restated here |
| 6b-w4-s3a s3a row 1097 mattercert/decode.go:43-45 | note | taken: `fix-cite-w4fix.md:178` | `fable-read-6b-w4-s3a.md:99` (row 1097) — a reference statement of the read, not restated here |
| 6b-w4-s3a s3a row 1098 mattercert/decode.go:217-219 | note | taken: `fix-cite-w4fix.md:178` | `fable-read-6b-w4-s3a.md:100` (row 1098) — Same range as row 1097 |
| 6b-w4-s3a s3a row 1103 mattercert/decode.go:491-494 | note | taken: `fix-cite-w4fix.md:178` | `fable-read-6b-w4-s3a.md:103` (row 1103) — a reference statement of the read, not restated here |
| 6b-w4-s3a s3a row 1122 mattercert/verify.go:206 | note | taken: `fix-cite-w4fix.md:178` | `fable-read-6b-w4-s3a.md:112` (row 1122) — a reference statement of the read, not restated here |
| 6b-w4-s3a s3a row 1125+a mattercert/verify.go:386 | note | taken: `fix-cite-w4fix.md:178` | `fable-read-6b-w4-s3a.md:114` (row 1125+a) — a reference statement of the read, not restated here |
| 6b-w4-s3a s3a row 155+e subscribe.go:145-157 | note | recorded: `fix-cite-w4fix.md:207` (a phase-7 carry) | `fable-read-6b-w4-s3a.md:46` (row 155+e) — Not performable: four citations labelled 0ceb202d under a db021da0 head were not read |
| 6b-w4-s3a s3a row 1208+a transport/mrp/ack.go:162-163 | note | taken: `fix-cite-w4fix.md:177` | `fable-read-6b-w4-s3a.md:89` (row 1208; row 1208+a) — a reference statement of the read, not restated here |
| 6b-w4-s3a s3a more-than-a-citation: bare parts under re-ranged heads | low | recorded: `fix-cite-w4fix.md:177`, `fix-cite-w4fix.md:212` (a phase-7 carry) | `fable-read-6b-w4-s3a.md:134` — Bare field ranges in timing.go and descriptor.go were not moved when their head was re-ranged, so they read as pin-verified under an at db021da0 label |
| 6b-w4-s3a s3a note groups/request.go:94 hand-over wording | note | recorded: `fix-cite-w4fix.md:214` (a phase-7 carry) | `fable-read-6b-w4-s3a.md:143` (row 872) — At the pin the group write goes through ClientNodeInteraction.write, not directly to ClientInteraction.write; wording, no text given |
| 6b-w4-s3a s3a seen beside the rows (unlabelled stale citations) | note | taken: `fix-cite-w4fix.md:177`, `fix-cite-w4fix.md:186` | `fable-read-6b-w4-s3a.md:146` — Stale unlabelled citations next to the rows (bdx, params, timing, write, report, group.go, tbs_der) with measured replacements, not counted as rows |
| 6b-w4-s3b s3b node 25 bridge/is1_write_bound_parity_matterjs_test.go:21 | note | taken: `fix-cite-w4fix.md:174` | `fable-read-6b-w4-s3b.md:29` (row 25) — a reference statement of the read, not restated here |
| 6b-w4-s3b s3b node 31 bridge/receive_dispatch.go:143-144 | note | taken: `fix-cite-w4fix.md:174` | `fable-read-6b-w4-s3b.md:30` (row 31) — a reference statement of the read, not restated here |
| 6b-w4-s3b s3b node 52 neighbour DeepEqual.ts bridge/value_compare.go:27 | note | taken: `fix-cite-w4fix.md:174` | `fable-read-6b-w4-s3b.md:39` (row 52) — a reference statement of the read, not restated here |
| 6b-w4-s3b s3b node 859 neighbour MockUdpSocket.ts fabrictest/multicast.go:261-262 | note | taken: `fix-cite-w4fix.md:174` | `fable-read-6b-w4-s3b.md:48` (row 859) — a reference statement of the read, not restated here |
| 6b-w4-s3b s3b node 979 node/attestation_internal_test.go:106-107 | note | taken: `fix-cite-w4fix.md:174` | `fable-read-6b-w4-s3b.md:49` (row 979) — a reference statement of the read, not restated here |
| 6b-w4-s3b s3b node 989 node/config.go:495 | note | taken: `fix-cite-w4fix.md:174` | `fable-read-6b-w4-s3b.md:50` (row 989) — a reference statement of the read, not restated here |
| 6b-w4-s3b s3b node 1019 node/mark_changed_parity_matterjs_test.go:6 | note | taken: `fix-cite-w4fix.md:174` | `fable-read-6b-w4-s3b.md:55` (row 1019; F5) — a reference statement of the read, not restated here |
| 6b-w4-s3b s3b node 1051 neighbour SessionIntervals.ts node/sessions.go:23 | note | taken: `fix-cite-w4fix.md:174` | `fable-read-6b-w4-s3b.md:59` (row 1051) — a reference statement of the read, not restated here |
| 6b-w4-s3b s3b clu 233 cluster/core/bridged_device_basic_information_test.go:166 (+ F6) | note | taken: `fix-cite-w4fix.md:175`, `fix-cite-w4fix.md:193` | `fable-read-6b-w4-s3b.md:60` (row 233; row 222; F6) — a reference statement of the read, not restated here |
| 6b-w4-s3b s3b DAV Context controller/attestation/validate.go:26 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:109` — DAV number of the older commit is replaced by (DAV:201-212) |
| 6b-w4-s3b s3b DAV Data validate.go:41 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:110` — DAV number of the older commit is replaced by (DAV:214-222) |
| 6b-w4-s3b s3b DAV Result validate.go:54 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:111` — DAV number of the older commit is replaced by (DAV:225-228) |
| 6b-w4-s3b s3b DAV local checks marker validate.go:101 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:112` — DAV number of the older commit is replaced by DAV:266 |
| 6b-w4-s3b s3b DAV Step 4 local validate.go:103 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:113` — DAV number of the older commit is replaced by (DAV:268-274) |
| 6b-w4-s3b s3b DAV Step 6 validate.go:109 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:114` — DAV number of the older commit is replaced by (DAV:276-287) |
| 6b-w4-s3b s3b DAV Step 7 validate.go:119 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:115` — DAV number of the older commit is replaced by (DAV:289-306) |
| 6b-w4-s3b s3b DAV DCL checks marker validate.go:132 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:116` — DAV number of the older commit is replaced by DAV:308 |
| 6b-w4-s3b s3b DAV no store validate.go:137 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:117` — DAV number of the older commit is replaced by DAV:312-319 at db021da0 |
| 6b-w4-s3b s3b DAV CD checks marker validate.go:149 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:118` — DAV number of the older commit is replaced by DAV:454 |
| 6b-w4-s3b s3b DAV Step 10 validate.go:189 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:119` — DAV number of the older commit is replaced by (DAV:636-642) |
| 6b-w4-s3b s3b DAV certificate_id validate.go:190 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:120` — DAV number of the older commit is replaced by (DAV:529-530) |
| 6b-w4-s3b s3b DAV checkChain validate.go:220 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:121` — DAV number of the older commit is replaced by DAV:320-452 |
| 6b-w4-s3b s3b DAV Step 2 validate.go:225 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:122` — DAV number of the older commit is replaced by (DAV:321-329) |
| 6b-w4-s3b s3b DAV test root validate.go:231 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:123` — DAV number of the older commit is replaced by (DAV:331-339) |
| 6b-w4-s3b s3b DAV Step 2b validate.go:239 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:124` — DAV number of the older commit is replaced by (DAV:341-354) |
| 6b-w4-s3b s3b DAV Step 3 validate.go:252 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:125` — DAV number of the older commit is replaced by (DAV:356-391) |
| 6b-w4-s3b s3b DAV PAA parse validate.go:253 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:126` — DAV number of the older commit is replaced by (DAV:358) |
| 6b-w4-s3b s3b DAV PAA key in the try validate.go:258 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:127` — DAV number of the older commit is replaced by (DAV:360-367) |
| 6b-w4-s3b s3b DAV PAI key validate.go:268 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:128` — DAV number of the older commit is replaced by (DAV:369-374) |
| 6b-w4-s3b s3b DAV mayIssue validate.go:273 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:129` — DAV number of the older commit is replaced by (DAV:376-382; |
| 6b-w4-s3b s3b DAV + `kinds/CertificateSignature.ts:40-54` validate.go:273 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:130` — a reference statement of the read, not restated here |
| 6b-w4-s3b s3b DAV DAC signature validate.go:278 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:131` — DAV number of the older commit is replaced by (DAV:384-391) |
| 6b-w4-s3b s3b DAV Step 3b validate.go:284 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:132` — DAV number of the older commit is replaced by (DAV:393-419) |
| 6b-w4-s3b s3b DAV Step 4 DCL validate.go:298 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:133` — DAV number of the older commit is replaced by (DAV:421-428) |
| 6b-w4-s3b s3b DAV Step 5 validate.go:304 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:134` — DAV number of the older commit is replaced by (DAV:430-451) |
| 6b-w4-s3b s3b DAV vendor_id validate.go:353 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:135` — DAV number of the older commit is replaced by (DAV:532-538) |
| 6b-w4-s3b s3b DAV product_id_array validate.go:357 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:136` — DAV number of the older commit is replaced by (DAV:540-546) |
| 6b-w4-s3b s3b DAV both or neither validate.go:362 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:137` — DAV number of the older commit is replaced by (DAV:548-556) |
| 6b-w4-s3b s3b DAV both validate.go:368 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:138` — DAV number of the older commit is replaced by (DAV:558-585) |
| 6b-w4-s3b s3b DAV neither validate.go:380 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:139` — DAV number of the older commit is replaced by (DAV:586-613) |
| 6b-w4-s3b s3b DAV authorized_paa_list validate.go:393 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:140` — DAV number of the older commit is replaced by (DAV:616-634) |
| 6b-w4-s3b s3b DAV idHex validate.go:450 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:141` — DAV number of the older commit is replaced by DAV:196-198 |
| 6b-w4-s3b s3b DAV OnAttestationFailure controller/commissioner/commissioner.go:169 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:142` — a reference statement of the read, not restated here |
| 6b-w4-s3b s3b DAV ValidationError controller/attestation/check.go:60 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:150` — DAV number of the older commit is replaced by DAV:91-92 |
| 6b-w4-s3b s3b DAV Level check.go:65 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:151` — DAV number of the older commit is replaced by (DAV:97-98) |
| 6b-w4-s3b s3b DAV Finding check.go:76 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:152` — DAV number of the older commit is replaced by (DAV:96-105) |
| 6b-w4-s3b s3b DAV Error check.go:84 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:153` — DAV number of the older commit is replaced by (DAV:108-116) |
| 6b-w4-s3b s3b DAV Policy controller/attestation/policy.go:9 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:154` — a reference statement of the read, not restated here |
| 6b-w4-s3b s3b DAV FetchedAt controller/attestation/store.go:26 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:155` — DAV number of the older commit is replaced by (DAV:341-354) |
| 6b-w4-s3b s3b DAV AcceptsTestCertificates store.go:39 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:156` — DAV number of the older commit is replaced by (DAV:331-339) |
| 6b-w4-s3b s3b DAV Revocations store.go:54 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:157` — DAV number of the older commit is replaced by (DAV:430-451) |
| 6b-w4-s3b s3b DAV levels controller/attestation/parity_matterjs_test.go:49 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:158` — DAV number of the older commit is replaced by (DAV:98) |
| 6b-w4-s3b s3b DAV levels, test message parity_matterjs_test.go:52 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:159` — DAV number of the older commit is replaced by "levels differ from DAV:98" |
| 6b-w4-s3b s3b DAV test CD finding controller/attestation/validate_test.go:31 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:160` — DAV number of the older commit is replaced by (DAV:521-526) |
| 6b-w4-s3b s3b DAV no PAA for the list validate_test.go:85 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:161` — DAV number of the older commit is replaced by (DAV:627-633) |
| 6b-w4-s3b s3b DAV store fault validate_test.go:250 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:162` — DAV number of the older commit is replaced by (DAV:358, the revocation lookup) |
| 6b-w4-s3b s3b DAV the package doc controller/attestation/doc.go:5-7 | note | taken: `fix-cite-w4fix.md:173` | `fable-read-6b-w4-s3b.md:163` — DAV number of the older commit is replaced by (DeviceAttestationValidator.validate, :258-645) at db021da0, ADR 0020 |
| 6b-w4-s3b s3b DAV policy.go:10-11 ControllerCommissioningFlow.ts ranges | note | recorded: `fix-cite-w4fix.md:207` (a phase-7 carry) | `fable-read-6b-w4-s3b.md:154` — a reference statement of the read, not restated here |
| 6b-w4-s3b s3b F1 attestation/validate.go:30-31 doc of Context.Store | low | taken: `fix-cite-w4fix.md:173`, `fix-cite-w4fix.md:197` | `fable-read-6b-w4-s3b.md:180` (F1) — a reference statement of the read, not restated here |
| 6b-w4-s3b s3b F2 abbreviation DAV has two commits | low | taken: `fix-cite-w4fix.md:172` | `fable-read-6b-w4-s3b.md:188` (F2; ctl86 F1) — doc.go binds DAV to 0ceb202d while validate.go cites db021da0, 45 numbers follow the first; widens ctl86 F1 from 33 sites to 46 in seven files |
| 6b-w4-s3b s3b not read, said once | note | recorded: `fix-cite-w4fix.md:218` (a phase-7 carry) | `fable-read-6b-w4-s3b.md:203` — a reference statement of the read, not restated here |
| 6b-w4-final final item 1 bdx/transfer_parity_matterjs_test.go:613 | note | taken: `497efb13` | `fable-read-6b-w4-final.md:10` (item 1) — a reference statement of the read, not restated here |
| 6b-w4-final final item 2a tlv/utf8_test.go:59-60 | note | taken: `497efb13` | `fable-read-6b-w4-final.md:11` (item 2a; row 1183) — a reference statement of the read, not restated here |
| 6b-w4-final final item 2b tlv/encode.go:147 | note | taken: `497efb13` | `fable-read-6b-w4-final.md:12` (item 2b) — a reference statement of the read, not restated here |
| 6b-w4-final final item 2c tlv/tlv_test.go:720 | note | taken: `497efb13` | `fable-read-6b-w4-final.md:13` (item 2c) — a reference statement of the read, not restated here |
| 6b-w4-final final item 2d tlv/tlv_test.go:756 | note | taken: `497efb13` | `fable-read-6b-w4-final.md:14` (item 2d) — a reference statement of the read, not restated here |
| 6b-w4-final final item 2e tlv/tlv_test.go:792 | note | taken: `497efb13` | `fable-read-6b-w4-final.md:15` (item 2e) — a reference statement of the read, not restated here |
| 6b-w4-final final item 3a bridged_device_basic_information.go:519-520 | note | taken: `497efb13` | `fable-read-6b-w4-final.md:16` (item 3a; F6) — a reference statement of the read, not restated here |
| 6b-w4-final final item 3b scenes_management.go:1075 | note | taken: `497efb13` | `fable-read-6b-w4-final.md:17` (item 3b) — a reference statement of the read, not restated here |
| 6b-w4-final final item 4+ client/interaction/write.go:51 | note | taken: `497efb13` | `fable-read-6b-w4-final.md:19` (item 4+) — a reference statement of the read, not restated here |
| 6b-w4-final final not read | note | not taken up: no fix note names it — see "Not placed, not verified" | `fable-read-6b-w4-final.md:25` — a reference statement of the read, not restated here |

## What earlier phases handed to 6b

The plan placed the carried rows of earlier phases in this half's units
(`maps/carry-in-6b.md`, 269 rows; its 55 unread lists are in "Unread" below);
each unit reported its status at `dd17ab06` in §3 of its report, and the
coordinator placed the 48 the map left unplaced (the 6a rows still open went
to decision 209). The status is the unit's word for it, shortened, and was not
re-measured for this file; "where it stands" is at `361102dd`. The three rows
decision 126 releases with the next release are added at the end. A row whose
split the held-back rule needs (U3-ctl-dev-1, U3-ctl-lan-1) appears as two
rows.

| ID | unit | status at the review | where it stands |
| --- | --- | --- | --- |
| RW-i4 | chs | still true, not reachable (one run of the step per flow) | fixed with R2-P6-fsm-1811 `2cf8ccf6` |
| N-finctl-3 | chs | still true: the watcher leaves on `Closing()`, which a deferred close fires while exchanges still run | phase 7 (the register editor's judgement): no finding row and no fix note names it |
| N3-ctl-2 | chs | still true: with every id taken the least recently used session of any peer is closed | phase 7 (the register editor's judgement): no fix note names it |
| N3-finctl-2 | chs | still true: `sentToAck` set before the send | phase 7 (the register editor's judgement): no fix note names it |
| N3-fixrrctl-1 | chs | still true; what `Delete` waits for is `cpeer`'s | phase 7 (the register editor's judgement): no fix note names it |
| U-dr-ctl-1 rest | chs | partly not performable: the UDP and TCP entries read whole, no finding beyond 1801/1802; the BLE feed not traced | phase 7 (the register editor's judgement): the BLE feed |
| R2-P3-rr-ctl-02 | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| N3-rwrace-1 | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| N3-rwrace-2 | chs, ctl | not performable in `chs`; this unit's side still true | fixed `5b0b1865`, `91d8df6e`: the post-dial half in wave 2; `ble.Dial` marks the race attempt answered once the peripheral accepted the connection (unit `fxo-ctl`); a clause of `BD-Matter-Controller-CommissioningRaceBounded` (`361102dd`) |
| N3-ctl-3 | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| U3-ctl-dev-1 first half | chs | still true | fixed with R2-P6-bound-1807 `b73efc62` |
| U3-ctl-dev-1 second half | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| DR5b-c3-07 | dcl | still true, measured by a probe | fixed with R2-P6-conc-1901 `9d8805f5`, `28b0a9d7`, `21ec388c` |
| N5b-fctl-1 | dcl | still true | fixed with R2-P6-bound-1903 `315546c6` |
| decision 105 refusal rule | dcl | read done; the rule to the wizard | decided: decisions 192, 214 (R2-P6-sec-1803) |
| 5b-dcl F6 | dcl | still true (the construct unchanged) | fixed `d880e851`, `bfa1a7b6`, `0df9cca2`: each request records its own proxy address and only that request's dial skips the destination check |
| 5b-dcl R7 | dcl | still true | fixed: the destination guard trims a host's leading and trailing dots; `docs/security.md` updated (commit `27f202e8` of the fix note, not found by the row's id on the branch) (the register editor's judgement) |
| 5b-dcl R8 | dcl | still true | fixed `31d4fb9c`, `137e9225`: no proxy is handed a host outside ASCII (decision 218) |
| 5b-dcl R9 | dcl | still true | fixed `9d8805f5`, `bfa1a7b6`: `forgetOrigin` returns its error; `Store` and the streamed download write nothing when the record cannot be removed |
| TM R6.4 | dcl | traced; no defect against the references | not a finding: no defect against the references (verify A); nothing to fix |
| N-secdrv-4 = R2-P1-drv-03 rest | radio | still true | fixed with R2-P6-err-2012 `bdd91cf4` |
| R2-P1-drv-04 rest | radio | still true as code; the trigger not performable (whether bluetoothd forwards an empty C1) | phase 7 (the register editor's judgement): not performable without the trigger |
| N-core2-3 | radio | still true | fixed with R2-P6-bound-2020 `21376b24` |
| N3-net-3 | radio | still true; on a real supplicant not performable | fixed with R2-P6-store-2021 `21376b24`, `e294316c`, `185cb545` |
| TM R5 5a.5 | radio | partly performable: the hand-over of a reassembled BLE message has no size check | fixed `4824803b`, `461e3499`, `292abf31`, `aa66b1a8`: an inbound BTP message above 2048 bytes ends the channel on the controller and the session on the device; `BD-Matter-BtpInboundMessageBounded` (`361102dd`) |
| TM R5 5b.4 | radio | closed by the read for the decoder: every length checked against the frame | not a finding: closed by the unit's read (and its completion pass) (the register editor's judgement) |
| TM R6.9 | radio | closed by the read with R2-P6-bound-2023, -sec-2028, -bound-2026, -2027 | fixed with those rows `26a960b5`, `b38fe78d` (the register editor's judgement) |
| N3-disc-5 | radio | still true in both halves → R2-P6-bound-2003, -2029 | fixed with R2-P6-bound-2003 and -2029 `b38fe78d`, `4f2973a0`, `d84891fa` |
| U3-ble-1 | radio | per item: 2002, 2004, 2005, 2014, 2001; the credentials item rests on step 4 | fixed with R2-P6-sec-2001, -bound-2002, -2004, -2005, -2014 `dd8357f1`; the credentials item with R2-P6-sec-1803 (decision 214) |
| N6b-ci-1 | radio | still true: the test is wrong (connectedhomeip leaves the failed network) | fixed with R2-P6-test-2018 `c700c60f`, `33e8eba2` |
| N3-disc-1 | cpeer | struck: staged addresses count against the bound with the cached records | struck (the unit's read) |
| N3-disc-2 | cpeer | still true | fixed `106a1359`: the staged-name order is a map with a linked list, every operation O(1) |
| TM R3.4 (Binding) | cpeer | still true in part: the kept peers → R2-P6-bound-2104 | fixed with R2-P6-bound-2104 `cf30ba2c`, `ef064f8a`, `3b2d7c1d`, `a732ad22`: the binding manager releases the peers it created |
| U3-ctl-lan-1 (1), (2) | cpeer | still true for the two parts in this cut | fixed `a8878106`, `10f1149b`: a resolution resolves at most four SRV target hosts at once; past the record bound the cache evicts the records of names no resolution observes first (decision 240); `BD-Matter-Controller-HostDiscoveriesBounded` and the eviction rule of `BD-Matter-Controller-DnssdCacheBounded` (`361102dd`) |
| U3-ctl-lan-1 (3)–(5) | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| N6a-im-1 | cpeer | needs a reference read | phase 7 (the register editor's judgement): the reference question was not put to a read; no note after the review names it |
| N6a-im-2 | cpeer | needs a reference read of the `im/decode_later.go` comment | phase 7 (the register editor's judgement): the reference question was not put to a read; no note after the review names it |
| N6a-im-3 | cpeer | needs a reference read | phase 7 (the register editor's judgement): the reference question was not put to a read; no note after the review names it |
| N-rw1-1 | cam | still true → R2-P6-conc-2201 (probe) | fixed with R2-P6-conc-2201 `fec1af8d` |
| cameratest Port.session | cam | — (the hand-over's carry list, no ID) | fixed with R2-P6-test-2205 `51d079ca`: the test port answers a re-offer with the session's streams |
| RW-i5 | ctl | fixed: the comment names the onboarding code and both ICD keys | closed before this phase |
| N-rw3ctl-1 | ctl | still true and traced: a deadlock with a host groupcast observer → R2-P6-conc-2301 | fixed with R2-P6-conc-2301 `11f418e5` |
| N3-finimg-1 | ctl | still true | fixed `6c3fcf65`, `2062406c`: `image_cut_at_load` only when something was dropped |
| N3-finimg-2 | ctl | not performable as asked: the margin needs a run that prints the stored bytes | not performable: a register note (the fix note) |
| N3-finimg-3 | ctl | still true | fixed `8100b3f7`: the image total is a running total, checked against a recount |
| TM R4 Q4 | ctl | answered → R2-P6-sec-2311 | fixed with R2-P6-sec-2311 `525dadcb`; the rate limit the finding named as optional was not added (the register editor's judgement) |
| N6a-seal-1 | ctl | still true in three parts → R2-P6-store-2302, -2303, -2304 | fixed with those rows `deaca6d4`, `01631318`, `bfd40f85`, `e977f3cc` (decisions 194, 195) |
| DR5b-F-05 | facade | still true | fixed `fe2a84d5` (doc) |
| N-fabric-2 | facade | the Thermostat half fixed before this phase; the stored-subscriptions half still true | open: findings row, not performable — missing input the bridge's load and re-establish path of the stored subscriptions `b8514237` |
| N-persist-1 | facade | struck: none of the three attributes is served | struck (the unit's read) |
| N-finp-3 | facade | still true, measured → R2-P6-conc-2405 | fixed with R2-P6-conc-2405 `fd208a9b` |
| N4-coresys-1 | facade | still true, as documented | decided: an *Ahead of matter.js* row (decision 230) `2bc73568` |
| D-comm-4 (d) | facade | still true → R2-P6-fsm-2411 | fixed with R2-P6-fsm-2411 `9a5a4da1`, `3316e132`, `2bced2d3` |
| U4-fr-sec-2 | facade | still true → R2-P6-sec-2401 | fixed in part with R2-P6-sec-2401 `af3b6c48`: the lock half; the limit half kept as matter.js (decision 228), a findings-register row (`361102dd`) |
| U-ctr-1 rest | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| R2-P5-apidev-16 rest | device | still true: the lifecycle methods return nothing | fixed `a7391365`, `b58d7768`: `Rename`, `Update`, `SetReachable` and `SetNodeLabel` return `error` (decisions 200, 226) |
| N5b-afacade-1 | device | still true; 21 types refused bare, 36 rows | decided: confirmed as built, a test pins the count (decision 203) `9028e42b` |
| N6a-ex-1 | examples | still true → R2-P6-par-2601 | fixed with R2-P6-par-2601 `a800c5c7` |
| N6a-ex-2 | examples, docs | the doc half fixed in 6a; the daemon half is N6a-ex-1 | fixed with R2-P6-par-2601 `a800c5c7` |
| S-11 | gen | still true for the script, held on the documented path | fixed `3e115b21` |
| S-12 | gen | still true, documented not held | fixed `3ca20082`, `76269095`: `make generate-matter-schema` builds packages/model first |
| chipdm class (ii) | guards | still open → R2-P6-doc-2817 | fixed with R2-P6-doc-2817 `048e6e05`, `64988804`, `f3e33959`; one findings-register entry per class (ii) entry `b8514237` |
| R2-P0-rv-C11 | guards | still true | phase 7 (the register editor's judgement): needs a rule for how a register sentence names its commit; no fix note names it |
| F-db021da0-15 | par-node | still true, a gap; one unverified sentence answered | fixed `0fde651b`, `aa66b1a8`: reported values are read through the generated definitions (unit `fxo-ctl`) |
| F-db021da0-47 rest | par-node | still true, a gap, host-visible only | fixed `ee550f6d` |
| F-db021da0-49 | par-node | still true, now measured → R2-P6-par-2904 | fixed with R2-P6-par-2904 `6842ca1f`, `3d26f26f`; the findings row moved and closed `b8514237` |
| F-db021da0-53 | par-node | still true; the two references differ | open: findings row `F-db021da0-53`, its sentence amended (R2-P6-doc-2907); nothing to change (the fxo-net unit) |
| F-db021da0-54 | par-node | still true, a gap of matter.js's, mirrored | open: findings row `F-db021da0-54`, unchanged (the register unit: a reference read found nothing) |
| N5b-notes-2 (5b-F Q5) | par-node | answered → R2-P6-par-2903 | parity: a recorded divergence with R2-P6-par-2903 (decision 206) |
| BD-Matter-Controller-UnsecuredSessionOneHandshake chip sentence | par-node | — | phase 7 (the register editor's judgement): the unit's report gives no verdict line for it and no fix note names it |
| N6a-sub-3 | par-node | a register candidate, not re-read | phase 7 (the register editor's judgement): no fix note names it |
| N6a-reg-1 | par-node | not this unit's to re-measure; all still stand in the open register (grep) | taken with the 6a open rows (decision 209): see F-db021da0-20, -42, N5b-notes-1, N-flake-1, -57 below (the register editor's judgement) |
| N6a-reg-2 | par-node | judgement from the 6a reads: no row for the certificate without basicConstraints | not a finding: the module follows matter.js (the unit's judgement); no fix note names it |
| F-db021da0-58 = N5b-guard-1 | tests | cause found → R2-P6-test-3101 | fixed with R2-P6-test-3101 `da4b8591`, `e779c6c9` |
| N5b-guard-2 | tests | not reproduced in 150 runs | not reproduced (150 runs; 6a 0 of 400) |
| N5b-guard-3 | tests | fixed in 6a | closed before this phase |
| N5b-guard-4 | tests | not reproduced in 50 runs; the cause not named (the failing message was not kept) | not reproduced (50 runs) |
| N-rw2-2 | tests | not performable: the failing output was cut to one line | not performable: missing input the failing run's output |
| N-rw2-3 | tests | still true → R2-P6-test-3110 | decided with R2-P6-test-3110: recorded, no test seam (the coordinator) |
| N3-coord-2 | tests | cause named, fixed before this phase | closed before this phase |
| N4-d4gate-2 | tests | cause named → R2-P6-test-3103 | fixed with R2-P6-test-3103 `72af5e9a` |
| N5-race-1 | tests | struck, inferred by reading | struck (the unit's read, not stressed) |
| N5-tflake-2 | tests | still true → R2-P6-test-3104 | fixed with R2-P6-test-3104 `96b112fa`, `e779c6c9` |
| N5-tflake-3 | tests | still true → R2-P6-test-3105 | fixed with R2-P6-test-3105 `265d6dcf` |
| N5-tflake-4 | tests | fixed in 6a | closed before this phase |
| N5-tflake-5 | tests | not reproduced in 90 runs | not reproduced (90 runs; 6a 0 of 200) |
| R2-P5-tassert-05 rest | tests | still true → R2-P6-test-3107 | fixed with R2-P6-test-3107 `8c24def6` |
| 5a-1 F7 | tests | struck: held since `8259c88d`; the comment → R2-P6-test-3114 | fixed with R2-P6-test-3114 `44eb35c7` (the comment) |
| W6-test-15 rest | tests | still true → R2-P6-test-3109 | fixed with R2-P6-test-3109 `a8576767`, `e779c6c9` |
| W6-test-17 rest | tests | still true → R2-P6-test-3111, -err-3112 | fixed with R2-P6-err-3112 `409f6f04`, `2e08700a` (decision 234) |
| N6a-test-3 | tests | still true → R2-P6-test-3113 | fixed in part with R2-P6-test-3113 `8c5b09e3`: the peers case; the rest recorded without a red test |
| N6a-flake-1 | tests | not reproduced in 90 runs | not reproduced (90 runs; 6a 0 of 200) |
| N6a-flake-2 | tests | not reproduced in 150 runs | not reproduced (150 runs; 6a 0 of 200) |
| N6a-flake-3 | tests | not reproduced in 100 runs → R2-P6-test-3106 | fixed with R2-P6-test-3106 `4d555317` |
| N6a-flake-4 | tests | not reproduced in 100 runs → R2-P6-test-3104 | fixed with R2-P6-test-3104 `96b112fa`, `e779c6c9` |
| 5a-1 F8 | docs | still present; needs a reference read | phase 7 (the register editor's judgement): no fix note names it |
| S-24 = N5-fxdocs-1 | docs | — (no verdict line in the unit's report) | phase 7 (the register editor's judgement): no fix note names it |
| N5-docs-5 | docs | still true and wider → R2-P6-doc-3001 | fixed with R2-P6-doc-3001 `4d9125de`, `59c95a14`, `5ec77d44` |
| N5-docs-6 | docs | still true → R2-P6-doc-3013 | fixed with R2-P6-doc-3013 `b3cb03cf` |
| N5-docs-9 | docs | still true | fixed with R2-P6-doc-3001 `4d9125de`, `59c95a14`, `5ec77d44` (the register editor's judgement) |
| D-1 | docs | still true → R2-P6-doc-3001 | fixed with R2-P6-doc-3001 `4d9125de`, `59c95a14`, `5ec77d44` |
| N6a-seal-2 | docs | still true → R2-P6-doc-3007 | fixed with R2-P6-doc-3007 `dd21bdb2` |
| N6a-net-2 | docs | still true → R2-P6-doc-3015 | fixed with R2-P6-doc-3015 `dd21bdb2` |
| N6a-spec-1 | docs | still true → R2-P6-doc-3006 | fixed with R2-P6-doc-3006 `dd21bdb2` |
| R2-P6-doc-0340 | cite | still true | fixed `502685ed` |
| R2-P6-doc-0343 | cite | still true | fixed `905f5695`, `3a273328` |
| R2-P6-doc-0746 | cite | not performable in the unit | fixed `0af5ca61`, `26b71394`: the citation; its second half is a phase-7 carry of the wave-4 notes |
| R2-P6-doc-0755 | cite | not performable in the unit | fixed `22d8267b` |
| R2-P6-doc-0773 | cite | still true | fixed `502685ed` |
| R2-P6-doc-0446 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0704 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0740 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0741 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0742 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0743 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0744 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0745 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0747 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0748 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0749 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0750 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0751 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0752 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0753 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0754 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0756 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0757 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0758 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0760 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0761 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0762 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0763 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0766 | cite | fixed in `conn.go` in 6a; one site left | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0767 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0771 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0774 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0775 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0777 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-0778 | cite | still true (a class) | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-1270 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-1467 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-1705 | cite | still true (a class) | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| R2-P6-doc-1706 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| N6a-reg-4 | cite | still true (measured) | fixed `f0aedcfc`, `c005fbe0`: `BD-chip-Thermostat-Schedules` read at the harness pin |
| handover side notes (the 6a register sample) | cite | open | phase 7 (the register editor's judgement): register prose handed to `docs`; no fix note names it |
| R2-P0-par-D01 | cite | still open — the unit is the re-read | re-cited in the citation pass of wave 4 (decision 208); the guard is R2-P6-test-3201 |
| W6-reg-12 | cite | still open — the unit is the re-read | re-cited in the citation pass of wave 4 (decision 208); the guard is R2-P6-test-3201 |
| R2-P0-par-D02 rest | cite | still open — the unit is the re-read | re-cited in the citation pass of wave 4 (decision 208); the guard is R2-P6-test-3201 |
| D-sess-5 | cite | open, covered by R2-P6-test-3202 | fixed with R2-P6-test-3202 `c5f6818a` |
| D-ctr-5 | cite | open, covered by R2-P6-test-3202 | fixed with R2-P6-test-3202 `c5f6818a` |
| N-cite-2 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| N-cite-3 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| N3-cite-1 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| N3-cite-3 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| N3-cite-5 | cite | handed to `docs` | fixed with R2-P6-doc-3001 `4d9125de`, `59c95a14`, `5ec77d44` (the register editor's judgement) |
| N-core2-2 | cite | re-read by 6a | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| N-reg-3 | cite | still true | fixed with R2-P6-doc-0343 `905f5695`, `3a273328` |
| N-sess-3 | cite | partly fixed | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| N3-rwpase-3 | cite | open | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| S-5 | cite | not performable: the row carries no file:line | not performable: missing input the statements' file:line |
| S-7 | cite | still true | re-cited in the citation pass of wave 4: every site of the row is a row of its patch plan (`phase-6-cite.md` §3); the fix notes do not name the row — not checked row by row (the register editor's judgement) |
| 5a-1 F9 | cite | still true | fixed with R2-P6-par-3354 `abc85505` |
| R2-P5-sup-07 rest | fx-sup | — | fixed `a8cad215` |
| N5-ci-9 | fx-sup | — | fixed `02113455` |
| N5-rwci-2 | fx-sup | — | fixed `ea7fe6f7` |
| N5b-guard-5 = N5-ci-6 | fx-lint | — | fixed with N6a-lint-1 `fcc850b1` |
| N6a-lint-1 | fx-lint | — | fixed `fcc850b1`: `examples/ble-wifi-light` linted with `GOOS=linux` |
| N6b-ci-2 | guards | holds, one note | the pin lift after phase 7 (decision 187) |
| F-28 + apidev-02 (fx-bdbi) | — | — | closed before this phase: done in 6a (the coordinator, plan-6b §6) |
| N-ctr-5 | — | — | decided: no longer needed (the coordinator, decisions 140, 172) |
| N3-coord-1 | hs (6a) | still true | fixed `7a991263`, `c12f72af`: an unsecured message is ordered by its initiator's node id (decision 199) |
| N-net-1 (b) | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| N-rw3req-3 | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| N-bridge2-3 | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| TM R3b.2 | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| TM R3b.4 | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| TM R3.7 (slots) | hs (6a) | read, not run | fixed `47c804df`, `53c3dbc6`, `b769fd59`: `Bridge.SetFabricCounter`; a fabric holding more than its share of the chunk waits is answered Busy from half the dispatch budget (decision 245); `BD-Matter-ChunkWaitsSharedByFabric` (`361102dd`) |
| U-alloc-1 (device parts) | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| U3-lan-dev-1 | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| N4-f3-2 | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| U3-admin-1 | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| TM R3.2 | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| TM R3.6 | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| TM R3.3 | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| TM R3.5 first half | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| U3-net-1 | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| R2-P6-par-1360 | spec (6a) | open findings row (6a) | open: findings row `R2-P6-par-1360`, no code; a verdict line (unit `fxo-node`) `fd633cc8` (the register editor's judgement) |
| R2-P6-fsm-1401 | applight (6a) | open findings row (6a) | fixed `147a8f46` (decision 209) |
| R2-P6-fsm-1402 | applight (6a) | open findings row (6a) | fixed `f65e9899`, `62fe5078` (decision 209) |
| R2-P6-fsm-1412 | applight (6a) | open findings row (6a) | fixed in part `49a5ea2e`: the Time-0 half (decision 209); the rest stays the findings row |
| R2-P6-err-1415 | applight (6a) | open findings row (6a) | fixed `f65e9899`, `62fe5078` (decision 209) |
| R2-P6-sec-1513 | appclim (6a) | open findings row (6a) | open: findings row `R2-P6-sec-1513`, no code; a verdict line (unit `fxo-node`) |
| R2-P6-fsm-1593 | appclim (6a) | open findings row (6a) | fixed `586b7bcb`, `8b0026e6`, `4ff64d2a`: `SetSupportedMaps` drops the areas of a removed map, as connectedhomeip |
| TM R2.1 rest | — | open (6a) | fixed `1b0b693a`: a test for a host on `bridge/` without `node` |
| R2-P4-nw-07b rest = F-db021da0-35 | — | open findings row (6a) | phase 7 (the register editor's judgement): no fix note names it |
| F-db021da0-13 | — | open findings row (6a) | fixed `9a369b6c`, `53c3dbc6`: the chunk status wait uses the peer's own retransmission schedule (decision 241) |
| F-db021da0-50 | — | open findings row (6a), the direction of ADR 0032 | open: findings row, a register line (unit `fxo-net`); ADR 0032 |
| F-db021da0-51 | — | open findings row (6a), the direction of ADR 0032 | fixed in part `4eb4517f`, `c3412963`: a chunked write on a client-stack session is refused whole; the rest stays with ADR 0032 (decision 242) |
| F-db021da0-55 | — | open findings row (6a) | fixed `e0b17ea3`, `217ee3da`: a MEI helper exported from `cluster/spec`, used by the dispatcher (decision 246) |
| F-db021da0-59 | — | open findings row (6a) | fixed `31fa3cc9` |
| R2-P5-tmut-18 rest | — | open (6a) | fixed in part `34b0a737`, `1e9eac1b`, `115fff1f`, `f3fd05fa`, `6904065a`, `649a2637`: 73 of 101 mutants killed, 28 recorded |
| TM R3.8 rest | — | — | held back until its fix merges (owner decisions 25, 126) — phase 7 |
| N4-d4wifi-1 | — | open findings row (6a) | fixed `f1f92c87`; the findings row closed |
| "cover/fan change naming" | — | open, as F-db021da0-57 (6a) | fixed with F-db021da0-57 `e30c2b06`, `6f02028e` |
| F-db021da0-9 | — | open findings row (6a) | fixed `34c52911`, `62fe5078` |
| F-db021da0-11 | — | open findings row (6a) | open: findings row, a register line (unit `fxo-net`) |
| F-db021da0-20 | — | open findings row (6a), the exercise missing | fixed `ff07c483`, `250fee3e`, `b8f5c064`: a test fix (the coordinator) |
| F-db021da0-42 | — | open findings row (6a) | fixed `438f9d3c`: the group-key mapping's check and store run under the write lock |
| F-db021da0-43 | — | open findings row (6a) | fixed `8a04f5e1`: a `mattercore.BridgedConfig` field (decision 241) |
| F-db021da0-44 | — | open findings row (6a) | not reproduced: a test built from the recipe passes (300 runs, 100 under `-race`) `3cbc23be` |
| F-db021da0-48 | — | open findings row (6a) | fixed `57d8d4ce`, `4931115b`: the message array readers refuse a tagged element and the filter readers a non-structure one |
| F-db021da0-57 | — | open findings row (6a) | fixed `e30c2b06`, `6f02028e`: `MatterWriteCommit` on the cover and fan servers (decision 241) |
| N4-f2-3 | — | open, as F-db021da0-42, -43, -44 (6a) | fixed with F-db021da0-42 and -43 `438f9d3c`; -44 not reproduced (the register editor's judgement) |
| N5b-notes-1 | — | open (6a): the `by_design` entry not written | phase 7 (the register editor's judgement): no fix note names it |
| N-flake-1 | — | open (6a): its findings row not written | fixed `0c509819`, `a25cb9f7` |
| R2-P5-redact-01 | — | — | held back until its fix merges (owner decisions 25, 126) — the release after 6b (decision 126) |
| N4-frsec-1 | — | — | held back until its fix merges (owner decisions 25, 126) — the release after 6b (decision 126) |
| DR5b-F-01 | — | — | held back until its fix merges (owner decisions 25, 126) — the release after 6b (decision 126) |

## The questions

The review's questions (56 in 21 reports, and those the verify raised) were
merged by the decision brief into step 0 and 21 owner steps (decisions 188 to
209) and 44 coordinator rows; a step names the questions it covers. The fix
rounds raised the questions after them; the owner answered those as decisions
210 to 257, the coordinator the rest, as the decisions file records them. The
coordinator rows' "decided" cell is the brief's, with the bin labels of its
sources left out.

| source | question | answer | by | where |
| --- | --- | --- | --- | --- |
| wizard-6b step 0 | the fix scope | owner decision 188 | owner | see the decisions table |
| wizard-6b step 1 | Peer-opened exchanges on an unsecured session this node opened (R2-P6-sec-1801 medium, -bound-1802 medium) | owner decision 189 | owner | see the decisions table |
| wizard-6b step 2 | JCM without `TrustAnchors` (R2-P6-sec-1850 medium) | owner decision 190 | owner | see the decisions table |
| wizard-6b step 3 | A stored CA or fabric record a constructor would replace (R2-P6-store-1851 medium, -1852, -1853, R2-P6-api-1859) | owner decision 191 | owner | see the decisions table |
| wizard-6b step 4 | What "attested" means (R2-P6-sec-1803; decision 105's open refusal rule; dcl Q1) | owner decision 192 | owner | see the decisions table |
| wizard-6b step 5 | chip's stricter attestation checks (R2-P6-par-1804, -1805) | owner decision 193 | owner | see the decisions table |
| wizard-6b step 6 | The seal mark of the controller's node registry (R2-P6-store-2304; `fx-csql`, N6a-seal-1) | owner decision 194 | owner | see the decisions table |
| wizard-6b step 7 | `Reseal` for a controller database (R2-P6-store-2302) | owner decision 195 | owner | see the decisions table |
| wizard-6b step 8 | The radio kits' defaults towards the LAN (R2-P6-sec-2001 medium, R2-P6-sec-2028) | owner decision 196 | owner | see the decisions table |
| wizard-6b step 9 | `OperationalDataset.Redact` (R2-P6-sec-2022 medium) | owner decision 197 | owner | see the decisions table |
| wizard-6b step 10 | R2-P6-sec-3320 | owner decision 198 | owner | see the decisions table |
| wizard-6b step 11 | N3-coord-1 | owner decision 199 | owner | see the decisions table |
| wizard-6b step 12 | The device lifecycle methods' result (R2-P5-apidev-16, R2-P6-api-2503) | owner decision 200 | owner | see the decisions table |
| wizard-6b step 13 | A config string beyond its constraint (R2-P6-api-2407 medium, R2-P6-wire-2502 medium) | owner decision 201 | owner | see the decisions table |
| wizard-6b step 14 | Additive API the fixes need (one step, each additive) | owner decision 202 | owner | see the decisions table |
| wizard-6b step 15 | Decision 17's refusal set as built (N5b-afacade-1) | owner decision 203 | owner | see the decisions table |
| wizard-6b step 16 | A part that already has an owner (R2-P6-fsm-2501 medium) | owner decision 204 | owner | see the decisions table |
| wizard-6b step 17 | A Sigma1 refused for the one-handshake bound (R2-P6-par-2902) | owner decision 205 | owner | see the decisions table |
| wizard-6b step 18 | The status after a refused store write (R2-P6-par-2903) | owner decision 206 | owner | see the decisions table |
| wizard-6b step 19 | Rows that are the reference's own behaviour (23 rows, B: "mirrors", 2307 in substance) | owner decision 207 | owner | see the decisions table |
| wizard-6b step 20 | The 575 `same` citations nobody read (decision 13; cite Q2) | owner decision 208 | owner | see the decisions table |
| wizard-6b step 21 | The 6a rows still open at HEAD (Oa §2: 45 rows; decisions 126/127 against 122) | owner decision 209 | owner | see the decisions table |
| chs Q5 (1807, U3-ctl-dev-1) | — | a message count per exchange, past it a flow error | coordinator | the rows named |
| chs-2 Q3 (1854) | — | 32 bytes at creation, load and `SetLabel` | coordinator | the rows named |
| chs Q4 half (1806) | — | → step 19 | coordinator | the rows named |
| dcl Q2 (1915) | — | no change; a register note | coordinator | the rows named |
| dcl Q3 (1902) | — | the production ledger's bytes replace the stored ones on promotion; a test source never overwrites a production entry | coordinator | the rows named |
| radio Q4 (2012 medium) | — | keep `registered` and the exported objects when the unregister fails; retry | coordinator | the rows named |
| R2-P6-test-2018 (N6b-ci-1) | — | the test's expectation and its chip citation move to the driver's behaviour | coordinator | the rows named |
| cpeer Q1 (2101 medium) | — | cached A/AAAA per name bounded at the staging's number (`names.go:50`) | coordinator | the rows named |
| cpeer Q2 (2102 medium, 2150 dup) | — | charge decoded elements, report and invoke path | coordinator | the rows named |
| cpeer Q3 (2104) | — | → step 19 | coordinator | the rows named |
| cam Q1 (2202 medium) | — | `Close` ends every requestor entry of its fabric the registry does not track | coordinator | the rows named |
| cam Q2 (2206) | — | an ended session discards its unread signals, the pump ends | coordinator | the rows named |
| cam Q3 (2207) | — | every camera-acting method answers `ErrClosed` after `Close`, `Release` excepted | coordinator | the rows named |
| cam-2 (2250 medium, 2251) | — | defects, fixed | coordinator | the rows named |
| ctl Q (2303 medium) | — | the registry opens with the scrub owed, as `store.NewSealed` | coordinator | the rows named |
| ctl-2 Q1 (2351 medium) | — | `admit` charges what a reported ServerList / PartsList would create | coordinator | the rows named |
| R2-P6-fsm-2350 medium | — | Stop emits false for a reported-active subscription | coordinator | the rows named |
| facade Q1 (2411 medium) | — | `Stop` expires an armed fail-safe and rolls back; the host hears `OnFabricRemoved` before `Stop` returns | coordinator | the rows named |
| facade Q2 (2405 medium) | — | a Forget batch reassembles and detaches first, then erases | coordinator | the rows named |
| facade (2401 medium, U4-fr-sec-2) | — | both sites take the ACL write lock | coordinator | the rows named |
| R2-P6-sec-2412 | — | abort a pending handshake on the updated fabric | coordinator | the rows named |
| par-node Q3 (2905) | — | send SUPPORTED_TRANSPORTS = 0 without a TCP role | coordinator | the rows named |
| R2-P6-wire-2204, -err-2318, -par-3208 | — | fixed as the reference | coordinator | the rows named |
| device Q3 → step 16; Q2 → step 13 | — | — | coordinator | the rows named |
| R2-P6-conc-2504, -2413 | — | fixed (the two lows Ob held at the edge of medium) | coordinator | the rows named |
| examples Q1 (2601 medium) | — | Accuracy entries for the three types the daemon sets, count 3 | coordinator | the rows named |
| examples Q2 (2604) | — | `--seal-key file:<path>` on the daemon and the reference controller + README | coordinator | the rows named |
| examples Q3 (2603) | — | `EpochKey` leaves `controller.group.<id>`; the key is read from the key set | coordinator | the rows named |
| examples-2 Q1 (2650) | — | not handled (`false`) | coordinator | the rows named |
| gen Q1 (2703–2708, 2714, 2715) | — | every generator refuses a form it does not render | coordinator | the rows named |
| gen Q2 (2713) | — | the merge stays, one `by_design` line; the level extractor fails on an id that differs by direction | coordinator | the rows named |
| guards Q1 (2801 medium) | — | `--match 'v[0-9]*'`; fail when the base's heading is absent | coordinator | the rows named |
| guards Q2 (2804–2806) | — | initializers seeded, methods in the population, an exact ratchet on annotations; the newly unreached are counted and ratcheted, their fixes phase 7 | coordinator | the rows named |
| guards Q3 (2803, 2808) | — | archcheck and apidiff per nested module | coordinator | the rows named |
| guards-2 (2850) | — | compare CHIP's composition element | coordinator | the rows named |
| docs Q1 (3001) | — | symbol form + a `docscheck` rule | coordinator | the rows named |
| docs Q2 (3013) | — | the guide says which version it was written against | coordinator | the rows named |
| docs-2b Q1 (3076–3086) | — | a dated status note + a `docscheck` rule for `Supersedes` | coordinator | the rows named |
| tests Q1 (W6-test-17, 3112) | — | one status per class in the server; the tests assert it | coordinator | the rows named |
| tests Q2 (3101) | — | both paths as ordered subtests; no assertion that the old session is closed by the new one | coordinator | the rows named |
| tests Q3 (1586, N6a-test-3) | — | no red test, recorded; the peers case of 3113 only | coordinator | the rows named |
| cite Q1 (3201, 3202) | — | the ratchet counts line citations at another matter.js commit; file-only, bare and chip counters printed, not ratcheted; the collector learns the two missed forms | coordinator | the rows named |
| N-ctr-5 | — | no longer needed | coordinator | the rows named |
| radio-2, cpeer-2, cam-2, gen-2, docs-2a | — | no question | coordinator | the rows named |
| fix rounds | R2-P6-wire-2153, an invoke response's path not matched | owner decision 210 | owner | R2-P6-wire-2153 |
| fix rounds | R2-P6-fsm-2109, the granted MaxInterval unchecked | owner decision 211 | owner | R2-P6-fsm-2109 |
| fix rounds | R2-P6-par-2156, the status budget of a wildcard-endpoint write chunk | owner decision 212 | owner | R2-P6-par-2156 |
| fix rounds | R2-P6-err-2106, the doc of `ErrNotConnectable`'s class | owner decision 213 | owner | R2-P6-err-2106 |
| fix rounds | R2-P6-sec-1803, the test-trust facts of decision 192 | owner decision 214 | owner | R2-P6-sec-1803 |
| fix rounds | R2-P6-fsm-1860, joint-commissioning consent without a context | owner decision 215 | owner | R2-P6-fsm-1860 |
| fix rounds | R2-P6-doc-1857 (b), the signature length at decode | owner decision 216 | owner | R2-P6-doc-1857 |
| fix rounds | R2-P6-err-1912, `QueryUpdates` and `CheckForUpdates` always nil | owner decision 217 | owner | R2-P6-err-1912 |
| fix rounds | 5b-dcl R8, a host outside ASCII handed to a proxy | owner decision 218 | owner | 5b-dcl R8 (the carried rows) |
| fix rounds | R2-P6-bound-1914, the OTA manager forgets a removed node late | owner decision 219 | owner | R2-P6-bound-1914 |
| fix rounds | R2-P6-bound-2102 follow-up: `DefaultMaxReportBytesPerSession` now counts memory | owner decision 220 | owner | R2-P6-bound-2102 |
| fix rounds | R2-P6-bound-2104, how far the binding manager releases a peer it created | owner decision 221 | owner | R2-P6-bound-2104 |
| fix rounds | R2-P6-bound-2362, two nodes claim one extended address or RLOC16 in the topology | owner decision 222 | owner | R2-P6-bound-2362 |
| fix rounds | R2-P6-bound-2357, the set of warned unknown events | owner decision 223 | owner | R2-P6-bound-2357 |
| fix rounds | R2-P6-api-2751, `cluster/lock` declares `StateSource` a second time by hand | owner decision 224 | owner | R2-P6-api-2751 |
| fix rounds | R2-P6-fsm-2501 again, after a reference read: matter.js moves an endpoint whose construction has started onto an owner whose parts are ready only by throwing, which leaves it without an owner (`Parts.ts:64-70`, `Construction.ts:284-287`, `Endpoint.ts:706-725`) | owner decision 225 | owner | R2-P6-fsm-2501 |
| fix rounds | R2-P5-apidev-16 rest: the second label write in `Endpoint.SetNodeLabel` is dropped and unlocked | owner decision 226 | owner | R2-P5-apidev-16 rest (the carried rows) |
| fix rounds | R2-P6-par-2713 (replaces the gen unit's second question — its basis "no id differs by direction" is false: Thermostat command 2 is a response at the pin and a request at v0.13.0, measured by a real level-generation run) | owner decision 227 | owner | R2-P6-par-2713 |
| fix rounds | R2-P6-sec-2401, the limit half: the node's own Operate entry at a full per-fabric ACL | owner decision 228 | owner | R2-P6-sec-2401 |
| fix rounds | R2-P6-api-2409, the bound of the mDNS host-name override | owner decision 229 | owner | R2-P6-api-2409 |
| fix rounds | N4-coresys-1, BootReason SoftwareUpdateCompleted on a start into the recorded version | owner decision 230 | owner | N4-coresys-1 (the carried rows) |
| fix rounds | R2-P6-sec-3320, the rest: the record was single-use only from the upsert (1 to 6 of 16 concurrent copies resumed, measured) | owner decision 231 | owner | R2-P6-sec-3320 |
| fix rounds | R2-P6-sec-3320: an initiator's failure StatusReport on Sigma2_Resume | owner decision 232 | owner | R2-P6-sec-3320 |
| fix rounds | R2-P6-sec-2412, the initiator role | owner decision 233 | owner | R2-P6-sec-2412 |
| fix rounds | R2-P6-err-3112 / W6-test-17, a value of the wrong Go type on an attribute write | owner decision 234 | owner | R2-P6-err-3112 |
| fix rounds | Decision 190 on the device side: `device.NewJointFabricAdministrator` cannot take a trust-anchor lookup | owner decision 235 | owner | unit `fxo-node` |
| fix rounds | R2-P6-sec-2361, `controller.PingAddress` pings any address | owner decision 236 | owner | R2-P6-sec-2361 |
| fix rounds | R2-P6-bound-2362 again (an RLOC16 is unique only within one Thread network, matterjs-server `topology-utils.ts:422-451`) | owner decision 237 | owner | R2-P6-bound-2362 |
| fix rounds | R2-P6-sec-2616 (a decision-207 row, doc only) | owner decision 238 | owner | R2-P6-sec-2616 |
| fix rounds | R2-P6-par-3102 (a decision-207 row, a label) | owner decision 239 | owner | R2-P6-par-3102 |
| fix rounds | U3-ctl-lan-1, the eviction half | owner decision 240 | owner | U3-ctl-lan-1 (the carried rows) |
| fix rounds | The exported additions of F-db021da0-13, -43 and -57 | owner decision 241 | owner | F-db021da0-13, -43, -57 (the carried rows) |
| fix rounds | F-db021da0-51 | owner decision 242 | owner | F-db021da0-51 (the carried rows) |
| fix rounds | R2-P6-sec-3320 (the dev6a unit's R7): the responder's claim is not released when the session is not installed | owner decision 243 | owner | R2-P6-sec-3320 |
| fix rounds | `handshakeStake.resolved` on a refused Sigma1 | owner decision 244 | owner | unit `fxo-net` |
| fix rounds | TM R3.7, the chunk waits a fabric holds | owner decision 245 | owner | TM R3.7 (the carried rows) |
| fix rounds | F-db021da0-55 | owner decision 246 | owner | F-db021da0-55 (the carried rows) |
| fix rounds | R2-P3-rr-ctl-02 (held back, decision 25) | owner decision 247 | owner | R2-P3-rr-ctl-02 (held back) |
| fix rounds | `camera.ErrReportsInUse` | owner decision 248 | owner | unit `fxo-ctl` |
| fix rounds | R2-P6-doc-3084, ADR 0026's principle 7 | owner decision 249 | owner | R2-P6-doc-3084 |
| fix rounds | A failed identity reload after UpdateNOC keeps answering CASE with the old identity | owner decision 250 | owner | unit `fxo-node` |
| fix rounds | One `Commissioning.Salt` for every basic window | owner decision 251 | owner | unit `fxo-node` |
| fix rounds | The guard on SQLite DSNs: the examples' `--seal-key file:<path>` sites (8) | owner decision 252 | owner | the post-integration pass |
| fix rounds | CLAUDE.md:26 | owner decision 253 | owner | the post-integration pass |
| fix rounds | Wave 3's fixture run: the helper branch was never pushed | owner decision 254 | owner | the fixture run |
| fix rounds | The citation sample: the ctl unit's relabels 3 of 10 off (the others 0 of 31) | owner decision 255 | owner | wave 4 `cite` |
| fix rounds | The verify's "confirmed" ranges were overlap-checked (7 of 12 off at ctl) | owner decision 256 | owner | wave 4 `cite` |
| fix rounds | `DAV:n` short citations at `0ceb202d` under a `db021da0` label | owner decision 257 | owner | wave 4 `cite` |
| fix round, wave 1 | R2-P6-doc-1857 (b): keep the hand decoder? | matter.js models it: no length at decode; the 64-byte cap lifted | coordinator | R2-P6-doc-1857 fixed |
| fix round, wave 2 | an *Ahead* row for R2-P6-bound-2203, -2210, -2362? | no: divergence paragraphs in the `BD-matterjs-server-*` entries | coordinator | the entries |
| fix-ctl Q2 | R2-P6-bound-2362's "logged once" without a logger | an optional `topology.Config.Logger` | coordinator | R2-P6-bound-2362 fixed |
| fix-ctl Q3 | R2-P6-err-2307's residual during a flush | mark the replacement dirty; revoked after a reference read measured it not real | coordinator | no change |
| fix-ctl Q4 | N3-rwrace-2's BLE-connect half | to `fx-open` (unit `fxo-ctl`) | coordinator | N3-rwrace-2 fixed |
| fix-tests Q3 | R2-P6-test-3110: a test seam for the counter rollover? | no: recorded as covered by the rollover tests | coordinator | R2-P6-test-3110 |
| wave 3 | F-db021da0-20's three kinds | test fix only: `spec.CheckRequest` already refuses them | coordinator | F-db021da0-20 fixed |
| wave 3 | R2-P6-fsm-1593: refuse or drop the areas of a removed map | drop them, as connectedhomeip | coordinator | R2-P6-fsm-1593 fixed |
| wave 3 | R2-P6-wire-2654: an own *Ahead* row? | no: a clause in the example's entry | coordinator | R2-P6-wire-2654 |
| wave 3 | the guards unit's three response-command differences | classified by the guards unit | coordinator | R2-P6-test-2853 |
| wave 3 | the class (ii) reason texts; Descriptor TagList | reworded; a new findings row for the TagList | coordinator | R2-P6-doc-2817 |
| wave 3 | F-db021da0-43: the internal seam or the field | the field decision 241 allows | coordinator | F-db021da0-43 fixed |

## The owner's decisions of this phase

Decisions 185 to 257, written into `decisions.md` with this pull request. 207,
209, 216 and 243 depart from the recommended option; 225 refines 204 and 237
refines 222. Decision 247 concerns a row that stays held; its text names the
row by ID only.

| # | on | decided | where |
| --- | --- | --- | --- |
| 185 | Completion of a full read in 6b (6a left 25–57 % of a unit unread on the first pass) | **A completion stage inside each review workflow**: after each code unit's review a script reads its unread-range table and a completion agent reads exactly the unread slices; up to 13 (review A) and 14 (review B) agents a workflow, the owner's word under decision 20 | the review workflows (review A, review B) |
| 186 | 6b's budget (≈ 34 M calibrated on 6a, range 28–40, against ≈ 22 M planned; the overrun is the fix side) | **Decision 122 holds for 6b** (everything confirmed is fixed); a checkpoint after the verify with measured counts — a fix side estimated above 20 M goes to the owner before the fix waves | the checkpoint, decision 188 |
| 187 | When the matter.js pin is lifted (the weekly marker check red since 2026-11-16: nine markers absent at matter.js origin/main `8addfb15`) | **After phase 7**, as its own pin-lift pull request with the nine register rows reconciled; phases 6 and 7 stay one convergence run at `db021da0`; the weekly job stays red until then by design | the pin lift after phase 7 |
| 188 | Step 0, the fix scope (the checkpoint of 186; fix side ≈ 27 M, range 22–34) | **Everything confirmed** (decision 122), in four waves | four fix waves |
| 189 | Peer-opened exchanges on an unsecured session this node opened (R2-P6-sec-1801, -bound-1802) | **Refuse**: Sigma1, PBKDFParamRequest and Check-In open an exchange only on an inbound unsecured session; an *Ahead of matter.js* row | R2-P6-sec-1801, -bound-1802 |
| 190 | Joint commissioning without `TrustAnchors` (R2-P6-sec-1850) | **Fail closed** (VENDOR_ID_VERIFICATION_FAILED); a self-anchored lookup is a named host opt-in; `### Changed` | R2-P6-sec-1850 |
| 191 | A stored CA or fabric record a constructor would replace (R2-P6-store-1851 … -1853, R2-P6-api-1859) | **Never replace**: `ErrConfig`; `CreateFabric` errors on a held fabric; an *Ahead* row | R2-P6-store-1851, -1852, -1853, -api-1859 |
| 192 | What "attested" means (R2-P6-sec-1803, decision 105's rule) | **The pair is withheld under test trust** (a production PAA and a CD signer that is not a test one), no rule at QueryImage | R2-P6-sec-1803 (with 214) |
| 193 | connectedhomeip's stricter attestation checks (R2-P6-par-1804, -1805) | **Error findings under the policy**, the pair withheld; two *Ahead* rows; `Context` gets a clock | R2-P6-par-1804, -1805 |
| 194 | The controller registry's seal mark (R2-P6-store-2304, N6a-seal-1) | **Its own sealed canary**, sqlstore schema v3, in the shape of decision 130 | R2-P6-store-2304 |
| 195 | Reseal for a controller database (R2-P6-store-2302) | **`controller.Reseal(ctx, db, old, next)`**: one transaction, both canaries last, one scrub; `sqlstore.Reseal` exported beside it | R2-P6-store-2302 |
| 196 | The radio kits' defaults towards the LAN (R2-P6-sec-2001, -2028) | **Both strict**: ESPHome keyed unless `DiscoverOptions.AllowPlaintext`; the OTBR default client refuses redirects | R2-P6-sec-2001, -2028 |
| 197 | `OperationalDataset.Redact` (R2-P6-sec-2022) | **The copy carries no `Raw`**; a `by_design` row | R2-P6-sec-2022 |
| 198 | R2-P6-sec-3320, the device's resumption record and its resumed session | **Keep the single-use record and record it**; close the installed session when its establishment does not finish; correct the two comments; trace the give-up path first | R2-P6-sec-3320 |
| 199 | N3-coord-1, the unsecured messages of one handshake initiator | **An order key for session 0 by the initiator's node id**, where both references apply the counter check in arrival order | N3-coord-1 (the carried rows) |
| 200 | The device lifecycle methods' result (R2-P5-apidev-16, R2-P6-api-2503) | **`Rename`, `Update` and `SetReachable` return `error`**, in place, `### Changed`; the four hosts grepped for `.Update(` first | R2-P6-api-2503, R2-P5-apidev-16 (the carried rows) |
| 201 | A configuration string beyond its constraint (R2-P6-api-2407, -wire-2502) | **Refuse it in `node.Config.Validate` and `device.New`**; the assembler's VendorName truncation follows | R2-P6-api-2407, -wire-2502 |
| 202 | Additive API the fixes need (R2-P6-bound-1908, -api-2450, -par-3209, -api-2312, -par-3351) | **All five** | R2-P6-bound-1908, -api-2450, -par-3209, -api-2312, -par-3351 |
| 203 | Decision 17's refusal set (N5b-afacade-1) | **Confirmed as built**: 21 types, 36 rows; a test pins the count | N5b-afacade-1 (the carried rows) |
| 204 | A part that already has an owner (R2-P6-fsm-2501) | **Move it, as matter.js** (`Endpoint.ts:706-725`); refined by 225 | R2-P6-fsm-2501 (refined by 225) |
| 205 | A Sigma1 refused for the one-handshake bound (R2-P6-par-2902) | **Busy with a minimum wait, as connectedhomeip** (`CASEServer.cpp:85-122`); the chip-tool suite before the pull request | R2-P6-par-2902 |
| 206 | The status after a refused store write (R2-P6-par-2903) | **Keep the per-path FAILURE**, its scope recorded in `BD-Matter-NonVolatileWrite-StoredBeforeApplied` — a divergence by choice, the consistent reading of the order of authority | R2-P6-par-2903 |
| 207 | The 23 rows that are the reference's own behaviour ("mirrors") | **Fix every one ahead of the reference**, 23 *Ahead of matter.js* rows. Not the recommended option | the 23 rows; their *Ahead* rows in `notes/parity/by_design.md` |
| 208 | The 575 `same` citations nobody read (decision 13) | **Read each in the fix**, a reference read of the doubtful ones and a sample, then relabel; the ratchet to the tool's count | wave 4 `cite` |
| 209 | The 6a rows still open at the head (≈ 45) | **All now**, a reference read for each. Not the recommended option | units `fxo-net`, `fxo-node`, `fxo-ctl` (the carried rows) |
| 210 | R2-P6-wire-2153, an invoke response's path not matched | **The response path into `Result`** (additive; both references hand it to the caller, connectedhomeip `CommandSender.cpp:509-513`); no discard rule (interop risk); a gap behind the references, not an *Ahead* row | R2-P6-wire-2153 |
| 211 | R2-P6-fsm-2109, the granted MaxInterval unchecked | **Register only**: not performable, the missing input is the peer's IdleModeDuration; connectedhomeip leaves it unchecked too (`ReadClient.cpp:1122`) | R2-P6-fsm-2109 |
| 212 | R2-P6-par-2156, the status budget of a wildcard-endpoint write chunk | **Register only**: not performable, the missing input is the device's endpoint count; connectedhomeip budgets none (`WriteClient.cpp:183-204`) | R2-P6-par-2156 |
| 213 | R2-P6-err-2106, the doc of `ErrNotConnectable`'s class | **The doc corrected**, the behaviour matter.js's, no *Ahead* row | R2-P6-err-2106 |
| 214 | R2-P6-sec-1803, the test-trust facts of decision 192 | **Two additive bools `attestation.Result.TestPAA` and `.TestCDSigner`**, set by `Validate`; the findings list unchanged | R2-P6-sec-1803 |
| 215 | R2-P6-fsm-1860, joint-commissioning consent without a context | **`Consent func(ctx, TrustInfo) (bool, error)` in place**: the fail-safe extended before the call, the context ends the wait; `### Changed` (no host uses it) | R2-P6-fsm-1860 |
| 216 | R2-P6-doc-1857 (b), the signature length at decode | **A reference read of both references first**; the decoder then follows them. Not the recommended option | R2-P6-doc-1857 |
| 217 | R2-P6-err-1912, `QueryUpdates` and `CheckForUpdates` always nil | **Return the per-product failures** (`errors.Join`); `### Changed` | R2-P6-err-1912 |
| 218 | 5b-dcl R8, a host outside ASCII handed to a proxy | **One rule for every proxy hand-over**: no proxy is handed a host outside ASCII | 5b-dcl R8 (the carried rows) |
| 219 | R2-P6-bound-1914, the OTA manager forgets a removed node late | **An additive Manager method**, wired to `client.Hooks.Deleted` by the `ctl` unit in wave 2 | R2-P6-bound-1914 |
| 220 | R2-P6-bound-2102 follow-up: `DefaultMaxReportBytesPerSession` now counts memory | **16 MiB** (≈ 120 000 scalar attributes; was 2 MiB of payload, ≈ 80 000); `### Changed` | R2-P6-bound-2102 |
| 221 | R2-P6-bound-2104, how far the binding manager releases a peer it created | **Disconnect only**; the peer stays in the set, nothing another holder may use is closed | R2-P6-bound-2104 |
| 222 | R2-P6-bound-2362, two nodes claim one extended address or RLOC16 in the topology | **A collision resolves to neither node and is logged once**, no new cap; a divergence paragraph from matterjs-server `0bd06f22` in `BD-matterjs-server-NetworkTopology` | R2-P6-bound-2362 |
| 223 | R2-P6-bound-2357, the set of warned unknown events | **256 per node**, a named constant, one "further unknown events are not logged" line; an *Ahead* row | R2-P6-bound-2357 |
| 224 | R2-P6-api-2751, `cluster/lock` declares `StateSource` a second time by hand | **An alias of the generated type**, `### Changed` (one declaration, as every kit port; no host imports `cluster/lock`) | R2-P6-api-2751 |
| 225 | R2-P6-fsm-2501 again, after a reference read: matter.js moves an endpoint whose construction has started onto an owner whose parts are ready only by throwing, which leaves it without an owner (`Parts.ts:64-70`, `Construction.ts:284-287`, `Endpoint.ts:706-725`) | **Refuse the move of a part bound to a running node** (a clean refusal, the part stays with its owner); before start it moves, as matter.js; the two Aggregator refusals stay; `BD-Matter-Device-PartBoundWithItsOwner` with the liveness qualification | R2-P6-fsm-2501 |
| 226 | R2-P5-apidev-16 rest: the second label write in `Endpoint.SetNodeLabel` is dropped and unlocked | **`SetNodeLabel(label) error`, locked, the bridge route returns it**; `### Changed`; unit `fx-open` in wave 3 | R2-P5-apidev-16 rest (the carried rows) |
| 227 | R2-P6-par-2713 (replaces the gen unit's second question — its basis "no id differs by direction" is false: Thermostat command 2 is a response at the pin and a request at v0.13.0, measured by a real level-generation run) | **The level extractor writes request and response ids separately; the six levels re-extracted** (an own commit, its diff read) | R2-P6-par-2713 |
| 228 | R2-P6-sec-2401, the limit half: the node's own Operate entry at a full per-fabric ACL | **As matter.js** (it appends, `AccessControlServer.ts:154-156` before `:178-181`), the consequence in the findings register; headroom is a later, separate decision | R2-P6-sec-2401 |
| 229 | R2-P6-api-2409, the bound of the mDNS host-name override | **40 bytes, as connectedhomeip** (`src/lib/dnssd/Constants.h:32-36`) | R2-P6-api-2409 |
| 230 | N4-coresys-1, BootReason SoftwareUpdateCompleted on a start into the recorded version | **An *Ahead* row**, marker `OtaSoftwareUpdateRequestorServer.ts:153-155`, source connectedhomeip `ESP32/DiagnosticDataProviderImpl.cpp:192-201` | N4-coresys-1 (the carried rows) |
| 231 | R2-P6-sec-3320, the rest: the record was single-use only from the upsert (1 to 6 of 16 concurrent copies resumed, measured) | **The responder claims the resumption id at lookup**, so the record is single-use from acceptance | R2-P6-sec-3320 |
| 232 | R2-P6-sec-3320: an initiator's failure StatusReport on Sigma2_Resume | **Keep the installed session, as matter.js** (`CaseServer.ts:211`, `:216`; connectedhomeip discards it, `CASESession.cpp:2583-2593`); the report is not authenticated | R2-P6-sec-3320 |
| 233 | R2-P6-sec-2412, the initiator role | **Abort in both roles, as connectedhomeip** (`CASESession.cpp:476`, `:529`, `:423-433`), after a reference read of matter.js's CaseClient | R2-P6-sec-2412 |
| 234 | R2-P6-err-3112 / W6-test-17, a value of the wrong Go type on an attribute write | **INVALID_DATA_TYPE per path** (matter.js's validation, `action/errors.ts:55-57`); commands keep INVALID_COMMAND | R2-P6-err-3112 |
| 235 | Decision 190 on the device side: `device.NewJointFabricAdministrator` cannot take a trust-anchor lookup | **An additive port field in the generator** (`script/clustergen/ports_jointfabric.go`); unit `fx-open` in wave 3 | unit `fxo-node` |
| 236 | R2-P6-sec-2361, `controller.PingAddress` pings any address | **Refuse an address that is not in the node's record** (`ErrConfig`), `### Changed`; both hosts that use it pass `Record().Addresses` | R2-P6-sec-2361 |
| 237 | R2-P6-bound-2362 again (an RLOC16 is unique only within one Thread network, matterjs-server `topology-utils.ts:422-451`) | **An RLOC16 collision counts per ExtendedPanId**; extended addresses are global | R2-P6-bound-2362 |
| 238 | R2-P6-sec-2616 (a decision-207 row, doc only) | **The doc sentence** on `ApplyUpdate` and in the README's OTA part, citing matter.js `OtaSoftwareUpdateRequestorServer.ts:1121`; no ledger-checksum behaviour | R2-P6-sec-2616 |
| 239 | R2-P6-par-3102 (a decision-207 row, a label) | **No code**; the 3101 (b) case asserts that the old session is neither lost nor closed and that `NewestSession` is the device's; one register sentence (`SessionManager.ts:703-708`) | R2-P6-par-3102 |
| 240 | U3-ctl-lan-1, the eviction half | **Evict the records of names no resolution observes first** (a second order list); a per-name bound is a later decision | U3-ctl-lan-1 (the carried rows) |
| 241 | The exported additions of F-db021da0-13, -43 and -57 | **All three allowed**: an optional peer-schedule interface, an `OperationalSessionLookup` method and an `operational.Entry` accessor; a `mattercore.BridgedConfig` field; `MatterWriteCommit` on the cover and fan servers; `### Added` each | F-db021da0-13, -43, -57 (the carried rows) |
| 242 | F-db021da0-51 | **Split**: a chunked write on a client-stack session is refused whole (fails closed) until ADR 0032's session layer — unit `fxo-ctl`; the rest stays with ADR 0032 | F-db021da0-51 (the carried rows) |
| 243 | R2-P6-sec-3320 (the dev6a unit's R7): the responder's claim is not released when the session is not installed | **An exported field on `sigma.Sigma1ProcessResult`**, which `CaseAdapter.ProcessSigma1` calls when the session is not installed; `### Added`. Not the recommended option | R2-P6-sec-3320 |
| 244 | `handshakeStake.resolved` on a refused Sigma1 | **Commit only at acceptance**: an optional capability on the resolver (an unexported interface), which sigma calls at its commit points; no exported move | unit `fxo-net` |
| 245 | TM R3.7, the chunk waits a fabric holds | **A new exported bridge setter for the fabric count**, wired by `node`; each fabric's share of the chunk waits applies from half the dispatch budget | TM R3.7 (the carried rows) |
| 246 | F-db021da0-55 | **Export a MEI helper from `cluster/spec`**, used by the dispatcher; `### Added` | F-db021da0-55 (the carried rows) |
| 247 | R2-P3-rr-ctl-02 (held back, decision 25) | **Decided, no code**; the decision's text is held back with the row (decision 25) | R2-P3-rr-ctl-02 (held back) |
| 248 | `camera.ErrReportsInUse` | **An additive sentinel that also matches `ErrInvalidArgument`** (`errors.Is`); `### Added` | unit `fxo-ctl` |
| 249 | R2-P6-doc-3084, ADR 0026's principle 7 | **Two findings-register rows** (the OTA requestor's image, the DCL snapshot fetch), fixed in phase 7; a dated note in the ADR points to them | R2-P6-doc-3084 |
| 250 | A failed identity reload after UpdateNOC keeps answering CASE with the old identity | **Withdraw**: the error arm forgets the identity and withdraws the old instance; the stored fabric stays (connectedhomeip deletes it, `FabricTable.cpp:2064-2070`, not followed) | unit `fxo-node` |
| 251 | One `Commissioning.Salt` for every basic window | **As matter.js, a fresh salt per window** (`DeviceCommissioner.ts:130-137`, `:214`); a new row for `node/`; `### Changed` | unit `fxo-node` |
| 252 | The guard on SQLite DSNs: the examples' `--seal-key file:<path>` sites (8) | **Extend `keyReferences`** with exact counts (the reference bridge's tests 3, the reference controller's 5), the guard's counted mechanism, as for `--ca-key` | the post-integration pass |
| 253 | CLAUDE.md:26 | **`make ci-net` names its three guards** | the post-integration pass |
| 254 | Wave 3's fixture run: the helper branch was never pushed | **The coordinator pushes the helper branch and dispatches `chiptool.yml` with `update_fixtures`** (run 44252157107: all green, the artifacts byte-identical, no fixture commit) | the fixture run |
| 255 | The citation sample: the ctl unit's relabels 3 of 10 off (the others 0 of 31) | **A reference read of all 86 of ctl's reason-for-behaviour citations** (70 read, 23 corrected, boundaries only) | wave 4 `cite` |
| 256 | The verify's "confirmed" ranges were overlap-checked (7 of 12 off at ctl) | **A boundary read of the 111 verify-sourced citations of the cli, sec, node and clu units** (cli 10 of 39, sec 5 of 25, node 5 of 18, clu 1 of 9 off) | wave 4 `cite` |
| 257 | `DAV:n` short citations at `0ceb202d` under a `db021da0` label | **Re-measured in 6b, and the citation check learns the DAV alias** (46 sites in seven files and a package doc) | wave 4 `cite` |

## Noticed by the fix agents, the guard runs and the coordinator, not fixed where it was noticed

No severity: none of these was reviewed or verified as a finding. What a later
round, a read or a decision took up is in the rows above and is not repeated
here. The ids are this register's (`N6b-<group>-<n>`).

| ID | noticed in | what | disposition | where |
| --- | --- | --- | --- | --- |
| N6b-csql-1 | fix-fx-csql.md:52 | the controller logs no line for an owed scrub of the store or the registry (decision 132's one warn line); a hunk was proposed for `controller.go` | phase 7 (the register editor's judgement): no later note names the hunk | `controller/controller.go` |
| N6b-csql-2 | fix-fx-csql.md:53 | no `sqlstore` erase-all: a registry-alone host has no way back to an unmarked registry | phase 7 (the register editor's judgement): the fix note's question 1 reached no decision | `controller/client/sqlstore` |
| N6b-radio-1 | fix-radio-esp.md:63 | no separate rate limit on the DNS-SD rescan a loss triggers; the growing backoff paces it | not a finding: paced by the backoff (the fix note) | `ble/esphome` |
| N6b-radio-2 | fix-radio-esp.md:64 | the encryption-without-key skip in `candidates` logs at every browse | phase 7 (the register editor's judgement) | `ble/esphome` |
| N6b-radio-3 | fix-radio-nix.md:82, :83 | R2-P6-bound-2023's list caps take reference values; R2-P6-bound-2027's Added/Removed flood case not covered | phase 7 (the register editor's judgement) | `thread/otbr`, `thread/brdiscovery` |
| N6b-radio-4 | fix-radio-nix.md:84; fix-fxo-ctl.md:77 | the bluez peripheral's `connFor` accepts per method goroutine without serialisation (the class of R2-P6-conc-2051); the deprecated `contract.BLEPeripheral` does not promise non-overlap | the release after 6b (decision 125): the nested modules move onto `GATTPeripheral` (the register editor's judgement) | `ble/bluez` |
| N6b-radio-5 | fix-radio-nix.md:85 | no retry of an owed `UnregisterApplication` in `peripheral.Close` | not a finding: the bus closes there (the fix note) | `ble/bluez` |
| N6b-cpeer-1 | fix-cpeer.md:67 | R2-P6-conc-2112's `c.mu` sites left as the row states | phase 7 (the register editor's judgement) | `client/` |
| N6b-cam-1 | fix-cam.md:67, :68 | no `GOARCH=386` test leg for `controller/camera` (R2-P6-bound-2250, -2251 at run time) and no lint for a new `int(uint32)` conversion; the 32-bit leg runs `./controller/...` (`fix-guards.md:39`) | not a finding: the guards unit found the 32-bit leg present | `controller/camera` |
| N6b-gen-1 | fix-gen.md:107 | the extractor writes a bigint as a string, so a bigint default renders as a Go string | phase 7 (the register editor's judgement) | `script/extract-from-matter-js.ts` |
| N6b-gen-2 | fix-gen.md:109 | the level extractor and `generate-level.sh` were run against matter.js tags only once, in the round-3 re-extraction | not a finding: the run of decision 227 (`fix-gen.md` round 3) (the register editor's judgement) | `script/` |
| N6b-facade-1 | fix-facade.md:84 | R2-P6-store-2406: a re-Add of a half-forgotten key before the next `New` still meets the leftovers | phase 7 (the register editor's judgement) | `node/` |
| N6b-facade-2 | fix-facade.md:85 | R2-P6-err-2451 has no red test | phase 7 (the register editor's judgement) | `node/` |
| N6b-device-1 | fix-device.md:82, :83 | R2-P6-conc-2513 has no regression test short of a flake run; the raw-Spec check at the assembler (`materialize.go:619-625`) | phase 7 (the register editor's judgement) | `device/`, `endpoint/` |
| N6b-dev6a-1 | fix-dev6a.md:113 | `node/security.go:402` still cites a `0ceb202d` line of `ServerNetworkRuntime.ts` | re-cited in the citation pass of wave 4 if its site was a patch-plan row; not traced (the register editor's judgement) | `node/security.go` |
| N6b-dev6a-2 | fix-dev6a.md:114 | `FromRawName` compares raw bytes where matter.js decodes invalid UTF-8 to U+FFFD; no parity case needs it | phase 7 (the register editor's judgement) | `secure/attestation` |
| N6b-ctl-1 | fix-ctl.md:86 | N3-finimg-3's second half: the PartsList walk per report | phase 7 (the register editor's judgement) | `controller/client` |
| N6b-ctl-2 | fix-ctl.md:88 | R2-P6-sec-2311's optional rate limit of discovery events | phase 7 (the register editor's judgement) | `controller/` |
| N6b-ex-1 | fix-examples.md:68 | R2-P6-fsm-2608: the CMAF session number of an event emitted after a later transition (a module port change; demo only) | phase 7 (the register editor's judgement) | `examples/reference-bridge` |
| N6b-ex-2 | fix-examples.md:69 | R2-P6-sec-2615: `ble-wifi-light`'s `--passcode` takes no file or environment form | phase 7 (the register editor's judgement) | `examples/ble-wifi-light` |
| N6b-ex-3 | fix-examples.md:70 | R2-P6-conc-2614: no deterministic test of the swap race | phase 7 (the register editor's judgement) | `examples/` |
| N6b-tests-1 | fix-tests-6b.md:101 | the wide mutant run of the 28 remaining `R2-P5-tmut-18` mutants stopped after three for its run time | phase 7 (the register editor's judgement) | `cluster/core`, `bridge`, `node` |
| N6b-docs-1 | fix-docs.md:80 | `store/schema.go:220` cites a `vid_verification.go` range for a key no longer there | phase 7 (the register editor's judgement) | `store/schema.go` |
| N6b-docs-2 | fix-docs.md:81 | the ADR index row 0022 says CastingVideoClient stays out, while a generated device file exists | phase 7 (the register editor's judgement) | `docs/adr/` |
| N6b-docs-3 | fix-docs.md:83 | `notes/parity/matter/README.md:33` repeats S-12's stale build step | fixed `3ca20082`, `76269095` (the register editor's judgement) | `notes/parity/matter/README.md` |
| N6b-ctlf-1 | fix-fxo-ctl.md:80, :81, :82 | `reconnect_probe.go:436` reads a reported NetworkInterfaces list through the constraint-checking decoder; the Networks skip has no flow-level test; no standing guard fits the F-db021da0-15 class | phase 7 (the register editor's judgement) | `controller/commissioner` |
| N6b-net-1 | fix-fxo-net.md:163, :164 | F-db021da0-48 sites outside `im`: `ota_software_update_requestor.go:1220`, `mattercert/decode.go:722` (no verdict in the item) | phase 7 (the register editor's judgement) | `cluster/core`, `secure/mattercert` |
| N6b-cite-1 | fix-cite-sec.md:169, :170 | 2 578 unbound bare parts, 3 bound parts with a hard error and 115 new-form off-pin citations in other units' paths were counted, not reworked | phase 7 (the register editor's judgement) | the citation inventory |
| N6b-cite-2 | fix-cite-cli.md:79-82 | unlabelled ranges in `client/interaction/sustained.go:657-660`, `transport/mrp/ack.go:162`, `client/handshake/case.go` and four `groups/` `0ceb202d` citations not read | phase 7 (the register editor's judgement) | `client/`, `transport/`, `groups/` |
| N6b-cite-3 | fix-cite-node.md:82 | drift entries in `node/` that no bin covers, and the R2-P6-doc-2906 unlabelled ranges | phase 7 (the register editor's judgement) | `node/` |
| N6b-cite-4 | fix-cite-w4fix.md:81-88 (F1) | `bridge/receive.go:216-222` cites the duplicate branch of `MessageExchange.onMessageReceived`; whether the bridge has the client side's equivalent was not read | phase 7 (the register editor's judgement) (the notes' phase-7 carry) | `bridge/receive.go` |
| N6b-cite-5 | fix-cite-w4fix.md:88-91 (902) | `im/read.go` omits an empty list where matter.js sets it; a `by_design` candidate only if the owner wants the degenerate case recorded | phase 7 (the notes' phase-7 carry) | `im/read.go` |
| N6b-cite-6 | fix-cite-w4fix.md:92-94 (cli 167) | a parity test file claims cases of `SustainedSubscriptionTest.ts` it has no subtest for | phase 7 (the notes' phase-7 carry) | `client/interaction` |
| N6b-cite-7 | fix-cite-w4fix.md:207-222; handover.md:424 | not performable at `0ceb202d` (`subscribe.go:145-157`, `policy.go:10-11`); `controller/open.go:28-30` without a source; bare parts under re-ranged heads in units not read; `groups/request.go:94` wording; the boundary reads' not-read list | phase 7 (the hand-over's phase-7 carries) | `client/`, `controller/`, `groups/` |
| N6b-cite-8 | handover.md:424 | the DAV alias is global; a package doc names `db021da0` in prose | the pin lift after phase 7 (the hand-over) | `controller/attestation` |
| N6b-ci-3 | handover.md:405-406 | the units' guard runs on their own packages missed repo-wide guards (sleep ratchet, testquality, slogkeys, sqlitedsn, the certifiability block, cite-check); fixed by the post-integration pass | fixed in the post-integration pass (the hand-over); decision 252 | the guards |

## Unread

What each review said it did not read, compare or run (§7 of its report, and
of its completion stage where one ran), summarised per unit, and the unread
lists of earlier phases the plan placed in this half. The lists are the
reviewers', not re-measured here. Phase 7 repeats a full-cut run; placing
these lists there is the register editor's judgement, as each row says.

| Unit | area | not read | placed |
| --- | --- | --- | --- |
| chs | `controller/commissioner`, `client/exchange`, `controller/ca` | nothing of the given ranges (0 of 5 504 left after completion); no test file; the other `Mirrors` citations of the ranges; no `make race` (`phase-6-chs-pass2.md` §7) | phase 7 (the register editor's judgement) |
| dcl | `controller/dcl`, `controller/softwareupdate`, `bdx` | no test file in full; the three held destination rows not re-measured; matter.js's DCL, BDX and OTA files and connectedhomeip's `bdx/` and OTA requestor not read (§7) | phase 7 (the register editor's judgement) |
| radio | `ble/`, `thread/`, `wifi/` | 0 of 3 960 given lines left; the filtered comment lines, the tests, the ESPHome and aioesphomeapi sources the kits cite (every firmware-fidelity claim unverified); no run for R2-P6-conc-2053 (`phase-6-radio-pass2.md` §7) | phase 7 (the register editor's judgement) |
| cpeer | `client/` | 0 of 2 199 given lines left; no probe for R2-P6-bound-2150 and -2151 (`phase-6-cpeer-pass2.md` §7) | phase 7 (the register editor's judgement) |
| cam | `controller/camera` | 0 of 1 979 given lines left; the parity test files; every matterjs-server `Mirrors` line unchecked; no 32-bit test run (`phase-6-cam-pass2.md` §7) | phase 7 (the register editor's judgement) |
| ctl | `controller/` | 0 of 7 061 given lines left; no `-race` or probe for several rows (each names its test); the package's own tests not run (`phase-6-ctl-pass2.md` §7) | phase 7 (the register editor's judgement) |
| par-node | the citation blocks of the five maps | 255 of 311 citation blocks (1 941 of 2 518 lines) checked by the script only; four upstream changes under older-pin ranges read as diffs, the Go side not compared; parts of matter.js not read (§7) | wave 4 `cite` read the citations (decision 208); the rest phase 7 (the register editor's judgement) |
| facade | `node/`, `internal/` | every given range read by the completion pass (`phase-6-facade-pass2.md`) | — |
| device | `device/` | no `-race` run of `./device/...` and `./internal/...`; no node-level run of R2-P6-wire-2502 and -api-2503; no reference source read (§7) | phase 7 (the register editor's judgement) |
| examples | `examples/` | 0 of 8 133 given lines left; the packages' own tests and `-race`; a run of R2-P6-conc-2651's interleavings and of R2-P6-err-2650 (`phase-6-examples-pass2.md` §7) | phase 7 (the register editor's judgement) |
| gen | `script/` generators | 0 of 2 556 given lines left; no probe or build; the error mapping behind two lines R2-P6-doc-2750 names (`phase-6-gen-pass2.md` §7) | phase 7 (the register editor's judgement) |
| guards | `script/` guards | 0 of 4 287 given lines left; chipdm's tests; `make chipdm-check` and `make reachability` not run (`phase-6-guards-pass2.md` §7) | phase 7 (the register editor's judgement) |
| docs | `docs/` | the reference half of 219 rows (the docs-reference verify read them); the four consumer repositories; CI run histories; no `mkdocs build` (`phase-6-docs-pass2b.md` §7) | phase 7 (the register editor's judgement) |
| tests | the flakes and the test register | no `make ci`, `make race` or chip-tool leg; no reference source; a longer CPU-bound load not repeated; the rows sent to verify B (§7) | phase 7 (the register editor's judgement) |
| cite | the citation inventory | 575 `same` rows of 931 unread by the spot-read rule (then read in wave 4, decision 208); no line comparison of connectedhomeip citations; `drift.json` beyond the counts (§7) | wave 4 `cite` (decision 208); the drift that remains phase 7 (the register editor's judgement) |
| chs, dcl, radio, cpeer, cam, ctl (carried) | the unread lists of phases 1, 2, 3 and 5b the plan placed in review A's units (39 lists: P1 ctl, sec, net, drv, dr-rest, cam; P2 comm, sess, dr-ctl, stop, alloc, rr-ctl; P3 net, dr-net, dr-ctl; P5b peer, dr5b-code-3) | each unit read its cut in full with its completion stage; the lists were not answered item by item | phase 7 (the register editor's judgement) |
| facade, device, examples, gen, guards (carried) | the unread lists the plan placed in review B's code units (16 lists: P1 misc, req, node, dr-req; P2 dr-noc, dr-node, rr-dev, rr-ctl; P5a dr-code; P5b apinode, apidev) | each unit read its cut in full with its completion stage; the lists were not answered item by item | phase 7 (the register editor's judgement) |
| cite (carried) | 6a's par-im and par-srv unread lists | handed to the verify by the unit; read in wave 4 as rows of the patch plan | wave 4 `cite` (the register editor's judgement) |

## Counts

Generated from the rows above, at `361102dd`: a row is counted by the head of
its disposition; a duplicate by its twin's. A fixed row that names a
`by_design` entry, a findings row or a part that stays is counted as fixed or
fixed in part.

The 350 findings, by their first disposition:

| disposition | review A | review B | the verify | all |
| --- | --- | --- | --- | --- |
| fixed in part | 1 | 2 | 0 | 3 |
| fixed | 143 | 170 | 17 | 330 |
| a recorded divergence | 1 | 0 | 0 | 1 |
| decided | 0 | 3 | 0 | 3 |
| closed before this phase | 0 | 1 | 0 | 1 |
| struck | 1 | 2 | 0 | 3 |
| not a finding | 0 | 2 | 0 | 2 |
| not reproduced | 1 | 0 | 0 | 1 |
| open | 4 | 0 | 0 | 4 |
| phase 7 | 1 | 1 | 0 | 2 |
| all | 152 | 181 | 17 | 350 |

By the severity their rows carry — review A: 20 medium, 78 low, 52 note, 2
struck or a duplicate; review B: 12 medium, 59 low, 108 note, 2 struck or a
duplicate; the verify: 3 low, 14 note.

The reference reads: 518 rows — 357 taken in a fix round, 25 recorded by a fix
note, 116 a fact note, used, 20 not taken up.

What earlier phases handed to 6b: 218 rows — 5 fixed in part, 92 fixed, 1 a
recorded divergence, 5 decided, 5 closed before this phase, 3 struck, 3 not a
finding, 6 not reproduced, 7 open, 45 re-cited in the citation pass, 17 phase
7, 1 the pin lift after phase 7, 1 taken with other rows, 3 not performable,
24 held back.

The questions: 126 rows, 70 answered by the owner and 56 by the coordinator.
The owner's decisions of this phase: 73 rows (185 to 257).

The 36 noticed items: 2 fixed, 4 not a finding, 1 re-cited in the citation
pass, 27 phase 7, 1 the release after 6b, 1 the pin lift after phase 7.
Unread: 18 rows.

**Held back: 24 rows, all carried rows of earlier phases that stay open after
this branch, or of the three decision 126 names** — R2-P3-rr-ctl-02,
N3-rwrace-1, N3-ctl-3, U3-ctl-dev-1 second half, U3-ctl-lan-1 (3)–(5), U-ctr-1
rest, N-net-1 (b), N-rw3req-3, N-bridge2-3, TM R3b.2, TM R3b.4, U-alloc-1
(device parts), U3-lan-dev-1, N4-f3-2, U3-admin-1, TM R3.2, TM R3.6, TM R3.3,
TM R3.5 first half, U3-net-1, TM R3.8 rest, R2-P5-redact-01, N4-frsec-1,
DR5b-F-01. No finding of this phase's review is held back: R2-P6-sec-3320, the
one the verify raised beside a held row, is fixed on this branch, and so are
the earlier phases' rows N3-coord-1, N3-rwrace-2, TM R3.7, TM R5 5a.5,
U4-fr-sec-2 (R2-P6-sec-2401), U3-ctl-lan-1 parts (1) and (2) and the 5b DCL
rows, public here. The rows phases 3, 4, 5 and 6a hold back for later phases
stay in their files as they were.

## Not placed, not verified

- Placed by the register editor's judgement (the row says so): most carried
  rows without a fix note (phase 7), the 34 carried citation rows of decision
  151 that the wave-4 notes do not name one by one, the noticed items and
  every unread row; R2-P6-doc-2906's 14 script-flagged ranges, R2-P6-doc-2909,
  R2-P6-test-2814.
- The reference reads' items that no note names (20): 6b-2412i Seen on the
  way, 6b-guardfix F1, 6b-chs 2 — 1850, 6b-chs 9 — held rows, 6b-chs F1,
  6b-chs F10, 6b-chs F11, 6b-chs F12, 6b-dcl D7, 6b-cpeer Note 2152, 6b-cpeer
  Note TestOmap_N3Disc2, 6b-cpeer Re-check note F3 residual, 6b-cpeer Re-check
  note F1 record lifetime, 6b-cpeer Re-check note F1 replaced peer, 6b-cpeer
  Re-check note F2 stale release, 6b-gen N3 latent silent pass, 6b-facade 1b,
  6b-w4 W1 transport/mrp/retransmit.go:58 (beyond the brief), 6b-w4-ctl86
  ctl86 F3 verify-6b-cite section 3 verified by overlap, 6b-w4-final final not
  read. The notes may have taken some of them without naming the item; not
  checked against the code.
- For the reads after the third wave's merge (`6b-w3r2`, `6b-register-w3`) the
  only record of what was applied is the hand-over's line on the
  post-integration pass (the corrections of the reads, the two mediums, the
  register merge); "taken" there is that line, not a per-item line.
- Contradictions between notes: the merged verify counts two duplicates, its
  table and bins name one (R2-P6-bound-2150); the `chs` fix note says
  R2-P3-rr-ctl-02's unit half was addressed, the `chs` read says it does not
  hold (the held row follows the read); `fix-dcl.md` round 1 leaves 5b-dcl R8
  unfixed, round 2 fixes it (decision 218); R2-P6-par-2713 was fixed in round
  1 on a premise round 2 found false (decision 227 replaces it).
- Commits a note names that are not on the branch: the fix notes name their
  worktree commits (`47233551`, `40b341f1`, `f72fef20`, `c960c7f3`, `27f202e8`
  and others); after the rebases the branch's ids differ, and each row's
  commit was found by its ID in the branch's messages. 5b-dcl R7's fix commit
  (`27f202e8` in `fix-dcl.md`) is not found by the row's id on the branch; its
  row says so.
- 143 of the 552 commits are named by no row. 93 by their subjects are
  regenerated snapshots, citation passes, CHANGELOG, README, docs and notes
  lines or coverage floors: `b9131d1b`, `e80609a2`, `825f9adf`, `aca750b1`,
  `993caa70`, `4b391256`, `7e8a3c2d`, `97bd50cb`, `20bf3841`, `0752e3c2`,
  `cda25420`, `1cb80f24`, `fc0bb76d`, `62d0d960`, `5aa517ee`, `4d8fdcc2`,
  `c1898715`, `1d271e9d`, `846c8078`, `0b10eae4`, `203561f4`, `ab2114b2`,
  `aaaaf01e`, `55c9e0aa`, `506d6972`, `59cd40ce`, `b09d184e`, `b80c26d4`,
  `33b23568`, `217e81e2`, `41264c72`, `2d95cc8b`, `5f8004bf`, `9465dd63`,
  `483e977e`, `b4f434f3`, `5a9817bc`, `fedea90c`, `b3728bcd`, `beb71669`,
  `b10805d6`, `e9ddbb71`, `df79c703`, `da055b24`, `8b6d03f0`, `ff8365a6`,
  `83b4057c`, `75a81741`, `ffaba840`, `898375e8`, `bec27b8d`, `2ebacc92`,
  `95b117d0`, `d99d57ef`, `418591bf`, `07d94834`, `4cbe6cc3`, `43121501`,
  `0d3743b6`, `6c718a8a`, `438e08bb`, `79f33947`, `3cbfb7f7`, `648f8bc6`,
  `365ea55d`, `6a540280`, `4a54d8a5`, `abacf2cd`, `d5670cd9`, `f6a5d0e9`,
  `ad324443`, `478238e8`, `567b9aab`, `85e00c36`, `6e529899`, `98aaaf46`,
  `52595b90`, `953a2402`, `f480dcc8`, `5ea71730`, `9c2058f8`, `40ba8f03`,
  `38d80da2`, `9e1b80e8`, `110422f8`, `1953a4e1`, `ad9ce62f`, `57b1c2b4`,
  `f9a43b6c`, `d4b17ee3`, `b9a73e5d`, `a6030d1f`, `886859bc`. The other 50
  change code or tests and name no row's id (a decision, a lint or guard
  follow-up, an integration fix): `afb25c96` ble/esphome; `d603ad91`
  controller/ca/keyfile; `50bd8755` tests; `1c8f6271` binding; `019ceb66`
  examples/ble-wifi-light; `0b60137e` controller/commissioner; `996fb86d`
  node; `d455700d` node; `02f4bafb` examples/host-view; `99b21a4d` controller;
  `a8075e60` controller; `61835868` cluster/jointfabric; `e60612e0` node;
  `776d50ca` tests; `e1641c82` errprefix; `575592a1` cover; `693fcedd` guards;
  `bd8fb3ca` tests; `2e57f018` chipdm; `ca9e2bf2`
  examples/reference-controller; `1e3a218d` examples; `02b646fc` client/peer;
  `ec4398ae` groups; `a1550254` groups; `4a630fa9` groups; `a3e8d376` node;
  `76a17bef` secure/sigma, node; `5ff186eb` node; `1262853a` device;
  `c3e10435` node; `b28d5781` node; `3c03b64c` node; `deb33f59` node;
  `b1dbd62f` core; `86dd11c0` clustergen; `f51a28d2` lint; `7dd9fe71`
  endpoint; `3cda0f21` fan; `a1baacc5` node; `3a1c2c0d` controller/dcl;
  `66cbe6cf` controller/commissioner; `8cbf5b2b` controller/commissioner;
  `37183771` citations; `e49828db` comments; `fdf8e1fd` node; `7aadaa53`
  controller/commissioner; `4588dfc2` sqlitedsn; `7700b360` CLAUDE.md;
  `2da27e08` secure/operational; `7b7526fd` bridge. None of them was read one
  by one.
- The verify's verdicts and severities are its merged file's (later bin wins);
  R2-P6-sec-3320's are the follow-up read's (its counts: low, not a security
  finding). The completion stages' rows are the units'.
