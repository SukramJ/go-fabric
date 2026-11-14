// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"testing"
)

// versionedRecorder answers every read with Success, records every write
// and reports version as the cluster's current DataVersion.
type versionedRecorder struct {
	version uint32
	written int
}

func (d *versionedRecorder) Read(_ context.Context, p ConcreteAttributePath) []ReadResult {
	return []ReadResult{{Path: p, Status: StatusSuccess, DataVersion: d.version, HasDataVersion: true}}
}

func (d *versionedRecorder) Write(_ context.Context, p ConcreteAttributePath, _ AttributeValue) []WriteResult {
	d.written++
	return []WriteResult{{Path: p, Status: StatusSuccess}}
}

func (d *versionedRecorder) Invoke(context.Context, ConcreteCommandPath, any) InvokeResult {
	return InvokeResult{}
}

func (d *versionedRecorder) CurrentDataVersion(context.Context, uint16, uint32) (uint32, bool) {
	return d.version, true
}

// TestWriteParity_DataVersionZeroIsCompared (review II, R2-P2-ctr-06): a
// DataVersion that is present is compared whatever its value, 0 included —
// matter.js packages/protocol/src/action/server/AttributeWriteResponse.ts:254-257
// (`if (version !== undefined && version !== cluster.version) … DataVersionMismatch`),
// connectedhomeip src/app/WriteHandler.cpp:818-825
// (`if (aPath.mDataVersion.HasValue())`). A cluster whose version is 0
// accepts a write that names 0.
func TestWriteParity_DataVersionZeroIsCompared(t *testing.T) {
	t.Parallel()
	// BasicInformation NodeLabel: writable, not timed.
	path := ConcreteAttributePath{Endpoint: 0, HasEndpoint: true, Cluster: 0x0028, HasCluster: true, Attribute: 0x0005, HasAttribute: true}
	for _, c := range []struct {
		name    string
		current uint32
		named   uint32
		want    StatusCode
	}{
		{name: "0 against a non-zero version", current: 0x1234, named: 0, want: StatusDataVersionMismatch},
		{name: "1 against version 0", current: 0, named: 1, want: StatusDataVersionMismatch},
		{name: "0 against version 0", current: 0, named: 0, want: StatusSuccess},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			d := &versionedRecorder{version: c.current}
			wr := HandleWriteRequest(context.Background(), d, WriteRequest{Writes: []AttributeWrite{{
				Path: path, Value: AttributeValue{Value: "x"}, DataVersion: c.named, HasDataVersion: true,
			}}})
			if len(wr.Responses) != 1 || wr.Responses[0].Status.Status != c.want {
				t.Fatalf("responses %+v, want one with %v", wr.Responses, c.want)
			}
			if wantWritten := c.want == StatusSuccess; (d.written == 1) != wantWritten {
				t.Fatalf("written %d times, want written %v", d.written, wantWritten)
			}
		})
	}
}
