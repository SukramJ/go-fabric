
# Brief: issue 461 — a passive Thread border-router registry for controller/topology (2026-11-08)

Owner 2026-11-08: "Ergänze die #461" — in wave W8 with #457 and #460,
one Opus, one PR, after W7 (#458) merges. Disjoint paths from the other
two sections: `client/discovery` ingress, a new registry package,
`controller/topology`, `controller.Config`. Host hand-over from
go-fabric-server (the HA network topology). Pins: matterjs-server 0bd06f2
(`git -C ../matterjs-server show 0bd06f2:<path>`), matter.js db021da0
(`git -C ../matter.js show db021da0:<path>`). Every citation below
verified 2026-11-08 at the pins.

## Why

`controller/topology/doc.go:32-39` records the gap: no `_meshcop` browse,
a border router neighbour is `thread_unknown`. `build.go:22-27` (`Build` =
matterjs-server `#build` without the registry), `thread.go:60-66`
(`unknowns` = `findUnknownDevices` without the classification),
`topology.go:19-24` (no `KindBorderRouter`). matterjs-server
`packages/ws-controller/src/controller/NetworkTopologyService.ts:88-89`
takes `borderRouters: Pick<BorderRouterRegistry, "list" | "events">`,
`:170-172` schedules a rebuild on added/updated/removed, `:309-316` builds
`brByExt` (ext address upper-case hex) and `networkNameByXp` (extended PAN
id → network name), `:380` passes `brByExt` to `findUnknownDevices`,
`:452-466` emits the `border_router` node (`id`, `kind`, `network_type`
"thread", `role` "router", `ext_address`, `ext_pan_id`, `network_name`,
`host_name` stripped, `vendor_name`, `model_name`, `last_seen`); the
`thread_unknown` node keeps `:467-478`. matter.js
`packages/thread-br-client/src/discovery/BorderRouterRegistry.ts:34-37`
(the `_meshcop._udp.local` / `_trel._udp.local` names and suffixes),
`:59-65` (passive discovery, per-extended-address registry, `list`),
`:133-135` (an observer on `names.discovered`), `:171-185` (`list`/`get`
prune expired entries, `STALE_RETENTION` cutoff on `lastSeen` for an
entry without sources), `:335-346` (`xa` from the TXT, 8 bytes hex; for a
TREL record the instance name is the fallback, Thread §11.4.2);
`BorderRouterEntry.ts:15-27` (`extAddressHex` key, `extendedPanIdHex`,
`networkName` "nn", `vendorName` "vn", `modelName` "mn", `hostname` = SRV
target). matter.js tests: `packages/thread-br-client/test/BorderRouterRegistryTest.ts`
at db021da0 (port by meaning: TXT parsing, TREL fallback, expiry,
events). matterjs-server tests: `packages/ws-controller/test/NetworkTopologyServiceTest.ts`
(`:235` maps matter / border_router / wifi_ap, `:313` and `:332` host_name
omitted, `:358` a Matter node linked to the external border router) and
`packages/ws-client/test/TopologyUtilsTest.ts:248` ("promotes a neighbor
that matches the border-router registry"). No `by_design` row: parity;
`doc.go`'s "Left out" shrinks to the route-table supplement.

## Scope and shape (decisions)

1. **Ingress.** `client/discovery/names.go:46` `matterServiceSuffixes`
   admits `._meshcop._udp.local` and `._trel._udp.local` beside the
   three Matter suffixes; `Names.Browse` (`browse.go:52`) serves them.
   Cite `MdnsService.ts` for the Matter three and
   `BorderRouterRegistry.ts:34-37` for the two; update the doc at
   `names.go:78-79`. Both types, as the pin (open question answered:
   both).
2. **Registry.** New package `thread/brdiscovery` (sibling of
   `thread/otbr`, which stays the REST client; the registry needs no
   OTBR): `Registry` built on a `*discovery.Names` (an observer on
   discovered records, as `:133-135`), `Entry{ExtAddress, ExtPanID,
   NetworkName, VendorName, ModelName, HostName string; LastSeen time.Time}`
   keyed by upper-case ext-address hex, `List()`, `Get(ext)`, events
   added/updated/removed through the module's notifier shape (as
   `controller/topology` already consumes events), lazy expiry on
   `List`/`Get` with `STALE_RETENTION` taken from the pin (cite the
   constant's line). TXT parsing of `xa` (8 bytes), `xp`, `nn`, `vn`,
   `mn`; SRV target as hostname; the TREL instance-name fallback
   (`:335-346`). Clock injected (`clock.Clock`), no `time.Sleep`.
3. **Topology.** `KindBorderRouter Kind = "border_router"`; `Build`
   takes the registry's entries (a `[]brdiscovery.Entry` parameter or a
   `Source` field — keep `Build` pure: entries in, graph out);
   `unknowns` classifies by ext address (`id` `br_<EXT>`, role router),
   the network name by extended PAN id for every Thread node
   (`:309-316`), the node fields of `:452-466`; the controller's
   topology service subscribes to the registry's events and schedules
   its existing debounced rebuild (`:170-172`).
4. **Controller.** `controller.Config` gains the registry, on by default
   when `Names` opens (matterjs-server's is passive and always on); a
   field to disable. `Node`/facade untouched.
5. Out of scope, named in `doc.go` "Left out": the route-table supplement
   (`via_route_table`, `path_cost`); feeding `thread/otbr`'s border-router
   list from the registry (separate decision).
6. Docs: the controller page's topology section; `CHANGELOG.md`
   `### Added`. apidiff: additive unless `Build`'s signature changes —
   if it does, a Break line naming `topology.Build` and a migration
   section (go-fabric-server is the consumer).

## Tests

Ports by meaning: `BorderRouterRegistryTest.ts` (registry), the three
matterjs-server cases named above (`controller/topology`), plus a
`client/discovery` case that a `_meshcop` record enters the cache and a
`_matter` browse does not return it. Harness: none (controller side).

## Guards and report

Full bundle under the lock; `make cite-report && make reachability` with
the citations; `mkdocs build --strict`. Report per § with the #457 and
#460 sections.

