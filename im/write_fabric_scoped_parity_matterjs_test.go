// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/SukramJ/go-fabric/parity"
)

// fabricScopedRecorder serves every attribute on endpoint 0 — or answers
// every path with missing, when set — and records the writes that reach it.
type fabricScopedRecorder struct {
	missing StatusCode
	version uint32
	written []ConcreteAttributePath
}

func (d *fabricScopedRecorder) Read(_ context.Context, p ConcreteAttributePath) []ReadResult {
	return []ReadResult{{Path: p, Status: d.missing}}
}

func (d *fabricScopedRecorder) Write(_ context.Context, p ConcreteAttributePath, _ AttributeValue) []WriteResult {
	if d.missing != StatusSuccess {
		return []WriteResult{{Path: p, Status: d.missing}}
	}
	d.written = append(d.written, p)
	return []WriteResult{{Path: p, Status: StatusSuccess}}
}

// WriteAuthorized expands a wildcard-endpoint path to endpoint 0 and skips
// a location authorize denies, as endpoint.TopologyDispatcher does.
func (d *fabricScopedRecorder) WriteAuthorized(ctx context.Context, p ConcreteAttributePath, v AttributeValue, authorize WriteAuthorizer) []WriteResult {
	if p.HasEndpoint {
		return d.Write(ctx, p, v)
	}
	if authorize != nil && authorize(0, p.Cluster, p.Attribute) != StatusSuccess {
		return nil
	}
	p.Endpoint, p.HasEndpoint = 0, true
	return d.Write(ctx, p, v)
}

func (d *fabricScopedRecorder) Invoke(context.Context, ConcreteCommandPath, any) InvokeResult {
	return InvokeResult{}
}

func (d *fabricScopedRecorder) CurrentDataVersion(context.Context, uint16, uint32) (uint32, bool) {
	return d.version, d.version != 0
}

var (
	_ Dispatcher        = (*fabricScopedRecorder)(nil)
	_ AuthorizingWriter = (*fabricScopedRecorder)(nil)
	_ DataVersionReader = (*fabricScopedRecorder)(nil)
)

