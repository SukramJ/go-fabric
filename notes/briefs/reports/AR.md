# Report AR — the media and content clusters, the three media device types, the media families (ADR 0022)

Branch `wave24/media` from `planning/briefs-wave-24` (061b129), pushed, no
PR. matter.js read at `0ceb202d`, chip at the harness pin `6170af84`
(`git -C … show <pin>:<path>`; the tv-app sources and the media cluster
servers were read from the pin). Every commit carries its regenerated
reachability inventory; unreached stays **5** throughout, the ratchet is
untouched.

| Commit | Item |
| --- | --- |
| 825b253 `feat(cluster/spec): generate the fifteen media and content cluster definitions` | §1 |
| 18f4788 `feat(cluster/media): the fourteen media and content servers over host ports; cluster/core: the Messages server` | §2 |
| 675ed69 `feat(device): the media device types BasicVideoPlayer, CastingVideoPlayer and ContentApp, generated` | §3 |
| d50fec1 `feat(examples/reference-bridge): --media, … TESTING ONLY` | §4 |
| d7188d6 `test(internal/chiptool): the media families against the --media pair; the player serves audio and text tracks` | §5 (+ CHANGELOG, docs/certifiability.md) |
| c2c8569 `fix(controller/client): link the fifteen media definitions so spec.Lookup knows them` | §1 fix-up (found by `make ci`) |
| 8f5d2f3 `chore(script/coverfloor): floors for the fifteen generated media definitions` | §1 fix-up (found by `make ci`) |
| (this commit) `notes: report AR` | — |

## Per section

- **§1** — the fifteen names in `script/clustergen/clusters.go`
  (`// cluster/media (ADR 0022)` and `// cluster/core Messages`), generated.
  `go test ./script/clustergen/... ./cluster/spec/... ./schema/...` and
  `make chipdm-check` green. Two follow-ups `make ci` found later:
  `controller/client/known.go` must link every generated definition
  (`TestEveryGeneratedClusterIsKnown`; only ContentAppObserver was not
  linked transitively, all fifteen are listed), and the fifteen generated
  packages need floors (100, as every generated package).
- **§2** — `cluster/media`: one constructor per cluster,
  `media.NewX(cfg XConfig) (*XServer, error)` on `spec.NewServer`
  (Playback, Keypad, ContentLauncher, AppLauncher, AppBasic, AccountLogin,
  Outputs, Channels, Targets, Inputs, LowPower, WakeOnLan, ContentControl,
  ContentAppObserver). Shared plumbing in `media.go`: `StatusError{Status,
  Err}`, `ErrNilPort`, `ErrMissingState`, `ErrNotImplemented`, the generic
  `handle`/`handleState` (decode → port → status), and `stateful[S]`, the
  command transaction (see Decisions). LowPower `Sleep` → the port, cited to
  `LowPowerServer.ts:16-18`. Events (MediaPlayback StateChanged,
  TargetNavigator TargetUpdated, AccountLogin LoggedOut, ContentControl's
  two) are `EmitX` on the server, the endpoint stamped by `SetEndpoint`.
  Tests: `parity_matterjs_test.go` (all fourteen: `spectest.CheckDefinition`,
  id and revision from the element file, `spectest.CheckServer` on a full
  feature selection), per-cluster behaviour tests (port called with the
  fields decoded by the generated decoder, the response round-tripped
  through the generated codec, `StatusError` → its status, plain error →
  FAILURE and no commit, optional command not implemented → absent from
  AcceptedCommandList and UNSUPPORTED_COMMAND, `ErrNotImplemented`,
  `ErrMissingState`, the state as read). Coverage 93.1 % (floor 85).
  Messages: `cluster/core/messages.go` (+ `messages_test.go`) after
  `binding.go`'s shape — see Decisions.
