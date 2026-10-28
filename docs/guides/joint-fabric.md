# Joint Fabric

A Joint Fabric lets the administrators of several ecosystems share one
fabric. One administrator is the **anchor**: its NOC carries the
administrator CAT (`0xFFFF`) and the anchor CAT (`0xFFFE`), and it keeps
the fabric's registry. Another ecosystem's administrator joins through
**Joint Commissioning (JCM)**. The commissioner checks the joining
administrator's own fabric before it issues the NOC, and the host
consents.

matter.js has no Joint Fabric logic at its pin `862ad75f`:
`JointFabricDatastoreServer.ts:14` and `JointFabricAdministratorServer.ts:14`
are empty subclasses, and its commissioning flow has no JCM step. The
module follows connectedhomeip at `6170af84`, ahead of matter.js. The
register rows are `BD-chip-JointFabricDatastore-RulesInServer`,
`BD-chip-JointFabricAdministrator-RulesInServer` and
`BD-chip-JCM-Commissioner`.

| Part | Package | Source of the rules | Host ports |
| --- | --- | --- | --- |
| JointFabricDatastore (`0x0752`) | `jointfabric.Datastore` | `src/app/server/JointFabricDatastore.cpp`, `joint-fabric-datastore-server.cpp` | `DatastoreSync` (required), `DatastoreStore` |
| JointFabricAdministrator (`0x0753`) | `jointfabric.Administrator` | `joint-fabric-administrator-server.cpp`, `JCMCommissionee.cpp` | `AdministratorPort` (required), `CommissionerReach` |
| The trust verification both roles share | `client/jcm` | `src/credentials/jcm/` | — |
| The commissioner's JCM | `controller/commissioner` (`Options.JointFabric`) | `src/controller/jcm/` | `JointFabricOptions.Consent` |

## The administrator device

The JointFabricAdministrator device type (`0x0130`) mandates both
clusters. The kit mounts the module's servers when the declarations carry
their ports. Without the ports, the generated servers stay.

```go
jfa, err := device.NewJointFabricAdministrator(opts,
	dsdef.Declare{Sync: mySync, Store: myNode.Store()},
	jfadef.Declare{AdministratorPort: myKey, CommissionerReach: myReach})
```

The node gives the Administrator its fabric table, its commissioning
window and its fail-safe when the endpoint joins the topology. It tells
both servers about a fabric's removal. When the Joint Fabric is removed,
the datastore is wiped.

## The datastore and its sync

The Datastore holds the fabric's registry: the anchor (root CA, node id,
vendor id), the nodes, the administrators, the groups and group key sets,
the endpoints with their group memberships and bindings, and the nodes'
ACL entries. Each entry carries chip's state: `Pending`, `Committed`,
`DeletePending` or `CommitFailed`. The 20 commands change the registry
with chip's status codes. Examples: a duplicate is `CONSTRAINT_ERROR`, a
group of the administrator or anchor CAT is `CONSTRAINT_ERROR`, and a
missing key set is `NOT_FOUND`. A full table, a missing administrator and
an operation without a `DatastoreSync` answer `FAILURE`, which is chip's
answer for a plain `CHIP_ERROR`.

`DatastoreSync` is chip's delegate. `Sync…` writes an entry, or a node's
whole binding or ACL list, to the member node. `Fetch…` reads a node's
lists. A command answers once its change is in the registry. The push
runs after the answer, and its entry is committed when the push returns,
found again by its key. A push that fails leaves the entry `Pending`, and
the next `RefreshNode` pushes it again. `RefreshNode` reconciles one node
completely: endpoints, groups, bindings, key sets and ACLs, then the node
goes `Committed`. A fetch that fails ends the refresh with the node still
`Pending`. The host methods of the same names, such as
`Datastore.AddACLToNode`, push before they return. `Datastore.Wait` waits
for the pushes that commands started.

The tables persist as one settings record under the device's StableKey
when the declaration carries a `DatastoreStore`. The node's store is one.
A change whose save fails is undone and answers `FAILURE`, as the TLS
servers do. chip keeps the tables in memory only. Every attribute that a change touches is
reported. chip reports only NodeList.

## The administrator

- **AdministratorFabricIndex** is set by the host. It is also set when a
  fabric whose NOC carries the administrator and anchor CATs completes
  commissioning. That fabric then becomes the datastore's anchor, as chip's
  administrator application records it
  (`examples/jf-admin-app/linux/JFAManager.cpp:95-157`). The application's
  other setup stays the host's: the default groups, the key set and the
  admin entry.
- **OpenJointCommissioningWindow** opens the node's enhanced window as a
  joint commissioning window, advertised with `CM=3`. It requires the
  accessing fabric, an AdministratorFabricIndex, a disarmed fail-safe and
  no open window. These failures answer the cluster statuses.
- **AnnounceJointFabricAdministrator** runs the JCMCommissionee. It reads
  the announcing commissioner's AdministratorFabricIndex, Fabrics,
  TrustedRootCertificates and NOCs through `CommissionerReach`, verifies
  the vendor id of its administrator fabric, and checks that fabric
  against the accessing one. On success the accessing fabric's VID is
  verified. A fail-safe expiry ends a verification in progress.
- **ICACCSRRequest** returns the `AdministratorPort`'s CSR. It works only
  over CASE, under the fail-safe and after the VID verification.
  **AddICAC** checks an ICAC against the accessing fabric's root and the
  CSR's key, once per fail-safe. As in chip, it keeps nothing; installing
  the ICAC is the host's job.
- **TransferAnchorRequest** and **TransferAnchorComplete** answer
  `SUCCESS` without effect. chip's handlers are empty.

The global trust anchors of a vendor come from the DCL.
`AdministratorConfig.TrustAnchors` and `JointFabricOptions.TrustAnchors`
take that lookup. Without one, both roles use chip's placeholder: the
other side's own RCAC.

## The commissioner side

```go
res, err := com.Commission(ctx, commissioner.Options{
	Fabric: jointFabric, Passcode: code, Addresses: addrs,
	CATs: []uint32{0xFFFF_0001}, // the administrator CAT for the joining administrator
	JointFabric: &commissioner.JointFabricOptions{
		Consent: func(info commissioner.TrustInfo) bool { return askTheUser(info) },
	},
})
```

With `JointFabric` set, the flow runs step 10.2
`JointFabric.TrustVerification` after `OperationalCredentials.DeviceAttestation`
and before the certificates. chip runs the stage at the same place. The
step reads the device's AdministratorFabricIndex and its Fabrics, NOCs
and TrustedRootCertificates over the commissioning session. It then
verifies the administrator's endpoint, fabric and administrator CAT, and
verifies the vendor id: the device signs `SignVIDVerificationRequest`
over the session's attestation challenge. Last, it asks `Consent`. The
step fails with `ErrJointFabric` and a `jcm.Failure` that names the stage
and chip's result, for example `USER_DENIED_CONSENT`. A device whose
administrator fabric has no ICAC, or that carries a VID Verification
Statement, is refused. `JointFabricOptions.Progress` reports each stage.
`Options.CATs` sets the CATs of the issued NOC. Commissioning an anchor
uses the administrator and anchor CATs without `JointFabric`.

## Not covered

- chip's endpoint-scoped `SyncNode(nodeId, endpointId, bindings)` overload
  is not in the port; the datastore never calls it.
- The commissionee's `CommissionerReach` has no node adapter yet. The host
  reads the commissioner itself, for example through the node's client
  stack.
