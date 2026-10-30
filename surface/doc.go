// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package surface holds the served surface of a node as a value, and the
// policies that decide it (concept v11, ADR 0030 "The served surface is a
// value under policies").
//
// A [Surface] is what a controller's wildcard read of every endpoint
// returns: per endpoint the device types the Descriptor lists and per
// server cluster its revision, FeatureMap, AttributeList,
// AcceptedCommandList, GeneratedCommandList and the events it emits. It is
// canonically ordered, hashable ([Surface.Digest]) and JSON-encodable, and
// two of them compare element by element ([Diff]).
//
// The surface is decided before any server is built. The device layer
// states what it would compose as candidate [Element]s, each with its
// [Origin] — why it exists — and whether it is mandatory at the pin. The
// [Policies] judge every candidate ([Evaluate]): every policy judges every
// element, the verdicts merge with Refuse over Remove over Keep, a Remove
// of a mandatory element is kept (an operator-side policy never removes
// what the pin mandates), and a Refuse fails the composition
// ([ErrRefused]). The [Report] states the policies' digest, the effective
// surface read from the built servers, and at least one [Decision] per
// candidate.
//
// What the candidates cover in this first phase (concept v11 S1): the
// device type, each server cluster the device layer composes, each
// FeatureMap bit of a cluster whose default server it builds, and each
// attribute, command and event the declarations or the device type add to
// such a server. An element a hand-written server decides internally —
// thermo's optional attributes for its feature selection, say — is no
// candidate: it appears in the effective surface only. A host's own server
// (a HostServer) is one cluster candidate; its features and elements are
// its own. The clusters the endpoint assembler mounts (Descriptor,
// BridgedDeviceBasicInformation, Identify, Groups, ScenesManagement) and
// the clusters a measurement source projects are neither candidates nor
// part of a device's effective surface; a node's report reads them from
// its assembled topology once it has one ([Report].Assembled). The node
// adds one candidate per
// root feature its configuration selects that a root server declares as
// ahead of matter.js (concept v11 S2), and one per root cluster it mounts
// (concept v11 S3): mandatory for the RootNode device type's mandatory
// set, [OriginHostDeclared] for a server its configuration adds,
// [OriginKitDefault] for one it mounts by default.
//
// One policy is built in and judges first: the reference policy. A server
// declares which of its elements implement which row of the "Ahead of
// matter.js" register ([AheadSet]); the device layer and the node tag each
// such candidate ([Reference].Ahead), and the reference policy refuses a
// tagged candidate whose row [Policies].Extensions does not enable
// ([ErrExtensionNotEnabled], the error names the register ID): code that
// selects an ahead element the same code did not enable is a bug (ADR 0030
// decision 3). A host-supplied server (device.HostServer) is neither
// tagged nor refused by the reference policy: its features are the host's
// own behaviour, and the *Ahead of matter.js* register describes this
// module's code.
//
// The second built-in policy, judged next, is the level policy
// ([Policies].Level, concept v11 §C.5): the specification level is a
// Matter revision matter.js has shipped, and what exists at each is the
// generated table of package schema ([schema.Levels], from matter.js's
// release tags), which the device layer and the node copy into each
// candidate's [Element].Levels. At a level below the pin it removes an
// optional element the level does not know, keeps one the pin mandates
// ("kept: mandatory at pin (absent at level <L>)"), re-adds features
// whose removal would leave a selection the pin's conformance rejects,
// never removes BridgedDeviceBasicInformation's ConfigurationVersion, and
// withholds an endpoint whose device type or mandated cluster the level
// does not know ([ErrWithheld]). An element no level knows — a vendor's —
// is not level-controlled, and [Policies.ServerInternal] records what a
// hand-written server or the endpoint assembler chose itself. The served
// ClusterRevision stays the pin's.
//
// The third axis is the controller profile ([Policies].Profiles, concept
// v11 §C.6, ADR 0030 decision 8): what a named controller ecosystem
// tolerates. The [Profiles] are generated from the evidence register
// notes/parity/controller_profiles.md (script/profilegen); each selected
// profile is a built-in policy "profile:<name>", judged after the level
// policy. Only a row of the confidence [Reproduced] removes an element
// (the chain keeps a mandatory one); a Reported or Inherited row keeps it
// with a warning decision. A profile may imply a level, which applies when
// [Policies].Level is "" ([Policies.LevelOrigin]). A profile without a
// Reproduced row removes nothing.
//
// A surface that changes under a paired controller is announced (ADR 0030
// decision 6, concept v11 §C.9): a node persists every endpoint's
// effective surface, raises the ConfigurationVersion when an endpoint
// comes back with another one, and reports each change as a
// [SurfaceChange] in [Report].Changed. [Policies.Custom] takes the host's own policies, and package
// surfacetest has the policies the tests use.
//
// The built-in policies never gate a whole cluster: which clusters reach
// an endpoint is the host's decision through the device types it declares
// (ADR 0030 decision 7). A Custom policy may still remove an opted-in
// optional cluster. The level policy removes an optional cluster only
// because the operator's level does not know it.
package surface
