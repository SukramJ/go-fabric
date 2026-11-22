// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/bridge"
	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/cluster/spec"
	ecodef "thingfabric.dev/go-fabric/cluster/spec/ecosysteminformation"
	icddef "thingfabric.dev/go-fabric/cluster/spec/icdmanagement"
	messagesdef "thingfabric.dev/go-fabric/cluster/spec/messages"
	"thingfabric.dev/go-fabric/cluster/spec/spectest"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/schema"
	"thingfabric.dev/go-fabric/tlv"
)

// TestFabricSensitiveFieldsWithheldFromOtherFabrics walks every cluster
// server of a node — the root with ICD configured, a bridged endpoint for
// every device type the device kit builds with its defaults (the walk of
// TestNoAttributeReadableBelowItsReadPrivilege), the two servers no
// device type mounts that hold fabric-scoped lists, Messages and
// EcosystemInformation, and host servers for the lists no server of the
// node serves at this composition (scopeStubs) — on a node commissioned
// into fabric 1. For every fabric-scoped list attribute whose entry struct
// has a fabric-sensitive field, and every list whose own access is
// fabric-sensitive (spectest.FabricSensitiveLists of the generated
// definition), it gives the attribute an entry of fabric 1 where the
// server offers a way (populate), reads it through the node's dispatcher
// (endpoint.TopologyDispatcher, which scopes every fabric-scoped attribute
// of the model on a request) unfiltered as fabric 2 and as a session
// without a fabric, and holds the wire bytes — the bridge's writer with
// the read's scope applied (im.MaskFabricScoped) — to what matter.js
// serves at db021da0 (spectest.CheckWithheld): an entry of another fabric
// carries its other fields and FabricIndex, and none of its
// fabric-sensitive fields; a fabric-sensitive list carries the reader's
// entries alone. The attributes it cannot give an entry of fabric 1 are
// pinned in unpopulatedFabricSensitive, exactly: an unpopulated list is
// not a checked one.
//
// The model's ten lists with fabric-sensitive entry fields and its six
// fabric-sensitive lists (parity/schema.json) are all walked: AccessControl's
// Arl (no ARL feature) and the six through a host server each, which
// returns two fabrics' entries whole as a host's own server may.
func TestFabricSensitiveFieldsWithheldFromOtherFabrics(t *testing.T) {
	t.Parallel()
	site := quietSite(t)
	messages, err := core.NewMessages(core.MessagesConfig{Presenter: fsPresenter{}})
	if err != nil {
		t.Fatal(err)
	}
	added, refused := 0, 0
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Configure: func(c *node.Config) {
			// The ICD configuration of the read-privilege walk: CIP, so
			// RegisteredClients is served.
			c.ICD = &node.ICDConfig{
				IdleModeDuration: 4 * time.Second, MaximumCheckInBackoff: 4 * time.Second,
				ActiveModeDuration: 2 * time.Second, ActiveModeThreshold: time.Second,
				ClientsSupportedPerFabric: 2,
			}
		},
		Prepare: func(n *node.Node) {
			ctx := context.Background()
			for _, ty := range device.Types() {
				dev, err := device.New(ty, device.Options{StableKey: endpoint.StringKey("fs:" + ty.Name), Name: ty.Name, Logger: slog.New(slog.DiscardHandler)})
				if err != nil {
					refused++ // a type whose defaults need the host's declarations
					continue
				}
				if err := n.Aggregator().Add(ctx, dev); err != nil {
					refused++ // a type the Aggregator does not bridge
					continue
				}
				added++
			}
			for _, ep := range []node.Endpoint{
				// Basic Video Player, the device type a host serves Messages on.
				fabrictest.Endpoint("fs:messages", 0x0028, messages),
				// Air Quality Sensor, the bridged device the reference
				// bridge serves its ecosystem directory on.
				fabrictest.Endpoint("fs:ecosystem", 0x002C, node.NewEcosystemInformation(fsEcosystem{})),
			} {
				if err := n.Aggregator().Add(ctx, ep); err != nil {
					t.Errorf("aggregator: %v", err)
				}
			}
		},
	}})
	if added < 40 {
		t.Fatalf("only %d device types were bridged (%d refused)", added, refused)
	}
	topo := pair.Device.Node().Bridge().Topology()
	dispatcher := endpoint.NewTopologyDispatcher(topo)

	// First every entry of fabric 1, then every read: one server's entry
	// can be another's (a Groupcast membership grants an AuxiliaryAcl
	// entry).
	type target struct {
		d   im.Dispatcher
		ep  uint16
		srv contract.ClusterServer
		l   spectest.FabricSensitiveList
	}
	var targets []target
	// The host servers stand on a topology of their own: three of their
	// clusters are root singletons the node's composition places on its
	// root alone.
	stubs := scopeStubTopology()
	stubDispatcher := endpoint.NewTopologyDispatcher(stubs)
	for _, ep := range stubs.Endpoints {
		for _, srv := range endpoint.ClusterServers(ep) {
			for _, l := range spectest.FabricSensitiveLists(spec.Lookup(srv.MatterClusterID())) {
				if answersRead(srv, l.Attribute.ID) {
					targets = append(targets, target{stubDispatcher, ep.ID, srv, l})
				}
			}
		}
	}
	for _, ep := range topo.Endpoints {
		for _, srv := range endpoint.ClusterServers(ep) {
			def := spec.Lookup(srv.MatterClusterID())
			if def == nil {
				continue // no generated definition: no field marks to read
			}
			for _, l := range spectest.FabricSensitiveLists(def) {
				if !answersRead(srv, l.Attribute.ID) {
					continue // not served at this composition
				}
				targets = append(targets, target{dispatcher, ep.ID, srv, l})
				if fill := populate[fmt.Sprintf("0x%04X/0x%04X", def.ID, l.Attribute.ID)]; fill != nil {
					if err := fill(ep.ID, srv); err != nil {
						t.Errorf("endpoint %d %s: an entry of fabric 1: %v", ep.ID, l, err)
					}
				}
			}
		}
	}
	checked := map[string]bool{}
	unpopulated := map[string]bool{}
	for _, tg := range targets {
		key := tg.l.String()
		own := readAs(t, tg.d, tg.ep, tg.srv, tg.l.Attribute.ID, true, 1)
		n := 0
		for _, reader := range []uint8{2, 0} {
			remote := readAs(t, tg.d, tg.ep, tg.srv, tg.l.Attribute.ID, false, reader)
			n = spectest.CheckWithheld(t, tg.l, own, remote, 1, reader)
		}
		if n == 0 {
			unpopulated[key] = true
			continue
		}
		checked[key] = true
	}
	for key := range unpopulated {
		if checked[key] {
			delete(unpopulated, key) // populated on another endpoint
		}
	}
	for key := range unpopulated {
		if !unpopulatedFabricSensitive[key] {
			t.Errorf("%s: the walk could not give it an entry of fabric 1 — add a populate entry", key)
		}
	}
	for key := range unpopulatedFabricSensitive {
		if !unpopulated[key] {
			t.Errorf("%s is pinned in unpopulatedFabricSensitive and is checked or no longer served: remove the entry", key)
		}
	}
	// Every list the walk selects over the model's linked definitions is
	// checked (script/fabricscopecheck holds the selection to the
	// snapshot's ten lists with fabric-sensitive entry fields and six
	// fabric-sensitive lists).
	for id := range schema.ClusterNames {
		def := spec.Lookup(id)
		if def == nil {
			continue
		}
		for _, l := range spectest.FabricSensitiveLists(def) {
			if !checked[l.String()] && !unpopulatedFabricSensitive[l.String()] {
				t.Errorf("%s: not walked — serve it on the node or through scopeStubTopology", l)
			}
		}
	}
	keys := func(m map[string]bool) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		slices.Sort(out)
		return out
	}
	t.Logf("checked: %q", keys(checked))
	t.Logf("could not populate: %q", keys(unpopulated))
	t.Logf("%d device types bridged, %d refused", added, refused)
}

