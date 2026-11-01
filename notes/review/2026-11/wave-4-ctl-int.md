# Wave 4 — theme `ctl-int` (DCL, OTA provider, BLE transports, OTBR, OTA image)

- **Scope**: `controller/dcl/`, `controller/softwareupdate/`, `controller/ble/`,
  `ble/esphome/`, `ble/bluez/`, `thread/otbr/`, `bdx/otaimage/` (non-test
  files; the OTA-provider parity test was read for the token/consent
  checks only).
- **HEAD**: `769ea3c2`. No `notes/reads/read-review-w4-*.md` map existed;
  the code was read directly.
- **Pins**: matter.js `862ad75f` (local `../matter.js` HEAD is that commit);
  connectedhomeip `6170af84` read with `git show` from the local checkout.
- **Reviewer**: Opus, 2026-11-01. Severities: high / medium / low / note.
  One scratch measurement backs W4-ctlint-01 (see its confidence line).

---

## W4-ctlint-01 — high — a nested CRL crashes the controller process (fatal stack overflow)

- `controller/dcl/crl.go:87-135` (`derDecodeRec`) and
  `controller/dcl/revocation.go:333-356` (`downloadCRL`, `io.ReadAll` at
  `:355`); the size check against `DataFileSize` runs only after the whole
  body was read (`revocation.go:309-311`).
- **Claim.** `derDecodeRec` recurses once per constructed DER level with no
  depth bound, and `downloadCRL` reads the body without a size bound. A CRL
  built as `30 84 LLLLLLLL` nested ~3.2 M deep (about 19 MiB) exhausts the
  goroutine's 1 GiB stack; a Go stack overflow is a fatal runtime error,
  not a recoverable panic, so the whole controller process dies. The CRL
  `DataURL` is the vendor's own web host named in its ledger revocation
  point (`revocationPoint.DataURL`, `types.go`), so a compromised or hostile
  CRL host — a weaker trust than the ledger — can crash every commissioner
  that attests one of that vendor's devices while the 3 s download window
  (`revocationTimeout`, `revocation.go:41`) admits ~50 Mbit/s; a CRL host on
  the same LAN or a CDN does.
- **Reference.** matter.js's `DerCodec.#decodeRec`
  (`packages/general/src/codec/DerCodec.ts:429-442` at `862ad75f`) is
  recursive too, and its download (`DclCertificateService.ts:1543-1552`,
  `fetch` + `arrayBuffer`) has no size bound either — but a JS stack
  overflow is a `RangeError` the `#processRevocationPoint` catch turns into
  a rejected point; the Go consequence differs. chip never fetches CRLs
  (the revocation set is a file, `generate_revocation_set.py`).
- **Fix.** Bound the recursion (a depth counter in `derDecodeRec`, refused
  above e.g. 32 — a CRL nests four or five levels) and bound the download
  with `io.LimitReader(resp.Body, limit+1)` before reading, `limit` =
  `DataFileSize` when the point states one, else a hard cap (1 MiB covers
  any Matter CRL); a body over the limit is a rejection. Add a fuzz seed
  and a unit case with a 1000-level nesting.
- **Confidence.** Confirmed. A scratch copy of `derDecodeRec` fed the
  nested input: 100 000 levels parse within a 64 MiB stack cap, 200 000 do
  not; at the default cap 3 500 000 levels (21 000 003 bytes) end with
  `runtime: goroutine stack exceeds 1073741824-byte limit` (scratch at
  `/tmp/claude-1000/-home-markus-Dokumente-GitHub-go-fabric/4d457ac2-e167-45db-b3db-00744a2c2d9f/scratchpad/derdepth/main.go`).

## W4-ctlint-02 — medium — the OTA image download is buffered in memory without a size bound

- `controller/softwareupdate/images.go:454-461` (`bytes.Buffer`,
  `ledger.Download`), `controller/dcl/ota.go:238-277` (`Download`,
  `io.Copy` at `:273` under a 5 min timeout).
- **Claim.** The ledger states the image's size (`SoftwareVersion.OTAFileSize`,
  `types.go`), but `Download` copies the whole response into the caller's
  buffer and `DownloadUpdate` holds it in RAM (and `Store` clones it again
  through `Blobs.Put`, `images.go:122-127`). A vendor's OTA host that
  answers more than it announced — misconfigured or compromised — can
  take the controller's memory for up to five minutes per product checked;
  the periodic check runs unattended (`manager.go:340-353`).
