// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	gcdef "thingfabric.dev/go-fabric/cluster/spec/groupcast"
	tcldef "thingfabric.dev/go-fabric/cluster/spec/tlsclientmanagement"
	"thingfabric.dev/go-fabric/contract"
	endpointpkg "thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/tlv"
)

// hostListServer is a host's own cluster server for one list attribute: no
// contract.FabricScopedReader, no declaration — it returns every fabric's
// entries whole, as a server does under the dispatcher's scope.
type hostListServer struct {
	cluster, attribute uint32
	value              any
}

func (s hostListServer) MatterClusterID() uint32 { return s.cluster }
func (s hostListServer) MatterRead(attrID uint32) (any, bool) {
	if attrID == s.attribute {
		return s.value, true
	}
	return nil, false
}
func (hostListServer) MatterWrite(context.Context, uint32, any) error { return nil }
func (hostListServer) MatterInvoke(context.Context, uint32, any) (any, error) {
	return nil, nil
}
func (hostListServer) MatterReportable() []uint32 { return nil }

// scopeSource serves its servers on one endpoint.
type scopeSource struct{ servers []contract.ClusterServer }

func (scopeSource) MatterDeviceType() uint16 { return 0x0100 }
func (s scopeSource) MatterClusterServers() []contract.ClusterServer {
	return s.servers
}

// Two fabrics' entries of a class-A list (Groupcast Membership: KeySetId,
// tag 2, is fabric-sensitive) and of a class-B list (TlsClientManagement
// ProvisionedEndpoints, access "S").
func membershipOf(fabric uint8) gcdef.MembershipStruct {
	endpoints := []uint16{1}
	return gcdef.MembershipStruct{GroupId: 0x0100 + uint16(fabric), Endpoints: &endpoints, KeySetId: 0x0200 + uint16(fabric), FabricIndex: fabric}
}

func tlsEndpointOf(fabric uint8) tcldef.TLSEndpointStruct {
	return tcldef.TLSEndpointStruct{EndpointId: uint16(fabric), Hostname: []byte("host.example"), Port: 443, Caid: 1, Ccdid: spec.Nullable[uint16]{Null: true}, FabricIndex: fabric}
}

