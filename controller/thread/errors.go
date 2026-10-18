// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thread

import "errors"

var (
	// ErrThreadTLV is a MeshCoP TLV stream that cannot be walked: a
	// truncated header, extended-length header or value.
	//
	// Mirrors matter.js packages/protocol/src/thread/BasicTlvCodec.ts:9-10
	// (ThreadTlvError).
	ErrThreadTLV = errors.New("thread: malformed MeshCoP TLV stream")

	// ErrThreadDataset is an operational dataset whose TLV stream walks but
	// whose content does not decode: a fixed-width TLV of the wrong length,
	// a security policy that is not four bytes, or hex that does not parse.
	//
	// Mirrors matter.js packages/protocol/src/thread/errors.ts:9-10
	// (ThreadDatasetError).
	ErrThreadDataset = errors.New("thread: malformed operational dataset")

	// ErrInvalidTLV is an entry [EncodeTLVs] cannot write: a value longer
	// than 0xFFFF bytes.
	//
	// Mirrors the InternalError of matter.js
	// packages/protocol/src/thread/BasicTlvCodec.ts:54-64 (BasicTlv.encode).
	ErrInvalidTLV = errors.New("thread: TLV cannot be encoded")
)