- **Reference.** matter.js streams the response into storage
  (`DclOtaUpdateService.ts:697`, `store` → `fileDesignator.writeFromStream`
  `:573`) and verifies on read-back, so a large answer costs disk, not
  heap; it has no size bound either.
- **Fix.** In `Download`, wrap the body in `io.LimitReader(body, OTAFileSize+1)`
  when the record states a size and refuse an overrun as `OTAError`; add
  an `ImagesConfig.MaxImageSize` cap for records without one. Optionally
  let `Blobs` take an `io.Reader` so the image is not held twice.
- **Confidence.** Confirmed (code); the impact needs a hostile or broken
  vendor host.

## W4-ctlint-03 — medium — `Images.Store` serves an image whose payload was never verified

- `controller/softwareupdate/images.go:212-235` (`Store`: `ReadHeader` +
  `verifyHeader` only), `:237-249` (`verifyHeader` compares vendor,
  product, version).
- **Claim.** `Store` is the public entry for a host's local images
  (`ModeLocal`) and checks the 16-byte prefix and header TLV only; the
  payload digest, the payload size and the whole-file checksum are not
  computed. A truncated or corrupted local image is listed by `Find`,
  offered by `UpdateExistsFor` (`manager.go:700-752`) and served over BDX;
  the requestor's own check fails only after the transfer. The ledger path
  is safe because `DownloadUpdate` runs `dcl.VerifyImage` first
  (`images.go:462-464`).
- **Reference.** matter.js's `store` writes, then re-reads and validates
  the stored file through `#verifyUpdate` → `OtaImageReader.file`
  (`DclOtaUpdateService.ts:573-574`, `:388-424`), which checks the payload
  size and digest (`OtaImageReader.ts:65-110`, `#processPayloadDigest`
  `:246-278`) and the full checksum when the info carries one, deleting the
  file on failure (`:590-604`).
- **Fix.** In `Store`, run `otaimage.ValidateFile(bytes.NewReader(image),
  FileOptions{SkipFullChecksum: true})` (payload digest and size) before
  `Put`, and the checksum when `info` carries one; refuse with `ErrOTA`.
- **Confidence.** Confirmed.

## W4-ctlint-04 — low — BlueZ `WriteC1` has no timeout while the BTP session writes under a never-cancelled context