var (
	membershipPath = im.ConcreteAttributePath{Endpoint: 1, Cluster: gcdef.ClusterID, Attribute: gcdef.AttrMembership, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	tlsPath        = im.ConcreteAttributePath{Endpoint: 1, Cluster: tcldef.ClusterID, Attribute: tcldef.AttrProvisionedEndpoints, HasEndpoint: true, HasCluster: true, HasAttribute: true}
)

// scopeDispatcher is a topology of one endpoint serving servers.
func scopeDispatcher(servers ...contract.ClusterServer) im.Dispatcher {
	ep := &endpointpkg.Endpoint{ID: 1, DeviceType: 0x0100, SourceKey: endpointpkg.StringKey("scope"), Scope: "s", DeviceAddress: "d", Source: scopeSource{servers}}
	return endpointpkg.NewTopologyDispatcher(&endpointpkg.Topology{Endpoints: []*endpointpkg.Endpoint{ep}})
}

// hostDispatcher serves both lists from host servers holding fabrics 1 and 2.
func hostDispatcher() im.Dispatcher {
	return scopeDispatcher(
		hostListServer{gcdef.ClusterID, gcdef.AttrMembership, []gcdef.MembershipStruct{membershipOf(1), membershipOf(2)}},
		hostListServer{tcldef.ClusterID, tcldef.AttrProvisionedEndpoints, []tcldef.TLSEndpointStruct{tlsEndpointOf(1), tlsEndpointOf(2)}},
	)
}

// wireEntry is one list entry as a reader decodes it from the wire: the
// fabric its FabricIndex names and the context tags it carries.
type wireEntry struct {
	fabric uint64
	tags   []uint32
}

func (e wireEntry) String() string { return fmt.Sprintf("fabric %d %v", e.fabric, e.tags) }

// readWireEntries decodes a list value — or one appended entry — into its
// entries.
func readWireEntries(_ im.ConcreteAttributePath, el tlv.Element, dec *tlv.Decoder) (im.AttributeValue, error) {
	switch el.Type {
	case tlv.TypeStructure:
		entry, err := readWireEntry(dec)
		return im.AttributeValue{Value: []wireEntry{entry}}, err
	case tlv.TypeArray:
	default:
		return im.AttributeValue{}, fmt.Errorf("value type 0x%02X, want an array", el.Type)
	}
	entries := []wireEntry{}
	for {
		e, err := dec.Next()
		if err != nil {
			return im.AttributeValue{}, err
		}
		if e.IsEndContainer {
			return im.AttributeValue{Value: entries}, nil
		}
		entry, err := readWireEntry(dec)
		if err != nil {
			return im.AttributeValue{}, err
		}
		entries = append(entries, entry)
	}
}

// readWireEntry reads the fields of the entry struct dec has just opened.
func readWireEntry(dec *tlv.Decoder) (wireEntry, error) {
	var entry wireEntry
	for {
		f, err := dec.Next()
		if err != nil {
			return entry, err
		}
		if f.IsEndContainer {
			return entry, nil
		}
		if f.IsContainer {
			if err := skipTestContainer(dec); err != nil {
				return entry, err
			}
		}
		entry.tags = append(entry.tags, f.Tag.Number)
		if f.Tag.Number == 0xFE {
			entry.fabric = f.Uint
		}
	}
}

func skipTestContainer(dec *tlv.Decoder) error {
	for depth := 1; depth > 0; {
		el, err := dec.Next()
		if err != nil {
			return err
		}
		switch {
		case el.IsEndContainer:
			depth--
		case el.IsContainer:
			depth++
		}
	}
	return nil
}

// onWire encodes reports as the bridge sends them and decodes them as a
// reader does: a value list's entries, or a status.
func onWire(t *testing.T, reports ...im.AttributeReport) []im.AttributeReport {
	t.Helper()
	body, err := EncodeReportData(im.ReportData{Reports: reports})
	if err != nil {
		t.Fatal(err)
	}
	rd, err := im.UnmarshalReportDataTLV(tlv.NewDecoder(body), readWireEntries, nil)
	if err != nil {
		t.Fatal(err)
	}
	return rd.Reports
}

// wantScoped holds a report of the class-A list read as reader to the
// scoped value: the reader's own entry whole, another fabric's without
// KeySetId (tag 2) on an unfiltered read and left out on a filtered one;
// and of the class-B list to the reader's own entry alone.
func wantScoped(t *testing.T, what string, rep im.AttributeReport, filtered bool, reader uint8) {
	t.Helper()
	if rep.IsStatus {
		t.Fatalf("%s: %v answered status %v", what, rep.Path, rep.Status.Status)
	}
	entries, _ := rep.Value.Value.([]wireEntry)
	var want []wireEntry
	switch rep.Path.Cluster {
	case gcdef.ClusterID:
		whole := []uint32{0, 1, 2, 4, 0xFE}
		masked := []uint32{0, 1, 4, 0xFE}
		for _, f := range []uint64{1, 2} {
			switch {
			case f == uint64(reader):
				want = append(want, wireEntry{f, whole})
			case !filtered:
				want = append(want, wireEntry{f, masked})
			}
		}
	case tcldef.ClusterID:
		if reader == 2 {
			want = append(want, wireEntry{2, []uint32{0, 1, 2, 3, 4, 5, 0xFE}})
		}
	}
	if !slices.EqualFunc(entries, want, func(a, b wireEntry) bool { return a.fabric == b.fabric && slices.Equal(a.tags, b.tags) }) {
		t.Errorf("%s: %v reads %v, want %v", what, rep.Path, entries, want)
	}
}

// TestDispatcherScopesAHostServersFabricScopedLists: the dispatcher scopes
// every fabric-scoped attribute of the model, whatever the server returns.
// A host's own server without contract.FabricScopedReader returns two
// fabrics' entries whole; a read of fabric 2 — unfiltered, filtered, and a
// session without a fabric — receives on the wire what the model scopes:
// the class-A list (fabric-sensitive entry fields) with another fabric's
// KeySetId withheld or the entry left out, the class-B list (an "S"
// attribute) with its reader's entries alone.
func TestDispatcherScopesAHostServersFabricScopedLists(t *testing.T) {
	t.Parallel()
	d := hostDispatcher()
	for _, tc := range []struct {
		filtered bool
		reader   uint8
	}{{false, 2}, {true, 2}, {false, 0}, {true, 0}} {
		ctx := im.WithAuthModePASE(im.WithFabricFilter(context.Background(), tc.filtered, tc.reader))
		reports := im.NewReadReports(ctx, d, im.ReadRequest{AttributeRequests: []im.ConcreteAttributePath{membershipPath, tlsPath}})
		var got []im.AttributeReport
		for rep, ok := reports.Next(); ok; rep, ok = reports.Next() {
			got = append(got, rep)
		}
		wire := onWire(t, got...)
		if len(wire) != 2 {
			t.Fatalf("filtered %v reader %d: %d reports, want 2", tc.filtered, tc.reader, len(wire))
		}
		for _, rep := range wire {
			wantScoped(t, fmt.Sprintf("read filtered %v reader %d", tc.filtered, tc.reader), rep, tc.filtered, tc.reader)
		}
	}
}

// TestDispatcherScopesPrimingAndChangeReports: the scope rides the
// subscription's priming report and its change reports as it rides a
// read.
func TestDispatcherScopesPrimingAndChangeReports(t *testing.T) {
	t.Parallel()
	d := hostDispatcher()
	b := &Bridge{logger: slog.New(slog.DiscardHandler)}
	ctx := im.WithAuthModePASE(im.WithFabricFilter(context.Background(), false, 2))
	p := b.newPrimingSource(ctx, d, im.SubscribeRequest{AttributeRequests: []im.ConcreteAttributePath{membershipPath, tlsPath}})
	var priming []im.AttributeReport
	for rep, ok := p.next(); ok; rep, ok = p.next() {
		priming = append(priming, rep)
	}
	for _, rep := range onWire(t, priming...) {
		wantScoped(t, "priming", rep, false, 2)
	}

	sb := newStartedBridge(t)
	sub := &subscription.Subscription{AttributePaths: []im.ConcreteAttributePath{membershipPath, tlsPath}}
	changes := sb.subscriptionAttributeReports(context.Background(), d, subTarget{fabricIndex: 2, pase: true}, sub, []im.ConcreteAttributePath{membershipPath, tlsPath})
	wire := onWire(t, changes...)
	if len(wire) != 2 {
		t.Fatalf("change reports %+v, want both paths", wire)
	}
	for _, rep := range wire {
		wantScoped(t, "change report", rep, false, 2)
	}
}

// TestDispatcherScopesAListBeforeItIsSplit: a scoped list too large for one
// message is scoped before the chunker splits it — no part carries
// another fabric's fabric-sensitive field, and the class-B list's parts
// carry the reader's entries alone.
func TestDispatcherScopesAListBeforeItIsSplit(t *testing.T) {
	t.Parallel()
	memberships := make([]gcdef.MembershipStruct, 0, 60)
	endpoints := make([]tcldef.TLSEndpointStruct, 0, 60)
	for i := range 60 {
		memberships = append(memberships, membershipOf(uint8(1+i%2)))
		endpoints = append(endpoints, tlsEndpointOf(uint8(1+i%2)))
	}
	d := scopeDispatcher(
		hostListServer{gcdef.ClusterID, gcdef.AttrMembership, memberships},
		hostListServer{tcldef.ClusterID, tcldef.AttrProvisionedEndpoints, endpoints},
	)
	ctx := im.WithFabricFilter(context.Background(), false, 2)
	var reports []im.AttributeReport
	for _, path := range []im.ConcreteAttributePath{membershipPath, tlsPath} {
		for _, rr := range d.Read(ctx, path) {
			reports = append(reports, im.AttributeReport{Path: rr.Path, Value: rr.Value, FabricScope: rr.FabricScope})
		}
	}
	chunks, err := chunkReportData(nil, im.ReportData{Reports: reports}, 400, reportChunkHardCap)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 3 {
		t.Fatalf("%d chunks: the lists were not split", len(chunks))
	}
	counts := map[uint32]map[uint64]int{gcdef.ClusterID: {}, tcldef.ClusterID: {}}
	for _, chunk := range chunks {
		for _, part := range chunk.Reports {
			if part.FabricScope != nil {
				t.Errorf("%v: a part still carries its scope", part.Path)
			}
			for _, rep := range onWire(t, part) {
				if rep.IsStatus {
					t.Fatalf("%v answered %v", rep.Path, rep.Status.Status)
				}
				entries, _ := rep.Value.Value.([]wireEntry)
				for _, e := range entries {
					counts[rep.Path.Cluster][e.fabric]++
					if e.fabric != 2 && slices.Contains(e.tags, 2) && rep.Path.Cluster == gcdef.ClusterID {
						t.Errorf("%v: fabric %d's entry carries KeySetId", rep.Path, e.fabric)
					}
				}
			}
		}
	}
	if counts[gcdef.ClusterID][1] != 30 || counts[gcdef.ClusterID][2] != 30 || counts[tcldef.ClusterID][1] != 0 || counts[tcldef.ClusterID][2] != 30 {
		t.Errorf("entries per fabric on the wire %v, want 30/30 memberships and the 30 endpoints of fabric 2", counts)
	}
}

// indexlessEntry is a list entry that names no owner: a FabricIndex of 0,
// or none at all.
type indexlessEntry struct {
	index *uint8
}

func (e indexlessEntry) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutUint(tlv.ContextTag(0), 0x0101)
	enc.PutUint(tlv.ContextTag(2), 0x0202)
	if e.index != nil {
		enc.PutUint(tlv.ContextTag(0xFE), uint64(*e.index))
	}
	_ = enc.EndContainer()
}

