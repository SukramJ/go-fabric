// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of the read, subscribe, write and invoke cases of matter.js
// packages/node/test/endpoint/server/InteractionProtocolTest.ts at 862ad75f
// that run end to end on a server node: matter.js calls
// InteractionServer's handlers on mocked exchanges of a MockServerNode, the
// port speaks over the wire from a commissioned fabrictest pair. The Go
// node's composition differs from the mock's where the rows say so:
// endpoint 1 is the Aggregator (the mock has none), so the mock's
// "unsupported endpoint" paths address an endpoint the node does not have,
// and data versions are seeded at random (the mock's are 0x80808081), so
// the version cases read the version first. The cases that need a mocked
// command server, a group or more than one message on one exchange are in
// bridge:TestInteractionProtocolBridgeParityMatterJS.
package node_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	acldef "github.com/SukramJ/go-fabric/cluster/spec/accesscontrol"
	admincomm "github.com/SukramJ/go-fabric/cluster/spec/administratorcommissioning"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

const (
	unsupportedCluster  uint32 = 0x99
	unsupportedEndpoint uint16 = 99
	descriptorCluster   uint32 = 0x1D
)

func TestInteractionProtocolParityMatterJS(t *testing.T) {
	t.Parallel()

	biAttr := func(attr uint32) im.ConcreteAttributePath { return attrPath(0, basicinfo.ClusterID, attr) }
	biEvent := func(ev uint32) im.ConcreteEventPath {
		return im.ConcreteEventPath{Endpoint: 0, Cluster: basicinfo.ClusterID, Event: ev, HasEndpoint: true, HasCluster: true, HasEvent: true}
	}
	startUp := func(t *testing.T, m *mockPair) {
		t.Helper()
		if err := m.pair.Device.Act(func(n *node.Node) error { n.Root().BasicInformation.EmitStartUp(); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	readWith := func(t *testing.T, m *mockPair, req interaction.ReadRequest) *interaction.Report {
		t.Helper()
		r, err := fabrictest.ReadWith(parityCtx(t), m.pair, req)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	statusOf := func(r *interaction.Report, p im.ConcreteAttributePath) (im.StatusCode, bool) {
		for _, s := range r.Statuses {
			if s.Path.Endpoint == p.Endpoint && s.Path.Cluster == p.Cluster && s.Path.Attribute == p.Attribute {
				return s.Status, true
			}
		}
		return 0, false
	}

	// InteractionProtocolTest.ts:1202-1212 with READ_REQUEST (:200-219):
	// the attribute paths answer data or their status, the wildcard path
	// of an unsupported cluster nothing; the event paths answer the
	// StartUp events and the statuses of the unsupported ones.
	t.Run("replies with attributes and events", func(t *testing.T) {
		t.Parallel()
		m := newMockPair(t, mockOptions{})
		startUp(t, m)
		wildEP := func(cluster, attr uint32) im.ConcreteAttributePath {
			return im.ConcreteAttributePath{Cluster: cluster, Attribute: attr, HasCluster: true, HasAttribute: true}
		}
		r := readWith(t, m, interaction.ReadRequest{
			Attributes: []im.ConcreteAttributePath{
				biAttr(basicinfo.AttrVendorId), biAttr(basicinfo.AttrProductId),
				biAttr(400), attrPath(0, unsupportedCluster, 4), attrPath(unsupportedEndpoint, basicinfo.ClusterID, 1),
				wildEP(basicinfo.ClusterID, basicinfo.AttrProductName), wildEP(unsupportedCluster, 3),
				attrPath(0, descriptorCluster, 1),
			},
			Events: []im.ConcreteEventPath{
				biEvent(basicinfo.EventStartUp), biEvent(254),
				{Endpoint: 0, Cluster: unsupportedCluster, Event: 4, HasEndpoint: true, HasCluster: true, HasEvent: true},
				{Endpoint: unsupportedEndpoint, Cluster: basicinfo.ClusterID, Event: 1, HasEndpoint: true, HasCluster: true, HasEvent: true},
			},
		})
		for p, want := range map[im.ConcreteAttributePath]im.StatusCode{
			biAttr(400):                       im.StatusUnsupportedAttribute,
			attrPath(0, unsupportedCluster, 4): im.StatusUnsupportedCluster,
			attrPath(unsupportedEndpoint, basicinfo.ClusterID, 1): im.StatusUnsupportedEndpoint,
		} {
			if got, ok := statusOf(r, p); !ok || got != want {
				t.Errorf("status of %+v %v (%v), want %v", p, got, ok, want)
			}
		}
		for _, p := range []im.ConcreteAttributePath{biAttr(basicinfo.AttrVendorId), biAttr(basicinfo.AttrProductId), biAttr(basicinfo.AttrProductName), attrPath(0, descriptorCluster, 1)} {
			if _, ok := r.Value(p.Endpoint, p.Cluster, p.Attribute); !ok {
				t.Errorf("no data for %+v", p)
			}
		}
		if len(r.Attributes) != 4 || len(r.Statuses) != 3 {
			t.Errorf("%d data, %d statuses, want 4 and 3", len(r.Attributes), len(r.Statuses))
		}
		var startUps int
		for _, ev := range r.Events {
			if ev.Path.Cluster == basicinfo.ClusterID && ev.Path.Event == basicinfo.EventStartUp {
				startUps++
			}
		}
		if startUps < 2 || len(r.EventStatuses) != 3 {
			t.Errorf("%d StartUp events, %d event statuses (%+v), want the boot's and the emitted one, and 3", startUps, len(r.EventStatuses), r.EventStatuses)
		}
	})

	// :1238-1248 and :1250-1260 with READ_REQUEST_WILDCARD_EVENTS
	// (:236-246): every event of endpoint 0 in ascending event numbers,
	// the emitted StartUp last; with an event filter at its number only it.
	t.Run("replies with events for wildcard read  returns correct order", func(t *testing.T) {
		t.Parallel()
		m := newMockPair(t, mockOptions{})
		startUp(t, m)
		r := readWith(t, m, interaction.ReadRequest{Events: []im.ConcreteEventPath{{Endpoint: 0, HasEndpoint: true}}})
		if len(r.Events) < 3 {
			t.Fatalf("%d events, want the boot's StartUp and BootReason and the emitted StartUp", len(r.Events))
		}
		for i := 1; i < len(r.Events); i++ {
			if r.Events[i].Number <= r.Events[i-1].Number {
				t.Fatalf("event %d number %d after %d, want ascending", i, r.Events[i].Number, r.Events[i-1].Number)
			}
		}
		if last := r.Events[len(r.Events)-1]; last.Path.Cluster != basicinfo.ClusterID || last.Path.Event != basicinfo.EventStartUp {
			t.Fatalf("last event %+v, want the emitted StartUp", last.Path)
		}
	})
	t.Run("replies with events for wildcard read active version filter", func(t *testing.T) {
		t.Parallel()
		m := newMockPair(t, mockOptions{})
		startUp(t, m)
		all := readWith(t, m, interaction.ReadRequest{Events: []im.ConcreteEventPath{{Endpoint: 0, HasEndpoint: true}}})
		last := all.Events[len(all.Events)-1]
		r := readWith(t, m, interaction.ReadRequest{
			Events:       []im.ConcreteEventPath{{Endpoint: 0, HasEndpoint: true}},
			EventFilters: []im.EventMinimumNumber{{EventMin: last.Number}},
		})
		if len(r.Events) != 1 || r.Events[0].Number != last.Number {
			t.Fatalf("events %+v, want the emitted StartUp alone", r.Events)
		}
	})

	// :1339-1366 with INVALID_SUBSCRIBE_REQUEST (:542-555): no path matches
	// anything, the subscription is refused with INVALID_ACTION.
	t.Run("errors when no path match the requested path's", func(t *testing.T) {
		t.Parallel()
		m := newMockPair(t, mockOptions{})
		_, err := fabrictest.Subscribe(parityCtx(t), m.pair, interaction.SubscribeRequest{
			Attributes: []im.ConcreteAttributePath{attrPath(0, unsupportedCluster, 2), {Endpoint: unsupportedEndpoint, HasEndpoint: true}},
			Events: []im.ConcreteEventPath{
				{Endpoint: 0, Cluster: unsupportedCluster, Event: 2, HasEndpoint: true, HasCluster: true, HasEvent: true},
				{Endpoint: unsupportedEndpoint, HasEndpoint: true},
			},
		})
		var se *interaction.StatusError
		if !errors.As(err, &se) || se.Status != im.StatusInvalidAction {
			t.Fatalf("subscribe: %v, want a StatusResponse of INVALID_ACTION", err)
		}
	})

	// :1429-1440 with WRITE_REQUEST and WRITE_RESPONSE (:557-627): one
	// write, a status per path — unsupported attribute, cluster, endpoint;
	// NodeLabel at the current version succeeds; Location at a stale
	// version is DATA_VERSION_MISMATCH; the read-only ProductName is
	// UNSUPPORTED_WRITE.
	t.Run("write values and return errors on invalid values", func(t *testing.T) {
		t.Parallel()
		m := newMockPair(t, mockOptions{})
		ctx := parityCtx(t)
		cur, err := fabrictest.Read(ctx, m.pair, biAttr(basicinfo.AttrNodeLabel))
		if err != nil {
			t.Fatal(err)
		}
		version, stale := cur.DataVersion, cur.DataVersion+15
		u8 := spec.Uint8(3)
		writes := []interaction.WriteRequest{
			{Path: biAttr(100), Value: u8},
			{Path: attrPath(0, unsupportedCluster, 4), Value: u8},
			{Path: attrPath(unsupportedEndpoint, basicinfo.ClusterID, 4), Value: u8, DataVersion: &version},
			{Path: biAttr(basicinfo.AttrNodeLabel), Value: spec.String("test"), DataVersion: &version},
			{Path: biAttr(basicinfo.AttrLocation), Value: spec.String("AB"), DataVersion: &stale},
			{Path: biAttr(basicinfo.AttrProductName), Value: spec.String("test"), DataVersion: &version},
		}
		statuses := writeBatch(t, m, writes, interaction.WriteOptions{})
		want := []im.StatusCode{
			im.StatusUnsupportedAttribute, im.StatusUnsupportedCluster, im.StatusUnsupportedEndpoint,
			im.StatusSuccess, im.StatusDataVersionMismatch, im.StatusUnsupportedWrite,
		}
		if len(statuses) != len(want) {
			t.Fatalf("statuses %+v, want %v", statuses, want)
		}
		for i := range want {
			if statuses[i].Status != want[i] {
				t.Errorf("path %+v: %v, want %v", statuses[i].Path, statuses[i].Status, want[i])
			}
		}
		label, err := fabrictest.Read(ctx, m.pair, biAttr(basicinfo.AttrNodeLabel))
		if err != nil || label.Value.El.String != "test" {
			t.Fatalf("NodeLabel %+v (%v), want test", label.Value.El, err)
		}
	})

	// :1442-1479 with CHUNKED_ARRAY_WRITE_REQUEST (:715-771): a list write
	// the client chunks — the REPLACE-ALL of an empty list, then one append
	// per entry — succeeds, and the entries carry the session's fabric
	// index whatever the request said. The controller's own Administer
	// entry is one of them, so it keeps its access.
	t.Run("write chunked array values with Fabric Index handling", func(t *testing.T) {
		t.Parallel()
		m := newMockPair(t, mockOptions{})
		ctx := parityCtx(t)
		fabrics, err := m.pair.Device.Node().FabricInfos(ctx)
		if err != nil || len(fabrics) != 1 {
			t.Fatalf("fabrics %+v (%v)", fabrics, err)
		}
		own := fabrics[0]
		acl := spec.List[acldef.AccessControlEntryStruct]{
			{Privilege: acldef.AccessControlEntryPrivilegeAdminister, AuthMode: acldef.AccessControlEntryAuthModeCase, Subjects: spec.ValueOf([]uint64{fabrictestControllerNodeID(t, m)}), Targets: spec.NullOf[[]acldef.AccessControlTargetStruct](), FabricIndex: 2},
			{Privilege: acldef.AccessControlEntryPrivilegeView, AuthMode: acldef.AccessControlEntryAuthModeGroup, Subjects: spec.NullOf[[]uint64](), Targets: spec.NullOf[[]acldef.AccessControlTargetStruct](), FabricIndex: 2},
		}
		statuses := writeBatch(t, m, []interaction.WriteRequest{{Path: attrPath(0, acldef.ClusterID, acldef.AttrAcl), Value: acl}}, interaction.WriteOptions{})
		for _, s := range statuses {
			if s.Status != im.StatusSuccess {
				t.Fatalf("chunked ACL write: %+v", statuses)
			}
		}
		entries, err := m.pair.Device.Node().Store().ListACL(ctx, own.FabricIndex)
		if err != nil || len(entries) != 2 {
			t.Fatalf("ACL %+v (%v), want two entries", entries, err)
		}
		for _, e := range entries {
			if e.FabricIndex != own.FabricIndex {
				t.Errorf("entry %+v carries fabric %d, want the session's %d", e, e.FabricIndex, own.FabricIndex)
			}
		}
	})

	// :2008-2026 and :2068-2085: a write inside a timed interaction
	// succeeds.
	for _, name := range []string{
		"write values and return errors on invalid values timed interaction required by attribute success",
		"write values and return errors on invalid values in timed interaction",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newMockPair(t, mockOptions{})
			ctx := parityCtx(t)
			if err := fabrictest.WriteTimed(ctx, m.pair, biAttr(basicinfo.AttrNodeLabel), spec.String("test")); err != nil {
				t.Fatal(err)
			}
			label, err := fabrictest.Read(ctx, m.pair, biAttr(basicinfo.AttrNodeLabel))
			if err != nil || label.Value.El.String != "test" {
				t.Fatalf("NodeLabel %+v (%v), want test", label.Value.El, err)
			}
		})
	}

	onOffOf := func(t *testing.T, m *mockPair) bool {
		t.Helper()
		v, err := fabrictest.Read(parityCtx(t), m.pair, attrPath(m.lightEP, onoff.ClusterID, onoff.AttrOnOff))
		if err != nil {
			t.Fatal(err)
		}
		return v.Value.El.Bool
	}

	// :2112-2124: On with empty arguments turns the light on.
	t.Run("invoke command with empty args", func(t *testing.T) {
		t.Parallel()
		m := newMockPair(t, mockOptions{light: true})
		if _, err := fabrictest.Invoke(parityCtx(t), m.pair, cmdPath(m.lightEP, onoff.ClusterID, onoff.CmdOn), nil); err != nil {
			t.Fatal(err)
		}
		if !onOffOf(t, m) {
			t.Fatal("OnOff false after On")
		}
	})

	// :2140-2152: a command the cluster does not have is
	// UNSUPPORTED_COMMAND and changes nothing.
	t.Run("invalid invoke command", func(t *testing.T) {
		t.Parallel()
		m := newMockPair(t, mockOptions{light: true})
		_, err := fabrictest.Invoke(parityCtx(t), m.pair, cmdPath(m.lightEP, onoff.ClusterID, 0x10), nil)
		var se *interaction.StatusError
		if !errors.As(err, &se) || se.Status != im.StatusUnsupportedCommand {
			t.Fatalf("invoke: %v, want UNSUPPORTED_COMMAND", err)
		}
		if onOffOf(t, m) {
			t.Fatal("OnOff true after an unsupported command")
		}
	})

	// :2193-2212: On and Off in one InvokeRequest, both run, Off last.
	t.Run("multi invoke commands are ok", func(t *testing.T) {
		t.Parallel()
		m := newMockPair(t, mockOptions{light: true})
		results := invokeBatch(t, m, []interaction.CommandRequest{
			{Path: cmdPath(m.lightEP, onoff.ClusterID, onoff.CmdOn)},
			{Path: cmdPath(m.lightEP, onoff.ClusterID, onoff.CmdOff)},
		})
		if len(results) != 2 || results[0].Status != im.StatusSuccess || results[1].Status != im.StatusSuccess {
			t.Fatalf("results %+v, want two successes", results)
		}
		if onOffOf(t, m) {
			t.Fatal("OnOff true after On, Off")
		}
	})

	// :2616-2632: a timed invoke succeeds.
	t.Run("invoke command with with timed interaction success", func(t *testing.T) {
		t.Parallel()
		m := newMockPair(t, mockOptions{light: true})
		if _, err := fabrictest.InvokeTimed(parityCtx(t), m.pair, cmdPath(m.lightEP, onoff.ClusterID, onoff.CmdOn), nil); err != nil {
			t.Fatal(err)
		}
		if !onOffOf(t, m) {
			t.Fatal("OnOff false after a timed On")
		}
	})

	// :2653-2670 with INVOKE_COMMAND_REQUEST_TIMED_REQUIRED (:829-845): a
	// command that requires a timed interaction succeeds inside one.
	// matter.js's root serves the Basic feature and opens a basic window;
	// the Go root does not (AdministratorCommissioning without BC), so the
	// port revokes a window the node opened — RevokeCommissioning requires
	// a timed interaction as well.
	t.Run("invoke command with timed interaction required by command success", func(t *testing.T) {
		t.Parallel()
		m := newMockPair(t, mockOptions{})
		ctx := parityCtx(t)
		if err := m.pair.Device.Act(func(n *node.Node) error { return n.OpenOwnWindow(ctx, 3*time.Minute) }); err != nil {
			t.Fatal(err)
		}
		if _, err := fabrictest.InvokeTimed(ctx, m.pair, cmdPath(0, admincomm.ClusterID, admincomm.CmdRevokeCommissioning), nil); err != nil {
			t.Fatal(err)
		}
	})
}

// writeBatch writes several attributes in one WriteRequest through the
// pair's client node.
func writeBatch(t *testing.T, m *mockPair, writes []interaction.WriteRequest, opts interaction.WriteOptions) interaction.WriteStatuses {
	t.Helper()
	n, err := m.pair.Node()
	if err != nil {
		t.Fatal(err)
	}
	var out interaction.WriteStatuses
	if err := m.site.Run(parityCtx(t), func(ctx context.Context) error {
		var err error
		out, err = n.Write(ctx, writes, opts)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// invokeBatch invokes several commands in one InvokeRequest.
func invokeBatch(t *testing.T, m *mockPair, cmds []interaction.CommandRequest) []interaction.Result {
	t.Helper()
	n, err := m.pair.Node()
	if err != nil {
		t.Fatal(err)
	}
	var out []interaction.Result
	if err := m.site.Run(parityCtx(t), func(ctx context.Context) error {
		var err error
		out, err = n.InvokeBatch(ctx, cmds, interaction.InvokeOptions{})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// fabrictestControllerNodeID is the pair's controller node id on the
// device's fabric.
func fabrictestControllerNodeID(t *testing.T, m *mockPair) uint64 {
	t.Helper()
	entries, err := m.pair.Device.Node().Store().ListACL(parityCtx(t), firstFabric(t, m))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if len(e.Subjects) > 0 {
			return e.Subjects[0]
		}
	}
	t.Fatal("no CASE subject in the commissioned ACL")
	return 0
}

func firstFabric(t *testing.T, m *mockPair) uint8 {
	t.Helper()
	fabrics, err := m.pair.Device.Node().FabricInfos(parityCtx(t))
	if err != nil || len(fabrics) == 0 {
		t.Fatalf("fabrics %+v (%v)", fabrics, err)
	}
	return fabrics[0].FabricIndex
}
