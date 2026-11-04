# Brief M9 module wave — the facade and client facilities go-fabric-ui U4 needs

Date 2026-11-04. Milestone 9 (UI), after phase 32 merges. One Opus agent.
The specification is **issue #388** (G-N1 facade attribute values, G-N2
element names in `schema`, G-N3 served events + `Observers.OnEvent`, G-C2
the per-node event ring in `controller/client`, the roadmap version fix),
written as a complete handover with the verified lines; this brief adds
the working rules, G-N4 and the order. Owner decisions 2026-11-03/04:
the controller inventory reads the cached image only (U4 0.1); the event
log is a bounded per-node ring in go-fabric's controller client, the node
side through the facade (0.3); live values by refetch on `node_updated`,
node-side `attribute_changed` ids (0.4); G-N4 is fixed in this wave and a
listed-but-unread cluster keeps 503 in the UI (not this module's concern).

## 0 Working rules

Branch `m9/module-wave` from `origin/main` after the phase-32 merge;
worktree `../go-fabric-m9`; logs and the summary log under
`~/.cache/go-fabric-scratch/m9mod/`; guards under `flock
~/.cache/go-fabric-scratch/guards.lock` (`make ci`, `make race`,
`make cover-check`, `make reachability-check`, `make cite-check`,
`make apidiff-check` — additive only —, `make ahead-check`, `mkdocs build
--strict`; no chip-tool run: the daemon's composition and FeatureMaps do
not move, say so in the report). `export PATH=$HOME/go/bin:$PATH` and
`GOTMPDIR=$HOME/.cache/gotmp`. Commits as `SukramJ <sukramj@icloud.com>`,
`<package>: <what> (M9 module wave)`, no attribution lines. Pull request
against main, body = report, auto-merge off. Security paths untouched.
Facade changes are judged as API design: one shape, no seam, the
developer's view (`node`, `device`, `contract` are the product).

## 1 Deliverables

- **G-N1, G-N2, G-N3, G-C2 and the roadmap fix exactly as #388 states
  them** (scope, shape, acceptance, parity notes). G-N2 is generated
  (`script/generate_matter_schema.go` → `schema/`), never hand-written;
  the generator test guards it. G-C2: read `packages/node/src/node/client/`
  at 862ad75f first; if matter.js's `ClientNode` keeps no event history,
  the ring is an *Ahead of matter.js* register row with that marker.
- **G-N4 — TagList decodes to null in the controller client** (found by
  go-fabric-ui U4b; #388 comment of 2026-11-04). Facts verified at
  53b0d93d: the server returns `cloneTags(d.tagList)` as `[]Semtag`
  (`cluster/core/descriptor.go:217-221`); the controller's cached image
  holds a TLV null for Descriptor attribute 4 (`DecodeJSON` and `Decoded`
  return nil without error, `controller/client/json.go:316,340-343`: a
  null element decodes to nil by design, so the null is *stored*, i.e.
  sent). The device-side tests read TagList through `MatterRead` only
  (`device/descriptor_device_types_tags_parity_matterjs_test.go:147`),
  never over the wire. The encoder is `bridge/reply.go:733`
  `defaultAttributeValueWriter` (an `im.AttributeValueWriter`, the type
  switch the dispatcher's read values go through). Root cause located
  2026-11-04 (Fable, at 53b0d93d): the switch has no case for
  `[]descdef.Semtag` and falls to `default:` → `enc.PutNull` — the same
  class of gap the file's own comments name at `reply.go:161` and `:222`
  — so every host with `device.Options.Tags` sends a null TagList today.
  Fix it where the value is encoded (a general rule for a slice of a
  type that implements `EncodeTLV`, not one more case), not in
  Descriptor. Test over the wire: a
  fabrictest bridged plug built with `device.Options.Tags` (phase 30
  deliverable 7), the controller reads TagList and the image holds the
  entries (`Cluster(0x1D).Value(4)`, `DecodeJSON` NameBased renders
  `mfgCode`, `namespaceId`, `tag`, `label`). Check LabelList (FixedLabel/
  UserLabel, `[]LabelStruct`) on the same path; a chunked-write test
  already covers UserLabel writes
  (`controller/client/client_chunked_write_parity_matterjs_test.go:58-95`).
  CHANGELOG `### Fixed`.

- **SpecificationVersion behind a seam** (owner decision 2026-11-04 on
  the phase-32 handback). Phase 32 (#390) added `cluster/core`
  BasicInformation `Config.SpecificationVersion`, an exported TESTING
  ONLY field reached through `internal/nodeseam.UseSpecificationVersion`,
  so a controller test can stand in for a 1.3.0 node
  (`IcdClientTest.ts:567-582` at 862ad75f). The facade rule — no
  testing-only field in a public config — moves it behind an unexported
  seam: the field goes; `cluster/core` keeps the served version in the
  server and exposes it only through a function variable
  `internal/nodeseam` installs (the `internal/bridgeseam` /
  `internal/channelseam` pattern: declaration-only package, the install
  from an init, `script/archcheck/layers.json` and the `coverfloor`
  exemption as `internal/exchangeseam` has them). `TestIcdClientKeyStoreParity`
  #50 (the 1.3.0 node) keeps passing through the seam. The field is in
  `[Unreleased]`, so no break is announced: amend the phase-32 `### Added`
  line (`CHANGELOG.md`, "BasicInformation `Config.SpecificationVersion`")
  to name the seam instead of the field. `make apidiff-check` stays
  additive against the last tag.

## 2 Report shape

Per deliverable, guards with HEAD and exit, differences from #388,
decisions, left out, open questions, by_design/Ahead rows written,
citations re-read at 862ad75f, the G-N4 root cause in one paragraph.
