// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"testing"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
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
	small, err := chunkInvokeResponse(im.InvokeResponse{Responses: []im.InvokeResponseEntry{entry(4, 10), entry(5, 10)}}, reportChunkPayloadBudget, reportChunkHardCap)
	if err != nil || len(small) != 1 {
		t.Fatalf("small response: %d chunks, %v", len(small), err)
	}

	big, err := chunkInvokeResponse(im.InvokeResponse{Responses: []im.InvokeResponseEntry{entry(4, 800), entry(5, 600)}}, reportChunkPayloadBudget, reportChunkHardCap)
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

	huge, err := chunkInvokeResponse(im.InvokeResponse{Responses: []im.InvokeResponseEntry{entry(4, 2000)}}, reportChunkPayloadBudget, reportChunkHardCap)
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

// TestChunkInvokeResponseAtTheBudgetAndHardCap pins the invoke-response
// split at its exact limits (R2-P6-bound-0813): a response exactly the
// budget long is one message; a single response exactly the hard cap long
// goes out as it is; while splitting, a chunk that reaches the budget
// exactly takes the response, a response alone past the budget opens no
// empty chunk, and one exactly the hard cap long is kept.
func TestChunkInvokeResponseAtTheBudgetAndHardCap(t *testing.T) {
	t.Parallel()
	entry := func(cmd uint32, n int) im.InvokeResponseEntry {
		return im.InvokeResponseEntry{
			Path:     im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x0033, Command: cmd, HasEndpoint: true, HasCluster: true, HasCommand: true},
			Response: mattercore.PayloadTestResponse{Payload: bytes.Repeat([]byte{'A'}, n)}, HasResponse: true,
			CommandRef: uint16(cmd), HasCommandRef: true,
		}
	}
	size := func(more bool, ents ...im.InvokeResponseEntry) int {
		b, err := EncodeInvokeResponse(im.InvokeResponse{Responses: ents, MoreChunkedMessages: more})
		if err != nil {
			t.Fatal(err)
		}
		return len(b)
	}
	// sized returns the payload length that makes build(n) encode to
	// exactly want bytes.
	sized := func(want int, build func(n int) int) int {
		t.Helper()
		for n := 300; n < 2*reportChunkHardCap; n++ {
			if build(n) == want {
				return n
			}
		}
		t.Fatalf("no payload encodes to %d bytes", want)
		return 0
	}
	resourceExhausted := []byte{0x24, 0x00, byte(im.StatusResourceExhausted)}
	isExhausted := func(c []byte) bool { return bytes.Contains(c, resourceExhausted) }

	// Two responses exactly the budget long in one message: one chunk.
	n := sized(reportChunkPayloadBudget, func(n int) int { return size(false, entry(4, 10), entry(5, n)) })
	if got, err := chunkInvokeResponse(im.InvokeResponse{Responses: []im.InvokeResponseEntry{entry(4, 10), entry(5, n)}}, reportChunkPayloadBudget, reportChunkHardCap); err != nil || len(got) != 1 {
		t.Errorf("a response exactly the budget long: %d chunks, %v; want 1", len(got), err)
	}

	// One response exactly the hard cap long: sent as it is.
	n = sized(reportChunkHardCap, func(n int) int { return size(false, entry(4, n)) })
	if got, err := chunkInvokeResponse(im.InvokeResponse{Responses: []im.InvokeResponseEntry{entry(4, n)}}, reportChunkPayloadBudget, reportChunkHardCap); err != nil || len(got) != 1 || isExhausted(got[0]) {
		t.Errorf("a single response exactly the hard cap long: %d chunks, %v; want it sent as it is", len(got), err)
	}

	// Splitting: the first chunk reaches the budget exactly with its
	// second response, a large third follows in its own chunk.
	n = sized(reportChunkPayloadBudget, func(n int) int { return size(true, entry(4, 10), entry(5, n)) })
	got, err := chunkInvokeResponse(im.InvokeResponse{Responses: []im.InvokeResponseEntry{entry(4, 10), entry(5, n), entry(6, 1000)}}, reportChunkPayloadBudget, reportChunkHardCap)
	if err != nil || len(got) != 2 {
		t.Fatalf("a chunk exactly at the budget: %d chunks, %v; want 2", len(got), err)
	}
	if size(true, entry(4, 10), entry(5, n)) != len(got[0]) {
		t.Errorf("the first chunk is %d bytes, want the two responses that reach the budget exactly", len(got[0]))
	}

	// Splitting: a first response alone past the budget opens no empty
	// chunk; one exactly the hard cap long (with MoreChunkedMessages) is
	// kept.
	n = sized(reportChunkHardCap, func(n int) int { return size(true, entry(4, n)) })
	got, err = chunkInvokeResponse(im.InvokeResponse{Responses: []im.InvokeResponseEntry{entry(4, n), entry(5, 10)}}, reportChunkPayloadBudget, reportChunkHardCap)
	if err != nil || len(got) != 2 {
		t.Fatalf("a first response at the hard cap: %d chunks, %v; want 2", len(got), err)
	}
	if isExhausted(got[0]) {
		t.Error("a response exactly the hard cap long was replaced by ResourceExhausted")
	}
}
