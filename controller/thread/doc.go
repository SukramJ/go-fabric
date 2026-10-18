// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package thread is the commissioner's codec for the Thread operational
// dataset and the MeshCoP TLV stream it is written in (Thread spec
// §8.10): the dataset a controller hands a Thread device in
// AddOrUpdateThreadNetwork. It is a semantic port of matter.js
// packages/protocol/src/thread/ at 862ad75f — [WalkTLVs] and [EncodeTLVs]
// are BasicTlvCodec.ts, [TLVType] is meshcopTlvTypes.ts, [SecurityPolicy]
// is SecurityPolicy.ts, [OperationalDataset] is OperationalDataset.ts, and
// the sentinel errors are errors.ts and the ThreadTlvError of
// BasicTlvCodec.ts.
//
// A dataset from [DecodeDataset] re-encodes byte-identically while no
// field moved, including the non-canonical forms real Border Routers emit
// (the one-byte CHANNEL, vendor orderings); a changed field is written in
// its canonical form, a dataset built in memory canonically throughout.
//
// The package is standard library only and holds no state.
package thread