- **§3** — the three names moved from `excluded` to `committed`
  (`script/devicegen/devicetypes.go`, comment updated), generated
  (`device/{basic_video_player,casting_video_player,content_app}_gen.go`,
  `types_gen*.go`). The generator expects no default factory for them:
  `defaultFactories` holds only port-less self-held servers, so the media
  servers are mounted with `device.WithServer` (an unconfigured default is
  the bare `spec.Server`, which answers every command UNSUPPORTED_COMMAND).
  The conditions are expressible: the generated `Type.Conditions` lists
  them and `device.Options.Conditions` states them to the validator, as the
  root's; ObserverClient also needs `device.WithClientClusters(0x0510)`.
  `device/media_test.go` assembles each type with the `cluster/media`
  servers and wants zero violations, and a ContentAppPlatform condition
  without ApplicationLauncher to be one. `device/registry_test.go` used
  CastingVideoPlayer as its "excluded" probe → CastingVideoClient now;
  `device/type.go`'s `Lookup` doc listed the media types (and OtaProvider)
  as excluded → corrected.
- **§4** — `examples/reference-bridge/media.go`, `--media` (TESTING ONLY in
  the flag text, the file doc and the README). Two bridged endpoints after
  the whole fleet (numbers of the existing devices unchanged): "player", a
  **CastingVideoPlayer** (see brief differences 1) stating
  ContentAppPlatform and PhysicalInputs with OnOff (device default),
  MediaPlayback (AS, VS, TT, AT, AA), KeypadInput (NV, LK, NK),
  ContentLauncher (CS, UP), ApplicationLauncher (AP, CurrentApp), MediaInput
  (NU), AudioOutput (NU), Channel (CL, LI, CurrentChannel), TargetNavigator
  (CurrentTarget), LowPower, WakeOnLan (MACAddress); "app", a **ContentApp**
  with ApplicationBasic, ApplicationLauncher (no AP), AccountLogin,
  KeypadInput. Every port mirrors the tv-app manager at the pin (cited per
  method: MediaPlaybackManager, KeypadInputManager, ContentLauncherManager,
  ApplicationLauncherManager + application-launcher-server's platform
  branch, MediaInputManager, AudioOutputManager, ChannelManager,
  TargetNavigatorManager, LowPowerManager, WakeOnLanManager,
  AccountLoginManager with the test app's PIN `34567890`). The player's
  LaunchApp/HideApp/StopApp move the app's ApplicationBasic.Status;
  CurrentApp names the app's endpoint (resolved by StableKey in
  `attachNodeProviders`). **App-pipe commands added:** one,
  `{"Name":"MediaPlaybackState","Operation":"Playing|Paused|NotPlaying|Buffering"}`
  — no media YAML case drives the app pipe (their state-change steps are
  `UserPrompt`s under `PICS_USER_PROMPT=0`), so none was needed by a family;
  the tv-app has no app pipe, so the command is this daemon's. `node/` is
  not touched: the pair goes through `Aggregator().Add` like every other
  fleet device. Proof: `media_test.go` (flag on/off, numbering, no
  violations, the MEDIAPLAYBACK 6.1–6.4/6.9, CHANNEL, MEDIAINPUT,
  AUDIOOUTPUT, TGTNAV, KEYPADINPUT, CONTENTLAUNCHER, ALOGIN and APPLAUNCHER
  sequences against the mounted servers).