- `ble/bluez/central_linux.go:548-556` (`WriteC1`: `call(ctx, …)` with the
  caller's ctx only); `controller/ble/channel.go:127-128` (`gattCtx :=
  context.WithoutCancel(ctx)` for every session write); the session calls
  the write callback at `transport/btp/session.go:484` and `:624`.
- **Claim.** A `WriteValue` that BlueZ never answers (a stuck ATT request,
  a D-Bus daemon hiccup) blocks the BTP send path indefinitely; the only
  bound is the peripheral-side `AckTimeout` (15 s, `transport/btp/consts.go:59`),
  which fires only when a packet was actually sent. The ESPHome transport
  bounds the same write by 30 s and ends the connection on overrun
  (`ble/esphome/gatt.go:145-183`, `:235-239`).
- **Reference.** matter.js's session write callback is unbounded too
  (`packages/nodejs-ble/src/NobleBleChannel.ts:776`, `writeAsync`); only
  the handshake writes are wrapped in `Abort` (`:603-640`). chip bounds
  every GATT write in its BlueZ layer through the Linux platform's
  `BLE_CONNECT_TIMEOUT`/ATT timeout path; not read at the pin.
- **Fix.** Bound `WriteC1` with `context.WithTimeout(ctx, bleTimeout)` as
  `ble/esphome` does (30 s) and translate the timeout into
  `btp.ErrDisconnected`, so the session closes instead of hanging.
- **Confidence.** Plausible (the hang needs BlueZ to withhold the reply).

## W4-ctlint-05 — low — a late ESPHome GATT reply can be taken for the next operation's

- `ble/esphome/gatt.go:118-143` (`onReply` matches `typ` + `handle`),
  `:145-183` (`do`: on `ctx.Done` the op is unpended but the request is in
  flight).
- **Claim.** The ESPHome API carries no request id. After a cancelled
  `WriteC1` (ctx ended, `:180-182`), the proxy's `BluetoothGATTWriteResponse`
  for C1 still arrives and the next `WriteC1` on the same handle takes it as
  its own reply, so one segment's write is reported before the proxy
  wrote it. The timeout branch (`:175-179`) avoids this by ending the
  connection; the ctx branch does not. In the channel's session the write
  context is never cancelled (`controller/ble/channel.go:128`), so this
  needs a caller cancelling `Dial` during the handshake write and reusing
  the connection — rare.
- **Reference.** aioesphomeapi matches replies by address and handle alone
  (`client.py:1215-1244` as cited in `gatt.go:55-58`); the limitation is
  the API's.
- **Fix.** Treat a cancelled operation like a timed-out one (`markGone`),
  or keep a "discard the next reply of (typ, handle)" mark until it arrives.
- **Confidence.** Confirmed (code), low exposure.

## W4-ctlint-06 — low — `IsApplicable` reads an absent `maxApplicableSoftwareVersion` as 0

- `controller/dcl/ota.go:222-235`; the JSON field is a plain `uint32`
  (`types.go`, `rawSoftwareVersion.MaxApplicableSoftwareVersion`).
- **Claim.** A version record without the field decodes to 0, so
  `current <= 0` fails for every current version and the update is never
  offered; the comment (`:224-226`) argues the ledger always states both
  bounds. The DCL schema marks both fields required, so the divergence
  only shows for a record that omits it.
- **Reference.** matter.js checks `maxApplicableSoftwareVersion !== undefined
  && currentVersion > max` (`DclOtaUpdateService.ts:806-815`): absent is
  unbounded.
- **Fix.** Decode the bound as `*uint32` and treat nil as unbounded, or
  record the assumption as a `by_design` row with the ledger schema cited.
- **Confidence.** Confirmed (code), practical impact needs a schema change.

## W4-ctlint-07 — low — `otaimage.ReadHeader` accepts any header size the prefix states

- `bdx/otaimage/otaimage.go:259-275` (`io.ReadAll(io.LimitReader(r,
  HeaderSize))`, allocating as the stream delivers).
- **Claim.** A prefix claiming a 4 GiB header costs what the stream
  delivers (the comment says so); every caller in this module feeds
  in-memory bytes, so the exposure is bounded by W4-ctlint-02. A direct
  consumer streaming from the network pays the full claim.
- **Reference.** chip caps the header TLV at 1024 bytes
  (`src/lib/core/OTAImageHeader.cpp:53` `kMaxHeaderSize`, `:128`); matter.js
  has no cap (`OtaImageReader.ts:157-191`).
- **Fix.** Refuse `HeaderSize > 1024` as `ErrInvalid` before reading; an
  *Ahead of matter.js* entry naming chip's constant.
- **Confidence.** Confirmed.

## W4-ctlint-08 — low — the OTA filename pattern is case-sensitive, matter.js's is not

- `controller/softwareupdate/images.go:160` (`filenamePattern`, no `(?i)`).
- **Claim.** matter.js's `OTA_FILENAME_REGEX` carries the `i` flag
  (`DclOtaUpdateService.ts:55`), so `FFF1.8000.PROD.3` is a valid
  designator there and refused here by `Open`/`Delete`/`Find`
  (`images.go:280-339`). `fileName` writes lower case (`:203-205`), so only a
  host-chosen name differs; `entry` would then also read `Mode("PROD")`,
  which matches no `Mode` constant.
- **Fix.** `(?i)` on the pattern and `strings.ToLower` on `parts[2]` in
  `entry`, or document the lower-case contract on `Blobs`.
- **Confidence.** Confirmed.

## W4-ctlint-09 — note — a CD signer fetched by SKID is trusted without a chain check (mirrored)

- `controller/dcl/certstore.go:491-538` (`CDSigner`, `fetchCDSigner`: the
  first certificate of `/dcl/pki/all-certificates?subjectKeyId=` is stored
  as a production CD signer and its key returned).
- **Claim.** Any ledger-approved certificate whose SKID field equals the
  CD's signer SKID is accepted as the CD signer; the SKID is a free field
  of the certificate's author. The snapshot layer answers the five known
  signers first (`by_design` BD-Controller-DclSnapshotLayered), so only an
  unknown SKID reaches this path, and an unknown signer otherwise skips the
  CD check with a warning (`controller/attestation/validate.go:123-138`).
- **Reference.** Mirrors matter.js `getOrFetchCdSigner`
  (`DclCertificateService.ts:864-891`: `certs[0]`, `addCertificate(…,
  "CDSigner", { isProduction: true })`). chip accepts CD signers from a
  fixed set only (`src/credentials/attestation_verifier/
  DefaultDeviceAttestationVerifier.cpp:146-256`).
- **Fix.** None required for parity; a `by_design` row stating the trust
  decision, or a chip-shaped tightening (accept only a signer issued by
  the CSA CD root) as an *Ahead of matter.js* entry. Owner decision.
- **Confidence.** Confirmed (code and reference).

## W4-ctlint-10 — note — unbounded response bodies on the trusted HTTP paths

- `controller/dcl/client.go:161` (`fetchJSON`, `io.ReadAll`),
  `controller/dcl/github.go:200` (`fetch`), `thread/otbr/client.go:134`
  (`do`); `fetchPaginated` (`client.go:189-220`) follows `next_key` without
  a page cap.
- **Claim.** All three read the whole body; the ledger and GitHub are
  reached over TLS with the default verifier (`http.DefaultClient`,
  `client.go:98-103`), the OTBR over plain HTTP on the LAN. A
  misbehaving endpoint costs memory, not safety.
- **Reference.** matter.js `response.json()` / `arrayBuffer()` (`DclClient.ts:91-121`,
  `Github.ts:120-141`, `OtbrRestClient.ts:757-788`) — no bound either.
- **Fix.** `http.MaxBytesReader` with a generous cap (e.g. 16 MiB) on all
  three, and a page cap on `fetchPaginated`.
- **Confidence.** Confirmed.

## W4-ctlint-11 — note — the OTBR client is HTTP only, the network key travels in clear

- `thread/otbr/client.go:50` (`"http://" + host`), `:598-604` (`setDataset`
  PUTs the operational dataset hex, network key and PSKc included).
- **Claim.** The REST API of ot-br-posix is served without TLS, so this is
  the surface's property; a host that reaches the border router across an
  untrusted segment exposes the Thread credentials. ADR 0027 names the
  REST surface and the dataset flow but no transport-security statement
  was found in the grep (`docs/adr/0027-…`, lines matching tls/https: none).
- **Reference.** matter.js's `OtbrRestClient` builds `http://` URLs the same
  way (cited throughout `client.go`).
- **Fix.** One sentence in ADR 0027 / the package doc: the REST link is
  plaintext and belongs on the border router's own LAN.
- **Confidence.** Confirmed.

## W4-ctlint-12 — note — `CertificateStore.Close` waits for a multi-minute update

- `controller/dcl/certstore.go:200-216` (`Close` → `awaitUpdate` on
  `fetchMu`), `:186-198` (the timer update runs under `context.Background()`),
  `:697-715` (one 5 s-bounded fetch per root, 74 in the production ledger).
- **Claim.** A `Close` during the daily update blocks until every
  remaining root fetched or timed out — minutes on a slow ledger — and the
  host's shutdown waits with it.
- **Reference.** Mirrors matter.js (`close` `:896-906` awaits
  `#fetchPromise`).
- **Fix.** Keep a cancellable context for the periodic update and cancel
  it in `Close` before `awaitUpdate`.
- **Confidence.** Confirmed.

## W4-ctlint-13 — note — ESPHome's silence watch presumes a lost subscription in a BLE-quiet environment

- `ble/esphome/proxy.go:339-356` (`checkSilence`), `central.go:27`
  (`DefaultSilenceTimeout` 30 s).
- **Claim.** A proxy that saw no advertisement of any kind for 30 s is
  reconnected, and a second such silence marks it busy until the next
  fresh scan. In a shielded lab or a very quiet site this is a false
  positive; the by_design entry (BD-Controller-BleOverEsphomeProxies)
  records the mechanism but not the false-positive case.
- **Fix.** Mention it in the entry and the package doc; a host that sees
  `ble.esphome.proxy_busy` with no other client should raise
  `SilenceTimeout`.
- **Confidence.** Confirmed.

## W4-ctlint-14 — note — Go API consistency

- `thread/otbr/probe.go:49` stamps `ProbedAt` with `clock.System{}` while
  the rest of the package takes a `clock.Clock` (`borderrouter.go:36-39`);
  `thread/otbr/translate.go:197` and `ble/esphome/discover.go:203` log on
  `slog.Default()` where the services around them take a `Logger`.
  `controller/dcl.(*Client).Timeout` is a field, `thread/otbr.Config.Timeout`
  a config value, `ble/esphome.Options.ConnectTimeout` an option — three
  spellings of the same knob. `ReadHeader` maps every read error, a
  transport error included, to "OTA file ended unexpectedly"
  (`otaimage.go:263-265`), hiding the cause from `errors.Is`.
- **Fix.** Thread a clock/logger through `Probe`/`Translate`/`Discover`;
  wrap the underlying read error in `ReadHeader`.
- **Confidence.** Confirmed.

---

## Checked and sound

- **BTP handshake parameters.** `controller/ble/channel.go:158-183` sends
  the segment size (ATT_MTU − 3) in the request's ATT_MTU field with the
  maximum window, timer `btp.ConnRspTimeout` armed before the write — as
  matter.js `performBtpHandshake` (`NobleBleChannel.ts:603-626`). chip's
  peripheral subtracts 3 again (`src/ble/BLEEndPoint.cpp:991-1004`), a
  matter.js quirk this module mirrors knowingly. Write-then-subscribe
  order matches `:636-642`; the handshake response is routed before the
  session exists (`channel.go:73-99`); a lost GATT link closes the session
  and the channel (`:185-193`); `Close` is idempotent through `closeOnce`.
- **ESPHome transport.** Frame length bounded (`internal/wire/frame.go:96`,
  `noise.go:255`); the framers serialise writes with a mutex
  (`frame.go:51`, `noise.go:90`), so the keep-alive, the GATT operations and
  the ping replies do not interleave a Noise frame. One GATT operation at a
  time (`gatt.go:146`); a timeout ends the connection; indications are
  delivered in order on the connection's goroutine (`:344-376`); `markGone`
  is never called with `proxy.mu` held (`proxy.go:570-573`, `:358-380`);
  lock order `setMu` → `proxy.mu` → `Central.mu` holds in every path read.
  Connect uses the advertisement's address type (`proxy.go:574`), takes the
  MTU from the connect response and refuses a database without C1/C2/CCCD
  (`gatt.go:188-227`). Reconnect backoff 1 s → 60 s; `Close` cancels every
  run loop and waits. Advertisement parsing is bounds-checked
  (`adv.go:35-57`).
- **BlueZ transport.** Scan filter without `UUIDs` (by_design
  BD-Controller-BluezScanWithoutUuidFilter); service data selected by UUID
  and length 8; one connection per device; the disconnect-between-resolve-
  and-register window re-checked (`central_linux.go:489-495`); `Connected
  = false`, `InterfacesRemoved` and a closed bus end the connection; C2
  values queued and delivered in order; `StopNotify`/`Disconnect` bounded
  by 5 s as noble's.
- **Scanner.** `transition` bounded by 10 s with the client call cancelled;
  a failed start fails every waiter and leaves `scanning = true`
  (`BleScanner.ts:192-193`); `Close` queues behind a running transition;
  the waiter of `FindContinuously` is registered under the closed check
  (#306 fix); stale/age rules follow `:260-297`.
- **OTA provider side.** The token and consent checks of
  `ApplyUpdateRequest` live in `cluster/core/ota_provider.go` (its parity
  test `ota_provider_parity_matterjs_test.go:183-193` pins Discontinue 120
  s), so `providerPorts.ApplyUpdate` answering Proceed with the rounded,
  day-capped delay (`provider.go:106-113`) matches `:466-482`.
  `UpdateExistsFor` mirrors `updateExistsFor` (`manager.go:700-752`); the
  queue, stall reset (`ProgressTimeout` 15 min + apply delay), the StartUp
  suppression by session id, the apply-failed detection on re-subscribe
  and `cancelUpdate` follow `SoftwareUpdateManager.ts`. Observers deliver
  off the lock; `WaitGroup.Add` always precedes `Wait` (closed flag under
  the same lock, `manager.go:355-383`, `announcements.go:134-166`, `:232-243`).
  `Open` refuses any designator outside the pattern, which admits no path
  traversal (`images.go:160`, `:329-339`).
- **DCL client.** TLS verification is the default client's; every record
  is checked for the vid/pid/version asked and the schema version
  (`client.go:258-422`); `otaFileSize` strict decimal and `dataFileSize`
  `parseInt`-like exactly as `DclClient.ts:388`, `:416`; `encodeURIComponent`
  byte-exact; NOT_FOUND mapped by code 5.
- **Trust store.** DER ≤ 600 bytes, required extensions, SKID stated vs
  derived (`cert.go`, `certstore.go:767-781`); never downgrades production
  to test; atomic file save (`store.go`, temp + rename); the stored index
  is the host's store. `PAA` answers stored certificates only (by_design
  MirroredScope). Attestation on an unreachable ledger: PAA lookup fails
  closed (no certificate, no match); revocation fails open — not revoked —
  as matter.js `:715-718` (documented at `revocation.go:186-216`).
- **CRL authentication.** AKID = signer SKID, issuer name = signer
  subject (RFC 5280 §5.1.2.3), partitioned-CRL IDP URI, ecdsa-with-SHA256
  over the re-encoded TBS, unknown critical extensions refused, indirect
  CRL entries of another issuer dropped (`revocation.go:364-437`,
  `crl.go:324-499`); signer forms and chain anchoring per §6.2.6.1 steps
  2-5 and §11.23.11.6 (`revocation.go:439-624`), the self-signed PAA signer
  compared byte for byte with the approved PAA. One fill per AKID, failed
  fills not cached, incomplete lookups dropped (`:99-165`). Installed
  revocation sets canonicalise serials on both sides (`:662-672`).
- **OTA image codec.** Every header field typed and bounded
  (`otaimage.go:100-221`: uint widths, string length in UTF-16 units 1–64 /
  1–256, digest ≤ 64 bytes, mandatory tags 0,1,2,3,4,8,9), payload size and
  digest verified before success, total size vs expected with the
  checksum-wins rule of `OtaImageReader.ts:89-106`; the fuzz target covers
  the reader.
- **OTBR.** Host bracketed for IPv6, 5 s per request, 204 handled, JSON
  typed with `UseNumber` and range checks, hex lengths enforced, `rloc16`
  both forms (`json.go`); `Probe` returns `nil, nil` on any REST error as
  `OtbrRestProbe.ts:42-46`; dataset activation disables the interface
  first when one is active (the 409 of `OtbrRestClient.ts:421-422`), polls
  `/node/state` until attached or the fail-safe's ctx ends, and the revert
  tolerates the 405 of a firmware without DELETE (`borderrouter.go:151-209`).

## Not read

- `cluster/core/ota_provider.go` (W3) beyond the parity-test lines cited;
  `transport/btp` (W1) beyond `session.go` call sites and `consts.go`;
  `bdx/` outside `otaimage/`; `thread/` outside `otbr/`
  (`DecodeDatasetHex`); `controller/attestation` beyond `validate.go:110-145`
  and the `CDSigner` call sites; `client/discovery`.
- `ble/esphome/internal/wire/*.go` beyond the grep for frame bounds and
  mutexes; `ble/esphome/doc.go`, `ble/bluez/doc.go`, `controller/dcl/doc.go`,
  `controller/softwareupdate/doc.go`.
- All `*_test.go` except `controller/dcl/fuzz_revocation_test.go` (target
  list) and `cluster/core/ota_provider_parity_matterjs_test.go:183-193`.
- `docs/adr/0024`, `0027` beyond the grep slices; `notes/parity/by_design.md`
  beyond the entries listed in the header's grep (DCL-OptIn,
  DCL-MirroredScope, DclSnapshotLayered, BleReliableExchange, BtpRejections,
  ThreadDatasetNameOnly, BluezWithoutNobleRetries, BleOverEsphomeProxies,
  BluezScanWithoutUuidFilter, BTP-NoStallRenegotiation).
- chip `src/ble/BtpEngine.cpp`, `src/protocols/bdx/*`,
  `src/app/clusters/ota-provider/*`, `src/platform/OpenThread/*` at the pin
  (only `BLEEndPoint.cpp:991-1077` and `OTAImageHeader.cpp:50-139` were
  read).
