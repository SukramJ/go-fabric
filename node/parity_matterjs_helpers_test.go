// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

// Shared support of the ports of matter.js packages/node/test/node at
// 862ad75f: the Go counterparts of MockServerNode.createOnline
// (packages/node/src/testing/mock-server-node.ts:195-240) and of
// read-helpers.ts countAttrs, on fabrictest.

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"
)

// mockVendorName is MockServerNode's BasicInformation.vendorName, the value
// AttributeReadResponseTest.ts:52 reads back.
const mockVendorName = "Matter.js Test Vendor"

// parityCtx bounds one port on the wall clock.
func parityCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// quietSite is a site whose hosts log nowhere.
func quietSite(t *testing.T) *fabrictest.Site {
	t.Helper()
	return fabrictest.NewSite(t, fabrictest.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
}

// mockOptions shapes a mock pair.
type mockOptions struct {
	// light adds an OnOffLight — MockServerNode.createOnline's default
	// device (mock-server-node.ts:215-218); without it the node is the
	// root and the Aggregator only, `{ device: undefined }`.
	light bool
	// configure edits the node's configuration after the mock defaults.
	configure func(*node.Config)
}

// mockPair is a commissioned pair whose device carries MockServerNode's
// vendor name, and with o.light an OnOffLight under the Aggregator.
type mockPair struct {
	site  *fabrictest.Site
	pair  *fabrictest.Pair
	light *device.OnOffLight
	// lightEP is the light's endpoint, 0 without one.
	lightEP uint16
}

func newMockPair(t *testing.T, o mockOptions) *mockPair {
	t.Helper()
	site := quietSite(t)
	m := &mockPair{site: site}
	topo := fabrictest.Topology{Configure: func(cfg *node.Config) {
		cfg.BasicInformation.VendorName = mockVendorName
		if o.configure != nil {
			o.configure(cfg)
		}
	}}
	if o.light {
		m.light = newLight(t, site, "light:1")
		topo.Bridged = []node.Endpoint{m.light}
	}
	m.pair = site.CommissionedPair(fabrictest.PairOptions{Device: topo})
	if o.light {
		ep, ok := m.pair.Device.EndpointOf("light:1")
		if !ok {
			t.Fatal("the light has no endpoint")
		}
		m.lightEP = ep
	}
	return m
}

func newLight(t *testing.T, site *fabrictest.Site, key string) *device.OnOffLight {
	t.Helper()
	l, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey(key), Name: key, Clock: site.Clock()})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// countAttrs is read-helpers.ts countAttrs: the number of attribute values
// per endpoint and cluster.
func countAttrs(r *interaction.Report) map[uint16]map[uint32]int {
	out := map[uint16]map[uint32]int{}
	for _, a := range r.Attributes {
		if out[a.Path.Endpoint] == nil {
			out[a.Path.Endpoint] = map[uint32]int{}
		}
		out[a.Path.Endpoint][a.Path.Cluster]++
	}
	return out
}

// listLen is the number of elements of a list value.
func listLen(r *interaction.Report, ep uint16, cluster, attr uint32) (int, bool) {
	v, ok := r.Value(ep, cluster, attr)
	if !ok {
		return 0, false
	}
	return len(v.Children), true
}

// strVal, boolVal and uintVal are values a write or a command field sends.
func strVal(v string) spec.Encodable {
	return interaction.Raw(spec.Node{El: tlv.Element{Type: tlv.TypeUTF8Str1, String: v}})
}

func boolVal(v bool) spec.Encodable {
	typ := tlv.TypeBoolFalse
	if v {
		typ = tlv.TypeBoolTrue
	}
	return interaction.Raw(spec.Node{El: tlv.Element{Type: typ, Bool: v}})
}

func uintVal(v uint64) spec.Encodable {
	return interaction.Raw(spec.Node{El: tlv.Element{Type: tlv.TypeUnsignedInt8, Uint: v}})
}

// rawAnswer is the device's answer to a raw request: the decoded response,
// or the status of a StatusResponse in its place.
type rawAnswer[T any] struct {
	resp   T
	status *im.StatusCode
}

// rawWrite sends req as it stands — matter.js writeAttrRaw /
// writeAttrRawAsAdmin (AttributeWriteResponseTest.ts:392-409) — past the
// controller's own formulation, over fabrictest.Exchange.
func rawWrite(t *testing.T, m *mockPair, req im.WriteRequest) rawAnswer[im.WriteResponse] {
	t.Helper()
	enc := tlv.NewEncoder()
	req.MarshalTLV(enc, func(e *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) {
		if value, ok := v.Value.(spec.Encodable); ok {
			value.EncodeTLV(e, tag)
		}
	})
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	msg, err := fabrictest.Exchange(parityCtx(t), m.pair, im.OpcodeWriteRequest, b)
	if err != nil {
		t.Fatal(err)
	}
	var out rawAnswer[im.WriteResponse]
	switch msg.Opcode {
	case im.OpcodeStatusResponse:
		sr, err := im.UnmarshalStatusResponseTLV(tlv.NewDecoder(msg.Payload))
		if err != nil {
			t.Fatal(err)
		}
		out.status = &sr.Status
	case im.OpcodeWriteResponse:
		out.resp, err = im.UnmarshalWriteResponseTLV(tlv.NewDecoder(msg.Payload))
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("answer opcode 0x%02x", msg.Opcode)
	}
	return out
}
