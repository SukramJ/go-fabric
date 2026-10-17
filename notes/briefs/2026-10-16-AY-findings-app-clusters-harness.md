# Brief AY — open findings: application clusters and the harness

Owner: Fable. Implementer: Opus. Part of one findings wave with brief AX
(codec, IM, controller, tooling) and the owner (security paths:
`bridge/`, `node/`, `transport/`, `secure/`, `cluster/core/{operational_credentials,general_commissioning,access_control}.go`).
Stay out of their paths; `CHANGELOG.md` and `notes/parity/` are the
owner's. Branch `findings/ay` from `planning/briefs-findings`, pushed,
**no PR**. Report `notes/briefs/reports/AY.md`. **You own the chip-tool
harness** for this wave (single user): run the narrowed families you
touch with `-update-chip-cases` (never `GOFABRIC_CHIP_CASES` with the
flag), then the same run without it, then `make chiptool-test` once.

Read `CLAUDE.md` (incl. Token use), the register at the lines below and
`notes/briefs/reads/open-findings.md`. For every item read matter.js at
`0ceb202d` and, where the register says so, chip at the harness pin
(`git -C ../connectedhomeip show 6170af8461b10b1766044122ac83332c6d00ab20:<path>`);
mirror, cite, test first. A register entry the code has outgrown is
closed with a note, not reworked. One commit per item group;
`make reachability` in each.

## Items (register line → decision)

1. **Constraint.In (351):** checked after all initial values are applied
   and on host `Set`, not only on controller writes; add the localization
   start-up cases.
2. **FanControl (361):** verify the percent/speed formulas and the
   On/Smart mapping against chip's `fan-control-server.cpp` and Matter
   §4.4.6; correct where they differ.
3. **SmokeCoAlarm (368, 374):** default ExpressedState priority against
   chip's `smoke-co-alarm-app`; PowerSource (0x0011) in the
   DeviceTypeList when the endpoint serves PowerSource.
4. **ModeBase (383, 394):** DIRECTMODECH bit and the 0x80 status range
   against `mode-base-server`; enforce InvalidInMode where the server can
   see the dependency, report where it cannot.
5. **CountdownTime (401):** follows matter.js — by_design candidate, no
   change.
6. **Thermostat (427):** SetpointRaiseLower in Heat/Cool on an AUTO
   thermostat keeps the deadband (compute the companion setpoint); two
   refusals in the negative-write table.
7. **Not built (408):** check each listed cluster against what is
   generated and served today (most arrived in later waves); build the
   still-missing optional ones a target device type lists, through the
   standard server; list what stays out and why.
8. **WindowCovering maintenance (494):** port the Mode/ConfigStatus sync
   and the refusal; declare `WNCV.S.M.Maintenance=1`.
9. **EEVSE-2.3 step 19 (501):** decide decoder vs server for the list
   bound by reading matter.js; answer RESOURCE_EXHAUSTED if the case and
   matter.js agree.
10. **Descriptor TagList semtag.Label (566):** conformance
    "MfgCode != null, O" where the TagList is served.
11. **PICS E01 (577):** `gen_pics.py` marks OperationCompletion present
    where the device types require it; regenerate the slices.
12. **chipdm (ii) 551, 560:** GroupKeyMulticastPolicy stays (snapshot
    gate) and ColorControl's `hsvToXy` call stays mirrored until upstream
    — by_design candidates, no change.

Families to run: FAN, SMOKECO, MOD (and the mode families you touch),
TSTAT, WNCV, EEVSE, DESC, plus IDM if an endpoint's composition moved.
Guards: `make ci`, `make race`, `make reachability-check`,
`make chipdm-check`, the families above, `make chiptool-test`.
Report per item: done / closed-stale / by_design candidate with sources;
family results; CHANGELOG lines.
