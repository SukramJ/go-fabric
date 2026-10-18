// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

// A port of matter.js packages/protocol/test/action/request/ReadTest.ts at
// 862ad75f: the ReadRequest a client formulates — attribute and event
// paths with wildcards, fabric filtering, data version and event filters,
// the revision.
//
// matter.js tests the Read() request object; the Go controller formulates
// the request inside Interaction.ReadWith, so each case reads the
// ReadRequest off the wire of a scripted peer. An absent id of a matter.js
// path is a false Has* flag of im.ConcreteAttributePath.

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// readCaptured runs Interaction.ReadWith against a scripted peer, answers
// with an empty final report that suppresses the response, and returns the
// ReadRequest as sent with its payload.
func readCaptured(t *testing.T, r interaction.ReadRequest) (parsed im.ReadRequest, payload []byte) {
	t.Helper()
	s := newScripted(t)
	c := interaction.New(s.mgr, s.session, nil)
	done := make(chan error, 1)
	go func() {
		_, err := c.ReadWith(context.Background(), r)
		done <- err
	}()
	req := s.next(t)
	if req.proto.Opcode != im.OpcodeReadRequest {
		t.Fatalf("opcode 0x%02X", req.proto.Opcode)
	}
	parsed, err := im.UnmarshalReadRequestTLV(tlv.NewDecoder(req.payload))
	if err != nil {
		t.Fatal(err)
	}
	s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeReportData, reportChunk(t, false, true))
	if err := await(t, done); err != nil {
		t.Fatal(err)
	}
	return parsed, req.payload
}

// readFieldPresent reports whether the request message carries context
// tag tag.
func readFieldPresent(t *testing.T, payload []byte, tag uint32) bool {
	t.Helper()
	n, err := spec.ReadElement(tlv.NewDecoder(payload))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range n.Children {
		if c.El.Tag.Kind == tlv.TagKindContext && c.El.Tag.Number == tag {
			return true
		}
	}
	return false
}

func readAttr(endpoint uint16, cluster, attribute uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: endpoint, Cluster: cluster, Attribute: attribute, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

func readCluster(endpoint uint16, cluster uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: endpoint, Cluster: cluster, HasEndpoint: true, HasCluster: true}
}

func readEvents(endpoint uint16, cluster uint32) im.ConcreteEventPath {
	return im.ConcreteEventPath{Endpoint: endpoint, Cluster: cluster, HasEndpoint: true, HasCluster: true}
}

func TestReadRequestBuildAttributes(t *testing.T) {
	t.Parallel()
	t.Run("creates read request for single attribute using numeric IDs", func(t *testing.T) {
		t.Parallel()
		req, _ := readCaptured(t, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{readAttr(1, 6, 0)}})
		if len(req.AttributeRequests) != 1 || req.AttributeRequests[0] != readAttr(1, 6, 0) {
			t.Fatalf("attributes %+v", req.AttributeRequests) // ReadTest.ts:24-27
		}
	})
	t.Run("creates read request for multiple attributes", func(t *testing.T) {
		t.Parallel()
		req, _ := readCaptured(t, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{readAttr(1, 6, 0), readAttr(1, 6, 16384)}})
		if len(req.AttributeRequests) != 2 || req.AttributeRequests[0].Attribute != 0 || req.AttributeRequests[1].Attribute != 16384 {
			t.Fatalf("attributes %+v", req.AttributeRequests) // ReadTest.ts:46-48
		}
	})
	t.Run("creates cluster-wide attribute read (wildcard)", func(t *testing.T) {
		t.Parallel()
		req, _ := readCaptured(t, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{readCluster(1, 6)}})
		if len(req.AttributeRequests) != 1 {
			t.Fatalf("attributes %+v", req.AttributeRequests)
		}
		p := req.AttributeRequests[0]
		if !p.HasEndpoint || p.Endpoint != 1 || !p.HasCluster || p.Cluster != 6 || p.HasAttribute {
			t.Fatalf("path %+v", p) // ReadTest.ts:61-64
		}
	})
	t.Run("creates endpoint-wide wildcard read", func(t *testing.T) {
		t.Parallel()
		req, _ := readCaptured(t, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{{Endpoint: 1, HasEndpoint: true}}})
		if len(req.AttributeRequests) != 1 {
			t.Fatalf("attributes %+v", req.AttributeRequests)
		}
		p := req.AttributeRequests[0]
		if !p.HasEndpoint || p.Endpoint != 1 || p.HasCluster || p.HasAttribute {
			t.Fatalf("path %+v", p) // ReadTest.ts:76-79
		}
	})
	t.Run("creates full wildcard read", func(t *testing.T) {
		t.Parallel()
		req, _ := readCaptured(t, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{{}}})
		if len(req.AttributeRequests) != 1 {
			t.Fatalf("attributes %+v", req.AttributeRequests)
		}
		if p := req.AttributeRequests[0]; p.HasEndpoint || p.HasCluster || p.HasAttribute {
			t.Fatalf("path %+v", p) // ReadTest.ts:87-90
		}
	})
}

