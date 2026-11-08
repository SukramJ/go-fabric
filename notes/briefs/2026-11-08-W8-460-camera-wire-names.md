
# Brief: issue 460 — controller/camera wire names, a test camera for hosts, ICE/Value/bounds (2026-11-08)

Owner 2026-11-08: "Nimm #460 mit auf" — in wave W8 together with #457
(brief `2026-11-08-457-factory-reset-counter.md`), one Opus, one PR, after
W7 (#458) merges. Disjoint paths: #457 endpoint store, #460
`controller/camera`. Host hand-over from go-fabric-server S8
(SukramJ/go-fabric-server#53). Pins: matterjs-server 0bd06f2
(`git -C ../matterjs-server show 0bd06f2:<path>`), matter.js db021da0.

All citations below verified 2026-11-08 at the pins.

## (a) Exported wire names

`controller/camera/policy_sdp.go:787-824` holds `videoCodecNames`,
`audioCodecNames`, `videoCodecName`, `audioCodecName` (unexported);
`manager_snapshot.go:17-24` `imageCodecName` as a switch. matterjs-server
`packages/ws-controller/src/camera/wireNames.ts:16-31` has the three maps
(video H264/H265/H266/AV1, audio OPUS/AAC, image JPEG/HEIC), `:44-46`
`wireName` (decimal fallback), `:48-54` `wireValue` (case-insensitive),
`:55-73` the exported name/byName functions, `imageCodecByName` accepting
the decimal spelling (`:68-73`).

Export: `VideoCodecName(csdef.VideoCodecEnum) string` /
`VideoCodecByName(string) (csdef.VideoCodecEnum, bool)`, the same for
Audio, Image, `StreamUsageName/ByName`, `TwoWayTalkSupportName` — one
table per enum, the ByName case-insensitive with the decimal fallback as
matterjs-server. Keep the citations; move the image switch onto a table.
Fix the narrow cite at `policy_sdp.go:790` (`:11-21` → `:16-21`).
Tests: a parity table test against the names of `wireNames.ts:16-31`;
ByName round-trips, case, decimal fallback, unknown.

## (b) A test camera for hosts

`service_fabrictest_test.go:410` `newAcceptanceCamera`, `:445-457`
`cameraPort` (offers answered, candidates and ends recorded, numbered
allocations, settable refusals). Lift it into a public package for hosts'
in-process tests: `controller/camera/cameratest` (the camera device plus
its scripted port; signals `Offer`, `Answer`, `IceCandidates`, `End`
callable without importing `cluster/camera`). go-fabric's own acceptance
test uses it; apidiff additive. Document in docs (the controller page's
testing section) one paragraph.

## (c) ICE candidates from JSON

`session.go:189` `Session.ProvideICECandidates(ctx, []wpdef.ICECandidateStruct)`;
the JSON→struct conversion is unexported (`provider_arguments.go:591`
`toProviderCommandFields`, struct case `:323`). Add
`camera.ParseICECandidates(json.RawMessage) ([]wpdef.ICECandidateStruct, error)`
on the same converter (one code path, no second parser), errors in the
converter's wording. Test: the server's cases (a candidate object, the
list, a wrong key) ported as a table.

## (d) A typed value from Provider.Send

`provider_route.go:35-46` `Provider.Send` returns `result any` (the
generated response struct or nil); the only JSON route is
`controller/client/json.go:301` `DecodeJSON(f Field, v Value, form)`.
Add `client.ValueOf(spec.Encodable) (Value, error)` so a host renders the
result through `DecodeJSON` without its own encoder — the smaller surface
(Send's signature stays). Test: ProvideOfferResponse through ValueOf +
DecodeJSON equals the generated JSON of the server's fixture.

## (e) Stated zero in the bounds

`session.go:61-69` `VideoBounds` (`MinFrameRate, MaxFrameRate uint16`,
`MinBitRate, MaxBitRate uint32`) and `AudioBounds`: 0 is unstated
(`checkRange`, `:471-478`), so a stated 0 passes where the model's floor
is 1 (`cluster/spec/cameraavstreammanagement/definition_gen.go:640`
MinFrameRate "1 to maxFrameRate") and matterjs-server refuses it
(`cameraCommands.ts:378-382` `toOptionalNumber` against
`CAMERA_FIELD_RANGES.minFrameRate`; `cameraFieldRanges.ts:48-56`
`fieldRange`: min from the constraint, 1 here). Decision for the wave:
pointer fields (`*uint16`/`*uint32`; nil = unstated) — the idiomatic Go
mapping of an optional JSON number, one shape, no flag beside a value.
It is a **Break** on `controller/camera` (CHANGELOG `### Changed` Break
line naming `VideoBounds`/`AudioBounds`, migration-guide section
"Changed after v0.18.0: the camera bounds"); the only consumer is
go-fabric-server (S8), which adapts with its bump. `checkRange` then
refuses a stated 0 below the floor exactly as matterjs-server.
Tests: stated 0 refused, nil passes, the existing range cases.

## Guards

Full bundle under the lock; apidiff (additive for a–d, the Break of e
named); `mkdocs build --strict`. No chip-tool run: device composition
unchanged (say so). The camera fabrictest family (`service_fabrictest_test.go`)
must pass under `-race`.

## Report

Per § (a)–(e) and the #457 section: files, guards with HEAD, differences,
decisions, left out, questions; by_design candidates: none expected.

