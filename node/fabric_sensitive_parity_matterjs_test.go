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

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ecodef "github.com/SukramJ/go-fabric/cluster/spec/ecosysteminformation"
	icddef "github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	messagesdef "github.com/SukramJ/go-fabric/cluster/spec/messages"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestFabricSensitiveFieldsWithheldFromOtherFabrics walks every cluster
// server of a node — the root with ICD configured, a bridged endpoint for
// every device type the device kit builds with its defaults (the walk of
// TestNoAttributeReadableBelowItsReadPrivilege), and the two servers no
// device type mounts that hold fabric-scoped lists, Messages and
// EcosystemInformation — on a node commissioned into fabric 1. For every
// fabric-scoped list attribute whose entry struct has a fabric-sensitive
// field (spec.Field.FabricSensitive of the generated definition), it
// gives the attribute an entry of fabric 1 where the server offers a way
// (populate), reads it the way the dispatcher reads it
// (endpoint/dispatcher.go readOne) unfiltered as fabric 2 and as a session
// without a fabric, and holds the wire bytes the bridge writes
// (bridge.AttributeValueTLV) to what matter.js serves at db021da0
// (spectest.CheckWithheld): an entry of another fabric carries its other
// fields and FabricIndex, and none of its fabric-sensitive fields. The
// attributes it cannot give an entry of fabric 1 are pinned in
// unpopulatedFabricSensitive, exactly: an unpopulated list is not a
// checked one.
//
// Of the model's ten such attributes (parity/schema.json) the walk serves
// nine; AccessControl's Arl is not served (no ARL feature). The six lists
// whose attribute access is "S" (the WebRTC and PushAV sessions, the TLS
// certificates and endpoints) are outside the class: their entry structs
// carry no "S" field, and matter.js filters the whole list for a remote
// read instead (ListManager.ts:610-616).
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

	// First every entry of fabric 1, then every read: one server's entry
	// can be another's (a Groupcast membership grants an AuxiliaryAcl
	// entry).
	type target struct {
		ep  uint16
		srv contract.ClusterServer
		l   spectest.FabricSensitiveList
	}
	var targets []target
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
				targets = append(targets, target{ep.ID, srv, l})
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
		own := wireOf(t, key, readAs(tg.srv, tg.l.Attribute.ID, true, 1))
		n := 0
		for _, reader := range []uint8{2, 0} {
			remote := wireOf(t, key, readAs(tg.srv, tg.l.Attribute.ID, false, reader))
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

// readAs reads attr the way the dispatcher does (endpoint/dispatcher.go
// readOne) for a read of a session of fabric (0: a session without a
// fabric), stamped as the bridge stamps a read
// (bridge/receive_dispatch.go, im.WithFabricFilter).
func readAs(srv contract.ClusterServer, attr uint32, filtered bool, fabric uint8) any {
	ctx := im.WithFabricFilter(context.Background(), filtered, fabric)
	if fsr, ok := srv.(contract.FabricScopedReader); ok {
		if v, ok := fsr.MatterReadFiltered(ctx, attr); ok {
			return v
		}
	}
	v, _ := srv.MatterRead(attr)
	return v
}

// wireOf is v as the bridge writes it.
func wireOf(t *testing.T, what string, v any) []byte {
	t.Helper()
	b, err := bridge.AttributeValueTLV(v)
	if err != nil {
		t.Fatalf("%s: encode %T: %v", what, v, err)
	}
	return b
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