- **§5** — `internal/chiptool`: `dtCastingVideoPlayer`/`dtContentApp` in
  `endpointDeviceTypes`; thirteen family rows (player for all but ALOGIN and
  APBSC, which target the tv-app's content app, config `endpoint: 3`);
  `startBridgeWith` passes `--media` whenever the daemon offers it (like
  `--wifi`, so the PICS slices describe one device); the families leg
  requires the flag. Edits: APPLAUNCHER 3.8/3.9 set the config variable
  `ApplicationBasic.Endpoint: 3` to `{ep:0x0024}` (as TC-G-2.4's endpoints).
  Exclusion: MEDIAPLAYBACK 6.8, class (c) (see family results). No
  `knownProblems` were needed (every failing case was YAML anyway).
  PICS: no hand declaration was needed (`reference-bridge.pics`
  unchanged); the update run added `pics/ep45.txt` (player) and
  `ep46.txt` (app), every other slice unchanged. `doc.go` names `--media`.
  `docs/certifiability.md`: the media row of "Families this module does not
  run yet" replaced by CONCON/MESSAGES (no chip case), one paragraph on the
  media families, the generated block regenerated.
  `.github/workflows/chiptool.yml`: a new group `app-media` (see Questions).

## Guards (HEAD logged before/after each run)

- `make ci` at c2c8569: one `node` failure,
  `TestSessionAndSubscriptionDiagnostics` "Commission: commissioner: Could
  not connect to device" — rerun alone: ok; the run also showed the missing
  floors (fixed in 8f5d2f3). The earlier run at d7188d6 found
  `controller/client/known.go` (fixed in c2c8569). **At 8f5d2f3: EXIT 0**
  (lint 0 issues; cover-check 201 packages at or above floor; cluster/media
  93.1 / 85, cluster/core 90.5 / 90).
- `make race` at 8f5d2f3 — **EXIT 0**.
- `make reachability-check` at 8f5d2f3 — **EXIT 0** (unreached 5).
- `make chipdm-check` at 825b253 and at 8f5d2f3 — **EXIT 0** (it compares
  the whole snapshot, so the generated set does not move it).
- `make chiptool-test` once at 8f5d2f3 — **EXIT 0** (commissioning, the
  chip-tool suite with its data-model sweep over the daemon with `--media`,
  the Go controller legs; `TestMatterJSController` skips: no built matter.js
  beside the worktree).
- The families, `GOFABRIC_CHIP_FAMILIES=IDM,ALOGIN,APBSC,APPLAUNCHER,APPOBSERVER,AUDIOOUTPUT,CHANNEL,CONTENTLAUNCHER,KEYPADINPUT,LOWPOWER,MEDIAINPUT,MEDIAPLAYBACK,TGTNAV,WAKEONLAN`
  (IDM added per the briefs README's first-wave lesson: IDM/10.4 runs per
  endpoint and the golden needs the two new endpoints; `-timeout=4h`
  instead of 2h for IDM's 30 minutes):
  1. update run at d50fec1 + uncommitted §5: APPLAUNCHER 3.8, 3.9 and
     MEDIAPLAYBACK 6.9 failed — the image runs them (they are not manual in
     its descriptor although chip's `manualTests.json` lists them). 3.8/3.9:
     the `ApplicationBasic.Endpoint: 3` config → edit. 6.9: its
     ActivateAudioTrack step gates on `MEDIAPLAYBACK.S.C09.Rsp` (SkipBackward;
     Test_TC_MEDIAPLAYBACK_6_9.yaml) and the player had no AudioTracks → the
     player now serves the tv-app's feature map 0x1F with its tracks.
  2. second update run: only MEDIAPLAYBACK 6.8 failed → excluded (below).
  3. confirming run without the flag at d7188d6's tree: **EXIT 0**, every
     family green.

## Family results

| Family | Pass | Manual in the image (c) | Not applicable | Excluded |
| --- | --- | --- | --- | --- |
| ALOGIN | 12.1 (steps 1–3 gated off by `PICS_SKIP_SAMPLE_APP=0`) | 12.2 | 12.3 (`ALOGIN.S.F00`, OAuth: past the model) | — |
| APBSC | 9.1 | — | — | — |
| APPLAUNCHER | 3.5, 3.6, 3.7, 3.8, 3.9 | 3.7.1, 3.8.1, 3.9.1 | — | — |
| APPOBSERVER | 13.2 (its one step skips: `APPOBSERVER.S.C00.Rsp` is false, the player serves no ContentAppObserver) | 13.1 | — | — |
| AUDIOOUTPUT | 7.1, 7.2 | 7.3, 7.4 | — | — |
| CHANNEL | 5.1, 5.2, 5.3 | 5.4, 5.5, 5.6 | — | — |
| CONTENTLAUNCHER | 10.1, 10.3, 10.5, 10.7 | 10.4, 10.6 | 10.8 (`F05`), 10.9 (`F06`), 10.10 (`A0003`) | — |
| KEYPADINPUT | 3.2, 3.3 | 3.1 | — | — |
| LOWPOWER | 2.1 | 2.2 | — | — |
| MEDIAINPUT | 3.10–3.13 | 3.14–3.17 | — | — |
| MEDIAPLAYBACK | 6.1–6.4, 6.9 | 6.5, 6.7, 6.10 | 6.11 (`A000b` AvailableCommands), 6.12 (`A000c` ContentInfo) | 6.8 |
| TGTNAV | 8.1 | 8.2 | — | — |
| WAKEONLAN | 4.1 | — | — | — |

The not-applicable Python cases gate on PICS codes of chip's revisions past
the matter.js model (MediaPlayback rev 3, ContentLauncher rev 3,
AccountLogin rev 3 at the pin's tv-app.matter), so the device-derived PICS
leaves them false — the "need PICS past the pin" the brief names.
**MEDIAPLAYBACK/6.8** (class (c)): steps 1 and the reads after
DeactivateTextTrack expect ActiveTextTrack `{ID: "", TrackAttributes: null}`
(Test_TC_MEDIAPLAYBACK_6_8.yaml:26-31), the tv-app's empty TrackType under
chip's revision 3; the model at the pin has TrackStruct.TrackAttributes
mandatory and not nullable (media-playback.element.ts:206-208) and
ActiveTextTrack nullable (:71-74), so no active text track reads null.
No case is a known problem; no case is a class (a) defect.

## Brief lines that differ from the files

1. **D4 "player (BasicVideoPlayer: …ContentLauncher)"** — chip's tv-app
   endpoint 1, which the cases target, is `ma_casting_videoplayer = 35`
   (tv-app.matter:4384-4385), and the families need ContentLauncher and an
   ApplicationLauncher with AP there; BasicVideoPlayer lists neither
   (basic-video-player.element.ts:22-33) — ContentLauncher would be an extra
   cluster and ApplicationLauncher's AP needs ContentAppPlatform, a
   CastingVideoPlayer condition (casting-video-player.element.ts:31-39).
   The player is a CastingVideoPlayer; BasicVideoPlayer is generated and
   proven in `device/media_test.go` only.
2. **ADR Context "the conditions … gate MediaInput, ApplicationLauncher's AP
   feature and the ContentAppObserver client"** — ContentAppPlatform gates
   the ApplicationLauncher *cluster* on CastingVideoPlayer, with AP
   mandatory inside it (:33-36); on ContentApp ApplicationLauncher is
   mandatory with AP disallowed ("X", content-app.element.ts:27-30).
3. **D4 "three channels"** — the tv-app has four (ChannelManager.cpp:32-62);
   the daemon serves the first three, as the brief says; CHANNEL-5.3's
   skip from 6.0 lands on 9.1 either way.
4. **D5 / chip's `manualTests.json`** — lists APPLAUNCHER 3.8/3.9,
   APPOBSERVER 13.2 and MEDIAPLAYBACK 6.8/6.9 as manual, but the image's
   descriptor runs them as YAML (run 1); followed the image.
5. **Brief §5 command** — `-timeout=2h` and the media families only; IDM
   joined (README lesson) and the timeout is 4h.
6. **ADR "Messages' `Messages` attribute is `R F V`"** — confirmed
   (messages.element.ts:31-34); additionally both commands are fabric-scoped
   ("F O", :65-91).
7. **ADR / brief "list- and struct-typed mandatory attributes have no
   rendered default"** — two of the media lists *have* a model default the
   generator does not render: Channel.ChannelList `default: []`
   (channel.element.ts:31) and ContentLauncher.AcceptHeader `default: []`
   (content-launcher.element.ts:34). The servers apply them (nil → empty
   list); `defaultValue`'s missing list defaults are a findings-register
   candidate, as the ADR says.

## Decisions

- **The state transaction.** Each stateful port command gets
  `st *XState` (the attribute state) and changes it, as a matter.js
  command implementation writes `this.state`; the server commits the copy
  (checked by `SetAttributes`, one data-version bump, change report) only
  when the command succeeds, and drops it when it fails — a matter.js
  transaction rolling back on throw. The library itself infers no effect
  (matter.js has no logic); the tv-app effects live in the daemon.
  `Initial` in the config, `SetState`/`State` for device-side changes;
  `ErrMissingState` when a served list/struct attribute (without model
  default) is nil — at construction and on a later commit.
- **Optional commands** are optional Go interfaces (`PlaybackSkip`,
  `InputRenamer`, `ChannelGuide`, …): accepted only when implemented
  (matter.js ProtocolService.ts:541-545); a feature or mandatory command the
  port lacks is `ErrNotImplemented` (ValidatedElements.ts:357-371).
  ContentLauncher's two commands are one interface (feature-gated, not
  optional).
- **D2: Messages is core-held** (`cluster/core/messages.go`), not a
  `spec.Server` extension: a `FabricScopedReader` on `spec.Server` would put
  every generated server on the fabric-filtered read path and need
  fabric-scoped list writes too — not small. Behaviour: chip's
  messages-server (Responses without RESP → INVALID_COMMAND, :359-363) and
  tv-app's MessagesManager (replace an existing id, completing a presented
  one; cancel completes presented, drops queued; ActiveMessageIds = the
  presented). Beyond both (record as candidates): a ninth message is
  RESOURCE_EXHAUSTED (the list's `max 8`), a cancel touches only the
  accessing fabric's messages, the presentation lifecycle is the host's
  (`Presented`, `Complete` with a `MessageOutcome`), MessageComplete's
  ResponseId/Reply null with RESP/RPLY and absent without.
- The player serves MediaPlayback with TT/AT/AA (tv-app's 0x1F) so
  MEDIAPLAYBACK-6.9 runs instead of being a PICS-gated no-op.
- `request` accepts the generated empty payload, a pointer, or no fields for
  a payload-less command; fields of another type are INVALID_COMMAND.

## Questions

1. **Paths outside the allowed list**, each forced by an existing guard:
   `.github/workflows/chiptool.yml` (`TestWorkflowRunsEveryFamily` needs every
   family in one group: new group `app-media`), `controller/client/known.go`
   (`TestEveryGeneratedClusterIsKnown`), `device/registry_test.go` and
   `device/type.go` (doc). Acceptable, or should the owner take them?
2. `cluster/doc.go` lists the cluster sub-packages and does not name
   `cluster/media` (not in my paths) — the owner's line to add.
3. `script/clustergen` cannot resolve a cross-cluster datatype: Channel's
   `ExternalIdList` entries (`ContentLauncher.AdditionalInfoStruct`) are
   generated as empty structs (`GetProgramGuideRequestExternalIdListEntry
   struct{}` etc.), so their fields are dropped on decode. Not reached by
   any family; a generator finding for the register?
4. The Messages "beyond chip" choices above — by_design or a mirror to look
   for elsewhere (matter.js has none)?
5. The `MediaPlaybackState` app-pipe command is this daemon's own (no
   family needs it) — keep or drop?

## Left out

CastingVideoClient and every `.C` case; ContentControl and Messages
families (no chip case at the pin) and Messages on the daemon's player
(optional, `[Rev >= v2]`, no case); ContentAppObserver on the daemon (the
player type lists it as a client of ContentApp only; APPOBSERVER-13.2's
step is PICS-gated off); chip-revision-3 elements past the model
(AvailableCommands, ContentInfo, Presets, ContentReplication, OAuth); any
streaming/playing; an ecosystem proof (phase 5).

## by_design candidates (not written)

- `BD-Matter-Media-HostPorts`: the media servers are the generated
  definitions plus typed host ports and a state transaction; matter.js
  leaves the commands unimplemented for the application's subclass.
- `BD-chip-Messages-CoreHeld`: Messages' capacity answer, per-fabric
  cancel and host-reported presentation (see Decisions).
- Findings-register candidates: `defaultValue` does not render list
  defaults (ChannelList, AcceptHeader `[]`, applied by hand in
  `cluster/media`); clustergen's cross-cluster datatype entries (question 3).

## Owner's decisions (2026-10-14, tranche 24)

- **Q1:** accepted as committed; the four edits are what the guards ask
  for.
- **Q2:** `cluster/doc.go` names `media/` in the tranche's owner commit.
- **Q3:** recorded in the findings register (clustergen cross-cluster
  datatypes), together with the list-default hand-off.
- **Q4:** `BD-chip-Messages-CoreHeld`.
- **Q5:** kept — a TESTING-ONLY daemon may carry a lever no case needs
  today.
- Differences 1–6 accepted; 1 (the player is a CastingVideoPlayer,
  `tv-app.matter:4384-4385`) and 2 correct ADR 0022's wording in the
  docs PR; 4 (the image over `manualTests.json`) is the harness's
  existing rule; 6 is in `BD-Matter-Media-HostPorts`.