// unpopulatedFabricSensitive are the fabric-sensitive lists the walk
// serves and cannot give an entry of fabric 1, exactly.
var unpopulatedFabricSensitive = map[string]bool{}

// fsAdmin is the CASE subject of fabric 1's commands.
const fsAdmin uint64 = 0x0000_0000_0001_B669

// commandAs is a command's context on fabric's session, as the bridge
// stamps an invoke (bridge/receive_dispatch.go: the fabric filter of an
// invoke is unfiltered), with every privilege granted.
func commandAs(fabric uint8, ep uint16, clusterID, cmdID uint32) context.Context {
	ctx := im.WithSubject(im.WithFabricFilter(context.Background(), false, fabric), fsAdmin, nil)
	ctx = im.WithCommandPath(ctx, im.ConcreteCommandPath{Endpoint: ep, Cluster: clusterID, Command: cmdID})
	return im.WithAuthority(ctx, func(uint16, uint32, uint8) im.StatusCode { return im.StatusSuccess })
}

// invokeGenerated sends req through the generated decoder, as the bridge
// does, as fabric 1.
func invokeGenerated(ep uint16, srv contract.ClusterServer, def *spec.Cluster, cmdID uint32, req spec.Encodable) error {
	enc := tlv.NewEncoder()
	req.EncodeTLV(enc, tlv.AnonymousTag())
	b, err := enc.Bytes()
	if err != nil {
		return err
	}
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		return err
	}
	fields, err := def.Command(cmdID, spec.Request).Decode(n)
	if err != nil {
		return err
	}
	_, err = srv.MatterInvoke(commandAs(1, ep, def.ID, cmdID), cmdID, fields)
	return err
}