// TestFabricScopedEntryWithoutAnOwnerFailsThePath: an entry of a
// fabric-scoped list that names no fabric — no FabricIndex, or index 0 —
// cannot be scoped, and the request is answered FAILURE for the path: no
// entry of the list is served.
func TestFabricScopedEntryWithoutAnOwnerFailsThePath(t *testing.T) {
	t.Parallel()
	zero := uint8(0)
	one := uint8(1)
	for _, index := range []*uint8{nil, &zero} {
		d := scopeDispatcher(hostListServer{gcdef.ClusterID, gcdef.AttrMembership, []indexlessEntry{{&one}, {index}}})
		for _, filtered := range []bool{false, true} {
			ctx := im.WithAuthModePASE(im.WithFabricFilter(context.Background(), filtered, 1))
			reports := im.NewReadReports(ctx, d, im.ReadRequest{AttributeRequests: []im.ConcreteAttributePath{membershipPath}})
			rep, ok := reports.Next()
			if !ok {
				t.Fatal("no report")
			}
			wire := onWire(t, rep)
			if len(wire) != 1 || !wire[0].IsStatus || wire[0].Status.Status != im.StatusFailure {
				t.Errorf("index %v filtered %v: %+v, want FAILURE for the path", index, filtered, wire)
			}
			if chunked := applyFabricScope(rep); !chunked.IsStatus || chunked.Status.Status != im.StatusFailure {
				t.Errorf("index %v filtered %v: before chunking %+v, want FAILURE for the path", index, filtered, chunked)
			}
		}
	}
}