// fabricScopedWritableOfSnapshot is every attribute of parity/schema.json
// whose effective access is writable and fabric-scoped ("F") or
// fabric-sensitive ("S") — matter.js's limits.fabricScoped on a writable
// attribute, read from the snapshot itself rather than from the generated
// table the gate uses.
func fabricScopedWritableOfSnapshot(t *testing.T) []ConcreteAttributePath {
	t.Helper()
	var snap struct {
		Clusters []struct {
			ID         uint32 `json:"id"`
			Attributes []struct {
				ID        uint32 `json:"id"`
				Effective *struct {
					Access struct {
						RW     string `json:"rw"`
						Fabric string `json:"fabric"`
					} `json:"access"`
				} `json:"effective"`
			} `json:"attributes"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(parity.SchemaJSON(), &snap); err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	var out []ConcreteAttributePath
	for _, c := range snap.Clusters {
		for _, a := range c.Attributes {
			if a.Effective == nil {
				t.Fatalf("cluster 0x%04X attribute 0x%04X: the snapshot carries no effective layer", c.ID, a.ID)
			}
			acc := a.Effective.Access
			if acc.RW == "" || acc.RW == "R" || (acc.Fabric != "F" && acc.Fabric != "S") {
				continue
			}
			out = append(out, ConcreteAttributePath{Cluster: c.ID, Attribute: a.ID, HasEndpoint: true, HasCluster: true, HasAttribute: true})
		}
	}
	if len(out) == 0 {
		t.Fatal("the snapshot names no writable fabric-scoped attribute: the test checks nothing")
	}
	return out
}

func fabricScopedCtx(fabricIndex uint8, pase bool) context.Context {
	ctx := WithFabricFilter(context.Background(), false, fabricIndex)
	ctx = WithSubject(ctx, 0, nil)
	if pase {
		ctx = WithAuthModePASE(ctx)
	}
	return WithTimedInteraction(ctx, false)
}

// TestWriteFabricScopedAttributeNeedsAccessingFabric: a write to a
// fabric-scoped attribute on a session with no accessing fabric answers
// UNSUPPORTED_ACCESS before a cluster server sees it, for every writable
// fabric-scoped attribute of the snapshot. Mirrors matter.js
// packages/protocol/src/action/server/AttributeWriteResponse.ts:248-251 at
// db021da0 (`limits.fabricScoped && !this.session.fabric` →
// Status.UnsupportedAccess); chip src/app/WriteHandler.cpp:804-812 at
// 6170af84 answers the same status for a fabric-scoped list. A PASE session
// before AddNOC has no fabric (matter.js RemoteActorContext.ts:211,
// `fabric?.fabricIndex ?? FabricIndex.NO_FABRIC`; chip
// SecureSession.cpp:175-177); after AddNOC it carries the new one
// (OperationalCredentialsServer.ts:280-282 `session.fabric = fabric`; chip
// OperationalCredentialsCluster.cpp:543-546 AdoptFabricIndex) and writes.
func TestWriteFabricScopedAttributeNeedsAccessingFabric(t *testing.T) {
	t.Parallel()
	sessions := []struct {
		name        string
		fabricIndex uint8
		pase        bool
		want        StatusCode
	}{
		{"PASE before AddNOC", 0, true, StatusUnsupportedAccess},
		{"no fabric and not PASE", 0, false, StatusUnsupportedAccess},
		{"a fabric the bridge could not resolve, PASE", 255, true, StatusUnsupportedAccess},
		{"PASE after AddNOC", 1, true, StatusSuccess},
		{"CASE on a fabric", 1, false, StatusSuccess},
		{"CASE on the last fabric index", 254, false, StatusSuccess},
	}
	for _, p := range fabricScopedWritableOfSnapshot(t) {
		for _, s := range sessions {
			t.Run(fmt.Sprintf("0x%04X/0x%04X/%s", p.Cluster, p.Attribute, s.name), func(t *testing.T) {
				t.Parallel()
				d := &fabricScopedRecorder{}
				resp := HandleWriteRequest(fabricScopedCtx(s.fabricIndex, s.pase), d, WriteRequest{Writes: []AttributeWrite{{Path: p}}})
				if len(resp.Responses) != 1 || resp.Responses[0].Status.Status != s.want {
					t.Fatalf("responses = %+v, want one %v", resp.Responses, s.want)
				}
				if wrote := len(d.written) == 1; wrote != (s.want == StatusSuccess) {
					t.Fatalf("the write reached the dispatcher: %v (written %v), want %v", wrote, d.written, s.want == StatusSuccess)
				}
			})
		}
	}
}

// TestWriteFabricScopedGateOrder: the statuses matter.js answers before the
// accessing-fabric check stay the answer (AttributeWriteResponse.ts:221-251
// at db021da0: existence, writable, write privilege, timed, then the
// fabric), and the check comes before the DataVersion (:254-257).
func TestWriteFabricScopedGateOrder(t *testing.T) {
	t.Parallel()
	acl := ConcreteAttributePath{Cluster: 0x001F, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	pase := fabricScopedCtx(0, true)

	t.Run("a path that does not exist keeps its own status", func(t *testing.T) {
		t.Parallel()
		for _, missing := range []StatusCode{StatusUnsupportedEndpoint, StatusUnsupportedCluster, StatusUnsupportedAttribute} {
			d := &fabricScopedRecorder{missing: missing}
			resp := HandleWriteRequest(pase, d, WriteRequest{Writes: []AttributeWrite{{Path: acl}}})
			if len(resp.Responses) != 1 || resp.Responses[0].Status.Status != missing {
				t.Fatalf("responses = %+v, want one %v", resp.Responses, missing)
			}
		}
	})

	t.Run("the accessing fabric is judged before the DataVersion", func(t *testing.T) {
		t.Parallel()
		d := &fabricScopedRecorder{version: 7}
		resp := HandleWriteRequest(pase, d, WriteRequest{Writes: []AttributeWrite{{Path: acl, DataVersion: 3, HasDataVersion: true}}})
		if len(resp.Responses) != 1 || resp.Responses[0].Status.Status != StatusUnsupportedAccess {
			t.Fatalf("responses = %+v, want one UnsupportedAccess", resp.Responses)
		}
	})

	t.Run("a wildcard-endpoint write skips the attribute", func(t *testing.T) {
		t.Parallel()
		d := &fabricScopedRecorder{}
		wild := acl
		wild.HasEndpoint = false
		resp := HandleWriteRequest(pase, d, WriteRequest{Writes: []AttributeWrite{{Path: wild}}})
		if len(resp.Responses) != 0 || len(d.written) != 0 {
			t.Fatalf("responses = %+v, written = %v, want neither", resp.Responses, d.written)
		}
	})

	t.Run("an attribute that is not fabric-scoped is written over PASE before AddNOC", func(t *testing.T) {
		t.Parallel()
		d := &fabricScopedRecorder{}
		breadcrumb := ConcreteAttributePath{Cluster: 0x0030, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true}
		wild := breadcrumb
		wild.HasEndpoint = false
		resp := HandleWriteRequest(pase, d, WriteRequest{Writes: []AttributeWrite{{Path: breadcrumb}, {Path: wild}}})
		if len(resp.Responses) != 2 || resp.Responses[0].Status.Status != StatusSuccess || resp.Responses[1].Status.Status != StatusSuccess {
			t.Fatalf("responses = %+v, want two Success", resp.Responses)
		}
		if len(d.written) != 2 {
			t.Fatalf("written = %v, want both", d.written)
		}
	})
}