// populate gives a fabric-sensitive list an entry of fabric 1, keyed
// "0xCCCC/0xAAAA". AccessControl's ACL holds fabric 1's administrator
// entry from the commissioning, ScenesManagement's FabricSceneInfo an
// entry for every fabric of the node, and EcosystemInformation's
// directories are fsEcosystem's: none of them needs a step.
var populate = map[string]func(ep uint16, srv contract.ClusterServer) error{
	// AccessControl Extension: fabric 1 writes one.
	"0x001F/0x0001": func(_ uint16, srv contract.ClusterServer) error {
		return srv.MatterWrite(im.WithFabricFilter(context.Background(), false, 1), 0x0001,
			[]core.AccessControlExtensionEntry{{Data: []byte{0x17, 0x18}}})
	},
	// Groupcast Membership, and with it AccessControl's AuxiliaryAcl:
	// fabric 1 joins a group with an auxiliary ACL.
	"0x0065/0x0000": joinGroupcast,
	// ICDManagement RegisteredClients: fabric 1 registers a client.
	"0x0046/0x0003": func(ep uint16, srv contract.ClusterServer) error {
		return invokeGenerated(ep, srv, icddef.Definition, icddef.CmdRegisterClient, icddef.RegisterClientRequest{
			CheckInNodeId: 0x1122, MonitoredSubject: 0x3344, Key: make([]byte, 16), ClientType: icddef.ClientTypePermanent,
		})
	},
	// Messages: fabric 1 presents one.
	"0x0097/0x0000": func(ep uint16, srv contract.ClusterServer) error {
		return invokeGenerated(ep, srv, messagesdef.Definition, messagesdef.CmdPresentMessagesRequest, messagesdef.PresentMessagesRequestRequest{
			MessageId: make([]byte, 16), Priority: messagesdef.MessagePriorityMedium,
			StartTime: spec.Nullable[uint32]{Null: true}, Duration: spec.Nullable[uint64]{Null: true}, MessageText: "of fabric 1",
		})
	},
}

// joinGroupcast joins fabric 1 to group 0x0101 on endpoint 1 with a key
// set of its own and an auxiliary ACL, once.
func joinGroupcast(ep uint16, srv contract.ClusterServer) error {
	use := true
	_, err := srv.MatterInvoke(commandAs(1, ep, core.GroupcastClusterID, 0x00), 0x00, core.JoinGroupRequest{
		GroupID: 0x0101, Endpoints: []uint16{1}, KeySetID: 0x0101, Key: make([]byte, 16), UseAuxiliaryACL: &use,
	})
	return err
}

