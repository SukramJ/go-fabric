// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thread_test

import (
	"bytes"
	"errors"
	"testing"

	forwarded "github.com/SukramJ/go-fabric/controller/thread"
	"github.com/SukramJ/go-fabric/thread"
)

// TestForwardsToThread pins that every forwarder reaches package thread:
// the codec moved there (ADR 0027 decision 2) and this package forwards
// until it is removed.
func TestForwardsToThread(t *testing.T) {
	t.Parallel()
	blob := []byte{0x00, 0x03, 0x00, 0x00, 0x0f, 0x03, 0x02, 'g', 'f'}
	entries, err := forwarded.WalkTLVs(blob)
	if err != nil || len(entries) != 2 || entries[0].Type != forwarded.TLVChannel || entries[1].Type != thread.TLVNetworkName {
		t.Fatalf("WalkTLVs = %v, %v", entries, err)
	}
	out, err := forwarded.EncodeTLVs(entries)
	if err != nil || !bytes.Equal(out, blob) {
		t.Fatalf("EncodeTLVs = %x, %v", out, err)
	}
	ds, err := forwarded.DecodeDataset(blob)
	if err != nil || ds.Channel == nil || *ds.Channel != 15 {
		t.Fatalf("DecodeDataset = %+v, %v", ds, err)
	}
	if _, err := forwarded.DecodeDatasetHex("zz"); !errors.Is(err, forwarded.ErrThreadDataset) || !errors.Is(err, thread.ErrThreadDataset) {
		t.Errorf("DecodeDatasetHex(zz) = %v, want ErrThreadDataset", err)
	}
	if _, err := forwarded.DecodeSecurityPolicy([]byte{1}); !errors.Is(err, thread.ErrThreadDataset) {
		t.Errorf("DecodeSecurityPolicy = %v", err)
	}
	if _, err := forwarded.WalkTLVs([]byte{1}); !errors.Is(err, forwarded.ErrThreadTLV) {
		t.Errorf("WalkTLVs(truncated) = %v", err)
	}
	if _, err := forwarded.EncodeTLVs([]forwarded.TLV{{Value: make([]byte, 0x10000)}}); !errors.Is(err, forwarded.ErrInvalidTLV) {
		t.Errorf("EncodeTLVs(oversized) = %v", err)
	}
}
