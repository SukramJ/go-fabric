// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package thread is the module's codec for the Thread operational dataset
// and the MeshCoP TLV stream it is written in (Thread spec §8.10), and the
// data model of a Thread node's diagnostics: the dataset a controller
// hands a Thread device in AddOrUpdateThreadNetwork, and the dataset a
// Thread Border Router serves and is given (ADR 0027).
//
// Two readings of a dataset live here, each after its reference:
//
//   - [DecodeDataset] and [OperationalDataset] are a semantic port of
//     matter.js packages/protocol/src/thread/ at 862ad75f — [WalkTLVs] and
//     [EncodeTLVs] are BasicTlvCodec.ts, [TLVType] is meshcopTlvTypes.ts,
//     [SecurityPolicy] is SecurityPolicy.ts, [OperationalDataset] is
//     OperationalDataset.ts, and the sentinel errors are errors.ts and the
//     ThreadTlvError of BasicTlvCodec.ts. A dataset from [DecodeDataset]
//     re-encodes byte-identically while no field moved, including the
//     non-canonical forms real Border Routers emit (the one-byte CHANNEL,
//     vendor orderings); a changed field is written in its canonical form,
//     a dataset built in memory canonically throughout. The commissioner
//     reads datasets this way.
//   - [DatasetView] is connectedhomeip's OperationalDatasetView
//     (src/lib/support/ThreadOperationalDataset.cpp at 6170af84): the first
//     sub-TLV of each type, held to chip's sizes. The cluster servers that
//     judge a dataset by chip's rules read it this way (cluster/netinfra,
//     cluster/threadbr).
//
// [NodeDiagnostics] and the types it carries are the diagnostics of a
// Thread node after matter.js packages/thread-br-client/src/diagnostic/
// DiagnosticResponse.ts and tlv/diag/ at 862ad75f: what a border-router
// adapter (thread/otbr) reads and the ThreadNetworkDiagnostics server
// serves.
//
// The package sits in the foundation layer (script/archcheck): standard
// library only, no state.
package thread
