// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bdx_test

import (
	"math"
	"testing"

	"thingfabric.dev/go-fabric/bdx"
)

// TestRangeFieldsNeverTruncate (review II, R2-P6-wire-1909) is the
// standing guard of every message with a range field — the Init (start
// offset, length) and the ReceiveAccept (length): a value above 2³²−1
// takes the 64-bit form whatever WideRange says, and decodes to itself; a
// value that fits keeps the 32-bit form unless WideRange asks otherwise. A
// new message with a range field joins the table.
func TestRangeFieldsNeverTruncate(t *testing.T) {
	t.Parallel()
	big, edge := uint64(math.MaxUint32)+1, uint64(math.MaxUint32)
	type rt func(start, length *uint64) (gotStart, gotLength *uint64, wide bool, err error)
	messages := map[string]rt{
		"Init": func(start, length *uint64) (*uint64, *uint64, bool, error) {
			payload, err := bdx.Init{
				TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 1024,
				StartOffset: start, MaxLength: length, FileDesignator: []byte("ota/img"),
			}.Encode()
			if err != nil {
				return nil, nil, false, err
			}
			m, err := bdx.DecodeInit(payload)
			return m.StartOffset, m.MaxLength, m.WideRange, err
		},
		"ReceiveAccept": func(_, length *uint64) (*uint64, *uint64, bool, error) {
			payload, err := bdx.ReceiveAccept{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 1024, Length: length}.Encode()
			if err != nil {
				return nil, nil, false, err
			}
			m, err := bdx.DecodeReceiveAccept(payload)
			return nil, m.Length, m.WideRange, err
		},
	}
	for name, roundTrip := range messages {
		for _, tc := range []struct {
			start, length *uint64
			wide          bool
		}{
			{nil, &big, true},
			{nil, u64(big + 5), true},
			{nil, u64(math.MaxUint64), true},
			{&big, u64(10), name == "Init"},
			{nil, &edge, false},
			{u64(7), u64(10), false},
		} {
			start, length, wide, err := roundTrip(tc.start, tc.length)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if length == nil || *length != *tc.length {
				t.Errorf("%s: length %d decodes as %v", name, *tc.length, length)
			}
			if name == "Init" && tc.start != nil && (start == nil || *start != *tc.start) {
				t.Errorf("%s: start %d decodes as %v", name, *tc.start, start)
			}
			if wide != tc.wide {
				t.Errorf("%s: start %v length %d: wide %v, want %v", name, tc.start, *tc.length, wide, tc.wide)
			}
		}
	}
}
