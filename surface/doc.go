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
// part of a device's effective surface. [OriginKitDefault] is defined for
// the phases to come; no candidate carries it yet. The node adds one
// candidate per root feature its configuration selects that a root
// server declares as ahead of matter.js (concept v11 S2).
//
// One policy is built in and judges first: the reference policy. A server
// declares which of its elements implement which row of the "Ahead of
// matter.js" register ([AheadSet]); the device layer and the node tag each
// such candidate ([Reference].Ahead), and the reference policy refuses a
// tagged candidate whose row [Policies].Extensions does not enable
// ([ErrExtensionNotEnabled], the error names the register ID): code that
// selects an ahead element the same code did not enable is a bug (ADR 0030
// decision 3). [Policies.Custom] takes the host's own policies, and
// package surfacetest has the policies the tests use.
//
// The built-in policies never gate a whole cluster: which clusters reach
// an endpoint is the host's decision through the device types it declares
// (ADR 0030 decision 7). A Custom policy may still remove an opted-in
// optional cluster.
package surface