// TestDispatcherScopesTheGeneratedServersSensitiveList: the generated
// server of a definition whose list attribute is fabric-sensitive ("S"),
// holding two fabrics' entries, is read by fabric 2 as its own entries
// alone — the scope applies to the module's own servers as to a host's.
func TestDispatcherScopesTheGeneratedServersSensitiveList(t *testing.T) {
	t.Parallel()
	list := spec.List[tcldef.TLSEndpointStruct]{tlsEndpointOf(1), tlsEndpointOf(2)}
	srv, err := spec.NewServer(tcldef.Definition, spec.Options{}, spec.ServerConfig{Unimplemented: true, Initial: map[uint32]any{tcldef.AttrProvisionedEndpoints: list}})
	if err != nil {
		t.Fatal(err)
	}
	d := scopeDispatcher(srv)
	for _, filtered := range []bool{false, true} {
		ctx := im.WithAuthModePASE(im.WithFabricFilter(context.Background(), filtered, 2))
		reports := im.NewReadReports(ctx, d, im.ReadRequest{AttributeRequests: []im.ConcreteAttributePath{tlsPath}})
		rep, ok := reports.Next()
		if !ok {
			t.Fatal("no report")
		}
		for _, w := range onWire(t, rep) {
			wantScoped(t, fmt.Sprintf("generated server, filtered %v", filtered), w, filtered, 2)
		}
	}
	// A read with no request behind it — a local actor — reads both.
	v, _ := srv.MatterRead(tcldef.AttrProvisionedEndpoints)
	if got, _ := v.(spec.List[tcldef.TLSEndpointStruct]); len(got) != 2 {
		t.Errorf("local read %v, want both fabrics' entries", v)
	}
}
