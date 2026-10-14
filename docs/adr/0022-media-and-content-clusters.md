# ADR 0022 — The media and content clusters and the three media device types enter the scope

- **Status**: Accepted (2026-10-14; concept v2 phase 9,
  [`../concept-v2-scope-extension.md`](../concept-v2-scope-extension.md) §B.3, decision 0.2)
- **Date**: 2026-10-14
- **Related**:
  [ADR 0013 — generated cluster definitions](./0013-generated-cluster-definitions.md),
  [ADR 0017 — the standard server](./0017-standard-server.md) (the mechanism that carries a generated definition with no logic),
  [ADR 0018 — device layer](./0018-device-layer-and-node-facade.md),
  [ADR 0011 — certifiability is a goal](./0011-certifiability-is-a-goal.md),
  concept v1 Appendix F.2 (the media family's exclusion, now lifted)

## Context

Concept v1 kept the media family out: fifteen clusters and three device
types with no consumer in a bridge. Concept v2 (decision 0.2) takes them
in before `v1.0.0`, generated, with the proof over chip's families and
the module's own controller, because a bridge host may front a player or
a content app and the parity count moves by fifteen clusters. The first
read at the pin (`0ceb202d`, 2026-10-14) measured:

- **The fifteen** (concept v1 Appendix F.2): MediaPlayback (0x0506),
  KeypadInput (0x0509), ContentLauncher (0x050A), ApplicationLauncher
  (0x050C), ApplicationBasic (0x050D), AccountLogin (0x050E), AudioOutput
  (0x050B), Channel (0x0504), TargetNavigator (0x0505), MediaInput
  (0x0507), LowPower (0x0508), WakeOnLan (0x0503), ContentControl
  (0x050F), ContentAppObserver (0x0510) and Messages (0x0097).
- **matter.js has logic for one of them.** Fourteen servers are the
  generated `extends XBehavior {}` stub; `LowPowerServer.ts:13-25`
  implements `sleep()` as an `enterLowPowerMode` event. The model (the
  element files) is the gold standard for the rest; there is no media
  example or test in matter.js at the pin.
- **The standard server carries fourteen.** `spec.NewServer` (ADR 0017)
  mounts a generated definition with `Source`/`Sink` hooks and
  `Server.Handle(cmdID, handler)` for commands; an unhandled command
  answers UNSUPPORTED_COMMAND. It refuses a fabric-scoped attribute
  (`cluster/spec/server.go:131-135`, `ErrFabricScoped`): Messages'
  `Messages` attribute is `R F V`, so Messages needs a core-held server as
  Binding and EcosystemInformation have (`device/device.go` default
  factories). List- and struct-typed mandatory attributes have no
  rendered default: the host supplies the initial state.
- **Host ports are unavoidable** for the commands that change state:
  MediaPlayback's transport commands, Channel, KeypadInput `SendKey`,
  TargetNavigator, MediaInput/AudioOutput `Select*`, ApplicationLauncher
  (whose `StopApp` also moves ApplicationBasic.Status on the content
  app's endpoint), ContentLauncher, AccountLogin (timed, fabric-scoped),
  ContentControl, ContentAppObserver, Messages, LowPower `Sleep`.
  WakeOnLan and ApplicationBasic are attribute-only.
- **Device types:** BasicVideoPlayer (0x0028), CastingVideoPlayer
  (0x0023), ContentApp (0x0024), all revision 2, generated with no logic;
  the conditions `PhysicalInputs`, `ContentAppPlatform` and
  `ObserverClient` gate MediaInput, ApplicationLauncher's AP feature and
  the ContentAppObserver client. CastingVideoClient stays out (the
  client role).
- **chip's families:** about 33 server-side YAML cases (ALOGIN, APBSC,
  APPLAUNCHER, APPOBSERVER, AUDIOOUTPUT, CHANNEL, CONTENTLAUNCHER,
  KEYPADINPUT, LOWPOWER, MEDIAINPUT, MEDIAPLAYBACK, TGTNAV, WAKEONLAN),
  hard-coded to endpoint 1 (3 for ALOGIN-12.1 and APBSC-9.1: a content
  app), user prompts automated under the image's CI PICS; the Python
  cases need PICS past the pin and skip. No CONCON or MESSAGES case.

## Decision

1. **The fifteen definitions are generated** (`script/clustergen`,
   `cluster/spec/<name>`), the fourteen without logic mounted by the
   standard server through one package, `cluster/media`, that wraps each
   in a typed host port (`media.Playback`, `media.Channels`, `media.Keypad`,
   …): the port is the interface a host implements, the server the
   generated definition plus the command handlers that call it and the
   attribute state the port reports. Messages gets a core-held server for
   its fabric-scoped list. LowPower's `Sleep` calls the host port, as
   matter.js emits its event.
2. **The three device types are generated** (`script/devicegen`); their
   conditions are stated by the host as the root's are.
3. **The reference daemon** exposes a player endpoint (BasicVideoPlayer
   with a simulated transport, channel list, inputs and outputs) and a
   content-app endpoint (ContentApp with ApplicationBasic and
   AccountLogin) behind a TESTING-ONLY flag, driven by the app-pipe
   where a case needs a state change; the chip media families join the
   harness table, the PICS and the golden. The two Python cases that
   need PICS past the pin are recorded as not applicable.
4. **The ecosystem proof waits for phase 5** (Apple Home, Alexa with
   the owner at the device); nothing built on this is described as
   certified (ADR 0011). The parity count moves to 116 clusters and 77
   device types, as concept v2's phase table says.

## Consequences

- `docs/matterjs-comparison.md` (the media row of the excluded families
  and the cluster count), concept v1 Appendix F.2 and
  `docs/certifiability.md` (the media families) are updated by the
  wave's docs PR; `CLAUDE.md`'s scope boundaries name no media
  exclusion and do not change.
- The host's obligation grows by the media ports; a host that wires none
  has no media endpoint, as before.
- The concept's "all fifteen are stubs" is corrected to fourteen in the
  docs PR; `defaultValue`'s missing list defaults are recorded in the
  findings register if the wave leaves them.
