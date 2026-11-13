# Brief (draft) — the controller keeps a node's onboarding code, and serves an inventory

Status: implemented on `w16/onboarding-code-inventory`, 2026-11-13 (issue 519). Pins: matter.js **db021da0**
(`parity/schema.json:7`), matterjs-server 0bd06f2. Owner decisions 2026-11-13: the module stores and
serves the code, go-fabric-ui renders and prints; a code can also be attached **after** pairing
(typed, scanned or photographed). Open decisions are at the end; they go to the owner as a wizard.

## Why

A device is paired with its QR code or manual code; the paper is then lost and the label on the
device is hard to reach. The code is needed again after a factory reset. It cannot be read back from
a commissioned device: the device holds a PAKE verifier, and `OpenCommissioningWindow` mints a new,
time-limited code (`controller/client/window.go:121-149`). The only place the original code exists
in software is the controller's input at `Controller.Pair` — and today it is dropped there
(`controller/controller.go:791` parses, `:810` hands the passcode to PASE, nothing persists it;
`grep -i passcode client/peer store` has no hit).

## Better than, and the right cut (architecture standard)

- **matter.js** (db021da0) does not keep the code: `CommissioningClient.State`
  (`packages/node/src/behavior/system/commissioning/CommissioningClient.ts:959` ff.) persists
  `discriminator` (`:1046`), vendor and product, but has no passcode or pairing-code field; the
  passcode is only passed on to the commissioning options (`:290-291`). This is therefore an *Ahead of
  matter.js* row with that marker.
- **matterjs-server** (0bd06f2): **unverified.** A grep shows the code decoded for commissioning
  (`packages/ws-controller/src/controller/ControllerCommandHandler.ts:1975-1998`) and treated as a
  secret in logs (`packages/ws-client/src/logging-redaction.ts:14-15`); whether anything stores it
  was not read. Read before this claim enters `by_design.md`.
- **Before:** nothing; a host that wants an inventory keeps its own copy before calling `Pair`.
- **Cut:** the module owns capture, validation, storage and the inventory rows (strings only);
  go-fabric-ui owns the QR image, the print page and the camera/photo decode; go-fabric-server wires
  the two; device-side hosts (loom, go-ha2fabric) change nothing. No image or QR-matrix dependency
  enters this module.

## §1 Storage — beside the record, not in it

- Not a `client.Record` field: `Node.Record()` is rendered by diagnostics and UIs, and the code is a
  secret. A separate store cluster id next to `recordCluster`/`settingsCluster`/`icdCluster`
  (`controller/client/record.go:28`, `network.go:258`, `icd.go:642`), written with `n.store.Save`.
- Stored: the payload as entered, normalised (`MT:…` or the 11/21 digits), the source
  (`paired` | `attached`) and the time. Nothing derived is stored.
- Deleted with the node (`Registry.Forget`, `Node.Erase`, `Controller.Erase`) — verify that a
  per-node store delete covers the new cluster id; add the test.
- Never logged, never in a `diag` event or trace detail. Check `tracePairing`
  (`controller/diagnostics.go:235`) and `recordPaired` (`:274`) for what they emit today.

## §2 Capture at pairing

`Controller.pair` keeps the validated code string and, on success, saves it to the new node before
`Pair` returns. Gated by a `Config` switch (decision 1). A save failure is logged and does not fail
the pairing.

## §3 Attach after pairing — the owner's addition

`Node.SetOnboardingCode(ctx, code string) error`, `Node.ClearOnboardingCode(ctx)`. The module takes
a **string**; scanning and photographing produce that string UI-side (§5).

- Validation: `ParsePairingCode` (`controller/code.go:58`) — checksum, passcode, identity.
- Plausibility against the record, where both sides carry the value: QR vendor/product against
  `Record` vendor/product; QR long discriminator against the recorded one; a manual code's 4-bit
  short discriminator against the recorded one's upper bits; a 21-digit manual code's vendor/product
  likewise. A record field that is unknown is skipped, and the result says which checks ran.
- **What this cannot prove:** that the passcode is the device's. A commissioned device offers no
  way to test a passcode without a commissioning window. The row is marked `attached, identity
  matched` or `attached, unchecked`, never `verified`. (A real proof — PASE against an opened basic
  window — is out of scope; open question.)
- Mismatch handling is decision 2.

## §4 Inventory

`Controller.Inventory() []InventoryEntry`, one per commissioned node: node id, the name the
controller already shows (`controller/client/label.go`), vendor/product/serial from the cached
BasicInformation, QR string and manual string, source, the checks that ran, and "no code" for nodes
without one. From a QR payload both strings are produced (`secure/setup/setup.go:111` and the
manual encoder `node/credentials.go:102` uses); from a manual code only the manual string — a QR is
**not** synthesised from recorded discovery values (flow and capabilities would be guesses).

## §5 go-fabric-ui / go-fabric-server (separate issues there, referencing this one)

- Page "Inventar": table and a print stylesheet (one label per node: name, QR image, manual code).
  The QR image is rendered in the UI from the string.
- Attach: type the code; scan with the camera; or pick/take a photo and decode it in the browser.
  Live camera needs a secure context (HTTPS or localhost); the photo upload works without — state
  this on the page instead of failing silently.
- The inventory route is not cached and is behind the UI's existing access control; an export is an
  explicit action.

## Tests and registers

- Unit: capture on pair (on/off), attach (each plausibility branch, unknown record fields), clear,
  delete-with-node, survives restart, no code in any log or trace (assert on the captured handler).
- No matter.js test to port (nothing there). `by_design.md`: one *Ahead of matter.js* row, marker
  `CommissioningClient.ts:959` ff. / `:290` at db021da0. CHANGELOG `### Added`. Docs: the controller
  page gains a section, including the security note below.
- Security path (credentials): Fable work, per "Which model does which work".

## Security note (goes into the docs verbatim in meaning)

The inventory is as sensitive as the paper it replaces: with a code and physical access (factory
reset) a device can be taken over. The store file and every export need the protection the fabric's
own credentials have.

## Decisions (owner, 2026-11-13) and what the implementation found

1. Capture at pairing is **opt-in**: `controller.Config.KeepOnboardingCodes`.
2. An attached code that differs from the node is **refused** (`client.ErrOnboardingMismatch`).
3. Implemented at once on `w16/onboarding-code-inventory`.

Found while implementing, and built in:

- **Enhanced Commissioning Window.** A device paired through another administrator's window (CM=2)
  was paired with the window's passcode and advertised the window's discriminator. That code is not
  kept, and the discriminator check of an attach is skipped for such a node — otherwise the genuine
  label code would be refused.
- **Vendor/product** are compared with the cached Basic Information first, the advertised ids second.
- **Long manual code.** A QR code with a non-standard flow yields the 21-digit manual code
  (connectedhomeip `ManualSetupPayloadGenerator.cpp:116` at 6170af84).
- **Shape.** `client.CommissionOptions.OnboardingCode` carries the code into `Registry.Commission`;
  `Node.SetOnboardingCode`/`ClearOnboardingCode` take no context (the store has none).
- matterjs-server (0bd06f2): QR rendering exists for windows it opens only (grep, not a full read).

Open: a real passcode proof for an attached code (PASE against an opened basic window); whether a
code should outlive `Decommission` (today it is deleted with the node).