// readAs is srv's attr at endpoint ep as a read of a session of fabric
// (0: a session without a fabric) receives it, stamped as the bridge
// stamps a read (bridge/receive_dispatch.go, im.WithFabricFilter): the
// dispatcher's result written by the bridge's writer
// (bridge.AttributeValueTLV) with the result's scope applied
// (im.MaskFabricScoped), as an attribute report's encoder applies it.
func readAs(t *testing.T, d im.Dispatcher, ep uint16, srv contract.ClusterServer, attr uint32, filtered bool, fabric uint8) []byte {
	t.Helper()
	ctx := im.WithFabricFilter(context.Background(), filtered, fabric)
	path := im.ConcreteAttributePath{Endpoint: ep, Cluster: srv.MatterClusterID(), Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	results := d.Read(ctx, path)
	if len(results) != 1 || results[0].Status != im.StatusSuccess {
		t.Fatalf("endpoint %d 0x%04X/0x%04X: read %+v", ep, path.Cluster, attr, results)
	}
	b, err := bridge.AttributeValueTLV(results[0].Value.Value)
	if err != nil {
		t.Fatalf("endpoint %d 0x%04X/0x%04X: encode %T: %v", ep, path.Cluster, attr, results[0].Value.Value, err)
	}
	if results[0].FabricScope == nil {
		t.Fatalf("endpoint %d 0x%04X/0x%04X: a request's read carries no scope", ep, path.Cluster, attr)
	}
	masked, ok := im.MaskFabricScoped(b, *results[0].FabricScope)
	if !ok {
		t.Fatalf("endpoint %d 0x%04X/0x%04X: the value cannot be scoped: % X", ep, path.Cluster, attr, b)
	}
	return masked
}

// scopeStub is a host's own server of a cluster's fabric-scoped lists:
// each returns an entry of fabric 1 and one of fabric 2, whole, and the
// server is no contract.FabricScopedReader.
type scopeStub struct {
	cluster uint32
	attrs   []uint32
}

func (s scopeStub) MatterClusterID() uint32 { return s.cluster }
func (s scopeStub) MatterRead(attr uint32) (any, bool) {
	switch {
	case slices.Contains(s.attrs, attr):
		return []scopeStubEntry{1, 2}, true
	case attr == 0xFFFC:
		return uint32(0), true
	case attr == 0xFFFD:
		rev, _ := schema.ClusterRevision(s.cluster)
		return rev, true
	}
	return nil, false
}
func (scopeStub) MatterWrite(context.Context, uint32, any) error         { return nil }
func (scopeStub) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (scopeStub) MatterReportable() []uint32                             { return nil }

// scopeStubEntry is an entry of its fabric: fields 0 to 2 and FabricIndex.
type scopeStubEntry uint8

func (e scopeStubEntry) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	for f := range uint8(3) {
		enc.PutUint(tlv.ContextTag(f), uint64(e)*10+uint64(f))
	}
	enc.PutUint(tlv.ContextTag(0xFE), uint64(e))
	_ = enc.EndContainer()
}

// scopeStubSource serves one stub on an endpoint.
type scopeStubSource struct{ srv contract.ClusterServer }

func (scopeStubSource) MatterDeviceType() uint16 { return 0x0016 }
func (s scopeStubSource) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{s.srv}
}

// scopeStubTopology serves, one per endpoint, AccessControl's Arl (no
// server of the node holds the ARL feature) and the six lists whose own
// access is fabric-sensitive.
func scopeStubTopology() *endpoint.Topology {
	stubs := []scopeStub{
		{0x001F, []uint32{0x0006}},         // AccessControl.Arl
		{0x0553, []uint32{0x0000}},         // WebRtcTransportProvider.CurrentSessions
		{0x0554, []uint32{0x0000}},         // WebRtcTransportRequestor.CurrentSessions
		{0x0555, []uint32{0x0001}},         // PushAvStreamTransport.CurrentConnections
		{0x0801, []uint32{0x0001, 0x0003}}, // TlsCertificateManagement.ProvisionedRoot/ClientCertificates
		{0x0802, []uint32{0x0001}},         // TlsClientManagement.ProvisionedEndpoints
	}
	topo := &endpoint.Topology{}
	for i, s := range stubs {
		topo.Endpoints = append(topo.Endpoints, &endpoint.Endpoint{
			ID: uint16(i + 1), DeviceType: 0x0016, SourceKey: endpoint.StringKey(fmt.Sprintf("stub:%d", i)),
			Scope: "s", DeviceAddress: "d", Source: scopeStubSource{s},
		})
	}
	return topo
}

// fsPresenter accepts every message.
type fsPresenter struct{}

func (fsPresenter) PresentMessage(context.Context, messagesdef.MessageStruct) error { return nil }
func (fsPresenter) CancelMessage(context.Context, []byte) error                     { return nil }

// fsEcosystem is a directory of fabric 1: one device in one location.
type fsEcosystem struct{}

func (fsEcosystem) EcosystemDevices(context.Context) []ecodef.EcosystemDeviceStruct {
	name, original := "lamp", uint16(1)
	return []ecodef.EcosystemDeviceStruct{{
		DeviceName: &name, OriginalEndpoint: &original,
		DeviceTypes:       []ecodef.DeviceTypeStruct{{DeviceType: 0x0100, Revision: 3}},
		UniqueLocationIds: []string{"room"}, FabricIndex: 1,
	}}
}

func (fsEcosystem) EcosystemLocations(context.Context) []ecodef.EcosystemLocationStruct {
	return []ecodef.EcosystemLocationStruct{{
		UniqueLocationId: "room",
		LocationDescriptor: ecodef.Locationdesc{
			LocationName: "Room", FloorNumber: spec.Nullable[int16]{Value: 0}, AreaType: spec.Nullable[uint8]{Null: true},
		},
		FabricIndex: 1,
	}}
}
