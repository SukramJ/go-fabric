// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

// Shared support of the ports of matter.js packages/node/test/node at
// 862ad75f: the Go counterparts of MockServerNode.createOnline
// (packages/node/src/testing/mock-server-node.ts:195-240) and of
// read-helpers.ts countAttrs, on fabrictest.

import (
	"context"
	"log/slog"
	"strings"
	"sync"
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

// siteSlots runs the package's fabrictest sites one at a time. A fake site
// clock moves only while the whole test process is quiet (fabrictest
// Site.Run, the docs' "Limits"), so busy sites in parallel starve each
// other's clocks — under the race detector past parityCtx's budget; one at
// a time, every site reaches its quiet points at once. The slot belongs to
// a test and its subtests: a test that builds several sites, or a parent
// whose subtests build their own, takes it once.
var siteSlots = struct {
	mu     sync.Mutex
	free   *sync.Cond
	holder string
}{}

func init() { siteSlots.free = sync.NewCond(&siteSlots.mu) }

// holdSite takes the package's site slot until the test ends; a test inside
// the holder's tree goes ahead.
func holdSite(t *testing.T) {
	t.Helper()
	name := t.Name()
	siteSlots.mu.Lock()
	defer siteSlots.mu.Unlock()
	inTree := func() bool {
		h := siteSlots.holder
		return name == h || strings.HasPrefix(name, h+"/")
	}
	for siteSlots.holder != "" && !inTree() {
		siteSlots.free.Wait()
	}
	if siteSlots.holder != "" {
		return
	}
	siteSlots.holder = name
	t.Cleanup(func() {
		siteSlots.mu.Lock()
		siteSlots.holder = ""
		siteSlots.mu.Unlock()
		siteSlots.free.Broadcast()
	})
}

// quietSite is a site whose hosts log nowhere; it holds the site slot
// until the test ends.
func quietSite(t *testing.T) *fabrictest.Site {
	t.Helper()
	holdSite(t)
	return fabrictest.NewSite(t, fabrictest.Options{Logger: slog.New(slog.DiscardHandler)})
}

// steppedSite is a quiet site whose settle period is short, for a test
// that steps the clock deadline by deadline itself.
func steppedSite(t *testing.T) *fabrictest.Site {
	t.Helper()
	holdSite(t)
	return fabrictest.NewSite(t, fabrictest.Options{Logger: slog.New(slog.DiscardHandler), Settle: 2 * time.Millisecond})
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
	for i := range r.Attributes {
		a := &r.Attributes[i]
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

// encodeInvoke encodes req as a client sends it — every field
// im.MarshalInvokeRequestTLV writes, plus DelayReportData (tag 3) when
// set. A nil command field is an empty structure, matter.js's encoding of
// a command without arguments.
func encodeInvoke(t *testing.T, req im.InvokeRequest) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(0), req.SuppressResponse)
	enc.PutBool(tlv.ContextTag(1), req.TimedRequest)
	enc.StartArray(tlv.ContextTag(2))
	for _, inv := range req.Invokes {
		enc.StartStruct(tlv.AnonymousTag())
		inv.Path.MarshalTLV(enc, tlv.ContextTag(0))
		if f, ok := inv.Fields.(spec.Encodable); ok {
			f.EncodeTLV(enc, tlv.ContextTag(1))
		} else {
			enc.StartStruct(tlv.ContextTag(1))
			_ = enc.EndContainer()
		}
		if inv.HasCommandRef {
			enc.PutUint(tlv.ContextTag(2), uint64(inv.CommandRef))
		}
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
	if d := req.DelayReportData; d != nil {
		enc.StartStruct(tlv.ContextTag(3))
		enc.PutUint(tlv.ContextTag(0), uint64(d.DelayMinMs))
		enc.PutUint(tlv.ContextTag(1), uint64(d.DelayJitterWindowMs))
		_ = enc.EndContainer()
	}
	enc.PutUint(tlv.ContextTag(0xFF), uint64(im.MatterInteractionModelRevision))
	_ = enc.EndContainer()
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// rawInvoke sends req over p — matter.js invokeCmdRaw
// (CommandInvokeResponseTest.ts:830-865), on the wire — and returns the
// InvokeResponse or the status of a StatusResponse in its place.
func rawInvoke(t *testing.T, p *fabrictest.Pair, req im.InvokeRequest) rawAnswer[im.InvokeResponse] {
	t.Helper()
	msg, err := fabrictest.Exchange(parityCtx(t), p, im.OpcodeInvokeRequest, encodeInvoke(t, req))
	if err != nil {
		t.Fatal(err)
	}
	var out rawAnswer[im.InvokeResponse]
	switch msg.Opcode {
	case im.OpcodeStatusResponse:
		sr, err := im.UnmarshalStatusResponseTLV(tlv.NewDecoder(msg.Payload))
		if err != nil {
			t.Fatal(err)
		}
		out.status = &sr.Status
	case im.OpcodeInvokeResponse:
		out.resp, err = im.UnmarshalInvokeResponseTLV(tlv.NewDecoder(msg.Payload), func(_ im.ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element) (any, error) {
			if !el.IsContainer {
				return el, nil
			}
			return spec.ReadContainer(dec, el)
		})
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("answer opcode 0x%02x", msg.Opcode)
	}
	return out
}

// cmdPath is a concrete command path; wildcardCmd one without endpoint.
func cmdPath(ep uint16, cluster, cmd uint32) im.ConcreteCommandPath {
	return im.ConcreteCommandPath{Endpoint: ep, Cluster: cluster, Command: cmd, HasEndpoint: true, HasCluster: true, HasCommand: true}
}

func wildcardCmd(cluster, cmd uint32) im.ConcreteCommandPath {
	return im.ConcreteCommandPath{Cluster: cluster, Command: cmd, HasCluster: true, HasCommand: true}
}