func TestReadRequestBuildFabricFiltering(t *testing.T) {
	t.Parallel()
	paths := []im.ConcreteAttributePath{readCluster(0, 0x28)}
	t.Run("enables fabric filtering by default", func(t *testing.T) {
		t.Parallel()
		if req, _ := readCaptured(t, interaction.ReadRequest{Attributes: paths}); !req.FabricFiltered {
			t.Fatal("not fabric-filtered") // ReadTest.ts:105
		}
	})
	t.Run("disables fabric filtering when specified", func(t *testing.T) {
		t.Parallel()
		if req, _ := readCaptured(t, interaction.ReadRequest{Attributes: paths, NoFabricFilter: true}); req.FabricFiltered {
			t.Fatal("fabric-filtered") // ReadTest.ts:119
		}
	})
	t.Run("enables fabric filtering explicitly", func(t *testing.T) {
		t.Parallel()
		// fabricFilter: true is NoFabricFilter false.
		if req, _ := readCaptured(t, interaction.ReadRequest{Attributes: paths, NoFabricFilter: false}); !req.FabricFiltered {
			t.Fatal("not fabric-filtered") // ReadTest.ts:133
		}
	})
}

func TestReadRequestBuildDataVersionFilters(t *testing.T) {
	t.Parallel()
	t.Run("includes data version filters when provided", func(t *testing.T) {
		t.Parallel()
		req, _ := readCaptured(t, interaction.ReadRequest{
			Attributes:         []im.ConcreteAttributePath{readCluster(1, 6)},
			DataVersionFilters: []im.DataVersionFilter{{Endpoint: 1, Cluster: 6, DataVersion: 12345}},
		})
		if len(req.DataVersionFilters) != 1 || req.DataVersionFilters[0].DataVersion != 12345 {
			t.Fatalf("filters %+v", req.DataVersionFilters) // ReadTest.ts:154-155
		}
	})
	t.Run("includes multiple data version filters", func(t *testing.T) {
		t.Parallel()
		req, _ := readCaptured(t, interaction.ReadRequest{
			Attributes: []im.ConcreteAttributePath{readCluster(1, 6), readCluster(1, 8)},
			DataVersionFilters: []im.DataVersionFilter{
				{Endpoint: 1, Cluster: 6, DataVersion: 100},
				{Endpoint: 1, Cluster: 8, DataVersion: 200},
			},
		})
		if len(req.DataVersionFilters) != 2 || req.DataVersionFilters[0].DataVersion != 100 || req.DataVersionFilters[1].DataVersion != 200 {
			t.Fatalf("filters %+v", req.DataVersionFilters) // ReadTest.ts:182-184
		}
	})
}

func TestReadRequestBuildEvents(t *testing.T) {
	t.Parallel()
	t.Run("creates event read request", func(t *testing.T) {
		t.Parallel()
		ev := im.ConcreteEventPath{Endpoint: 0, Cluster: 0x28, Event: 0, HasEndpoint: true, HasCluster: true, HasEvent: true}
		req, _ := readCaptured(t, interaction.ReadRequest{Events: []im.ConcreteEventPath{ev}})
		if len(req.EventRequests) != 1 {
			t.Fatalf("events %+v", req.EventRequests)
		}
		if p := req.EventRequests[0]; !p.HasEndpoint || p.Endpoint != 0 || p.Cluster != 0x28 || !p.HasEvent || p.Event != 0 {
			t.Fatalf("path %+v", p) // ReadTest.ts:200-203
		}
	})
	t.Run("creates event wildcard read", func(t *testing.T) {
		t.Parallel()
		req, _ := readCaptured(t, interaction.ReadRequest{Events: []im.ConcreteEventPath{readEvents(0, 0x28)}})
		if len(req.EventRequests) != 1 || req.EventRequests[0].HasEvent {
			t.Fatalf("events %+v", req.EventRequests) // ReadTest.ts:216-217
		}
	})
	t.Run("includes event filters when provided", func(t *testing.T) {
		t.Parallel()
		req, _ := readCaptured(t, interaction.ReadRequest{
			Events:       []im.ConcreteEventPath{readEvents(0, 0x28)},
			EventFilters: []im.EventMinimumNumber{{EventMin: 1000}},
		})
		if len(req.EventFilters) != 1 || req.EventFilters[0].EventMin != 1000 {
			t.Fatalf("filters %+v", req.EventFilters) // ReadTest.ts:235-236
		}
	})
}

func TestReadRequestBuildCombined(t *testing.T) {
	t.Parallel()
	t.Run("creates request with both attributes and events", func(t *testing.T) {
		t.Parallel()
		req, _ := readCaptured(t, interaction.ReadRequest{
			Attributes: []im.ConcreteAttributePath{readCluster(1, 6)},
			Events:     []im.ConcreteEventPath{readEvents(0, 0x28)},
		})
		if len(req.AttributeRequests) != 1 || len(req.EventRequests) != 1 {
			t.Fatalf("request %+v", req) // ReadTest.ts:257-258
		}
	})
}

func TestReadRequestBuildValidation(t *testing.T) {
	t.Parallel()
	t.Run("permits empty options object with no attributes or events", func(t *testing.T) {
		t.Parallel()
		_, wire := readCaptured(t, interaction.ReadRequest{})
		// AttributeRequests (0) and EventRequests (1) are left out.
		if readFieldPresent(t, wire, 0) || readFieldPresent(t, wire, 1) {
			t.Fatal("an empty request carries a path list") // ReadTest.ts:282-283
		}
	})
}

func TestReadRequestBuildInteractionModelRevision(t *testing.T) {
	t.Parallel()
	t.Run("uses default interaction model revision when not specified", func(t *testing.T) {
		t.Parallel()
		_, wire := readCaptured(t, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{{Endpoint: 1, HasEndpoint: true}}})
		if !readFieldPresent(t, wire, 0xFF) {
			t.Fatal("no InteractionModelRevision") // ReadTest.ts:299
		}
	})
}
