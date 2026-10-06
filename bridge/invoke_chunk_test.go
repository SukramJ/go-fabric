// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestChunkInvokeResponseSplitsWhatDoesNotFit pins invoke-response
// chunking: responses that fit one message go out as one; responses that
// do not are split between whole responses, every chunk but the last
// carrying MoreChunkedMessages (tag 2), each within the budget; a single
// response no message can carry becomes ResourceExhausted for its path.
// Found by the CHIP Python harness (TC-IDM-1.4 step 11).
func TestChunkInvokeResponseSplitsWhatDoesNotFit(t *testing.T) {
	t.Parallel()
	entry := func(cmd uint32, n int) im.InvokeResponseEntry {
		return im.InvokeResponseEntry{
			Path:     im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x0033, Command: cmd, HasEndpoint: true, HasCluster: true, HasCommand: true},
			Response: mattercore.PayloadTestResponse{Payload: bytes.Repeat([]byte{'A'}, n)}, HasResponse: true,
			CommandRef: uint16(cmd), HasCommandRef: true,
		}
	}
	small, err := chunkInvokeResponse(im.InvokeResponse{Responses: []im.InvokeResponseEntry{entry(4, 10), entry(5, 10)}}, reportChunkPayloadBudget)
	if err != nil || len(small) != 1 {
		t.Fatalf("small response: %d chunks, %v", len(small), err)
	}

	big, err := chunkInvokeResponse(im.InvokeResponse{Responses: []im.InvokeResponseEntry{entry(4, 800), entry(5, 600)}}, reportChunkPayloadBudget)
	if err != nil {
		t.Fatal(err)
	}
	if len(big) != 2 {
		t.Fatalf("two large responses: %d chunks, want 2", len(big))
	}
	for i, c := range big {
		if len(c) > reportChunkPayloadBudget {
			t.Errorf("chunk %d is %d bytes, budget %d", i, len(c), reportChunkPayloadBudget)
		}
		if got, want := moreChunked(t, c), i == 0; got != want {
			t.Errorf("chunk %d MoreChunkedMessages = %v, want %v", i, got, want)
		}
	}

	huge, err := chunkInvokeResponse(im.InvokeResponse{Responses: []im.InvokeResponseEntry{entry(4, 2000)}}, reportChunkPayloadBudget)
	if err != nil || len(huge) != 1 {
		t.Fatalf("oversized response: %d chunks, %v", len(huge), err)
	}
	if !bytes.Contains(huge[0], []byte{0x24, 0x00, byte(im.StatusResourceExhausted)}) {
		t.Errorf("an oversized response was not replaced by ResourceExhausted: % x", huge[0])
	}
}

func moreChunked(t *testing.T, body []byte) bool {
	t.Helper()
	dec := tlv.NewDecoder(body)
	if _, err := dec.Next(); err != nil {
		t.Fatal(err)
	}
	m, err := decodeGenericTagMap(dec)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := m[2].(bool)
	return v
}
