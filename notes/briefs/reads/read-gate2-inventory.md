# Gate 2 inventory (architecture audit) - go-fabric, main c9e7c473

Base: origin/main c9e7c473a69bfbdd14620ec2f8feb9a70cda57d0 ("m4 BU-b: the ports in the declarations, the origin audit, the reference bridge on the declaration (#213)"), after pull request #213 merged. Map only: `file:line` per fact, no judgement, no recommendation, no statement about matter.js or chip behaviour beyond what a cited file says. Template: notes/briefs/reads/read-gate1-inventory.md (539 lines, a71223c2); gate-1 figures are quoted where a count moved. Standards: docs/adr/0025-facade-design-principles.md, docs/adr/0026-architecture-principles.md; brief: scratchpad 2026-10-22-gate2-audit.md part 1. Non-test code means *.go excluding *_test.go; script/ and examples/ are excluded unless a line says otherwise. Go 1.27.3 linux/amd64. Heuristics are named where used; a count or claim not verified says so. The tools behind the figures (go/ast and go/types programs, go list, go doc, go test -json, git archive of v0.9.1) ran from the scratchpad; nothing outside this file was edited.

Sections: 1 import graph (incl. client/* and controller/*), 2 goroutines and timers, 3 locks, 4 time and randomness, 5 errors, 6 logging, 7 persistence outside the seam, 8 facade surface (node, device, contract, fabrictest, controller/client, controller/commissioner, controller/ca, controller/dcl, controller/attestation), 9 tests and flakes, 10 the declaration, 11 the reference bridge and examples/host-view, 12 the migration guide against go doc.

## 1 Import graph

`make archcheck` (Makefile:65-67, `go test -count=1 ./script/archcheck/...`): exit 0; -v: TestLayering PASS, TestSpecificity PASS, TestCheckReportsUpwardStaleAndUnassigned PASS. Layers and exceptions are data in script/archcheck/layers.json; `"exceptions": []` (layers.json:14) - the gate-1 exceptions (binding -> controller/*, cluster/wire -> cluster/switch) are gone. Layers lowest first (layers.json:4-12): foundation; transport; security-and-storage; model-and-protocol (includes `client/...`, `contract`, `im`, `groups`, `cluster/spec/...`, `cluster/wire`); servers (`cluster/...`, `binding`); endpoints (`endpoint`, `device`, `internal/deviceseam`); roles (`bridge`, `controller/...`, `internal/bridgeseam`, `internal/nodeseam`); facade (`node`); support (`.`, `conformance`, `fabrictest`, spectest, bridgetest, endpointtest, bletest, `examples/...`, `script/...`, `ble/...`).

Method: `go list -json` Imports (non-test files) via a scratchpad script; identifier use counts via a go/ast walk over non-test files (`pkg.Ident` selectors whose qualifier is the import name). Counts are uses/files. Packages: 231 (`go list ./...`), of them 121 under cluster/spec/... (120 generated def packages + spectest), 5 under client/ and 11 under controller/ (attestation, ble, ble/bletest, ca, ca/keyfile, client, client/sqlstore, commissioner, dcl, dcl/snapshot, thread; controller itself has no files).

### 1a package import lists (module-internal imports, non-test; `cluster/spec/<N def packages>` collapses the generated cluster/spec/<cluster> packages)

- client/discovery -> clock mdns 
- client/exchange -> clock im secure/channel secure/sessionparams transport/message transport/mrp 
- client/handshake -> client/exchange secure/channel secure/mattercert secure/sessionparams secure/sigma secure/spake2 tlv transport/mrp 
- client/interaction -> client/exchange client/peer cluster/spec im tlv 
- client/peer -> client/discovery client/exchange client/handshake im secure/sessionparams tlv transport/mrp 
- controller/attestation -> cluster/spec secure/attestation tlv 
- controller/ble -> client/discovery client/exchange secure/sessionparams transport/btp 
- controller/ble/bletest -> controller/ble transport/btp 
- controller/ca -> clock secure/mattercert tlv 
- controller/ca/keyfile ->  
- controller/client -> client/discovery client/exchange client/handshake client/interaction client/peer cluster/spec controller/commissioner device endpoint im schema secure/sessionparams tlv cluster/spec/<117 def packages> 
- controller/client/sqlstore -> controller/client 
- controller/commissioner -> client/discovery client/exchange client/handshake client/interaction client/peer cluster/spec controller/attestation controller/ca im secure/mattercert secure/sessionparams secure/sigma tlv transport/btp transport/mrp cluster/spec/<6 def packages> 
- controller/dcl -> bdx/otaimage client/exchange controller/attestation 
- controller/dcl/snapshot -> clock controller/attestation controller/dcl 
- controller/thread ->  
- node -> bdx bdx/otaimage binding bootid bridge client/discovery client/exchange client/handshake client/interaction client/peer clock cluster/core cluster/spec cluster/wire contract device diagevent endpoint endpoint/sqlitestore groups im im/subscription internal/deviceseam internal/nodeseam mdns schema secure/attestation secure/channel secure/mattercert secure/operational secure/sessionparams secure/setup secure/sigma secure/spake2 store transport/mrp transport/udp cluster/spec/<9 def packages> | ext: modernc.org/sqlite
- device -> clock cluster/alarm cluster/alarmbase cluster/appliance cluster/boolcfg cluster/closure cluster/core cluster/cover cluster/energy cluster/fan cluster/filter cluster/levelcontrol cluster/light cluster/lock cluster/measurement cluster/media cluster/modebase cluster/modeselect cluster/onoff cluster/opstate cluster/pump cluster/servicearea cluster/spec cluster/switch cluster/thermo cluster/valve contract endpoint internal/deviceseam schema cluster/spec/<112 def packages> 
- contract ->  
- fabrictest -> bridge bridge/bridgetest client/discovery client/exchange client/handshake client/interaction client/peer clock clock/clocktest cluster/spec contract controller/attestation controller/ca controller/client controller/commissioner endpoint im internal/bridgeseam internal/nodeseam internal/udpseam mdns node store transport/udp | ext: github.com/miekg/dns modernc.org/sqlite
- binding -> client/exchange client/handshake client/interaction client/peer cluster/core cluster/spec im schema 
- endpoint -> bootid clock cluster cluster/core cluster/measurement cluster/onoff cluster/spec contract groups im schema store 

Import-list note: `controller/thread` and `controller/ca/keyfile` import no module-internal package; `controller/client` imports `device`, `endpoint`, `schema`, `im`, `controller/commissioner` (edges in 1d). `contract` imports no module package. `device` imports 24 cluster implementation packages (cluster/alarm, alarmbase, appliance, boolcfg, closure, core, cover, energy, fan, filter, levelcontrol, light, lock, measurement, media, modebase, modeselect, onoff, opstate, pump, servicearea, switch, thermo, valve) plus `endpoint`, `contract`, `schema`, `clock`, `internal/deviceseam`. Library packages only: examples/ imports are in section 11.

### 1b who imports the facade-side packages (non-test)

- client/... 13 importing packages (non-test)
  - binding -> client/exchange,client/handshake,client/interaction,client/peer
  - bridge/bridgetest -> client/discovery,client/exchange,client/handshake,client/interaction,client/peer
  - client/handshake -> client/exchange
  - client/interaction -> client/exchange,client/peer
  - client/peer -> client/discovery,client/exchange,client/handshake
  - controller/ble -> client/discovery,client/exchange
  - controller/client -> client/discovery,client/exchange,client/handshake,client/interaction,client/peer
  - controller/commissioner -> client/discovery,client/exchange,client/handshake,client/interaction,client/peer
  - controller/dcl -> client/exchange
  - examples/quickstart-controller -> client/discovery,client/exchange,client/interaction,client/peer
  - examples/reference-controller/cli -> client/discovery,client/exchange,client/interaction,client/peer
  - fabrictest -> client/discovery,client/exchange,client/handshake,client/interaction,client/peer
  - node -> client/discovery,client/exchange,client/handshake,client/interaction,client/peer
- controller/... 12 importing packages (non-test)
  - bridge/bridgetest -> controller/attestation,controller/ca,controller/commissioner
  - controller/ble/bletest -> controller/ble
  - controller/client -> controller/commissioner
  - controller/client/sqlstore -> controller/client
  - controller/commissioner -> controller/attestation,controller/ca
  - controller/dcl -> controller/attestation
  - controller/dcl/snapshot -> controller/attestation,controller/dcl
  - examples/quickstart-controller -> controller/attestation,controller/ca,controller/client,controller/client/sqlstore,controller/commissioner
  - examples/reference-bridge -> controller/dcl
  - examples/reference-controller/cli -> controller/attestation,controller/ble,controller/ca,controller/ca/keyfile,controller/client,controller/client/sqlstore,controller/commissioner,controller/dcl,controller/dcl/snapshot
  - fabrictest -> controller/attestation,controller/ca,controller/client,controller/commissioner
  - script/dclsnapshot -> controller/dcl,controller/dcl/snapshot
- node 5 importing packages (non-test)
  - examples/host-view -> node
  - examples/quickstart-device -> node
  - examples/reference-bridge -> node
  - examples/reference-controller/cli -> node
  - fabrictest -> node
- device 5 importing packages (non-test)
  - controller/client -> device
  - examples/host-view -> device
  - examples/quickstart-device -> device
  - examples/reference-bridge -> device
  - node -> device
- fabrictest 0 importing packages (non-test)

`contract` is imported by 156 packages (every cluster/spec/<cluster> def package, every cluster server package, bridge, endpoint, eligibility, device, node, fabrictest, three examples); `fabrictest` is imported by no non-test package; `controller/thread` by none.

### 1c cluster/* -> im (non-test files; no cluster package imports an im/* subpackage)

FILES importing im: 80; 26 cluster packages import im (alarm alarmbase appliance boolcfg closure core cover energy fan levelcontrol light lock measurement media modebase modeselect onoff opstate pump servicearea spec spec/spectest switch thermo valve wire); the cluster packages that do not: cluster (root), filter, transition and the 120 generated cluster/spec/<cluster> def packages. Gate 1: 75 files / 25 packages (measurement is new). Identifiers used, uses/files and first site:
StatusInvalidCommand 131/42 cluster/alarmbase/alarmbase.go:442; StatusConstraintError 129/40 cluster/appliance/laundry.go:172; StatusCode 85/38 cluster/alarm/smokecoalarm_server.go:850; StatusFailure 52/25 cluster/alarmbase/alarmbase.go:449; UnsupportedCommandf 41/38 cluster/alarm/smokecoalarm_server.go:698; StatusCodeError 38/24 cluster/alarm/smokecoalarm_server.go:857; FabricFilterFromContext 30/16 cluster/core/access_control.go:529; StatusInvalidInState 20/9 cluster/appliance/laundry.go:181; StatusResourceExhausted 20/10 cluster/core/access_control.go:712; StatusNotFound 16/7 cluster/core/actions.go:200; StatusUnsupportedAccess 15/11 cluster/core/binding.go:271; StatusSuccess 12/2 cluster/core/groups.go:291; StatusUnsupportedWrite 12/12 cluster/core/basic_information.go:188; LookupFabricFilter 9/7 cluster/core/access_control.go:535; SubjectFromContext 8/6 cluster/core/access_control.go:1174; StatusUnsupportedAttribute 7/4 cluster/appliance/laundry.go:263; StatusUnsupportedCommand 6/5 cluster/closure/closurecontrol_server.go:256; IsPASEFromContext 5/5 cluster/core/access_control.go:1175; MatterClusterStatusError 4/2 cluster/lock/users.go:40; StatusBusy 4/4 cluster/alarm/smokecoalarm_server.go:704; StatusFailsafeRequired 4/2 cluster/core/network_commissioning_wifi.go:426; AuthorityAt 2/2 cluster/core/groupcast.go:528; GroupSubjectFromContext 2/2 cluster/core/commissioner_control.go:255; StatusInvalidAction 2/1 cluster/core/group_key_management.go:551; CommandPathFromContext 1/1 cluster/thermo/atomic.go:352; DefaultMaxPathsPerInvoke 1/1 cluster/core/basic_information.go:282; DeferAfterResponse 1/1 cluster/core/diagnostic_logs.go:598; FieldsContainerConsumed 1/1 cluster/spec/codec.go:95; IsListAppendWrite 1/1 cluster/core/access_control.go:859; StatusAlreadyExists 1/1 cluster/core/groupcast.go:744; StatusInvalidDataType 1/1 cluster/energy/evse.go:1321; StatusUnsupportedEndpoint 1/1 cluster/core/groupcast.go:641; WithFabricFilter 1/1 cluster/core/scenes_management.go:443

Per package (identifier set; S. abbreviates Status):
- cluster/alarm: S.Busy,S.Code,S.CodeError,UnsupportedCommandf
- cluster/alarmbase: S.Code,S.Failure,S.InvalidCommand
- cluster/appliance: S.CodeError,S.ConstraintError,S.Failure,S.InvalidCommand,S.InvalidInState,S.UnsupportedAttribute
- cluster/boolcfg: S.Code,S.ConstraintError,S.Failure,S.InvalidCommand,S.InvalidInState
- cluster/closure: S.Code,S.CodeError,S.ConstraintError,S.Failure,S.InvalidCommand,S.InvalidInState,S.UnsupportedCommand,UnsupportedCommandf
- cluster/core: AuthorityAt,DefaultMaxPathsPerInvoke,DeferAfterResponse,FabricFilterFromContext,GroupSubjectFromContext,IsListAppendWrite,IsPASEFromContext,LookupFabricFilter,S.AlreadyExists,S.Busy,S.Code,S.CodeError,S.ConstraintError,S.FailsafeRequired,S.Failure,S.InvalidAction,S.InvalidCommand,S.NotFound,S.ResourceExhausted,S.Success,S.UnsupportedAccess,S.UnsupportedEndpoint,S.UnsupportedWrite,SubjectFromContext,UnsupportedCommandf,WithFabricFilter
- cluster/cover: S.Busy,S.Code,S.CodeError,S.ConstraintError,S.Failure,S.UnsupportedCommand,UnsupportedCommandf
- cluster/energy: S.Code,S.ConstraintError,S.Failure,S.InvalidCommand,S.InvalidDataType,S.InvalidInState,S.NotFound,S.ResourceExhausted
- cluster/fan: S.Code,S.CodeError,S.ConstraintError,S.InvalidCommand,S.InvalidInState,UnsupportedCommandf
- cluster/levelcontrol: S.Code,S.CodeError,S.ConstraintError,S.Failure,S.InvalidCommand,UnsupportedCommandf
- cluster/light: S.Code,S.ConstraintError,S.InvalidCommand,UnsupportedCommandf
- cluster/lock: FabricFilterFromContext,MatterClusterS.Error,S.Code,S.CodeError,S.ConstraintError,S.Failure,S.InvalidCommand,S.UnsupportedAccess,S.UnsupportedCommand,SubjectFromContext
- cluster/measurement: S.UnsupportedCommand,S.UnsupportedWrite
- cluster/media: S.Code,S.CodeError,S.Failure,S.InvalidCommand
- cluster/modebase: S.Code,S.CodeError,S.Failure,S.InvalidCommand,UnsupportedCommandf
- cluster/modeselect: S.Code,S.CodeError,S.InvalidCommand,UnsupportedCommandf
- cluster/onoff: FabricFilterFromContext,S.Failure,S.InvalidCommand
- cluster/opstate: S.Code,S.CodeError,S.Failure,UnsupportedCommandf
- cluster/pump: UnsupportedCommandf
- cluster/servicearea: S.InvalidCommand,UnsupportedCommandf
- cluster/spec: FieldsContainerConsumed,S.Code,S.CodeError,S.ConstraintError,S.InvalidCommand,S.UnsupportedAttribute,S.UnsupportedWrite,UnsupportedCommandf
- cluster/spec/spectest: S.CodeError,S.UnsupportedWrite
- cluster/switch: S.Code,S.ConstraintError
- cluster/thermo: AuthorityAt,CommandPathFromContext,GroupSubjectFromContext,LookupFabricFilter,S.Busy,S.Code,S.CodeError,S.ConstraintError,S.Failure,S.InvalidCommand,S.InvalidInState,S.NotFound,S.ResourceExhausted,S.UnsupportedAttribute,SubjectFromContext,UnsupportedCommandf
- cluster/valve: S.Code,S.CodeError,S.ConstraintError,UnsupportedCommandf
- cluster/wire: FabricFilterFromContext,MatterClusterS.Error,S.Code,S.CodeError,S.Failure,S.InvalidCommand,S.UnsupportedCommand,S.UnsupportedWrite,UnsupportedCommandf

### 1d other named edges (identifier uses/files first-site; Gate-1 edges first, then the client/ and controller/ edges)

- cluster/core -> groups (3 files, 7 identifiers): GroupcastMembership 8/1 cluster/core/groupcast.go:412; Manager 6/3 cluster/core/group_key_management.go:36; PolicyIanaAddr 3/1 cluster/core/groupcast.go:419; ErrResourceExhausted 2/2 cluster/core/groupcast.go:728; PolicyPerGroup 2/1 cluster/core/groupcast.go:417; GroupMessageEvent 1/1 cluster/core/groupcast.go:977; IANAGroupcastAddress 1/1 cluster/core/groupcast.go:988
- groups -> cluster/spec (1 files, 3 identifiers): Encodable 3/1 groups/request.go:44; ListElements 1/1 groups/request.go:110; Node 1/1 groups/request.go:115
- controller/client -> device (1 files, 2 identifiers): Lookup 1/1 controller/client/endpoint.go:79; Type 1/1 controller/client/endpoint.go:78
- controller/client -> endpoint (1 files, 3 identifiers): ClusterShape 4/1 controller/client/conformance.go:177; DeviceTypeViolation 2/1 controller/client/conformance.go:66; DeviceTypeTree 1/1 controller/client/conformance.go:124
- controller/client -> controller/commissioner (3 files, 3 identifiers): Fabric 3/2 controller/client/reactions.go:82; Commissioner 1/1 controller/client/registry.go:30; Options 1/1 controller/client/fabricops.go:125
- controller/client -> schema (3 files, 8 identifiers): CompositionFullFamily 3/1 controller/client/structure.go:515; DeviceTypeDefinitionOf 3/2 controller/client/conformance.go:242; Composition 2/1 controller/client/structure.go:70; AttributeChangesOmitted 1/1 controller/client/structure.go:220; ClusterFeatureNames 1/1 controller/client/cluster.go:158; ClusterHasChangesOmittedAttribute 1/1 controller/client/structure.go:218; ClusterName 1/1 controller/client/cluster.go:84; CompositionTree 1/1 controller/client/structure.go:513
- controller/client -> im (8 files, 5 identifiers): ConcreteAttributePath 7/5 controller/client/config.go:68; DataVersionFilter 5/1 controller/client/structure.go:157; ConcreteEventPath 4/3 controller/client/config.go:69; ConcreteCommandPath 3/2 controller/client/interact.go:142; EventMinimumNumber 3/2 controller/client/interact.go:98
- controller/commissioner -> controller/attestation (3 files, 9 identifiers): Finding 3/1 controller/commissioner/flow.go:725; Error 2/2 controller/commissioner/errors.go:130; LevelError 2/1 controller/commissioner/flow.go:738; Context 1/1 controller/commissioner/flow.go:728; Data 1/1 controller/commissioner/flow.go:729; PAAStore 1/1 controller/commissioner/commissioner.go:54; Policy 1/1 controller/commissioner/commissioner.go:142; Validate 1/1 controller/commissioner/flow.go:727; ValidationError 1/1 controller/commissioner/flow.go:740
- controller/commissioner -> controller/ca (1 files, 7 identifiers): CA 6/1 controller/commissioner/fabric.go:84; FabricRecord 4/1 controller/commissioner/fabric.go:172; P256PublicKey 3/1 controller/commissioner/fabric.go:141; KeyPair 2/1 controller/commissioner/fabric.go:262; CompressedFabricID 1/1 controller/commissioner/fabric.go:279; FabricStore 1/1 controller/commissioner/fabric.go:51; KeyRefOf 1/1 controller/commissioner/fabric.go:297
- controller/dcl -> bdx/otaimage (2 files, 6 identifiers): Header 8/1 controller/dcl/ota.go:295; NewDigest 3/2 controller/dcl/ota.go:303; DigestSHA256 2/1 controller/dcl/ota.go:301; ErrInvalid 2/1 controller/dcl/ota.go:318; FileOptions 1/1 controller/dcl/ota.go:311; ValidateFile 1/1 controller/dcl/ota.go:311
- controller/dcl -> client/exchange (1 files, 3 identifiers): Clock 2/1 controller/dcl/certstore.go:63; SystemClock 1/1 controller/dcl/certstore.go:144; Timer 1/1 controller/dcl/certstore.go:123
- controller/dcl -> controller/attestation (3 files, 6 identifiers): P256Key 5/2 controller/dcl/certstore.go:499; PAA 3/1 controller/dcl/certstore.go:264; VerifySignedBy 2/1 controller/dcl/revocation.go:581; MatterIDs 1/1 controller/dcl/cert.go:64; PAAStore 1/1 controller/dcl/certstore.go:133; Revocations 1/1 controller/dcl/certstore.go:134
- controller/ca -> secure/mattercert (2 files, 7 identifiers): Decode 3/2 controller/ca/ca.go:395; Verifier 2/1 controller/ca/ca.go:254; CurvePrime256v1 1/1 controller/ca/encode.go:151; NewVerifier 1/1 controller/ca/ca.go:339; PubKeyAlgoEC 1/1 controller/ca/encode.go:150; SigAlgoECDSAWithSHA256 1/1 controller/ca/encode.go:137; TBSToDER 1/1 controller/ca/encode.go:188
- controller/attestation -> secure/attestation (1 files, 4 identifiers): TestCMSSignerPrivateKey 1/1 controller/attestation/store.go:100; TestCMSSignerSKID 1/1 controller/attestation/store.go:100; TestPAAFFF1Cert 1/1 controller/attestation/store.go:96; TestPAANoVIDCert 1/1 controller/attestation/store.go:97
- binding -> client/peer (2 files, 3 identifiers): Peer 8/2 binding/manager.go:64; ConnectOptions 1/1 binding/resolve.go:485; Observer 1/1 binding/resolve.go:492
- binding -> client/interaction (2 files, 3 identifiers): Config 1/1 binding/manager.go:143; ForPeer 1/1 binding/resolve.go:110; Interaction 1/1 binding/manager.go:67
- binding -> client/handshake (2 files, 1 identifiers): PeerAddress 2/2 binding/manager.go:85
- binding -> client/exchange (1 files, 1 identifiers): Session 1/1 binding/resolve.go:492
- binding -> cluster/core (1 files, 2 identifiers): Binding 2/1 binding/manager.go:193; TargetStruct 2/1 binding/manager.go:23
- node -> client/peer (1 files, 4 identifiers): Set 2/1 node/client.go:59; Context 1/1 node/client.go:112; MemoryResumptionStore 1/1 node/client.go:114; NewSet 1/1 node/client.go:112
- node -> client/discovery (1 files, 2 identifiers): Names 2/1 node/client.go:46; Open 1/1 node/client.go:104
- node -> client/interaction (1 files, 4 identifiers): Config 1/1 node/client.go:187; NewSubscriptions 1/1 node/client.go:101; Subscriptions 1/1 node/client.go:62; SubscriptionsConfig 1/1 node/client.go:101
- node -> client/exchange (1 files, 3 identifiers): Config 1/1 node/client.go:93; Manager 1/1 node/client.go:56; NewManager 1/1 node/client.go:93
- node -> client/handshake (1 files, 2 identifiers): Fabric 4/1 node/client.go:155; PeerAddress 1/1 node/client.go:155
- fabrictest -> controller/client (2 files, 8 identifiers): Registry 3/1 fabrictest/controller.go:87; Node 2/2 fabrictest/controller.go:461; RegistryConfig 2/1 fabrictest/controller.go:54; CommissionOptions 1/1 fabrictest/node.go:55; Config 1/1 fabrictest/controller.go:218; MemoryStore 1/1 fabrictest/controller.go:124; NewRegistry 1/1 fabrictest/controller.go:224; Store 1/1 fabrictest/controller.go:94
- fabrictest -> controller/commissioner (2 files, 9 identifiers): Fabric 5/1 fabrictest/controller.go:82; Commissioner 3/1 fabrictest/controller.go:86; Options 2/1 fabrictest/controller.go:404; Context 1/1 fabrictest/controller.go:210; FabricConfig 1/1 fabrictest/controller.go:196; New 1/1 fabrictest/controller.go:210; NewFabric 1/1 fabrictest/controller.go:196; PASEOptions 1/1 fabrictest/pase.go:31; Result 1/1 fabrictest/controller.go:418
- fabrictest -> controller/ca (1 files, 4 identifiers): CA 3/1 fabrictest/controller.go:81; MemoryStore 2/1 fabrictest/controller.go:93; Config 1/1 fabrictest/controller.go:193; New 1/1 fabrictest/controller.go:193
- fabrictest -> controller/attestation (1 files, 2 identifiers): PAAStore 2/1 fabrictest/controller.go:57; TestStore 1/1 fabrictest/controller.go:238
- fabrictest -> node (3 files, 10 identifiers): Config 5/1 fabrictest/device.go:62; Node 5/2 fabrictest/device.go:69; Endpoint 2/2 fabrictest/device.go:52; BasicInformation 1/1 fabrictest/device.go:212; Commissioning 1/1 fabrictest/device.go:207; Network 1/1 fabrictest/device.go:206; New 1/1 fabrictest/device.go:153; ProductDescription 1/1 fabrictest/device.go:211; Store 1/1 fabrictest/device.go:216; WindowCredentials 1/1 fabrictest/window.go:22
- fabrictest -> bridge (1 files, 2 identifiers): Bridge 1/1 fabrictest/device.go:300; Snapshotter 1/1 fabrictest/device.go:57
- fabrictest -> endpoint (2 files, 3 identifiers): Spec 2/1 fabrictest/endpoint.go:34; Endpoint 1/1 fabrictest/device.go:338; StringKey 1/1 fabrictest/endpoint.go:36
- fabrictest -> client/peer (3 files, 6 identifiers): Set 4/2 fabrictest/controller.go:84; MemoryResumptionStore 3/1 fabrictest/controller.go:83; ConnectOptions 1/1 fabrictest/wire.go:51; Context 1/1 fabrictest/controller.go:202; NewSet 1/1 fabrictest/controller.go:202; Peer 1/1 fabrictest/controller.go:445
- fabrictest -> client/interaction (4 files, 20 identifiers): AttributeData 4/1 fabrictest/wire.go:64; Result 4/1 fabrictest/wire.go:149; StatusError 4/1 fabrictest/wire.go:72; InvokeOptions 3/1 fabrictest/wire.go:150; ReadRequest 3/1 fabrictest/wire.go:65; Subscriptions 3/1 fabrictest/controller.go:85; WriteOptions 3/1 fabrictest/wire.go:126; ExchangeOptions 2/1 fabrictest/raw.go:27; Interaction 2/2 fabrictest/controller.go:449; Report 2/1 fabrictest/wire.go:83; Subscription 2/1 fabrictest/wire.go:173; Bind 1/1 fabrictest/pase.go:37; Config 1/1 fabrictest/pase.go:37; ErrUnexpectedMessage 1/1 fabrictest/raw.go:40; EventData 1/1 fabrictest/wire.go:96; NewSubscriptions 1/1 fabrictest/controller.go:207; SessionProvider 1/1 fabrictest/pase.go:37; SubscribeRequest 1/1 fabrictest/wire.go:173; SubscriptionsConfig 1/1 fabrictest/controller.go:207; WriteRequest 1/1 fabrictest/wire.go:136
- fabrictest -> client/handshake (1 files, 1 identifiers): PeerAddress 1/1 fabrictest/controller.go:442
- fabrictest -> client/exchange (3 files, 7 identifiers): Manager 3/1 fabrictest/controller.go:79; Message 2/1 fabrictest/raw.go:24; SendOptions 2/1 fabrictest/raw.go:32; Session 2/2 fabrictest/controller.go:452; Config 1/1 fabrictest/controller.go:182; NewManager 1/1 fabrictest/controller.go:182; ReceiveOptions 1/1 fabrictest/raw.go:35
- fabrictest -> client/discovery (3 files, 6 identifiers): Names 4/2 fabrictest/controller.go:80; Device 3/1 fabrictest/node.go:22; NewNames 2/2 fabrictest/controller.go:192; ByLongDiscriminator 1/1 fabrictest/node.go:56; Identifier 1/1 fabrictest/node.go:22; NewScanner 1/1 fabrictest/node.go:23
- controller/ble -> client/exchange (3 files, 5 identifiers): Clock 6/3 controller/ble/channel.go:36; SystemClock 3/3 controller/ble/channel.go:101; Manager 2/1 controller/ble/channel.go:53; Config 1/1 controller/ble/channel.go:143; NewManager 1/1 controller/ble/channel.go:143
- controller/ble -> client/discovery (1 files, 2 identifiers): Identifier 7/1 controller/ble/scanner.go:95; Device 2/1 controller/ble/scanner.go:77
- controller/ble -> transport/btp (2 files, 17 identifiers): Conn 2/1 controller/ble/channel.go:52; ErrDisconnected 2/1 controller/ble/channel.go:28; Session 2/1 controller/ble/channel.go:51; Addr 1/1 controller/ble/channel.go:142; Callbacks 1/1 controller/ble/channel.go:125; ConnRspTimeout 1/1 controller/ble/channel.go:159; DecodeAdvertisementServiceData 1/1 controller/ble/scanner.go:193; EncodeHandshakeRequest 1/1 controller/ble/channel.go:157; HandshakeRequest 1/1 controller/ble/channel.go:157; IsHandshakeResponse 1/1 controller/ble/channel.go:76; MaximumWindowSize 1/1 controller/ble/channel.go:157; MinimumATTMTU 1/1 controller/ble/channel.go:110; NewCentral 1/1 controller/ble/channel.go:125; NewConn 1/1 controller/ble/channel.go:142; Options 1/1 controller/ble/channel.go:136; SegmentSizeFromATTMTU 1/1 controller/ble/channel.go:112; SupportedVersions 1/1 controller/ble/channel.go:157
- controller/ble -> client/handshake: no non-test import (0 files)
- controller/commissioner -> client/peer (1 files, 5 identifiers): ConnectOptions 1/1 controller/commissioner/commissioner.go:546; Descriptor 1/1 controller/commissioner/commissioner.go:537; Peer 1/1 controller/commissioner/commissioner.go:237; Set 1/1 controller/commissioner/commissioner.go:51; Timing 1/1 controller/commissioner/commissioner.go:152
- controller/commissioner -> client/interaction (4 files, 8 identifiers): Interaction 7/2 controller/commissioner/commissioner.go:237; InvokeOptions 4/2 controller/commissioner/flow.go:329; Result 3/1 controller/commissioner/flow.go:328; StatusError 3/3 controller/commissioner/errors.go:136; New 2/1 controller/commissioner/commissioner.go:506; Config 1/1 controller/commissioner/commissioner.go:238; ForPeer 1/1 controller/commissioner/commissioner.go:238; Subscriptions 1/1 controller/commissioner/commissioner.go:68
- controller/commissioner -> client/handshake (4 files, 11 identifiers): PeerAddress 14/3 controller/commissioner/commissioner.go:146; Fabric 4/1 controller/commissioner/fabric.go:76; PASE 2/1 controller/commissioner/commissioner.go:326; PASEOptions 2/1 controller/commissioner/commissioner.go:326; CASEServer 1/1 controller/commissioner/commissioner.go:183; CASEServerConfig 1/1 controller/commissioner/commissioner.go:209; ErrPasscodeMismatch 1/1 controller/commissioner/connection.go:311; ErrUnexpectedData 1/1 controller/commissioner/connection.go:315; FabricList 1/1 controller/commissioner/commissioner.go:205; NewCASEServer 1/1 controller/commissioner/commissioner.go:209; ResumptionStore 1/1 controller/commissioner/commissioner.go:78
- controller/commissioner -> client/exchange (3 files, 11 identifiers): Session 13/2 controller/commissioner/commissioner.go:212; Clock 2/2 controller/commissioner/commissioner.go:59; Manager 2/1 controller/commissioner/commissioner.go:49; PeerMessageMissingError 2/1 controller/commissioner/network.go:472; Timer 2/2 controller/commissioner/connection.go:99; ErrNetwork 1/1 controller/commissioner/connection.go:333; ErrRetransmissionLimit 1/1 controller/commissioner/connection.go:333; ErrTransient 1/1 controller/commissioner/connection.go:333; PeerUnresponsiveError 1/1 controller/commissioner/network.go:523; StatusReportError 1/1 controller/commissioner/commissioner.go:564; SystemClock 1/1 controller/commissioner/commissioner.go:195
- controller/commissioner -> client/discovery (2 files, 4 identifiers): Device 7/2 controller/commissioner/commissioner.go:128; Address 2/1 controller/commissioner/commissioner.go:424; CompareDesirability 1/1 controller/commissioner/commissioner.go:437; TXTData 1/1 controller/commissioner/commissioner.go:392

## 2 Goroutines and timers

Method: `grep -rn -E '(^\s*|[{;)] *)go (func|[A-Za-z0-9_.]+\()'` over non-test code, plus `.Go(func` (sync.WaitGroup.Go). 73 `go` statements in library code (test-support packages bridgetest, bletest, clocktest, fabrictest included) + 7 `.Go` sites = 80 sites, + 6 `go` in examples/ (listed last, not audited). Gate 1: 74 `go` + 5 `.Go`. Columns: site | owner | how it stops | does the owner's Stop/Close wait (read at the cited line).

- bridge/bridge.go:824 | Bridge.Start, UDP serve loop | serveCtx cancel (Stop) | yes: serveDone (bridge.go:853), waitServeStopped bridge.go:1759/1830, bounded by Stop's ctx; listener.WaitIdle :1839
- bridge/bridge.go:840 | Bridge.Start, TCP serve | serveCtx | yes: tcpDone (:851), waitServeStopped :1845, tcpListener.WaitIdle :1851
- bridge/bridge.go:867 | Bridge.Start, ack pump | pumpCtx (pumpCancel) | yes: pumpDone, own pumpStopGrace budget bridge.go:1760-1773
- bridge/securechannel.go:1287 | Bridge.triggerSessionReannounce | context.WithTimeout(base) :1289 | yes: b.reannounces.Wait at bridge.go:1781 (new since gate 1; WaitGroup field bridge.go:434, Add securechannel.go:1278)
- bridge/securechannel.go:1338 | closeSecureSessionsForShutdown, CloseAllGraceful | self-terminating, buffered chan | Stop waits via select with a clock timer (:1341-1350); the goroutine outlives a timeout
- bridge/health_probe.go:93 | StartHealthProbeWithClock | pctx cancel + done chan | yes: returned stop func cancels and waits done
- bridge/bdx_responder.go:50 | Bridge.goResponder, one per BDX exchange | ends when fn returns | yes (new since gate 1): b.bdx.active counter, waitBDXResponders bridge/bdx_responder.go:65 called at bridge.go:1784, bounded by ctx
- bridge/bdx_responder.go:67 | waitBDXResponders waiter (idle.Wait loop) | closes idle when active==0 | stays blocked until responders end even when ctx expired first
- bridge/pase_provider.go:170 (.Go) | PerExchangePaseProvider.StartReaper | ctx / Stop | yes: reaperWG.Wait pase_provider.go:195
- bridge/case_provider.go:163 (.Go) | PerExchangeCaseProvider.StartReaper | ctx / StopReaper | yes: reaperWG.Wait case_provider.go:186
- bridge/subscription_reestablish.go:136 (.Go) | reestablish per peer, local WaitGroup | self-terminating | yes: wg.Wait :143 in the same function
- secure/operational/manager.go:1204 (.Go) | Manager.StartReaper | rctx cancel (reaperStop :1202) | yes (changed): reaperWG.Wait in StopReaper :1229 (gate 1: no handle, no Stop)
- mdns/zeroconf.go:657 | Zeroconf.StartReannounceLoop | stopCh (returned cancel) or ctx | the returned cancel does not wait (release/close(stopCh)), as in gate 1
- mdns/subtype_responder.go:293,297 | SubtypeResponder.Start serveV4/V6 | cctx cancel in Close | yes: r.wg.Wait :324
- mdns/operational_resolver.go:285,290 (.Go) | multicastQueryConn read4/read6 | socket close | yes: c.wg.Wait :369
- node/node.go:502 | Node.reopenWhenIdle | n.life, ticker | yes: keeperWG, Wait node.go:770
- node/node.go:655 | Node.Start keepOperationalTime | n.life | yes: keeperWG (node.go:770)
- node/client.go:121 | clientStack Serve | ctx derived from n.life | yes: cs.close waits <-cs.served (client.go:144-145)
- node/ota.go:86 (.Go) | otaApplier.ApplyUpdate -> restartedWhenIdle | a.life | yes (changed from a bare `go`): a.work.Wait in otaApplier.close node/ota.go:75, after the requestor's Stop
- device/queue.go:64 | queue.start, the Async callback queue | stop() | yes: stop() signals and waits `<-done` (device/queue.go:81)
- client/interaction/subscriptions.go:371 | Subscription.Close waiter | waits s.sustained.finished (subscriptions.go:372) | n/a (exits after finish())
- client/interaction/sustained.go:210 | Interaction.sustain -> runSustained | sustained.finished closed sustained.go:249 | no WaitGroup; Subscription.Close waits finished in a goroutine (subscriptions.go:371)
- client/exchange/manager.go:467 | Manager.Shutdown waiter (m.work.Wait) | closes idle | Shutdown selects on idle vs ctx (:471-476); the waiter stays until work ends
- client/exchange/manager.go:809 (goWork, .Go at :487) | Manager.dispatch, one per inbound exchange | handler returns | yes (changed since gate 1): m.work WaitGroup (:104), Manager.Close waits (:455), Shutdown waits bounded (:464-477), goWork refuses after close (:481)
- client/exchange/manager.go:563 (goWork) | cleanupSessionExchanges `m.goWork(lru.Close)` | self-terminating | yes: m.work
- client/discovery/status.go:104 | Status.Connecting watcher | result chan or names.loop.done | no
- client/discovery/status.go:167 | Service.maybeStartResolving | ctx cancel (resolveCancel) | done chan stored in status.resolving (:166)
- client/discovery/status.go:218 | Service.Changes watcher | ctx or loop.done | no
- client/discovery/service.go:287 | Service.AddressChanges | ctx / loop closing | no
- client/discovery/names.go:173 | Names.run loop | loop.closing | yes: Names.Close closes loop.closing and waits `<-n.loop.done` (names.go:204-217)
- client/peer/peer.go:94 | Peer.goWork | peer ctx | yes: p.workers.Wait peer.go:429
- client/peer/peer.go:220 | Peer.changedLocked save | workers | yes: p.workers (peer.go:429)
- client/peer/probe.go:100 | readEmpty final StatusResponse | context.WithoutCancel(ctx); self-terminating | no
- client/peer/monitor.go:146 | monitor run | done chan | yes: monitor.close waits `<-r.done` (monitor.go:134)
- client/peer/connection.go:140 | initiateConnection `go c.kick` | self-terminating | no
- client/peer/connection.go:171 | initiateConnection c.run | cancel / c.done | yes: p.workers (peer.go:429)
- client/peer/connection.go:337 | connection.goWork | c.workers | yes: c.workers.Wait (connection.go:332)
- controller/client/reactions.go:55 | Registry.goWork | r.cancel | yes: r.work.Wait in Close (controller/client/registry.go:407)
- controller/commissioner/connection.go:269 | connection.launch, one per candidate | actx/abortCtx | counter c.pending (:268,:288); drain loops wait for pending==0 (connection.go:164-185)
- controller/commissioner/commissioner.go:330 | establishOverChannel watcher | ch.Closed() or s.Closing() | no
- controller/ble/channel.go:151 | Channel.watch | session.Closed / conn.Disconnected | no (Channel.Close channel.go:224-229 closes mgr and session, does not wait)
- controller/ble/scanner.go:366 | Scanner.transition `done <- call(ctx)` | ctx cancel deferred :364; buffered chan | no (returns on ScanTransitionTimeout :373 while call may still run)
- controller/dcl/revocation.go:260 | fetchAndValidateRevocation | self | yes: local wg.Wait :266
- ble/bluez/central_linux.go:119 | central.dispatch | c.done closed in Close | yes (changed): Close waits `<-c.dispatched` (central_linux.go:126-146)
- ble/bluez/central_linux.go:709 | connection.deliver | ends when queue empty | no
- cluster/core/general_commissioning.go:654, :905 | watchFailSafeExpiry (def :683) | request ctx or timer | no Close/Wait on GeneralCommissioning
- cluster/core/ota_software_update_requestor.go:829 | goRun | r.runCtx | yes: Stop does r.wg.Wait (:818)
- cluster/core/diagnostic_logs.go:586 | BDX log transfer | tctx = context.WithoutCancel(ctx) | yes (changed): MatterQuiesce (:525) waits idle until d.done() (:593); called by bridge quiesceServers (bridge.go:1857-1881, keep-map :1068)
- cluster/core/identify.go:270 | Identify countdown ticker | done | yes (changed): countdown WaitGroup, Close waits countdown.Wait identify.go:148
- cluster/core/commissioner_control.go:304 | requestCommissioningApproval -> approve | c.ctx | yes: c.wg.Wait in Close :246
- cluster/core/commissioner_control.go:322 | approve, Approver.Approve call | ctx with ApprovalTimeout; buffered chan | no
- cluster/core/commissioner_control.go:399 | commissionNode -> commission | c.ctx with CommissioningTimeout | yes: c.wg
- cluster/lock/doorlock_server.go:387 | goTimerAction, one per timer action (runExpireUser users.go:362, runAutoRelock) | skipped when the timer was stopped (:381) | yes (changed): MatterQuiesce waits `actions` to zero (doorlock_server.go:738-747); gate 1 listed two bare `go` in AfterFunc callbacks
- binding/manager.go:259 | Manager.run worker | m.life cancel | yes: Close waits m.wg (manager.go:533,558)
- binding/resolve.go:323 | establishClientKind connect | m.life | yes: m.wg
- im/subscription/manager.go:239 | Manager.run | stopCh / ctx | yes: Stop waits m.wg :248
- im/subscription/engine.go:87 | tick report workers | m.wg | yes: Stop waits m.wg
- transport/tcp/conn.go:223 | Conn.serve, one per message in an ordered chain | handler returns; skips when closed | yes (changed): handlers.Wait inside serve conn.go:199; Conn.Wait(ctx) waits reader + handlers (conn.go:152); Close does not wait (:143-150)
- transport/tcp/dialer.go:50 | Dialer.Dial c.serve read loop | conn close | via Conn.Wait only
- transport/tcp/listener.go:166 | Listener.Serve ctx watcher | done chan | exits with Serve
- transport/tcp/listener.go:225 | Listener.accept closed-watcher | <-c.Closed() | yes (changed): l.work (listener.go:75,213,226)
- transport/tcp/listener.go:236 | Listener.accept c.serve | conn close | yes: l.work; WaitIdle(ctx) waits :248-257 (called at bridge.go:1851)
- transport/tcp/listener.go:250 | WaitIdle waiter | l.work.Wait | stays blocked until work drains even when ctx expired first
- transport/udp/listener.go:192 | Listener.Serve ctx watcher | done chan | exits with Serve
- transport/udp/listener.go:242 | Listener.Serve, one per datagram | inflight WaitGroup | Close does not wait; WaitIdle(ctx) does (:287-297, bridge.go:1839)
- transport/udp/listener.go:289 | WaitIdle waiter | inflight.Wait | stays blocked until inflight drains
- transport/udp/ordered.go:91 | orderedDispatch.enqueue -> drain | queue empty | tracked by l.inflight (ordered.go:87)
- transport/udp/ordered.go:114 | drain, per datagram | l.inflight | WaitIdle
- clock/clocktest/fake.go:217 | Advance runs a timer callback and waits `<-done` :221 | n/a | yes
- clock/clocktest/fake.go:250 | Resolve `done <- wait()` | n/a | Resolve returns on ctx without waiting
Test-support packages:
- fabrictest/site.go:290 | Site.Run runs fn | fn returns | Run returns on ctx error without waiting `finished` (site.go:295-297 quiet err path)
- fabrictest/controller.go:188 | Controller listener Serve | st.stop | yes: Controller.Close waits `<-st.served` controller.go:309
- bridge/bridgetest/conn.go:65 | ConnServer per message | wg | ConnServer.Wait :85
- bridge/bridgetest/commissioner.go:225 | Commissioner listener Serve | c.stop | yes: `<-c.served` :293
- controller/ble/bletest/bletest.go:163 (pipe), :265, :277 (`go c.drop()`), :302, :345 (`go func() { _ = s.Close() }()`) | bletest fake link | pipe.stop / self-terminating | no wait
Examples (6, not audited): examples/quickstart-controller/main.go:133, reference-bridge/control.go:91,102, fleet_appliances.go:276, ota.go:101, reference-controller/cli/stack.go:172.

`.Go(func` sites (7): bridge/pase_provider.go:170, bridge/case_provider.go:163, bridge/subscription_reestablish.go:136, secure/operational/manager.go:1204, mdns/operational_resolver.go:285,290, node/ota.go:86; `m.work.Go(fn)` at client/exchange/manager.go:487 is the eighth `.Go` call (a function value, not a literal).

### 2b timers and tickers outside the clock port
- Non-test code outside clock/, examples/, script/ calls raw `time.Now/Sleep/After/Tick/NewTimer/NewTicker/AfterFunc/Since/Until` only at mdns/subtype_responder.go:361 and :389 (`SetReadDeadline(time.Now().Add(time.Second))`); clock/noraw_test.go:34-39 lists it as the one exception (rawTimeExceptions, 2 uses; reason in its text: the kernel judges socket deadlines against the wall clock). clock/clocktest/fake.go:239 `time.Sleep(time.Millisecond)` and clock/clock.go:74,78,82,85 are the port itself.
- Real-time `context.WithTimeout/WithDeadline` in non-test library code (not a clock-port wait): 38 sites in 24 files: bridge/bridge.go 6, ble/bluez/central_linux.go 5, bridge/subscription_persistence.go 4, fabrictest/controller.go 3, fabrictest/device.go 2, cluster/core/commissioner_control.go 2, bridge/ota_provider_link.go 2, bridge/bridgetest/commissioner.go 2, 1 each in node/node.go, internal/chipdm/source.go, controller/dcl/{revocation,ota,github,client}.go, controller/commissioner/commissioner.go, bridge/{subscription_reestablish,securechannel,group_messaging}.go, bridge/bridgetest/session.go, bdx/messenger.go. Gate 1: 37.
- examples/ and script/ use raw time freely: examples/reference-bridge ota.go:113,158,159,163; fleet_energy_market.go 7 (:80,95,176,211,250,264,532); fleet_sensors.go:334; fleet_energy.go:152; fleet_appliances.go:203,277; fleet_safety.go:110; fleet_closure.go:97,272,304; wifisim.go:60; fleet_evse.go:567; fleet.go:225,295,316,328,378,379,392; script/dclsnapshot/main.go:37.

## 3 Locks

Method: a go/types scan (golang.org/x/tools/go/packages, run from a scratchpad module with the repo's go.mod versions) of every `sync.Mutex`/`sync.RWMutex` declared as a struct field or package/function `var` in non-test code outside examples/ and script/; 'comment' = a // doc line directly above or a trailing // on the field. Gate 1 used a regex scan and a name-based AST walk; this run is typed, so counts are not strictly comparable (the gate-1 scan found 199 declarations, 50 with a comment).
Total 214 mutex declarations: 208 struct fields (37 declarations in total are RWMutex), 6 `var` declarations (package or function scope: bootid/bootid.go:34 rotationGuard, bridge/bridgetest/session.go:127 mu, bridge/subscription_reestablish.go:131 mu, cluster/spec/spectest/origin.go:38 mu, contract/matter.go:375 measurementKindsMu (RWMutex), controller/client/interact.go:74 sentMu). With a comment: 175; without: 39. Types with 2 or more mutex fields: 32.

### 3a mutex declarations per package (with comment / without)

bdx 2/1; binding 2/0; bootid 0/1; bridge 15/4; bridge/bridgetest 2/2; client/discovery 1/0; client/exchange 1/2; client/handshake 2/0; client/interaction 3/0; client/peer 4/1; clock/clocktest 2/0; cluster/alarm 1/0; cluster/alarmbase 2/0; cluster/appliance 1/0; cluster 2/0; cluster/boolcfg 1/0; cluster/closure 2/0; cluster/core 19/9; cluster/cover 1/1; cluster/energy 7/1; cluster/filter 1/0; cluster/levelcontrol 5/0; cluster/light 3/0; cluster/lock 2/0; cluster/measurement 1/0; cluster/media 3/0; cluster/modebase 1/0; cluster/onoff 3/0; cluster/opstate 1/0; cluster/pump 1/0; cluster/servicearea 1/0; cluster/spec 4/0; cluster/spec/spectest 0/1; cluster/switch 0/1; cluster/thermo 4/1; cluster/transition 2/0; cluster/wire 2/0; contract 0/1; controller/ble/bletest 2/0; controller/ble 3/1; controller/ca 3/0; controller/client 5/5; controller/commissioner 2/0; controller/dcl 5/0; device 4/0; diagevent 1/0; endpoint 4/0; fabrictest 2/3; groups 8/0; im 3/0; im/subscription 2/0; mdns 6/0; node 10/2; secure/channel 1/0; secure/operational 3/0; secure/sigma 1/1; transport/btp 0/1; transport/mrp 2/0; transport/tcp 2/0; transport/udp 2/0

### 3b mutex with no comment (file:line Type.field [RW])

bdx/responder.go:355 ServerSession.mu; bootid/bootid.go:34 rotationGuard; bridge/bdx_responder.go:182 responderTurn.mu; bridge/bridge.go:355 Bridge.mu RW; bridge/bridgetest/commissioner.go:150 Commissioner.mu; bridge/bridgetest/session.go:127 mu; bridge/commissioning_window.go:109 CommissioningWindow.mu RW; bridge/subscription_reestablish.go:131 mu; client/exchange/exchange.go:166 Exchange.mu; client/exchange/session.go:52 Session.mu; client/peer/connection.go:119 connection.mu; cluster/core/access_control.go:46 AccessControl.mu RW; cluster/core/basic_information.go:42 BasicInformation.mu RW; cluster/core/general_commissioning.go:42 GeneralCommissioning.mu RW; cluster/core/general_diagnostics.go:80 GeneralDiagnostics.mu RW; cluster/core/group_key_management.go:38 GroupKeyManagement.mu RW; cluster/core/groupcast.go:223 Groupcast.mu RW; cluster/core/operational_credentials.go:64 OperationalCredentials.mu RW; cluster/core/ota_software_update_requestor.go:66 OTASoftwareUpdateRequestor.mu; cluster/core/vid_verification.go:59 vidVerificationMemory.mu; cluster/cover/windowcovering_server.go:194 WindowCoveringServer.mu RW; cluster/energy/commoditymetering.go:65 CommodityMeteringServer.mu; cluster/spec/spectest/origin.go:38 mu; cluster/switch/server.go:156 Server.mu; cluster/thermo/thermostat_server.go:281 ThermostatServer.mu RW; contract/matter.go:375 measurementKindsMu RW; controller/ble/channel.go:64 handshakeState.mu; controller/client/cluster.go:33 Cluster.mu RW; controller/client/endpoint.go:29 Endpoint.mu RW; controller/client/interact.go:74 sentMu; controller/client/node.go:33 Node.mu RW; controller/client/structure.go:54 structure.mu RW; fabrictest/device.go:88 Device.mu; fabrictest/net.go:65 Network.mu; fabrictest/site.go:85 Site.mu; node/client.go:214 bindingSync.mu; node/ota.go:57 otaApplier.mu; secure/sigma/initiator.go:42 Initiator.mu; transport/btp/session.go:88 Session.mu

### 3c lock held across a host callback, `contract` method, interface/port call, or I/O

Method: go/types walk of every function body (non-test, outside examples/ and script/), tracking `X.Lock()/RLock()` until `X.Unlock()/RUnlock()` in statement order (`defer X.Unlock()`, also inside a deferred func literal = held to function end; if/for/switch/select bodies analysed with a copy of the held set; closures, `go` bodies analysed with an empty set; the first lock alphabetically is named when several are held). Flagged call kinds: [funcvalue] call of a variable of func type (local or parameter, closures included), [funcfield] call of a func-typed struct field, [ifacefield] method call on a struct field whose type is an interface declared in the module (clock.* excluded), [contract] any method named Matter*, [io-pkg] function of os/net/database/sql/net/http/os/exec, [io?] method named Send/Write/Publish/Persist/Save/Put/Invoke/Subscribe/Connect/Dial. Limits: callbacks reached through a helper called under the lock are not seen; local closures (e.g. `fail`, `visit`, `lookup`) and pure calls are in the list as flagged, not verified as host code. Format: file | call-line@lock-expression@L<line of the Lock()>: callee [kind].
Totals: 122 flagged calls in 51 files; by kind: funcvalue 55, funcfield 30, ifacefield 17, io-pkg 11, contract 6, io? 3.

- bdx/sink.go | 61@s.io@L55: os.CreateTemp [io-pkg]; 67@s.io@L55: s.tmp.Write [io?]; 88@s.io@L77: os.CreateTemp [io-pkg]; 97@s.io@L77: os.Remove [io-pkg]; 102@s.io@L77: os.Remove [io-pkg]; 106@s.io@L77: os.Remove [io-pkg]; 109@s.io@L77: os.Rename [io-pkg]; 110@s.io@L77: os.Remove [io-pkg]
- bridge/bridge.go | 1641@b.mu@L1638: s.MatterClusterID [contract]; 1683@b.mu@L1680: s.MatterClusterID [contract]
- bridge/case_provider.go | 86@p.mu@L77: p.factory [funcfield]
- bridge/group_messaging.go | 237@b.groupNetMu@L229: b.groupMember.LeaveGroup [ifacefield]
- bridge/handlers.go | 272@a.mu@L247: source [funcvalue]; 335@a.mu@L326: a.verifierFactory [funcfield]
- bridge/pase_provider.go | 92@p.mu@L83: p.factory [funcfield]
- client/interaction/subscriptions.go | 88@r.mu@L85: r.readingCancel [funcfield]
- client/interaction/sustained.go | 278@st.mu@L274: c.provider.VerifyReachability [ifacefield]
- client/peer/connection.go | 705@c.mu@L701: c.cancel [funcfield]
- client/peer/set.go | 205@s.mu@L197: s.ctx.Fabrics [funcfield]
- cluster/core/binding.go | 168@b.writeMu@L164: cfg.Load [funcfield]; 303@b.writeMu@L299: build [funcvalue]; 311@b.writeMu@L299: b.save [funcfield]; 402@b.writeMu@L390: b.save [funcfield]
- cluster/core/commissioner_control.go | 344@c.mu@L342: c.now [funcfield]; 421@c.mu@L410: c.now [funcfield]
- cluster/core/diagnostic_logs.go | 510@d.mu@L507: d.stopTransfer [funcfield]; 529@d.mu@L526: d.stopTransfer [funcfield]
- cluster/core/general_commissioning.go | 531@g.mu@L507: g.isCommissioningWindowOpen [funcfield]; 596@g.mu@L507: hook [funcvalue]; 674@g.mu@L507: armedHook [funcvalue]; 835@g.mu@L772: hook [funcvalue]; 838@g.mu@L772: commissioned [funcvalue]
- cluster/core/groupcast.go | 916@g.mu@L914: g.stopTesting [funcfield]; 926@g.mu@L914: g.timer [funcfield]
- cluster/core/ota_provider.go | 669@p.mu@L652: check [funcvalue]; 674@p.mu@L652: check [funcvalue]
- cluster/core/ota_requestor_query.go | 80@r.mu@L49: r.clock.AfterFunc [ifacefield]
- cluster/cover/windowcovering_server.go | 666@s.op@L642: s.mover.Stop [ifacefield]; 668@s.op@L642: s.mover.GoTo [ifacefield]; 743@s.mu@L730: store [funcvalue]
- cluster/levelcontrol/held.go | 197@s.mu@L196: s.onOff.OnOff [ifacefield]
- cluster/light/colorcontrol_modes.go | 43@s.commit@L38: change [funcvalue]
- cluster/light/colorcontrol_transitions.go | 162@s.commit@L157: change [funcvalue]
- cluster/lock/doorlock_server.go | 1363@s.mu@L1361: fn [funcvalue]
- cluster/lock/schedules.go | 136@s.mu@L134: fn [funcvalue]
- cluster/media/media.go | 364@s.mu@L362: s.clone [funcfield]; 386@s.mu@L381: s.clone [funcfield]; 386@s.mu@L381: s.read [funcfield]; 412@s.mu@L411: s.clone [funcfield]; 414@s.op@L409: fn [funcvalue]
- cluster/onoff/server.go | 356@s.op@L353: fn [funcvalue]; 562@s.mu@L554: s.clock.AfterFunc [ifacefield]; 712@s.op@L710: s.srv.MatterWrite [contract]; 868@s.op@L863: tick [funcvalue]
- cluster/servicearea/typed.go | 102@s.mu@L84: st.check [funcfield]; 107@s.mu@L84: st.store [funcfield]
- cluster/spec/server.go | 503@s.mu@L501: s.Instance.MatterAcceptedCommands [contract]; 516@s.mu@L513: s.Instance.MatterGeneratedCommands [contract]; 519@s.mu@L513: s.Instance.MatterAcceptedCommands [contract]
- cluster/switch/server.go | 779@s.mu@L773: fn [funcvalue]
- cluster/thermo/temperaturecontrol.go | 359@s.mu@L358: change [funcvalue]
- cluster/thermo/thermostat_server.go | 1109@s.op@L1057: clampCool [funcvalue]; 1111@s.op@L1057: clampHeat [funcvalue]; 1124@s.op@L1057: clampCool [funcvalue]; 1126@s.op@L1057: clampHeat [funcvalue]; 1129@s.op@L1057: clampHeat [funcvalue]; 1143@s.op@L1057: clampCool [funcvalue]
- cluster/thermo/thermostat_typed.go | 108@s.mu@L84: note [funcvalue]; 118@s.mu@L84: note [funcvalue]; 122@s.mu@L84: note [funcvalue]; 126@s.mu@L84: note [funcvalue]; 131@s.mu@L84: note [funcvalue]
- cluster/transition/transition.go | 239@e.op@L228: e.cfg.Apply [funcfield]
- controller/ble/bletest/bletest.go | 283@c.mu@L255: c.p.Serve [funcfield]
- controller/ca/ca.go | 640@c.issue@L607: c.cfg.Store.Save [ifacefield]
- controller/client/conformance.go | 154@e.mu@L132: visit [funcvalue]
- controller/client/reactions.go | 134@r.op@L103: h [funcvalue]
- controller/client/record.go | 704@n.mu@L702: f [funcvalue]
- controller/client/registry.go | 282@r.mu@L280: old [funcvalue]
- controller/commissioner/connection.go | 188@c.mu@L157: abort [funcvalue]; 284@c.mu@L275: c.abort [funcfield]
- fabrictest/device.go | 179@d.actMu@L144: d.topo.Prepare [funcfield]; 190@d.actMu@L144: net.ResolveUDPAddr [io-pkg]; 372@d.actMu@L366: fn [funcvalue]
- fabrictest/multicast.go | 200@n.mu@L196: net.IP [io-pkg]
- fabrictest/net.go | 161@n.mu@L160: n.filter [funcfield]
- groups/groupcast.go | 183@m.storeMu@L160: m.st.UpsertGroupcastGroup [ifacefield]; 220@m.storeMu@L207: m.st.RemoveGroupcastGroup [ifacefield]
- groups/manager.go | 417@m.storeMu@L409: m.st.ListFabrics [ifacefield]; 642@m.storeMu@L618: m.st.UpsertGroupTableEntry [ifacefield]; 719@m.storeMu@L688: m.st.RemoveGroupTableEntry [ifacefield]; 735@m.storeMu@L688: m.st.RemoveGroupcastGroup [ifacefield]; 745@m.storeMu@L688: m.st.UpsertGroupTableEntry [ifacefield]
- im/eventlog.go | 227@l.ceilingMu@L217: fn [funcvalue]; 245@l.ceilingMu@L235: fn [funcvalue]
- mdns/subtype_responder.go | 217@r.lifecycleMu@L211: net.IPv4 [io-pkg]; 220@r.lifecycleMu@L211: net.ParseIP [io-pkg]; 321@r.lifecycleMu@L314: r.cancel [funcfield]
- node/client.go | 287@n.bindSync.persistMu@L273: b.Persist [io?]
- node/node.go | 768@n.mu@L724: n.advertiser.Close [ifacefield]; 769@n.mu@L724: n.cancel [funcfield]
- secure/sigma/initiator.go | 266@i.mu@L256: fail [funcvalue]; 271@i.mu@L256: fail [funcvalue]; 275@i.mu@L256: fail [funcvalue]; 283@i.mu@L256: fail [funcvalue]; 289@i.mu@L256: fail [funcvalue]; 293@i.mu@L256: fail [funcvalue]; 297@i.mu@L256: fail [funcvalue]; 300@i.mu@L256: i.verifier.VerifyAndExtractPubKey [ifacefield]; 302@i.mu@L256: fail [funcvalue]; 305@i.mu@L256: fail [funcvalue]; 316@i.mu@L256: fail [funcvalue]; 319@i.mu@L256: fail [funcvalue]; 325@i.mu@L256: fail [funcvalue]; 328@i.mu@L256: fail [funcvalue]; 347@i.mu@L256: fail [funcvalue]; 359@i.mu@L256: fail [funcvalue]; 363@i.mu@L256: fail [funcvalue]; 367@i.mu@L256: fail [funcvalue]; 375@i.mu@L256: fail [funcvalue]
- secure/sigma/protocol.go | 496@r.mu@L472: r.sessionIDRenewer [funcfield]
- transport/tcp/conn.go | 105@c.writeMu@L103: c.nc.Write [io?]

## 4 Time and randomness

### 4a time
- Raw `time.Now/After/Sleep/NewTimer/Tick` (and AfterFunc/NewTicker/Since/Until) in non-test library code: 2 sites, both mdns/subtype_responder.go:361 and :389 (SetReadDeadline); guarded as the single exception in clock/noraw_test.go:34-39 (rawTimeExceptions, `mdns/subtype_responder.go:Now`, 2). Examples/script raw time: section 2b.
- Port implementation: clock/clock.go:74 (Now), :78 (AfterFunc), :82 (NewTimer), :85 (NewTicker); clock/clocktest/fake.go:239 (time.Sleep).
- `clock.System{}` (or an alias of the clock package) constructed directly in non-test library code, bypassing an injected clock at that site: client/discovery/names.go:161 (`fabricclock.System{}`), bridge/health_probe.go:72, secure/mattercert/verify.go:174, secure/attestation/builder.go:109, internal/clockvar/clockvar.go:29, groups/manager.go:195, controller/dcl/snapshot/snapshot.go:127, cluster/core/diagnostic_logs.go:228 (bootEpoch and clock), cluster/core/general_diagnostics.go:254, device/queue.go:118; plus clock/clock.go:102 (inside the port). Gate 1 listed 28 such sites (section 4a there, counting the braced groups); the cluster servers other than cluster/core, fabrictest, store, transport/btp and controller/ca no longer name `clock.System{}`.

### 4b randomness (non-test, outside script/ and examples/; crypto/rand unless noted; call-site lines; 48 files import crypto/rand or math/rand)
- transport/mrp: counter.go:40 Read(seed) - seam: none declared; retransmit.go has no generator (BackoffDuration takes `rand01 func() float64`, retransmit.go:53).
- bridge: handlers.go:310 Read - seam: field randomSource handlers.go:96 (used :268); case_initiator.go:458 Read - none; commissioning_window.go:962,989,1010 Read - none; receive_dispatch.go:948 math/rand/v2 (report jitter) - none; outbound_reliable.go:134,358 math/rand/v2 - none.
- bridge/bridgetest/commissioner.go:235 Read (test support) - none.
- secure/sigma: initiator.go:186,189; protocol.go:590,826,829,854; sigma.go:847 - none declared.
- secure/operational/manager.go:524,1308 Read - none.
- secure/attestation: builder.go:116,120,145,165,175,199; cd.go:90 - none.
- secure/spake2/spake2.go:690,692 - seam: package var randomScalarFn (:677).
- mdns/operational_resolver.go:136 math/rand/v2 (retry jitter) - none.
- groups: sender.go:259 Read - none; counter.go:80 Read - seam: field counterRandom func() (uint32, error) (groups/manager.go:157, default :193).
- node: node.go:420 Read(instanceID); security.go:631 ecdsa.GenerateKey(rand.Reader) - none.
- contract/dataversion.go:53,59 Read - package var InitialDataVersion (func() uint32) is the seam (:49).
- commissioning: attestation.go:110; csr.go:77,86,108 - none.
- bootid/bootid.go:39 cryptorand.Read(salt) - none.
- im/subscription/manager.go:220,226 Read - none.
- client/handshake/pase.go:104 Read - none.
- client/exchange/manager.go:129,264 Read - none; :160 default `m.rand = mrand.Float64` (math/rand/v2, imported as mrand :13); seam Config.Rand func() float64 (:67, field :90).
- client/interaction/interaction.go:124 default mrand.Float64 (mrand = math/rand/v2, :24); seam Config.Rand (:104), field :87; sustained.go:48.
- client/discovery/names.go:164 default rand.Uint32 (math/rand/v2, :10); seam field random func() uint32 (:92,:122), solicitor.go:81.
- client/peer/set.go:113 default rand.Float64 (math/rand/v2, :11); seam Config.Rand (:58), field :87.
- controller/client/node.go:85 default mrand.Float64 (math/rand/v2, :11); seam Config.Rand (controller/client/config.go:59), field node.go:28.
- controller/ca: ca.go:266 default rand.Reader; seam Config.Rand io.Reader (ca.go:179). controller/ca/keyfile/keyfile.go:174,193,201 Read - none.
- controller/commissioner: commissioner.go:198, fabric.go:122 default rand.Reader; seam Rand io.Reader (commissioner.go:62, fabric.go:44).
- cluster/core: operational_credentials.go:1396,1472,1523; vid_verification.go:214 (rand.Reader) - none; ota_software_update_requestor.go:340 default rand.IntN (math/rand/v2, :12), seam Config.Rand func(n int) int (:133); ota_provider.go:263 and commissioner_control.go:211 default rand.Reader, seam Config.Rand io.Reader (ota_provider.go:199, commissioner_control.go:127,178).
- cluster/lock: doorlock_server.go:621; auth.go:68 Read - none. cluster/thermo/presets.go:293 Read - none.
- fabrictest/net.go:85,106 math/rand/v2 PCG seeded from Options.Seed (test support).
- examples/: crypto/rand in quickstart-controller/main.go:259 and reference-controller/cli/{self.go:85,group.go:189,stack.go:108,window.go:253,301,313}; math/rand/v2 in reference-bridge/fleet_energy_market.go:214,267.
Imports of `math/rand/v2` in non-test library code: 10 files (fabrictest/net.go, client/exchange/manager.go, client/interaction/interaction.go, client/discovery/names.go, client/peer/set.go, bridge/receive_dispatch.go, bridge/outbound_reliable.go, mdns/operational_resolver.go, controller/client/node.go, cluster/core/ota_software_update_requestor.go); the other 38 import crypto/rand.

## 5 Errors

### 5a `errors.New("<prefix>:` per package directory vs package name (587 literals in 70 package dirs; counts after the prefix; 'none' = literal without `prefix:`)
68 of 70 package dirs use only their package name (or the dir name) as prefix; no literal is without a prefix (gate 1: 47 of 70 dirs conform, 23 differ, 596 literals). The 2 that differ:
- cluster/core [core] core:44, matter:1 (cluster/core/descriptor.go:110, a backquoted literal `matter: you must enable the descriptor "TagList" feature ...`)
- device [device] device:6, laundrydryercontrols:1 (device/defaults_declared.go:298), microwaveovenmode:1 (device/defaults_declared.go:254), valve:1 (device/defaults_declared.go:145)
(Method: go/ast over non-test files outside examples/ and script/, `errors.New` with a string-literal argument, prefix = text up to the first `:`; `fmt.Errorf` is not counted.)

### 5b typed error types (`func (e T) Error() string`, non-test; file:line of the Error method)
86 types with Error() in 36 package dirs (gate 1: 87); 55 types implement `MatterStatusCode() StatusCode` (im.StatusCodeError, im/status.go:31-34) in 41 files (gate 1: 58 / 42): bridge 4, cluster/alarm 1, alarmbase 1, closure 1, core 14, cover 1, energy 1, fan 1, levelcontrol 2, light 1, lock 1, media 1, modebase 1, modeselect 1, opstate 1, spec 2, switch 1, thermo 2, valve 1, wire 1, im 2 files.
- bdx: Error errors.go:31, StatusResponseError errors.go:83
- ble/bluez: Error doc.go:62
- bridge/bridgetest: StatusError session.go:38
- bridge: commWindowDurationInvalidErr commissioning_window.go:1038, fieldConstraintError fields_reader.go:108, fieldInvalidCommandError fields_reader_groupkeys.go:33, invokeStatusError ota_provider_link.go:85, chunkStatusError subscription_reestablish.go:211
- client/exchange: UnexpectedMessageError errors.go:70, PeerUnresponsiveError errors.go:86, PeerMessageMissingError errors.go:101, PeerInitiatedCloseError errors.go:121, StatusReportError errors.go:169
- client/interaction: StatusError interaction.go:66
- client/peer: UnreachableError connection.go:44
- cluster/alarm: statusError smokecoalarm_server.go:854; cluster/alarmbase: statusError alarmbase.go:514
- cluster/closure: closureUnsupportedCommandErr closurecontrol_server.go:254, closureInvalidCommandErr :263, closureConstraintErr :271
- cluster/core: aclStatusError access_control_auxiliary.go:24, bindingStatusError binding.go:467, refusedWrite definitions.go:35, diagLogsError diagnostic_logs.go:386, gendiagStatusErr general_diagnostics.go:784, groupKeyNotFoundErr group_key_management.go:92, groupKeyExhaustedErr :106, gkmStatusError :526, groupcastStatusError groupcast.go:160, groupsStatusError groups.go:113, netcommStatusError network_commissioning_wifi.go:264, opcredsInvalidArgErr operational_credentials.go:341, opcredsConstraintErr :358, opcredsFailsafeRequiredErr :724, opcredsInvalidCommandErr :2441, otaDownloadError ota_requestor_query.go:506, otaStatusError ota_software_update_requestor.go:438, scenesStatusError scenes_management.go:608, timeSyncReadOnlyErr time_synchronization.go:139, opcredsUnsupportedAccessErr vid_verification.go:242
- cluster/cover: windowCoveringConstraintErr windowcovering_server.go:464, maintenanceErr :518; cluster/energy: statusError waterheater.go:439; cluster/fan: statusError fancontrol_server.go:895
- cluster/levelcontrol: constraintErr levelcontrol_server.go:1312, invalidCommandErr :1319, failureErr transitions.go:436
- cluster/light: colorControlConstraintErr colorcontrol_server.go:605, colorControlInvalidCommandErr :611
- cluster/lock: LockOperationFailedError doorlock_server.go:198, occupiedError users.go:36
- cluster/measurement: writeRefusal definitions.go:100; cluster/media: StatusError media.go:89; cluster/modebase: statusError modebase_server.go:850; cluster/modeselect: invalidCommandErr modeselect_server.go:328; cluster/opstate: statusError opstate_server.go:1004; cluster/spec: FieldError codec.go:87, StatusError write.go:29; cluster/switch: statusError server.go:249
- cluster/thermo: tcStatusError temperaturecontrol.go:383, thermoConstraintErr thermostat_server.go:1457, thermoInvalidCommandErr :1466; cluster/valve: constraintError valve_server.go:499
- cluster/wire: admCommBusyErr admincommissioning.go:512, admCommPakeErr :531, admCommWindowNotOpenErr :546, admCommInvalidFieldsErr :560
- controller/attestation: Error check.go:66; controller/client: peerShutdownError fabricops.go:50
- controller/commissioner: CommissioningError errors.go:58, PairRetransmissionLimitReachedError errors.go:86, PeerCommunicationError errors.go:104, CommissioningTransitionError errors.go:122
- controller/dcl: Error client.go:42, ResponseError client.go:64, gitHubHTTPError github.go:72, OTAError ota.go:34, crlRejectedError revocation.go:65
- endpoint: DeviceTypeConformanceError devicetype_validation.go:363; fabrictest: eventStatusError wire.go:115; groups: NoKeyError decode.go:52; im: invokeDecodeStatusError invoke.go:96, unsupportedCommandError status.go:111; transport/btp: ProtocolError codec.go:16, FlowError codec.go:24

### 5c status carried by error text
- endpoint/dispatcher.go: `classifyError` (dispatcher.go:816-830, doc :809-815) maps by type only: `errors.AsType[im.MatterClusterStatusError]` :820, `errors.AsType[im.StatusCodeError]` :823, else FAILURE. The gate-1 string fallback (writeErrorStatus, invokeErrorStatus, containsAny) is gone; endpoint/dispatcher_test.go:893 TestWriteErrorStatus_WordingIsNoStatus asserts a plain error whose message names a status answers FAILURE. grep over endpoint, cluster, im, bridge for `strings.Contains/HasPrefix` on an error: no hit.
- Plain errors whose wording matches the removed fallback (read-only / unknown command ...), site only: cluster/core/{general_commissioning.go:392 (wrapped in refusedWrite with status from the definition, read), icd_management.go:87, network_commissioning.go:230, network_commissioning_wifi.go:245, groups.go:198, scenes_management.go:487, diagnostic_logs.go:373, general_diagnostics.go:509}, cluster/wire/{schedules.go:39, scenes_management.go:42}, cluster/lock/doorlock_server.go:109 (errUnknownCommand). Not read: whether each reaches the dispatcher unwrapped.
- A stale comment still names the removed substring path: cluster/core/group_key_management.go:746 ("substring \"constraint\" triggers that path"); the line below it returns `spec.Errorf(im.StatusConstraintError, ...)` (:747).
- `im.UnsupportedCommandf` (im/status.go) is called from 38 cluster files (41 uses); cluster servers import `im` for status spelling (section 1c).

## 6 Logging

Method: go/ast over non-test files outside examples/ and script/; first string literal of `.Info/.Warn/.Debug/.Error(` and the *Context variants (first argument after ctx). 571 calls with a literal first argument (gate 1: 565 + 7 on the next line). Dotted lower case `a.b.c` = regex `^[a-z][a-z0-9_]*(\.[a-z0-9_]+)+$`.

### 6a event names that are not dotted lower case
- Literal event names not matching the regex: 0 of 571 (gate 1: 10 literal sites + 3 variable-named).
- Non-literal first argument on a logger-named receiver: 5: binding/resolve.go:61 (`msg`), node/commissioning.go:240 (`event`), bridge/receive_dispatch.go:316 and :324 (`"matter.tx." + op + ".chunk_rejected"` / `".chunk_ack_timeout"`), bridge/subscribe.go:517 (`"matter.tx.subscribe." + op`).

### 6b printing and the global logger
- `fmt.Print*`, `fmt.Fprint*(os.Std...)`, `log.Print*`, `println` in non-test code outside examples/ and script/: ble/bluez/cmd/reference-controller/main.go:21 `fmt.Fprintln(os.Stderr, ...)` (a main package). No import of package `log` in non-test code. (The scan also lists `log.X` calls on variables named `log`, which are *slog.Logger values, and three fmt.Printf in notes/briefs/reports/BX3-scripts/bx3-mutex.go, a script under notes/; none is the global logger.)
- Structs with a `*slog.Logger` field: 87 fields (gate 1: 69) in 25 packages: bdx 1, binding 2, bridge 1, client/discovery 1, client/exchange 2, client/handshake 5, client/interaction 7, client/peer 3, cluster/core 15, cluster/cover 2, cluster/light 1, cluster/thermo 2, controller/ble 5, controller/client 6, controller/commissioner 4, controller/dcl 4, endpoint 1, fabrictest 3, groups 2, im/subscription 1, mdns 4, node 7, transport/btp 2, transport/tcp 4, transport/udp 2. Cluster servers outside cluster/core with a logger field: cluster/cover (2), cluster/light (1), cluster/thermo (2).
- `slog.Default()` calls: 37, none inline as `slog.Default().<Level>(...)` (gate 1: 44 inline calls in 15 files). Sites (line text: `logger = slog.Default()`, `cmp.Or(cfg.Logger, slog.Default())`, `return slog.Default()`, or a package variable): bdx/responder.go:102, bridge/bridge.go:740, client/discovery/names.go:144, cluster/core/{access_control.go:1245, basic_information.go:327 (`cmp.Or(cfg.Logger, slog.Default())`), :1083, bridged_device_basic_information.go:209, diagnostic_logs.go:228, :267, general_commissioning.go:249, :1003, general_diagnostics.go:1073, operational_credentials.go:480, :2481, ota_provider.go:267, ota_software_update_requestor.go:343}, cluster/cover/windowcovering_server.go:332, :933, cluster/light/colorcontrol_server.go:646, cluster/thermo/thermostat_server.go:371, :1484, endpoint/assembler.go:120, groups/manager.go:184, im/subscription/manager.go:194, mdns/{operational_resolver.go:78, service.go:318, :531, subtype_responder.go:104, zeroconf.go:775}, node/{node.go:145, persist.go:84}, transport/tcp/{conn.go:74, listener.go:113}, transport/udp/{listener.go:145, :350}; package variables initialised with it: device/defaults.go:91 (`var Logger = slog.Default()`), tlv/encode.go:17 (`var Logger = slog.Default()`). Package-level `slog.Warn/Info/...` calls: none.

## 7 Persistence outside the seam
`os.Open/OpenFile/Create/CreateTemp/WriteFile/ReadFile/MkdirAll/Mkdir/MkdirTemp/Rename/Remove/RemoveAll/Stat/Lstat/ReadDir/Truncate/Chmod/Symlink` and `sql.Open`, non-test, outside store/, endpoint/sqlitestore/, controller/client/sqlstore/, examples/, script/ (go/ast scan; node/store_path.go listed because it is the node's seam, bdx/sink.go listed because gate 1 excluded it):
- fabrictest/device.go:116 sql.Open (test support)
- secure/attestation/files.go:129 os.ReadFile(path)
- node/ota.go:108 os.Remove; :112 os.MkdirAll; :116 os.Rename; :117 os.Remove; :165 os.Open; :222 os.MkdirAll (gate 1: :71-:80, :127, :178)
- node/store_path.go:30 sql.Open (the node's store opener)
- controller/dcl/store.go:58 os.ReadFile; :67 os.CreateTemp; :73,:77 os.Remove; :80 os.Rename (FileStore.Path)
- controller/dcl/snapshot/fetch.go:91 os.RemoveAll; :94 os.MkdirAll; :99,:107 os.WriteFile
- controller/ca/keyfile/keyfile.go:125 os.ReadFile; :209 os.OpenFile; :215,:219 os.Remove
- cluster/core/ota_requestor_query.go:185 os.Stat; :189,:388,:474 os.Remove; :621 os.Open
- bdx/sink.go:61,:88 os.CreateTemp; :97,:102,:106,:110 os.Remove; :109 os.Rename
Gone since gate 1: bridge/subscribe_dispatch.go os.WriteFile("/tmp/subscribe-init-...") (the dumpSubscribeEnabled gate; no `GOFABRIC_DUMP` or `dumpSubscribe` string remains in the tree). No other file access with the listed functions.

## 8 Facade surface

Method: a go/types walk (golang.org/x/tools/go/packages from a scratchpad module) of every exported constant, variable, function, type (underlying type, exported fields) and exported method of the nine facade packages: node, device, contract, fabrictest, controller/client, controller/commissioner, controller/ca, controller/dcl, controller/attestation. A reference is reported when a declared type comes from a module package that is not itself one of the nine, nor `schema`, `eligibility`, `cluster/spec` or a `cluster/spec/<cluster>` package, or from a non-stdlib third-party package, and is not reached through an alias declared in the facade package. `(alias)` after a name = the type is reached through an alias declared in a lower package (not a facade-owned one). Alias declarations in a facade package are listed under 'owned aliases'. `go doc -all` line counts: node 1289, device 3032, contract 909, fabrictest 685, controller/client 988, controller/commissioner 430, controller/ca 325, controller/dcl 759, controller/attestation 306. Declaration line = file:line of the identifier.

### 8a identifiers whose type comes from a lower package without a facade-owned alias

- **contract**: 0 declarations
- **controller/attestation**: 0 declarations
- **controller/ca**: 2 declarations
  - controller/ca/ca.go:158 type Config -> clock.Clock
  - controller/ca/ca.go:587 method CA.Verifier -> mattercert.Verifier
- **controller/client**: 15 declarations
  - controller/client/addresses.go:153 method Registry.ForDevice -> discovery.Device
  - controller/client/config.go:19 type Config -> exchange.Clock (alias), interaction.Interaction, peer.Peer
  - controller/client/config.go:67 type SubscribeOptions -> im.ConcreteAttributePath, im.ConcreteEventPath
  - controller/client/config.go:88 type Hooks -> interaction.EventData
  - controller/client/conformance.go:57 type ConformanceReport -> endpoint.DeviceTypeViolation
  - controller/client/interact.go:132 method Node.Write -> interaction.WriteOptions, interaction.WriteRequest, interaction.WriteStatuses
  - controller/client/interact.go:142 method Node.Invoke -> im.ConcreteCommandPath, interaction.InvokeOptions, interaction.Result
  - controller/client/interact.go:18 type ReadRequest -> interaction.ReadRequest
  - controller/client/interact.go:32 method Node.Read -> interaction.Report
  - controller/client/interact.go:68 method Node.Subscribe -> interaction.SubscribeRequest, interaction.Subscription
  - controller/client/record.go:62 type Record -> discovery.SupportedTransports, handshake.PeerAddress (alias), sessionparams.Parameters
  - controller/client/record.go:680 method Node.PeerAddress -> handshake.PeerAddress (alias)
  - controller/client/registry.go:24 type RegistryConfig -> exchange.Clock (alias), exchange.Manager, interaction.Subscriptions, peer.Set
  - controller/client/registry.go:475 method Registry.GetByAddress -> handshake.PeerAddress (alias)
  - controller/client/registry.go:508 method Registry.ForAddress -> handshake.PeerAddress (alias)
- **controller/commissioner**: 10 declarations
  - controller/commissioner/commissioner.go:103 type Options -> discovery.Device, handshake.PeerAddress (alias), peer.Timing, sessionparams.Parameters
  - controller/commissioner/commissioner.go:157 type Result -> handshake.PeerAddress (alias)
  - controller/commissioner/commissioner.go:167 type PASEOptions -> discovery.Device
  - controller/commissioner/commissioner.go:237 method Commissioner.Interaction -> interaction.Interaction, peer.Peer
  - controller/commissioner/commissioner.go:343 method Commissioner.EstablishPASE -> discovery.Device, exchange.Session
  - controller/commissioner/commissioner.go:47 type Context -> exchange.Clock (alias), exchange.Manager, handshake.ResumptionStore, interaction.Subscriptions, peer.Set
  - controller/commissioner/commissioner.go:86 type Channel -> exchange.Manager
  - controller/commissioner/fabric.go:318 method Fabric.AddressOf -> handshake.PeerAddress (alias)
  - controller/commissioner/fabric.go:324 method Fabric.Lookup -> handshake.Fabric, handshake.PeerAddress (alias)
  - controller/commissioner/fabric.go:75 type Fabric -> handshake.Fabric
- **controller/dcl**: 2 declarations
  - controller/dcl/certstore.go:34 type CertificateOptions -> exchange.Clock (alias)
  - controller/dcl/ota.go:295 func VerifyImage -> otaimage.Header
- **device**: 1 declarations
  - device/device.go:553 method Device.Spec -> endpoint.Spec
- **fabrictest**: 27 declarations
  - fabrictest/access.go:25 method Pair.Grant -> store.Privilege
  - fabrictest/controller.go:326 method Controller.Exchanges -> exchange.Manager
  - fabrictest/controller.go:333 method Controller.Names -> discovery.Names
  - fabrictest/controller.go:354 method Controller.Peers -> peer.Set
  - fabrictest/controller.go:361 method Controller.Resumption -> peer.MemoryResumptionStore (alias)
  - fabrictest/controller.go:368 method Controller.Subscriptions -> interaction.Subscriptions
  - fabrictest/controller.go:437 type Pair -> exchange.Session, handshake.PeerAddress (alias), interaction.Interaction, peer.Peer
  - fabrictest/device.go:265 method Device.Peers -> peer.Set
  - fabrictest/device.go:300 method Device.Bridge -> bridge.Bridge
  - fabrictest/device.go:304 method Device.Store -> store.Store
  - fabrictest/device.go:330 method Device.Advertiser -> mdns.Advertiser
  - fabrictest/device.go:338 method Device.Endpoints -> endpoint.Endpoint
  - fabrictest/device.go:45 type Topology -> bridge.Snapshotter
  - fabrictest/node.go:22 method Controller.Discover -> discovery.Device, discovery.Identifier
  - fabrictest/raw.go:24 func Exchange -> exchange.Message
  - fabrictest/site.go:130 method Site.Clock -> clock.Clock
  - fabrictest/site.go:134 method Site.Fake -> clocktest.Fake
  - fabrictest/site.go:32 type Options -> clock.Clock
  - fabrictest/wire.go:125 func Write -> im.ConcreteAttributePath
  - fabrictest/wire.go:130 func WriteTimed -> im.ConcreteAttributePath
  - fabrictest/wire.go:149 func Invoke -> im.ConcreteCommandPath, interaction.Result
  - fabrictest/wire.go:155 func InvokeTimed -> im.ConcreteCommandPath, interaction.Result
  - fabrictest/wire.go:173 func Subscribe -> interaction.SubscribeRequest, interaction.Subscription
  - fabrictest/wire.go:46 func Connect -> interaction.Interaction
  - fabrictest/wire.go:64 func Read -> im.ConcreteAttributePath, interaction.AttributeData
  - fabrictest/wire.go:83 func ReadWith -> interaction.ReadRequest, interaction.Report
  - fabrictest/wire.go:96 func Events -> im.ConcreteEventPath, interaction.EventData
- **node**: 14 declarations
  - node/aggregator.go:29 type Endpoint -> endpoint.Spec
  - node/client.go:172 method Node.Peers -> peer.Set
  - node/client.go:183 method Node.Bindings -> binding.Manager
  - node/client.go:33 type Client -> discovery.Names
  - node/commissioner_control.go:56 method Node.CommissionerControl -> core.CommissionerControl
  - node/config.go:32 type Config -> clock.Clock, groups.Manager, mdns.Advertiser
  - node/config.go:362 type Store -> endpoint.Store, store.Store
  - node/node.go:792 method Node.Bridge -> bridge.Bridge
  - node/node.go:885 method Node.Store -> store.Store
  - node/node.go:898 method Node.Groups -> groups.Manager
  - node/ota_provider.go:93 method Node.OTAProvider -> core.OTAProvider
  - node/persist.go:68 type EndpointPersistence -> endpoint.SourceKey
  - node/persist.go:82 func NewEndpointPersistence -> store.Store
  - node/root.go:59 type RootClusters -> core.AccessControl, core.BasicInformation, core.Descriptor, core.DiagnosticLogs, core.GeneralCommissioning, core.GeneralDiagnostics, core.GroupKeyManagement, core.Groupcast, core.NetworkCommissioning, core.OTASoftwareUpdateRequestor, core.OperationalCredentials, core.PowerSourceConfiguration, core.TimeSynchronization, core.WiFiNetworkCommissioning, core.WiFiNetworkDiagnostics, wire.AdministratorCommissioning

### 8b owned aliases (facade-package `type X = lower.T`)

- device (4): device/options.go:53 SourceKey =endpoint.SourceKey; device/options.go:57 StringKey =endpoint.StringKey; device/options.go:61 AvailabilityProbe =endpoint.AvailabilityProbe; device/options.go:84 Option =device.Declaration
- node (26): node/attestation.go:17 AttestationFiles =attestation.ChainFiles; node/facade.go:129 OTAImageSource =core.OTAImageSource; node/facade.go:132 OTAConsent =core.OTAConsent; node/facade.go:135 OTAConsentState =core.OTAConsentState; node/facade.go:138 OTAApplyPolicy =core.OTAApplyPolicy; node/facade.go:141 OTAPeer =core.OTAPeer; node/facade.go:144 OTAUpdateStatus =core.OTAUpdateStatus; node/facade.go:147 OTAApplier =core.OtaApplier; node/facade.go:150 WiFiReporter =core.WiFiReporter; node/facade.go:153 CommissioningApprover =core.CommissioningApprover; node/facade.go:156 ReverseCommissioner =core.ReverseCommissioner; node/facade.go:159 CapabilityMinimaStruct =core.CapabilityMinimaStruct; node/facade.go:162 GeneralDiagnosticsFeatures =core.GeneralDiagnosticsFeatures; node/facade.go:165 DeviceTypeValidationMode =endpoint.DeviceTypeValidationMode; node/facade.go:168 WindowStatus =wire.WindowStatusSnapshot; node/facade.go:171 AttestationChain =attestation.Chain; node/facade.go:31 Snapshotter =bridge.Snapshotter; node/facade.go:35 Topology =endpoint.Topology; node/facade.go:39 TCPConfig =bridge.TCPConfig; node/facade.go:43 CommissioningWindow =bridge.CommissioningWindow; node/facade.go:48 SessionManager =operational.Manager; node/facade.go:53 SubscriptionManager =subscription.Manager; node/facade.go:57 DiagnosticEvent =diagevent.Event; node/health.go:16 HealthSample =bridge.HealthSample; node/mdns.go:25 MDNSFinding =mdns.Finding; node/mdns.go:29 MDNSService =mdns.Service

### 8c matter.js-internal vocabulary (Behavior / Agent / Transaction / Backing / Hull)

Exported identifier names containing any of the five words (case-sensitive) in the nine packages: none. Prose mentions in `go doc -all` output (lines containing the word, case-insensitive; doc line numbers in docs generated into the scratchpad): `behavio` node 4, device 7, contract 13, fabrictest 2, controller/client 13, controller/commissioner 0, controller/ca 0, controller/dcl 0, controller/attestation 0; `agent` 0 in all nine; `transaction` 0 in all nine; `hull` 0 in all nine; `backing` contract 2, controller/client 1, others 0.
Identifier-adjacent hits (matter.js class or file names written in doc text): node doc:550 `TimeSynchronizationBehavior (GeneralDiagnosticsServer.ts:168-182)` inside Config; node doc:9, :601 'behaviour ids' (matter.js ServerNode.Configuration); node doc:887 `packages/node/src/behaviors/administrator-commissioning/`; device doc:17 'overrides a behavior', doc:1638-1639 'an endpoint type and its behaviors (`Endpoint.with(...)`, the behaviors' initial state, a behavior subclass', doc:2331-2333 'matter.js host setting behavior state ... Datasource.ts', doc:2354-2356 'MutableEndpoint.with adds any behavior ... SupportedBehaviors.extend'; contract doc:109 'as a matter.js behavior does', doc:120 and :796 `ServerBehaviorBacking` (named in the text of the QuietEvent / quiet-property contract), doc:126 and :757 Datasource.ts, doc:168/:829/:847 behaviors/ paths; controller/client doc:9, :25, :27, :33, :152-153, :183, :210, :214, :220-221 `PeerBehavior.ts`, doc:221 `ClientBehaviorBacking.ts`, doc:52 'generated client behaviour types'; fabrictest doc:204 'MockSite.addController builds a ServerNode with the controller behavior', doc:426 behaviors/groupcast path.

## 9 Tests

### 9a packages without test files
`go list` (231 packages). With the briefed template (`{{if not .TestGoFiles}}`) 40 packages match; 33 of those have only external `_test` package files (XTestGoFiles). Packages with no test file of any kind (7; gate 1: 6): examples/reference-controller, internal/bridgeseam, internal/channelseam, internal/deviceseam (new), internal/nodeseam, internal/udpseam, script/dclsnapshot.
External-test-only (33): . (module root), bdx/otaimage, bridge/bridgetest, clock, clock/clocktest, cluster/{alarm,alarmbase,boolcfg,closure,cover,fan,filter,light,lock,media,modebase,modeselect,pump,transition,valve}, cluster/spec/spectest, commissioning, controller/ble/bletest, controller/ca, controller/client/sqlstore, endpoint/endpointtest, endpoint/sqlitestore, internal/clockvar, parity, secure, secure/sessionparams, store, transport.
Counts over test files (grep): 6,929 `t.Parallel()` calls; 187 `time.Sleep(` and 146 `time.After(` in `_test.go`; 169 `t.Skip*` calls (52 tests skipped in the run below); 6 test files mention `testing.Short()`.

### 9b one run of `go test -count=1 -json ./...` (this worktree, c9e7c473 = origin/main after #213)
Wall time 497 s (01:48:55 to 01:57:12 local); exit 1; 231 packages: 223 pass, 7 no test files, 1 fail (client/interaction); 10,364 tests passed, 1 failed, 52 skipped; summed per-package elapsed 470 s. The machine was shared with other jobs of the same session (a coordinator guard run, scratchpad files ci.log/race.log/cover-check.log appeared in the same period), so the elapsed figures include load; gate 1 ran 364 s wall on a quiet worktree. The 15 slowest packages (elapsed per package, parallel run):
- controller/client 161.8 s (gate 1: 108.8); node 81.4 s (73.2); script/clustergen 30.3 s (23.5); examples/reference-bridge 20.8 s (17.2); cluster/lock 17.9 s (18.8); examples/reference-controller/cli 14.0 s (12.1); cluster/thermo 13.7 s (8.4); fabrictest 12.9 s (12.8); bridge 7.8 s (6.3); internal/chipdm 6.3 s (5.7); . (module root) 5.8 s (7.1); controller/commissioner 4.8 s; cluster/onoff 4.4 s (3.8); mdns 4.4 s (4.0); store 4.3 s (4.2).
The one failure: `TestLiveSustainedSurvivesRestart` (client/interaction/live_test.go:322), 0.52 s, message `live_test.go:357: subscription 3070824150 active true, want 1363156072` (the subscription id after the restart differs from the id recorded before it; the check at live_test.go:356-358 is `sub.ID() != id || !sub.Active()`). Run alone (`go test -count=20 -run TestLiveSustainedSurvivesRestart ./client/interaction`): 20 of 20 pass (6.4 s total).

### 9c the named flaky tests (file:line; what each waits on)
- TestLiveSustainedSurvivesRestart - client/interaction/live_test.go:322 (failed in 9b). Waits: waitOnOff (live_test.go:143-155, lv.site.Eventually under a 30 s wall context :145), lv.wait("the subscription active", 10 s, sub.Active) :339, waitPersisted (:392-404, 30 s), restartCtx 30 s :346, lv.run(restartCtx, lv.pair.Device.Restart) :347. The assertion that failed is :356-358 (`sub.ID() != id || !sub.Active()`). Site.Eventually: fabrictest/site.go:302-318 (loops Site.quiet and advances the fake clock).
- TestRegistryDecommission - controller/client/registry_test.go:296 (t.Parallel). Registry on a clocktest.Fake. Decommission runs in a goroutine (:304), the test waits with `sr.next(t)` (:305, :315; controller/client/helpers_test.go:93-116: select on the scripted conn against `time.After(scriptedHangBound)` with `scriptedHangBound = time.Minute` helpers_test.go:89) and `<-done` (:321, no timeout). At :311 it asserts a second fabric operation is refused while the first is in flight (client.ErrFabricOperationInProgress).
- TestNodeLifecycleSeededPairParity - controller/client/node_lifecycle_seeded_parity_matterjs_test.go:49 (two parallel subtests). newClientPair (controller/client/parity_matterjs_helpers_test.go): CommissionNode under parityCtx = 3 min (:29-35), then `cp.eventually("the default subscription", n.SubscriptionActive)` :115 = Site.Eventually under parityCtx (:121-125); second subtest Controller.Restart(parityCtx) (node_lifecycle...:73).
- TestNetworkLossAndDelay - fabrictest/site_test.go:194. testContext = 60 s (site_test.go:61-66). Waits are inside fabrictest.Read: a read over Loss:1 must fail (Net().Set Loss:1 :198), then Delay 300 ms with Duplicate 1 (:204), then one dropped datagram via Net().Filter (:216); no explicit sleep or Eventually in the test body.
- TestDiagLogsBDX_Transfer - cluster/core/diagnostic_logs_bdx_test.go:277 (t.Parallel). Real time, channels only: `<-answered` (:302), select on `<-done` against `time.After(50 * time.Millisecond)` (:318-322, a negative wait: the transfer must not have ended before the response), `<-done`, `<-c.ended`, `<-third`, `<-done3`; a gate channel (:283) releases the collector; the unconditional receives carry no timeout.
- TestOtaParityMatterJS - node/ota_parity_matterjs_test.go:399. holdSite(t) (node/parity_matterjs_helpers_test.go:58) serialises sites; `s.eventually` = Site.Settle loop under parityCtx = 90 s (parity_matterjs_helpers_test.go:33-39); restartInto runs `site.Run(parityCtx, ...)`.
- TestDelayReportDataParity - node/delay_report_data_parity_matterjs_test.go:46 (seven or more parallel subtests, each newDeferHost :320). deferHost.advanceTo (:409-428) loops `site.Settle(ctx)`, `fake.Next()`, `fake.Advance(max(next-now, 0))` up to the target time, then a final `fake.Advance(rest)` and Settle; expectReportAt (:434-452) checks no report before `earliest-1ms`, then advances to `latest` and asserts the report's time within [earliest, latest] (fatal messages `a report at %v, before %v`, `no report by %v`, `the report at %v, want within [%v, %v]`). The sender's wait (the subscription's send delay) is observed only through Site.Settle.
- controller/commissioner errWon - controller/commissioner/connection.go: `errWon` defined :305-306 (cause the winner aborts the other attempts with); set at :284 (`c.abort(errWon)` under c.mu when the first PASE attempt succeeds, `c.winner` set at :283); a late success becomes a loser at :277-281 (`loserCause = context.Cause(actx)`, else errLostRace :31), and `loser.HandlePeerLoss(loserCause)` runs at :296 outside the lock; EstablishPASE additionally calls `res.session.HandlePeerLoss(...)` at controller/commissioner/commissioner.go:352 when ContinueAfterPASE declines; closePASESession (commissioner.go:300-305) calls `s.HandlePeerLoss(cause)`. `HandlePeerLoss` is defined on the exchange Session (client/exchange/peerloss_test.go:61 TestSessionHandlePeerLoss exercises it). Which test surfaces errWon was not determined here.
- secure/operational reaper tests - secure/operational/manager_idle_reaper_test.go: TestStartReaper_EvictsIdleSessionGracefully :85, TestStartReaper_ActiveSessionSurvivesSweeps :115, TestStartReaper_NeverMarkedSessionIsNotReaped :147, TestStopReaperWaitsForTheReaper :167. Changed since gate 1: they now run on a clocktest.Fake (newReaperManager :41-43; advanceUntilClosed advances reaperTestPoll = 10 s per step, bounded by reaperTestSteps), no wall-clock poll or time.Sleep.

## 10 The declaration

Sources: `go doc ./cluster/spec/<cluster> Declare` read through go/types (golang.org/x/tools/go/packages, scratchpad module); device/defaults*.go read for the kit's defaults. The generator that writes the Declare types is script/clustergen (ports in script/clustergen/ports_device.go, ports_media.go per the BU-b report); the generated files are not hand-edited.

### 10a Declare types and their fields

119 `Declare` types, one per cluster/spec/<cluster> package (all 121 packages under cluster/spec except `cluster/spec` itself and `cluster/spec/spectest`). Field counts: Initial 119, OnWrite 119, HostServer 119 (`contract.ClusterServer`), Features 87, Commands 74 (`[]uint32`), Events 41 (`[]uint32`); 264 `Handle<Command>` func fields in 74 packages (e.g. onoff: HandleOff/HandleOn/HandleToggle/HandleOffWithEffect/HandleOnWithRecallGlobalScene/HandleOnWithTimedOff); `Transitions bool` in colorcontrol, levelcontrol, windowcovering. Methods on every Declare: `ClusterDeclaration() spec.ClusterDeclaration`, `ClusterID() uint32` (go doc ./cluster/spec/onoff Declare).
Port fields (every Declare field that is not Initial, Features, Commands, Events, OnWrite, HostServer or Handle*): 48 packages. Format: package | Declare site | field type [kind from go/types; interface method count].
- accountlogin | cluster/spec/accountlogin/server_gen.go:134 | AccountLogin AccountLogin [interface, 3 methods]
- activatedcarbonfiltermonitoring | cluster/spec/activatedcarbonfiltermonitoring/server_gen.go:154 | Resetter Resetter [interface, 1 methods]; LastChangedTimeWriter LastChangedTimeWriter [interface, 1 methods]
- applicationlauncher | cluster/spec/applicationlauncher/server_gen.go:142 | AppLauncher AppLauncher [interface, 3 methods]
- audiooutput | cluster/spec/audiooutput/server_gen.go:134 | Outputs Outputs [interface, 1 methods]
- booleanstateconfiguration | cluster/spec/booleanstateconfiguration/server_gen.go:174 | Delegate Delegate [interface, 4 methods]
- channel | cluster/spec/channel/server_gen.go:180 | Channels Channels [interface, 3 methods]
- closurecontrol | cluster/spec/closurecontrol/server_gen.go:184 | Mover Mover [interface, 2 methods]
- closuredimension | cluster/spec/closuredimension/server_gen.go:187 | DimensionHandler DimensionHandler [interface, 2 methods]
- colorcontrol | cluster/spec/colorcontrol/server_gen.go:505 | Transitions bool [bool]; ColorTemperatureWriter ColorTemperatureWriter [interface, 1 methods]; ColorWriter ColorWriter [interface, 1 methods]
- commoditytariff | cluster/spec/commoditytariff/server_gen.go:223 | TimeSource TimeSource [interface, 1 methods]
- contentlauncher | cluster/spec/contentlauncher/server_gen.go:130 | ContentLauncher ContentLauncher [interface, 2 methods]
- deviceenergymanagement | cluster/spec/deviceenergymanagement/server_gen.go:234 | DemManager DemManager [interface, 11 methods]
- deviceenergymanagementmode | cluster/spec/deviceenergymanagementmode/server_gen.go:139 | ModeChanger ModeChanger [interface, 1 methods]
- dishwasheralarm | cluster/spec/dishwasheralarm/server_gen.go:145 | Delegate Delegate [interface, 2 methods]
- dishwashermode | cluster/spec/dishwashermode/server_gen.go:139 | ModeChanger ModeChanger [interface, 1 methods]
- doorlock | cluster/spec/doorlock/server_gen.go:516 | StateSource StateSource [interface, 3 methods]; AuthStore AuthStore [interface, 4 methods]
- energyevsemode | cluster/spec/energyevsemode/server_gen.go:139 | ModeChanger ModeChanger [interface, 1 methods]
- fancontrol | cluster/spec/fancontrol/server_gen.go:187 | StateSource StateSource [interface, 2 methods]; Stepper Stepper [interface, 1 methods]
- hepafiltermonitoring | cluster/spec/hepafiltermonitoring/server_gen.go:154 | Resetter Resetter [interface, 1 methods]; LastChangedTimeWriter LastChangedTimeWriter [interface, 1 methods]
- keypadinput | cluster/spec/keypadinput/server_gen.go:112 | Keypad Keypad [interface, 1 methods]
- laundrywashermode | cluster/spec/laundrywashermode/server_gen.go:139 | ModeChanger ModeChanger [interface, 1 methods]
- levelcontrol | cluster/spec/levelcontrol/server_gen.go:248 | Transitions bool [bool]; LevelSource LevelSource [interface, 13 methods]
- lowpower | cluster/spec/lowpower/server_gen.go:111 | LowPower LowPower [interface, 1 methods]
- mediainput | cluster/spec/mediainput/server_gen.go:148 | Inputs Inputs [interface, 3 methods]
- mediaplayback | cluster/spec/mediaplayback/server_gen.go:309 | Playback Playback [interface, 3 methods]
- microwaveovencontrol | cluster/spec/microwaveovencontrol/server_gen.go:177 | Oven Oven [interface, 6 methods]
- modeselect | cluster/spec/modeselect/server_gen.go:157 | ModeSource ModeSource [interface, 5 methods]
- onoff | cluster/spec/onoff/server_gen.go:172 | Sink Sink [interface, 1 methods]
- operationalstate | cluster/spec/operationalstate/server_gen.go:179 | CommandHandler CommandHandler [interface, 1 methods]
- ovencavityoperationalstate | cluster/spec/ovencavityoperationalstate/server_gen.go:179 | CommandHandler CommandHandler [interface, 1 methods]
- ovenmode | cluster/spec/ovenmode/server_gen.go:139 | ModeChanger ModeChanger [interface, 1 methods]
- pumpconfigurationandcontrol | cluster/spec/pumpconfigurationandcontrol/server_gen.go:355 | StateSource StateSource [interface, 2 methods]
- refrigeratoralarm | cluster/spec/refrigeratoralarm/server_gen.go:145 | Delegate Delegate [interface, 2 methods]
- refrigeratorandtemperaturecontrolledcabinetmode | cluster/spec/refrigeratorandtemperaturecontrolledcabinetmode/server_gen.go:139 | ModeChanger ModeChanger [interface, 1 methods]
- rvccleanmode | cluster/spec/rvccleanmode/server_gen.go:139 | ModeChanger ModeChanger [interface, 1 methods]
- rvcoperationalstate | cluster/spec/rvcoperationalstate/server_gen.go:185 | CommandHandler CommandHandler [interface, 1 methods]
- rvcrunmode | cluster/spec/rvcrunmode/server_gen.go:139 | ModeChanger ModeChanger [interface, 1 methods]
- servicearea | cluster/spec/servicearea/server_gen.go:161 | Selector Selector [interface, 1 methods]; Skipper Skipper [interface, 1 methods]
- smokecoalarm | cluster/spec/smokecoalarm/server_gen.go:265 | StateSource StateSource [interface, 1 methods]
- targetnavigator | cluster/spec/targetnavigator/server_gen.go:128 | Targets Targets [interface, 1 methods]
- temperaturealarm | cluster/spec/temperaturealarm/server_gen.go:181 | Delegate Delegate [interface, 2 methods]
- temperaturecontrol | cluster/spec/temperaturecontrol/server_gen.go:144 | TemperatureSetter TemperatureSetter [interface, 1 methods]
- thermostat | cluster/spec/thermostat/server_gen.go:518 | Sink Sink [interface, 2 methods]; PresetStore PresetStore [interface, 2 methods]
- thermostatuserinterfaceconfiguration | cluster/spec/thermostatuserinterfaceconfiguration/server_gen.go:122 | SettingStore SettingStore [interface, 2 methods]
- valveconfigurationandcontrol | cluster/spec/valveconfigurationandcontrol/server_gen.go:207 | StateSource StateSource [interface, 8 methods]
- waterheatermanagement | cluster/spec/waterheatermanagement/server_gen.go:160 | Booster Booster [interface, 2 methods]
- waterheatermode | cluster/spec/waterheatermode/server_gen.go:139 | ModeChanger ModeChanger [interface, 1 methods]
- windowcovering | cluster/spec/windowcovering/server_gen.go:214 | Transitions bool [bool]; Mover Mover [interface, 2 methods]

### 10b self-held servers: kit default, declared port, requirement

Registry: `selfHeldDefaults` (device/defaults.go:126, 22 entries) extended by init() in device/defaults_ports.go:42 (9 ModeBase derivations), defaults_opstate.go:28 (3 OperationalState derivations), defaults_declared.go:57 (16), defaults_media.go:39 (12): 62 self-held default servers; the other 57 clusters with a Declare get the generated `spec.Server`. Port requirement is read from the factory body: 'port required' = the factory returns `notServed(errNoPort)` when the declaration's port is nil (or the opstate/mode generic builder does); 'port optional' = the server runs without the port on its own state (or wraps a nil-able port); 'no port' = the Declare has no port field. Last column: the sentinel errors the factory maps to `notServed` (selection not served). 34 port required, 14 port optional, 14 no port.
Format: cluster | registration site | factory | kit server | declared port field(s) | requirement | notServed on
- accountlogin | device/defaults_media.go:50 | newAccountLoginServer | cluster/media.NewAccountLogin | AccountLogin AccountLogin | port required | -
- activatedcarbonfiltermonitoring | device/defaults.go:148 | filterServer(filter.NewActivatedCarbonFilterMonitoring) | cluster/filter.NewActivatedCarbonFilterMonitoring | Resetter Resetter, LastChangedTimeWriter LastChangedTimeWriter | port optional | filter.ErrNoResetter
- applicationbasic | device/defaults_media.go:52 | newApplicationBasicServer | cluster/media.NewAppBasic | - | no port | media.ErrMissingState
- applicationlauncher | device/defaults_media.go:44 | newApplicationLauncherServer | cluster/media.NewAppLauncher | AppLauncher AppLauncher | port required | -
- audiooutput | device/defaults_media.go:46 | newAudioOutputServer | cluster/media.NewOutputs | Outputs Outputs | port required | -
- binding | device/defaults.go:129 | newBindingServer | cluster/core.NewBinding | - | no port | -
- booleanstate | device/defaults_declared.go:74 | newBooleanStateServer | cluster/measurement.NewHeldBooleanState | - | no port | -
- booleanstateconfiguration | device/defaults.go:149 | newBooleanStateConfigurationServer | cluster/boolcfg.New | Delegate Delegate | port optional | boolcfg.ErrInvalidConfig
- channel | device/defaults_media.go:47 | newChannelServer | cluster/media.NewChannels | Channels Channels | port required | -
- closurecontrol | device/defaults_declared.go:63 | newClosureControlServer | cluster/closure.New | Mover Mover | port required | closure.ErrFeatureMap
- closuredimension | device/defaults_declared.go:64 | newClosureDimensionServer | cluster/closure.NewDimension | DimensionHandler DimensionHandler | port required | -
- colorcontrol | device/defaults.go:134 | newColorControlServer | cluster/light.NewColorControl | Transitions bool [bool], ColorTemperatureWriter ColorTemperatureWriter, ColorWriter ColorWriter | port optional | light.ErrColorFeatures
- commoditymetering | device/defaults.go:154 | newCommodityMeteringServer | cluster/energy.NewCommodityMetering | - | no port | -
- commodityprice | device/defaults.go:150 | newCommodityPriceServer | cluster/energy.NewCommodityPrice | - | no port | -
- commoditytariff | device/defaults_declared.go:72 | newCommodityTariffServer | cluster/energy.NewCommodityTariff | TimeSource TimeSource | port optional | -
- contentlauncher | device/defaults_media.go:43 | newContentLauncherServer | cluster/media.NewContentLauncher | ContentLauncher ContentLauncher | port required | -
- deviceenergymanagement | device/defaults_declared.go:69 | newDeviceEnergyManagementServer | cluster/energy.NewDeviceEnergyManagement | DemManager DemManager | port required | -
- deviceenergymanagementmode | device/defaults_ports.go:53 | modeServer(modebase.NewDeviceEnergyManagementMode) | cluster/modebase.NewDeviceEnergyManagementMode | ModeChanger ModeChanger | port required | -
- dishwasheralarm | device/defaults.go:145 | alarmServer(alarmbase.NewDishwasherAlarm) | cluster/alarmbase.NewDishwasherAlarm | Delegate Delegate | port optional | alarmbase.ErrAdjustableThresholds,alarmbase.ErrThresholds
- dishwashermode | device/defaults_ports.go:45 | modeServer(modebase.NewDishwasherMode) | cluster/modebase.NewDishwasherMode | ModeChanger ModeChanger | port required | -
- doorlock | device/defaults.go:131 | newDoorLockServer | cluster/lock.New | StateSource StateSource, AuthStore AuthStore | port optional | lock.ErrConfig
- ecosysteminformation | device/defaults.go:135 | newEcosystemInformationServer | cluster/core.NewEcosystemInformation | - | no port | -
- electricalgridconditions | device/defaults.go:152 | newGridConditionsServer | cluster/energy.NewElectricalGridConditions | - | no port | -
- energyevse | device/defaults.go:151 | newEnergyEvseServer | cluster/energy.NewEnergyEvse | - | no port | -
- energyevsemode | device/defaults_ports.go:51 | modeServer(modebase.NewEnergyEvseMode) | cluster/modebase.NewEnergyEvseMode | ModeChanger ModeChanger | port required | -
- energypreference | device/defaults_declared.go:73 | newEnergyPreferenceServer | cluster/energy.NewEnergyPreference | - | no port | -
- fancontrol | device/defaults.go:139 | newFanControlServer | cluster/fan.NewServer | StateSource StateSource, Stepper Stepper | port required | -
- hepafiltermonitoring | device/defaults.go:147 | filterServer(filter.NewHepaFilterMonitoring) | cluster/filter.NewHepaFilterMonitoring | Resetter Resetter, LastChangedTimeWriter LastChangedTimeWriter | port optional | filter.ErrNoResetter
- keypadinput | device/defaults_media.go:42 | newKeypadInputServer | cluster/media.NewKeypad | Keypad Keypad | port required | -
- laundrydryercontrols | device/defaults_declared.go:70 | newDryerControlsServer | cluster/appliance.NewLaundryDryerControls | - | no port | -
- laundrywashercontrols | device/defaults.go:143 | newLaundryWasherControlsServer | cluster/appliance.NewLaundryWasherControls | - | no port | appliance.ErrLaundryValue
- laundrywashermode | device/defaults_ports.go:44 | modeServer(modebase.NewLaundryWasherMode) | cluster/modebase.NewLaundryWasherMode | ModeChanger ModeChanger | port required | -
- levelcontrol | device/defaults.go:128 | newLevelControlServer | cluster/levelcontrol.New | Transitions bool [bool], LevelSource LevelSource | port optional | levelcontrol.ErrFeatures
- lowpower | device/defaults_media.go:49 | newLowPowerServer | cluster/media.NewLowPower | LowPower LowPower | port required | -
- mediainput | device/defaults_media.go:45 | newMediaInputServer | cluster/media.NewInputs | Inputs Inputs | port required | -
- mediaplayback | device/defaults_media.go:41 | newMediaPlaybackServer | cluster/media.NewPlayback | Playback Playback | port required | -
- microwaveovencontrol | device/defaults_declared.go:66 | newMicrowaveControlServer | cluster/appliance.NewMicrowaveOvenControl | Oven Oven | port required | -
- microwaveovenmode | device/defaults_declared.go:67 | newMicrowaveModeServer | cluster/modebase.NewMicrowaveOvenMode | - | no port | -
- modeselect | device/defaults_declared.go:61 | newModeSelectServer | cluster/modeselect.NewServer | ModeSource ModeSource | port required | -
- onoff | device/defaults.go:127 | newOnOffServer | cluster/onoff.New | Sink Sink | port optional | -
- operationalstate | device/defaults_opstate.go:29 | opstateServer(opstate.NewServer) | cluster/opstate.NewServer | CommandHandler CommandHandler | port required | -
- ovencavityoperationalstate | device/defaults_opstate.go:41 | opstateServer(opstate.NewOvenCavityServer) | cluster/opstate.NewOvenCavityServer | CommandHandler CommandHandler | port required | -
- ovenmode | device/defaults_ports.go:48 | modeServer(modebase.NewOvenMode) | cluster/modebase.NewOvenMode | ModeChanger ModeChanger | port required | -
- pumpconfigurationandcontrol | device/defaults_declared.go:59 | newPumpServer | cluster/pump.NewServer | StateSource StateSource | port required | -
- refrigeratoralarm | device/defaults.go:144 | alarmServer(alarmbase.NewRefrigeratorAlarm) | cluster/alarmbase.NewRefrigeratorAlarm | Delegate Delegate | port optional | alarmbase.ErrAdjustableThresholds,alarmbase.ErrThresholds
- refrigeratorandtemperaturecontrolledcabinetmode | device/defaults_ports.go:49 | modeServer(modebase.NewRefrigeratorAndTemperatureControlledCabinetMode func(p any) | cluster/modebase.NewRefrigeratorAndTemperatureControlledCabinetMode | ModeChanger ModeChanger | port required | -
- rvccleanmode | device/defaults_ports.go:47 | modeServer(modebase.NewRvcCleanMode) | cluster/modebase.NewRvcCleanMode | ModeChanger ModeChanger | port required | -
- rvcoperationalstate | device/defaults_opstate.go:35 | opstateServer(opstate.NewRvcServer) | cluster/opstate.NewRvcServer | CommandHandler CommandHandler | port required | -
- rvcrunmode | device/defaults_ports.go:46 | modeServer(modebase.NewRvcRunMode) | cluster/modebase.NewRvcRunMode | ModeChanger ModeChanger | port required | -
- servicearea | device/defaults_declared.go:62 | newServiceAreaServer | cluster/servicearea.New | Selector Selector, Skipper Skipper | port required | -
- smokecoalarm | device/defaults_declared.go:71 | newSmokeCoAlarmServer | cluster/alarm.NewServer | StateSource StateSource | port required | -
- switchcluster | device/defaults.go:130 | newSwitchServer | cluster/switch.New | - | no port | -
- targetnavigator | device/defaults_media.go:48 | newTargetNavigatorServer | cluster/media.NewTargets | Targets Targets | port required | -
- temperaturealarm | device/defaults.go:146 | alarmServer(alarmbase.NewTemperatureAlarm) | cluster/alarmbase.NewTemperatureAlarm | Delegate Delegate | port optional | alarmbase.ErrAdjustableThresholds,alarmbase.ErrThresholds
- temperaturecontrol | device/defaults_declared.go:65 | newTemperatureControlServer | cluster/thermo.NewTemperatureControl | TemperatureSetter TemperatureSetter | port required | -
- thermostat | device/defaults.go:133 | newThermostatServer | cluster/thermo.New | Sink Sink, PresetStore PresetStore | port optional | thermo.ErrFeatures
- thermostatuserinterfaceconfiguration | device/defaults.go:153 | newThermostatUIServer | cluster/thermo.NewThermostatUserInterfaceConfiguration | SettingStore SettingStore | port optional | -
- valveconfigurationandcontrol | device/defaults_declared.go:60 | newValveServer | cluster/valve.NewServer | StateSource StateSource | port required | -
- wakeonlan | device/defaults_media.go:51 | newWakeOnLanServer | cluster/media.NewWakeOnLan | - | no port | -
- waterheatermanagement | device/defaults_declared.go:68 | newWaterHeaterServer | cluster/energy.NewWaterHeaterManagement | Booster Booster | port required | -
- waterheatermode | device/defaults_ports.go:52 | modeServer(modebase.NewWaterHeaterMode) | cluster/modebase.NewWaterHeaterMode | ModeChanger ModeChanger | port required | -
- windowcovering | device/defaults.go:132 | newWindowCoveringServer | cluster/cover.New | Transitions bool [bool], Mover Mover | port optional | cover.ErrFeatureMap

Self-held servers without a declared port (14): applicationbasic, binding, booleanstate, commoditymetering, commodityprice, ecosysteminformation, electricalgridconditions, energyevse, energypreference, laundrydryercontrols, laundrywashercontrols, microwaveovenmode, switchcluster, wakeonlan. Self-held servers with a declared port: 48. Declare port fields without a self-held server: none.
EnergyEvse: the kit's `newEnergyEvseServer` (device/defaults_appliance.go:241) is the state machine without a host (`energy.NewEnergyEvse`); evsedef.Declare has no port field (HostServer, Initial, Features, Commands, Handle* only); the reference bridge mounts the delegate-model server as `evsedef.Declare{HostServer: energy.NewEnergyEvse(...)}` (section 11).

### 10c fallback rules (device/device.go, device/defaults.go)

- device/device.go:348-377 `defaultServer`: for a cluster with a registry entry the factory builds the server; on an error that is not `ErrSelectionNotServed` the device build fails with `ErrConfiguration` (:362-363); on `ErrSelectionNotServed` (defaults.go:78-90): if the cluster was declared and either the declaration has ports or the error is not `errNoPort`, the build fails with that error (:364-368); otherwise the cluster keeps the generated server and the kit logs `device.default_server.generated` at Info (:373, `Logger` = device/defaults.go:91, a package variable initialised with `slog.Default()`).
- The generated default is `spec.NewServer(def, opts, spec.ServerConfig{Initial: initial, Unimplemented: true})` (device.go:380): commands unimplemented until the host registers handlers (comment :377-379). `spec.ErrFabricScoped` from it gives `ErrNoDefaultServer` with 'supply its server as the declaration's HostServer' (device.go:382-383); a cluster without a committed definition gives `ErrNoDefaultServer` (device.go:287-292; error text device/device.go:28-31). OtaSoftwareUpdateRequestor has no registry entry (device/defaults.go:119-124 comment); Binding and EcosystemInformation take core servers because the generated one refuses fabric-scoped clusters (defaults.go:119-122; Binding and EcosystemInformation factories defaults.go:185-200).
- `coupledLast = [LevelControl]` (defaults.go:157-160): LevelControl is built after the endpoint's other servers. `defaultBuild.sibling` hands a factory the servers built before it.

### 10d WithServer

Definition device/options.go:261-273 (`WithServer(srv contract.ClusterServer) Option`; doc: mounts the host's own server for a cluster without a generated package, a manufacturer-specific one; refuses a cluster with a generated package with `ErrConfiguration` ... `declare it, <cluster>def.Declare{HostServer: …}`, options.go:158-163; `WithServer(nil)` refused :158). Uses outside the definition, non-test code: none (cluster/spec/declaration.go:24, device/doc.go:20, device/options.go:69,86,88 mention it in comments). Uses in test files: device/device_test.go (2: lines 127-128), device/host_options_test.go (3), device/lifecycle_internal_test.go:165, device/media_test.go (1), device/subscriber_test.go:122, device/typed_coverage_test.go:41-43, cluster/servicearea/serviceareaserver_parity_matterjs_test.go (1), node/declaration_consumer_test.go (1). Docs naming it: docs/getting-started/device.md:154, docs/guides/migrating-to-the-facade.md:54.

## 11 The reference bridge and examples/host-view

Method: module imports of every non-test `.go` file of examples/reference-bridge (29 files) and examples/host-view (1 file), by regex over the import blocks (python script in the scratchpad); identifier use by go/ast (edge tool of section 1) and grep for the `cluster/core` alias (`mattercore`). The import rule BU-b states (notes/briefs/reports/BU-b.md §7): `node`, `device`, `contract`, `schema`, `cluster/spec` and the `cluster/spec/<cluster>` packages; everything else is an exception. "def N" = N `cluster/spec/<cluster>` packages in that file. Routes are checked against `go doc -all ./node` (1289 lines) and `./device` (3032 lines) of this tree (grep of the identifier; "alias" = a `type X = lower.T` in node).

### 11a examples/host-view
- main.go | facade: contract, device, node; def 3 | non-facade: none. Package imports (go list): `contract device node cluster/spec/<3 def packages>`.

### 11b examples/reference-bridge, per non-test file (facade imports | non-facade imports)
- attestation.go | node | -
- control.go | def 6 | -
- control_actions.go | device | -
- control_energy_market.go | - | -
- dcl.go | def 1 | cluster/core (mattercore), controller/dcl
- fifo_other.go, fifo_unix.go, persist.go | - | -
- fleet.go | contract, device, node; def 5 | cluster/core
- fleet_appliances.go | cluster/spec, device; def 9 | -
- fleet_climate.go | cluster/spec, contract, device; def 7 | -
- fleet_closure.go | cluster/spec, device; def 2 | -
- fleet_energy.go | device; def 4 | -
- fleet_energy_market.go | cluster/spec, device; def 7 | -
- fleet_esa.go | device; def 1 | -
- fleet_evse.go | cluster/spec, device; def 5 | clock, cluster/energy
- fleet_kitchen.go | device; def 8 | -
- fleet_lighting.go | device; def 2 | -
- fleet_node.go | cluster/spec, contract, device, node; def 2 | cluster/core
- fleet_safety.go | contract, device; def 3 | -
- fleet_sensors.go | device; def 21 | cluster/core
- fleet_servicearea.go | cluster/spec, contract, device; def 2 | -
- main.go | contract, node, schema; def 1 | cluster/core, controller/dcl
- media.go | cluster/spec, device, node; def 12 | -
- ota.go | def 1 | bdx/otaimage, cluster/core
- ota_provider.go | def 1 | bdx, bdx/otaimage, cluster/core
- tcp.go | node | -
- wifi.go | node; def 1 | cluster/core
- wifisim.go | contract, node | -
Non-facade packages imported by non-test files: `cluster/core` (8 files: dcl.go, fleet.go, fleet_node.go, fleet_sensors.go, main.go, ota.go, ota_provider.go, wifi.go), `controller/dcl` (dcl.go, main.go), `bdx/otaimage` (ota.go, ota_provider.go), `bdx` (ota_provider.go), `clock` (fleet_evse.go), `cluster/energy` (fleet_evse.go). `bridge`, `endpoint`, `im`, `store`, `tlv`, `parity` are imported by no non-test file (BU-b: tests only). Third-party imports: none in the non-test files.

### 11c identifiers used from each non-facade import, and the node route that exists
- cluster/core (20 identifiers; per file: identifier@line):
  - dcl.go: OTAImage@62, OTAImageSource@52, OTAPeer@62; ota_provider.go: OTAConsent@74, OTAConsentDenied@107, OTAConsentGranted@109, OTAConsentState@103, OTAImage@79, OTAImageSource@73, OTAPeer@79; ota.go: BasicInformation@42, OtaApplier@49, OTASoftwareUpdateRequestor@45; main.go: ActionsConfig@189, NewActions@189, OTASoftwareUpdateRequestor@299; wifi.go: WiFiNetworkDiagnostics@66, WiFiReporter@69; fleet_node.go: ActionRequest@145, ActionsHost@99, EcosystemDirectory@165, PowerSourceConfiguration@59; fleet_sensors.go: NewEcosystemInformation@464; fleet.go: NewFixedLabel@108.
  - Routes in node (aliases `type X = mattercore.Y`): `node.OTAImageSource` (node doc:1014), `node.OTAConsent` (:1007), `node.OTAConsentState` (:1011), `node.OTAPeer` (:1018), `node.OTAApplier` = mattercore.OtaApplier (:999), `node.WiFiReporter` (:1238). Those six are owned names in node, yet the example spells them through `mattercore.` (dcl.go:52,62, ota_provider.go:73-79,103, ota.go:49, wifi.go:69).
  - No node name: `OTAImage` (the type; node doc 0 hits), `OTAConsentGranted`/`OTAConsentDenied` (constants of mattercore.OTAConsentState, cluster/core/ota_provider.go:111-112), `ActionsConfig`/`NewActions`/`ActionsHost`/`ActionRequest` (0 hits in node and device docs), `EcosystemDirectory`/`NewEcosystemInformation` (0 hits; the declared route is `ecodef.Declare{HostServer: ...}`, fleet_sensors.go:464), `NewFixedLabel` (0 hits; the declared route is `fixedlabeldef.Declare{HostServer: fixedLabel}`, fleet.go:114; a generated `fixedlabeldef.Declare` exists in the 119 Declare types).
  - Reached through the root: `mattercore.BasicInformation`, `mattercore.OTASoftwareUpdateRequestor`, `mattercore.PowerSourceConfiguration`, `mattercore.WiFiNetworkDiagnostics` are field types of `node.RootClusters` (node/root.go:59; `Node.Root()` node doc:928), so the example names the types to type-assert or hold what `n.Root()` returns.
- controller/dcl (6 identifiers): NewClient@dcl.go:42, Client@:37, ProductionURL@:42, TestNetURL@:44, OTAVersions@:53, VerifyImage@:77 -> no node route (node doc has no DCL name).
- bdx/otaimage (4): Header (ota.go:123, 4 uses), ExtractPayload (ota.go:134), Create and CreateOptions (ota_provider.go:61) -> no node route. bdx (2): Blob (ota_provider.go:95), FileDesignator (ota_provider.go:87) -> no node route.
- clock (1): `clock.System` (fleet_evse.go:178); node route for time: `node.Config.Clock clock.Clock` (node doc:590-598); `device.Options` (device doc:2360) carries no clock field in this tree (gate 1: `Options` held `clock.Clock`, device doc:4654), so a device's clock is the node's once it is added (device/defaults.go:213 comment 'the device's clock, the node's once it is added'); the example's own clock.System use is a charger clock (fleet_evse.go:175-178, 'timers run on the system clock').
- cluster/energy (21): ChargeSchedule, EvseChange, EvseChargeCurrentChanged, EvseChargingPreferencesChanged, EvseConfig, EvseDischargeCurrentChanged, EvseFaultNone, EvseFeatureChargingPreferences, EvseFeaturePlugAndCharge, EvseFeatureRfid, EvseFeatureSoCReporting, EvseFeatureV2X, EvseHost, EvseNotPluggedIn, EvsePluggedInDemand, EvsePluggedInNoDemand, EvseServer, EvseState, EvseStateChanged, NewEnergyEvse, SupplyChargingEnabled (fleet_evse.go:127-417) -> no node or device route; mounted as `evse.Declare{HostServer: e.evse}` (fleet_evse.go:207, comment :132 'the cluster's delegate model is not in the declarations yet'); the kit's own EnergyEvse default has no port (section 10b).
- Wi-Fi and TCP: wifi.go imports `node` plus `cluster/core` (WiFiNetworkDiagnostics, WiFiReporter); tcp.go imports only `node` (`node.TCPConfig`, owned alias node/facade.go:39). Attestation: attestation.go imports only `node` (`node.Config.Attestation`, `node.AttestationFiles`, node/attestation.go:17).
- Stale citation in the node godoc: node doc:306 cites `examples/reference-bridge/wiring.go:105-116, buildRootClusters`; examples/reference-bridge has no wiring.go (only wiring_test.go); CHANGELOG.md:2327 and notes/handover.md:285,311 record the removal.

## 12 The migration guide against `go doc`

Guide: docs/guides/migrating-to-the-facade.md (155 lines). Structure: `Symbol -> route` (heading line 9, 27 rows, loom symbols at the v0.6.0 pin); `Removed in v0.10.0: the declaration` (line 41, 18 rows); `Removed in v0.10.0: the deprecated surface` (line 70) with subsections controller forwarding packages (line 77, 1 row), cluster/wire request types and the switch wrapper (line 83, 7 rows), clock forms (line 95, 6 rows), session parameters (line 106, 4 rows), others (line 115, 6 rows); `What a migrating host meets` (line 126, 3 items); `Verify` (line 137).
Method: exported API of every public package (everything outside internal/, script/, examples/, notes/; 206 packages in both trees) dumped with go/types (golang.org/x/tools/go/packages, scratchpad module) at v0.9.1 (`git archive v0.9.1`, commit 1800fdc3, 2026-10-20, the latest tag) and at c9e7c473: constants, vars, funcs, types, exported struct fields, interface methods and methods, and the members of an alias target listed under the alias name; a symbol is "removed" when its package+name key is absent at HEAD. Counts: 19,811 symbols at v0.9.1, 25,187 at HEAD, 900 removed, 6,276 added, 550 present in both with a different kind or signature string.

### 12a symbols the guide names as the new form that do not exist
Checked: every dotted identifier in the Route column's code spans (150), every capitalised bare identifier there (48; all are an exported name somewhere), plus 84 explicit package-qualified checks (node, contract, device, secure/spake2, bridge.PaseAdapter, transport/btp, cluster/onoff, cluster/core, cluster/closure, cluster/energy, controller/ca, cluster, cluster/spec and the `cluster/spec/<cluster>` names behind the guide's aliases wcdef=windowcovering, ccdef=colorcontrol, tdef=thermostat, closuredef=closurecontrol, msdef=modeselect, bsdef=booleanstate, tmdef=temperaturemeasurement, plus fancontroldef, levelcontroldef, valvedef, mediaplaybackdef, smokecoalarmdef, onoffdef, groupsdef): all exist at HEAD. Not resolved by the first pass and explained: `switchserver.New`/`switchserver.Config` (guide-local alias of cluster/switch; both exist), `wire.Switch...Event` (SwitchInitialPressEvent, SwitchLongPressEvent, SwitchLongReleaseEvent, SwitchShortReleaseEvent and GenericSwitchSource exist in cluster/wire), `c.Attributes`/`c.Origin` (fields of contract.Change; exist), `d.TemperatureMeasurement` (device.TemperatureSensor.TemperatureMeasurement; exists), `req.TransitionTime.Null` (spec.Nullable.Null; exists), `Node.Fabrics` (named as removed), `time.Time`, `c.now`.
Aliases the guide asserts, confirmed at HEAD: cluster/levelcontrol.LevelSource, cluster/fan.StateSource and Stepper, cluster/closure.Mover, cluster/valve.OpenRequest and StateSource, cluster/media.PlaybackState (to mediaplayback.State), cluster/alarm.StateSource and SelfTester, cluster/thermo.PresetStore, SettingStore, TemperatureSetter, SetpointChange, cluster/energy.Booster and DemManager, cluster/servicearea.Selector and Skipper, cluster/lock.AuthStore, cluster/boolcfg.Delegate, cluster/appliance.Oven, cluster/modeselect.ModeSource, cluster/light.Color, ColorMode and both writers, cluster/pump (5 types), cluster/media (30 types) are `alias` entries to cluster/spec/<cluster> types at HEAD (they were `type` entries at v0.9.1).
Mismatches between the guide's text and the tree (facts, not judgement):
- guide line 135 says the typed accessor return types land in `v0.9.2`; CHANGELOG.md:11 heads the open section `[Unreleased]`, CHANGELOG.md:107 is `[0.9.1] - 2026-10-20`, and the guide's own sections say 'Removed in v0.10.0'.
- the guide's Source line (line 155) cites `fabrictest/site.go:84` and `fabrictest/controller.go:450`; at c9e7c473 `NewSite` is at site.go:94 and `CommissionedPair` at controller.go:496 (controller.go:450 is inside the `Pair` struct comment).
- the guide's 'Removed' row for `device.WithServer(srv)` and `WithOptional` is consistent with device/options.go:261-273 (WithServer kept for a cluster without a generated package; WithOptional absent).

### 12b exported symbols removed since v0.9.1 and their line in the guide
900 symbols removed:
- 667 in five removed packages: controller/discovery (145), controller/exchange (177), controller/handshake (63), controller/interaction (168), controller/peer (114); guide line 81 names all five in one row ('the same identifiers in `client/<name>`').
- 67 top-level symbols in surviving packages; every one has a line: 33 are named package-qualified in a code span (`wire.GenericSwitch`, `device.WithFeatures`, `btp.Clock`, `spake2.MRPParameters`, `store.DiagnosticsRecord`, `mattercert.SystemTime`, ...), 34 appear as a bare name in a code span of a row that names their package (the `wire.Decode...` family, `wire.*Request`, `levelcontrol.MoveRequest/StepRequest/StopRequest`, the `modeselect.*Field*` constants, `fan.RockSetter/WindSetter/AirflowDirectionSetter/ErrAirflowDirection`, `closure.StopHandler`, `mrp.NewRetransmitter/SendFunc/TickResult`).
- 166 members in surviving packages: 122 are members of the removed types above (fields and methods of State, Settings, MoveRequest, GenericSwitch, ChangeToModeRequest, Clock, ...), 44 are members of types that stay. Of the 44: 24 are named in the guide (`Config.Move`, `Config.Stop`, `DimensionConfig.Now`, `CommissionerControlConfig.Now`, `OTAProviderConfig.Now`, `OTARequestorConfig.Clock`, `onoff.Config.Clock`, `btp.Options.Clock`, `EvseConfig.Now`, `CommodityTariffConfig.Now`, `DeviceEnergyConfig.Now`, `controller/ca.Config.Now`, `Quieter.Now`, `Quieter.AfterFunc`, `device.Options.Clock`, `PaseAdapter.PeerMRPParams`, `SetResponderMRPParams`, `InitiatorMRPParams`, `ResponderMRPParams`, `OpenRequest.HasOpenDuration`, `Node.Fabrics`, `Store.LoadDiagnostics`, `Store.SaveDiagnostics`, `AttributeChangeNotifier.OnMatterAttributesChanged`); 20 are named only by method name through the contract row (guide line 59 names `contract.AttributeChangeNotifier.OnMatterAttributesChanged`) and not per type: `OnMatterAttributesChanged` on cluster.AttributeChanges, cluster/alarmbase.Server, cluster/appliance.{DryerServer,MicrowaveServer,WasherServer}, cluster/boolcfg.Server, cluster/core.ScenesManagement, cluster/cover.WindowCoveringServer, cluster/energy.{CommodityPriceServer,CommodityTariffServer,DeviceEnergyManagementServer,EvseServer,GridConditionsServer,PreferenceServer,WaterHeaterManagementServer}, cluster/filter.Server, cluster/levelcontrol.Server, cluster/light.ColorControlServer, cluster/onoff.Server, cluster/thermo.ThermostatServer; all 21 types that lost it (these 20 plus the contract interface) have `OnAttributeChange` at HEAD.
- Removed symbols with no line in the guide: none by name; the 20 per-type `OnMatterAttributesChanged` methods above are the only ones without a line of their own.
Present in both trees with a different kind or signature string (550): device 301 (typed accessors, `AirPurifier.FanControl` etc.), cluster/media 64, cluster/levelcontrol 22, controller/client 19, fabrictest 19, node 20, controller/commissioner 16, cluster/pump 14, cluster/energy 10, controller/ble 9, cluster/alarm 7, cluster/light 7, cluster/valve 6, and 1-5 each in binding, cluster/appliance, boolcfg, closure, cover, fan, lock, modeselect, opstate, servicearea, spec, thermo, controller/dcl. These include the types that changed to aliases (the signature string now names the cluster/spec type) and the packages that moved from controller/* to client/*; the guide's rows for the aliases (guide line 68) and for 'accessor return types' (lines 134-135) cover them in prose, per-symbol lines do not exist for them.

## Item counts per section
- 1: 231 packages (121 under cluster/spec, 5 client/*, 11 controller/*); 80 files / 26 packages import im under cluster/ (33 identifiers); 45 named-edge blocks in 1d; archcheck exceptions 0
- 2: 73 `go` statements + 7 `.Go(func` sites in library code + 1 `m.work.Go(fn)`; 6 `go` in examples/; 2 raw time.Now sites (the one guarded exception); 38 real-time context timeouts in 24 files
- 3: 214 mutex declarations (175 with a comment, 39 without; 208 fields, 6 vars); 122 flagged calls under a held lock in 51 files
- 4: 2 raw time sites; 10 `clock.System{}` sites; 48 files with randomness (10 of them math/rand/v2)
- 5: 587 errors.New literals in 70 dirs (2 dirs differ in prefix); 86 Error() types (55 MatterStatusCode implementers); the dispatcher's string fallback is gone
- 6: 571 literal event names, 0 not dotted, 5 non-literal; 87 logger-field structs in 25 packages; 37 slog.Default() sites, 0 inline
- 7: 8 packages with file-system or sql.Open access outside the seam, gate-1's os.WriteFile dump gone
- 8: 71 declarations with a foreign (non-aliased) type in the nine facade packages (fabrictest 27, controller/client 15, node 14, controller/commissioner 10, controller/ca 2, controller/dcl 2, device 1, contract 0, controller/attestation 0); 0 identifiers named Behavior/Agent/Transaction/Backing/Hull
- 9: 7 packages without any test file; 15 slowest listed; 1 failing test in a full run (client/interaction); 9 flaky-test entries
- 10: 119 Declare types; 48 with a port field; 62 self-held default servers (34 port required, 14 port optional, 14 no port); WithServer: 0 uses in non-test code outside its definition
- 11: 29 + 1 non-test files; 6 non-facade packages imported by the reference bridge (cluster/core 8 files, controller/dcl 2, bdx/otaimage 2, bdx 1, clock 1, cluster/energy 1); host-view 0
- 12: 0 symbols named as the new form are missing; 900 symbols removed since v0.9.1 (667 in 5 removed packages, 67 top-level, 166 members); every removed top-level symbol has a line in the guide; 20 member methods (OnMatterAttributesChanged per server type) have no line of their own; 2 factual mismatches (v0.9.2, two stale source-line citations)
