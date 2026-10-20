// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/ClientNodeTest.ts at 862ad75f:
// the controller's client node end to end on fabrictest — discovery,
// commissioning through the registry (its options, its refusals, the
// image after a controller restart), commands and reports, decommission,
// writes that reach the device, events, endpoints that come and go, the
// registry's culling of uncommissioned nodes and expired addresses. The
// cases that feed the image a report directly (the dropped optional
// attribute, Schrödinger's cluster, the changes-omitted version filters)
// sit in client_node_parity_matterjs_internal_test.go, beside the
// structure they drive.
//
// matter.js commissions with controller.peers.commission({ passcode,
// discriminator }); the port with fabrictest.Controller.CommissionNode
// (discover by the long discriminator, client.Registry.ForDevice,
// client.Registry.Commission). matter.js asserts on its ClientNode state
// objects; the port on the client.Node image (endpoints, clusters, the
// spec.Node values), the client.Record and the device's own answers.
//
// matter.js writes through the client node's state (agent.state,
// setStateOf, endpoint.set) and its RemoteWriter applies an accepted value
// to the cache; the Go node has one write surface, client.Node.Write,
// which goes to the peer and leaves the image to the next report
// (controller/client/doc.go "Not here"). A case whose subject is the
// device's answer to a client write is ported on Node.Write — the first
// variant of a describe group; the variants of the same write through
// another matter.js surface, and every case about the local cache,
// transactions, observers or the local validator, are missing with their
// reason in the register.
package client_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	gendiag "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	"github.com/SukramJ/go-fabric/cluster/spec/identify"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"
)

// deviceValue reads path from the device through the node, every value
// sent (includeKnownVersions) — what matter.js reads off the device's
// own state.
func deviceValue(t *testing.T, cp *clientPair, path im.ConcreteAttributePath) spec.Node {
	t.Helper()
	var v spec.Node
	cp.run(t, func(ctx context.Context) error {
		r, err := cp.node.Read(ctx, client.ReadRequest{ReadRequest: interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{path}}, IncludeKnownVersions: true})
		if err != nil {
			return err
		}
		var ok bool
		if v, ok = r.Value(path.Endpoint, path.Cluster, path.Attribute); !ok {
			return fmt.Errorf("no value for %v", path)
		}
		return nil
	})
	return v
}

// imageValue is the value of path in the node's image.
func imageValue(n *client.Node, path im.ConcreteAttributePath) (spec.Node, bool) {
	ep := n.Endpoint(path.Endpoint)
	if ep == nil {
		return spec.Node{}, false
	}
	c := ep.Cluster(path.Cluster)
	if c == nil {
		return spec.Node{}, false
	}
	return c.Value(path.Attribute)
}

// write writes one attribute through the node and returns the per-path
// error (WriteStatuses.Err) or the interaction's.
func write(t *testing.T, cp *clientPair, writes ...interaction.WriteRequest) error {
	t.Helper()
	var werr error
	err := cp.site.Run(parityCtx(t), func(ctx context.Context) error {
		st, err := cp.node.Write(ctx, writes, interaction.WriteOptions{})
		if err != nil {
			return err
		}
		werr = st.Err()
		return nil
	})
	if err != nil {
		return err
	}
	return werr
}

// statusOf is the IM status of a write's error, 0 when it carries none.
func statusOf(err error) im.StatusCode {
	var se *interaction.StatusError
	if errors.As(err, &se) {
		return se.Status
	}
	return 0
}

// nullVal and uint8Val are write values.
func nullVal() spec.Encodable {
	return interaction.Raw(spec.Node{El: tlv.Element{Type: tlv.TypeNull, IsNull: true}})
}

func uint8Val(v uint64) spec.Encodable {
	return interaction.Raw(spec.Node{El: tlv.Element{Type: tlv.TypeUnsignedInt1, Uint: v}})
}

func uint16Val(v uint64) spec.Encodable {
	return interaction.Raw(spec.Node{El: tlv.Element{Type: tlv.TypeUnsignedInt2, Uint: v}})
}

func strVal(v string) spec.Encodable {
	return interaction.Raw(spec.Node{El: tlv.Element{Type: tlv.TypeUTF8Str1, String: v}})
}

// snapshot is the image as values: endpoint, cluster, attribute.
func snapshot(n *client.Node) map[string]spec.Node {
	out := map[string]spec.Node{}
	for _, ep := range n.Endpoints() {
		for _, c := range ep.Clusters() {
			for _, a := range c.AttributeList() {
				if v, ok := c.Value(a); ok {
					out[fmt.Sprintf("%d/%#x/%#x", ep.Number(), c.ID(), a)] = v
				}
			}
		}
	}
	return out
}

// canonical prints a value with its integer and string widths folded, so
// a value stored and loaded again (re-encoded at its minimal width)
// compares equal to the reported one.
func canonical(n spec.Node) string {
	var b []byte
	var walk func(n spec.Node)
	walk = func(n spec.Node) {
		el := n.El
		switch {
		case el.Type <= tlv.TypeSignedInt8:
			el.Type = tlv.TypeSignedInt8
		case el.Type <= tlv.TypeUnsignedInt8:
			el.Type = tlv.TypeUnsignedInt8
		case el.Type >= tlv.TypeUTF8Str1 && el.Type <= tlv.TypeUTF8Str8:
			el.Type = tlv.TypeUTF8Str1
		case el.Type >= tlv.TypeOctetStr1 && el.Type <= tlv.TypeOctetStr8:
			el.Type = tlv.TypeOctetStr1
		}
		b = fmt.Appendf(b, "%v(", el)
		for _, c := range n.Children {
			walk(c)
		}
		b = append(b, ')')
	}
	walk(n)
	return string(b)
}

// lightPath is a path on the client pair's light.
func (cp *clientPair) lightPath(cluster, attr uint32) im.ConcreteAttributePath {
	return attributePath(cp.lightEP, cluster, attr)
}

func TestClientNodeParity(t *testing.T) {
	t.Parallel()

	t.Run("times out continuous discovery", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		c := site.AddController(fabrictest.ControllerOptions{})
		found, err := c.Discover(parityCtx(t), discovery.ByLongDiscriminator(1234), 90*time.Second)
		if err != nil || len(found) != 0 {
			t.Fatalf("discovered %+v, %v; want none", found, err)
		}
	})

	t.Run("discovers", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		p := site.UncommissionedPair(fabrictest.PairOptions{})
		found, err := p.Controller.Discover(parityCtx(t), discovery.ByLongDiscriminator(p.Device.Discriminator()), 90*time.Second)
		if err != nil || len(found) != 1 || found[0].D != p.Device.Discriminator() {
			t.Fatalf("discovered %+v, %v; want the device", found, err)
		}
	})

	t.Run("commissions and initializes endpoints after commissioning and restart", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		fabrics, err := cp.pair.Device.Node().FabricInfos(parityCtx(t))
		if err != nil || len(fabrics) != 1 {
			t.Fatalf("device fabrics %v, %v", fabrics, err)
		}
		reg := cp.pair.Controller.Registry()
		if reg.Size() != 1 {
			t.Fatalf("registry size %d, want 1", reg.Size())
		}
		// "Currently we only store 'last known good' address": one, the
		// device's.
		rec := cp.node.Record()
		if len(rec.Addresses) != 1 || rec.Addresses[0].UDPAddr().String() != cp.pair.Device.Addr().String() {
			t.Fatalf("addresses %v, want the device's %v", rec.Addresses, cp.pair.Device.Addr())
		}
		if cp.node.Endpoint(cp.lightEP) == nil || cp.node.Endpoint(cp.lightEP).Cluster(onoff.ClusterID) == nil {
			t.Fatal("the light endpoint is not in the image")
		}
		want, wantTree := snapshot(cp.node), tree(cp.node)
		id := cp.node.ID()
		if err := cp.pair.Controller.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		restored := cp.pair.Controller.Registry().Get(id)
		if restored == nil {
			t.Fatalf("node %q not restored", id)
		}
		if got := tree(restored); !reflect.DeepEqual(got, wantTree) {
			t.Fatalf("restored tree %v, want %v", got, wantTree)
		}
		got := snapshot(restored)
		for k, v := range want {
			if canonical(got[k]) != canonical(v) {
				t.Errorf("restored %s = %v, want %v", k, got[k], v)
			}
		}
		if len(got) != len(want) {
			t.Fatalf("restored image has %d values, want %d", len(got), len(want))
		}
	})

	t.Run("commissions and initializes endpoints even with a leave event in initial subscription data", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		p := site.UncommissionedPair(fabrictest.PairOptions{})
		if err := p.Device.Act(func(n *node.Node) error {
			n.Root().BasicInformation.EmitLeave(1)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		n, err := p.Controller.CommissionNode(parityCtx(t), p.Device, client.CommissionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if err := site.Eventually(parityCtx(t), "the default subscription", n.SubscriptionActive); err != nil {
			t.Fatal(err)
		}
		if !n.IsCommissioned() || p.Controller.Registry().Size() != 1 || p.Controller.Registry().Get(n.ID()) == nil {
			t.Fatalf("commissioned %v, registry size %d", n.IsCommissioned(), p.Controller.Registry().Size())
		}
		if n.Endpoint(0) == nil || n.Endpoint(0).Cluster(basicinfo.ClusterID) == nil {
			t.Fatal("the root is not in the image")
		}
	})

	t.Run("commissions with a short numeric passcode", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{configure: func(c *node.Config) { c.Commissioning.Passcode = 1 }})
		if cp.pair.Device.Passcode() != 1 {
			t.Fatalf("passcode %d, want 1", cp.pair.Device.Passcode())
		}
		if !cp.node.IsCommissioned() || cp.pair.Controller.Registry().Size() != 1 {
			t.Fatal("not commissioned")
		}
	})

	t.Run("initializes a newly-commissioned node with autoSubscribe:false without a manual reconnect", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{
			commission: client.CommissionOptions{AutoSubscribe: boolPtr(false)},
			configure:  func(c *node.Config) { c.BasicInformation.VendorName = "Matter.js Test Vendor" },
		})
		if err := cp.site.Settle(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if cp.node.SubscriptionActive() {
			t.Fatal("subscribed with AutoSubscribe false")
		}
		v, ok := imageValue(cp.node, attributePath(0, basicinfo.ClusterID, basicinfo.AttrVendorName))
		if !ok || v.El.String != "Matter.js Test Vendor" {
			t.Fatalf("vendor name %q %v", v.El.String, ok)
		}
		if cp.node.Endpoint(cp.lightEP) == nil {
			t.Fatal("the light is not in the image")
		}
	})

	t.Run("skips the post-commission read when autoStateInitialize is false", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{commission: client.CommissionOptions{AutoSubscribe: boolPtr(false), NoInitialRead: true}})
		if cp.pair.Controller.Registry().Get(cp.node.ID()) == nil {
			t.Fatal("the node is not in the registry")
		}
		// matter.js: no BasicInformation state and no parts; the Go image
		// keeps the root endpoint without clusters.
		for _, ep := range cp.node.Endpoints() {
			if ep.Number() != 0 || len(ep.Clusters()) != 0 {
				t.Fatalf("image %v, want nothing read", tree(cp.node))
			}
		}
	})

	t.Run("rejects an explicit node ID already held by a restored peer after a restart", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		addr, _ := cp.node.PeerAddress()
		if err := cp.pair.Controller.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		// The restored node binds its peer again: the node id is known.
		second := cp.site.AddDevice(fabrictest.Topology{Configure: func(c *node.Config) {
			c.Commissioning.Discriminator, c.Commissioning.Passcode = 999, 22223333
		}})
		o := client.CommissionOptions{}
		o.Passcode, o.NodeID = 11112222, addr.NodeID
		_, err := cp.pair.Controller.CommissionNode(parityCtx(t), second, o)
		if !errors.Is(err, commissioner.ErrNodeIDConflict) {
			t.Fatalf("commission with a used node id: %v, want ErrNodeIDConflict", err)
		}
		if fabrics, _ := second.Node().FabricInfos(parityCtx(t)); len(fabrics) != 0 {
			t.Fatal("the second device was commissioned")
		}
	})

	t.Run("rejects node-level commissioning without known addresses", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		p := site.UncommissionedPair(fabrictest.PairOptions{})
		found, err := p.Controller.Discover(parityCtx(t), discovery.ByLongDiscriminator(p.Device.Discriminator()), 90*time.Second)
		if err != nil || len(found) != 1 {
			t.Fatalf("discovered %+v, %v", found, err)
		}
		d := found[0]
		d.Addresses = nil
		reg := p.Controller.Registry()
		n, err := reg.ForDevice(parityCtx(t), d)
		if err != nil {
			t.Fatal(err)
		}
		o := client.CommissionOptions{}
		o.Passcode = p.Device.Passcode()
		if err := reg.Commission(parityCtx(t), n, o); !errors.Is(err, client.ErrNotLocated) {
			t.Fatalf("commission without addresses: %v, want ErrNotLocated", err)
		}
	})

	t.Run("rejects commissioning with an explicit node ID that is already in use before establishing PASE", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		addr, _ := cp.node.PeerAddress()
		second := cp.site.AddDevice(fabrictest.Topology{Configure: func(c *node.Config) {
			c.Commissioning.Discriminator, c.Commissioning.Passcode = 999, 22223333
		}})
		o := client.CommissionOptions{}
		o.Passcode, o.NodeID = 11112222, addr.NodeID
		_, err := cp.pair.Controller.CommissionNode(parityCtx(t), second, o)
		if !errors.Is(err, commissioner.ErrNodeIDConflict) {
			t.Fatalf("commission with a used node id and a wrong passcode: %v, want ErrNodeIDConflict before PASE", err)
		}
		if fabrics, _ := second.Node().FabricInfos(parityCtx(t)); len(fabrics) != 0 {
			t.Fatal("the second device was commissioned")
		}
	})

	t.Run("commissions via known-address flow even when first address has invalid credentials", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		c := site.AddController(fabrictest.ControllerOptions{})
		wrong := site.AddDevice(fabrictest.Topology{Configure: func(cfg *node.Config) {
			cfg.Commissioning.Discriminator, cfg.Commissioning.Passcode = 1234, 20202021
		}})
		matching := site.AddDevice(fabrictest.Topology{Configure: func(cfg *node.Config) {
			cfg.Commissioning.Discriminator, cfg.Commissioning.Passcode = 1234, 22223333
		}})
		_, err := c.Commission(parityCtx(t), matching, fabrictest.CommissionOptions{Edit: func(o *commissioner.Options) {
			o.Addresses = append(o.Addresses[:0], wrong.Addr(), matching.Addr())
		}})
		if err != nil {
			t.Fatal(err)
		}
		if f, _ := wrong.Node().FabricInfos(parityCtx(t)); len(f) != 0 {
			t.Fatal("the wrong-passcode device was commissioned")
		}
		if f, _ := matching.Node().FabricInfos(parityCtx(t)); len(f) != 1 {
			t.Fatal("the matching device was not commissioned")
		}
	})

	t.Run("invokes, receives state updates and emits changed events", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		cp.run(t, func(ctx context.Context) error {
			_, err := cp.node.Invoke(ctx, im.ConcreteCommandPath{Endpoint: cp.lightEP, Cluster: onoff.ClusterID, Command: onoff.CmdToggle}, nil, interaction.InvokeOptions{})
			return err
		})
		cp.eventually(t, "OnOff true in the image", func() bool {
			v, ok := imageValue(cp.node, cp.lightPath(onoff.ClusterID, onoff.AttrOnOff))
			return ok && v.El.Bool
		})
		cp.run(t, func(ctx context.Context) error {
			_, err := cp.node.Invoke(ctx, im.ConcreteCommandPath{Endpoint: cp.lightEP, Cluster: onoff.ClusterID, Command: onoff.CmdOffWithEffect},
				onoffdef.OffWithEffectRequest{}, interaction.InvokeOptions{})
			return err
		})
	})

	t.Run("receives state updates after the controller restarts", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{controller: fabrictest.ControllerOptions{
			Registry: func(rc *client.RegistryConfig) { rc.AutoStart = true },
		}})
		id := cp.node.ID()
		c := cp.pair.Controller
		if err := c.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		cp.run(t, c.Registry().Start)
		n := c.Registry().Get(id)
		if n == nil {
			t.Fatalf("node %q not restored", id)
		}
		cp.eventually(t, "the default subscription after the restart", n.SubscriptionActive)
		at := cp.site.Clock().Now()
		cp.light.OnOff().Set(true)
		cp.eventually(t, "OnOff true in the image", func() bool {
			v, ok := imageValue(n, cp.lightPath(onoff.ClusterID, onoff.AttrOnOff))
			return ok && v.El.Bool
		})
		if d := cp.site.Clock().Now().Sub(at); d >= 10*time.Second {
			t.Fatalf("the change arrived after %v, want under 10 s", d)
		}
	})

	t.Run("decommissions", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		reg := cp.pair.Controller.Registry()
		addr, _ := cp.node.PeerAddress()
		if reg.Size() != 1 {
			t.Fatalf("registry size %d", reg.Size())
		}
		cp.run(t, func(ctx context.Context) error { return reg.Decommission(ctx, cp.node) })
		if reg.Size() != 0 {
			t.Fatalf("registry size %d after the decommission", reg.Size())
		}
		if f, _ := cp.pair.Device.Node().FabricInfos(parityCtx(t)); len(f) != 0 {
			t.Fatal("the device is still commissioned")
		}
		if cp.pair.Controller.Peers().Get(addr) != nil {
			t.Fatal("the peer set still has the peer")
		}
		id := cp.node.ID()
		if err := cp.pair.Controller.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if r := cp.pair.Controller.Registry(); r.Size() != 0 || r.Get(id) != nil {
			t.Fatalf("restored registry size %d", r.Size())
		}
	})

	t.Run("closes the protocol peer on decommission", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		addr, _ := cp.node.PeerAddress()
		c := cp.pair.Controller
		if c.Peers().Get(addr) == nil {
			t.Fatal("no protocol peer")
		}
		if _, ok := c.Resumption().FindResumptionRecord(addr); !ok {
			t.Fatal("no resumption record")
		}
		cp.run(t, func(ctx context.Context) error { return c.Registry().Decommission(ctx, cp.node) })
		if c.Peers().Get(addr) != nil {
			t.Fatal("the protocol peer is still in the peer set")
		}
		if _, ok := c.Resumption().FindResumptionRecord(addr); ok {
			t.Fatal("the resumption record survived the decommission")
		}
	})

	t.Run("rejects delete after destroyed", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		cp.run(t, func(ctx context.Context) error { return cp.pair.Controller.Registry().Decommission(ctx, cp.node) })
		// The Go node is deleted through the registry (Decommission of a
		// node, commissioned or not); a deleted node is closed.
		if err := cp.pair.Controller.Registry().Decommission(parityCtx(t), cp.node); !errors.Is(err, client.ErrClosed) {
			t.Fatalf("delete after the decommission: %v, want ErrClosed", err)
		}
	})

	t.Run("writes attributes on commit", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		err := write(t, cp,
			interaction.WriteRequest{Path: cp.lightPath(onoff.ClusterID, onoff.AttrOnTime), Value: uint16Val(20)},
			interaction.WriteRequest{Path: cp.lightPath(identify.ClusterID, identify.AttrIdentifyTime), Value: uint16Val(5)})
		if err != nil {
			t.Fatal(err)
		}
		if v := deviceValue(t, cp, cp.lightPath(onoff.ClusterID, onoff.AttrOnTime)); v.El.Uint != 20 {
			t.Fatalf("device OnTime %d, want 20", v.El.Uint)
		}
		// IdentifyTime counts down from 5 once written; the device took it.
		if v := deviceValue(t, cp, cp.lightPath(identify.ClusterID, identify.AttrIdentifyTime)); v.El.Uint != 5 {
			t.Fatalf("device IdentifyTime %d, want 5", v.El.Uint)
		}
	})

	t.Run("writes nullable attribute: null, 0 and 2", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		path := cp.lightPath(onoff.ClusterID, onoff.AttrStartUpOnOff)
		for _, c := range []struct {
			val  spec.Encodable
			null bool
			want uint64
		}{{nullVal(), true, 0}, {uint8Val(0), false, 0}, {uint8Val(2), false, 2}} {
			if err := write(t, cp, interaction.WriteRequest{Path: path, Value: c.val}); err != nil {
				t.Fatal(err)
			}
			v := deviceValue(t, cp, path)
			if v.El.IsNull != c.null || (!c.null && v.El.Uint != c.want) {
				t.Fatalf("device StartUpOnOff %+v, want null %v value %d", v.El, c.null, c.want)
			}
		}
	})

	t.Run("emits Matter events", func(t *testing.T) {
		t.Parallel()
		var mu sync.Mutex
		var startUp []any
		cp := newClientPair(t, clientPairOptions{
			configure: func(c *node.Config) { c.BasicInformation.SoftwareVersion = 12 },
			controller: fabrictest.ControllerOptions{Registry: func(rc *client.RegistryConfig) {
				rc.Node.Hooks.Event = func(_ uint16, cluster, event uint32, _ interaction.EventData, v any) {
					if cluster == basicinfo.ClusterID && event == basicinfo.EventStartUp {
						mu.Lock()
						startUp = append(startUp, v)
						mu.Unlock()
					}
				}
			}},
		})
		mu.Lock()
		startUp = nil
		mu.Unlock()
		if err := cp.pair.Device.Act(func(n *node.Node) error {
			n.Root().BasicInformation.EmitStartUp()
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		cp.eventually(t, "the StartUp event", func() bool {
			mu.Lock()
			defer mu.Unlock()
			return len(startUp) > 0
		})
		mu.Lock()
		defer mu.Unlock()
		ev, ok := startUp[0].(basicinfo.StartUpEvent)
		if !ok || ev.SoftwareVersion != 12 {
			t.Fatalf("StartUp payload %#v, want SoftwareVersion 12", startUp[0])
		}
	})

	t.Run("handles endpoints that appear and disappear", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{rootOnly: true})
		agg := cp.node.Endpoint(1)
		if agg == nil || agg.DeviceType() != 0x000E || len(agg.Parts()) != 0 {
			t.Fatalf("aggregator %v", tree(cp.node))
		}
		light := newParityLight(t, cp.site, "late:1")
		if err := cp.pair.Device.Node().Aggregator().Add(parityCtx(t), light); err != nil {
			t.Fatal(err)
		}
		ep, ok := cp.pair.Device.EndpointOf("late:1")
		if !ok {
			t.Fatal("the light has no endpoint")
		}
		cp.eventually(t, "the light in the image", func() bool {
			e := cp.node.Endpoint(ep)
			return e != nil && e.DeviceType() == 0x0100
		})
		if err := cp.pair.Device.Node().Aggregator().Remove(parityCtx(t), endpoint.StringKey("late:1")); err != nil {
			t.Fatal(err)
		}
		cp.eventually(t, "the light erased", func() bool { return cp.node.Endpoint(ep) == nil })
		if parts := cp.node.Endpoint(1).Parts(); len(parts) != 0 {
			t.Fatalf("aggregator parts %d after the removal", len(parts))
		}
	})

	t.Run("invokes command on an undiscovered peer", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		light := newParityLight(t, site, "light:1")
		p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}}})
		reg := p.Controller.Registry()
		if reg.Size() != 0 {
			t.Fatalf("registry size %d", reg.Size())
		}
		n, err := reg.ForAddress(p.Address, "")
		if err != nil {
			t.Fatal(err)
		}
		ep, _ := p.Device.EndpointOf("light:1")
		err = site.Run(parityCtx(t), func(ctx context.Context) error {
			if _, err := n.Invoke(ctx, im.ConcreteCommandPath{Endpoint: ep, Cluster: onoff.ClusterID, Command: onoff.CmdToggle}, nil, interaction.InvokeOptions{}); err != nil {
				return err
			}
			_, err := n.Invoke(ctx, im.ConcreteCommandPath{Endpoint: ep, Cluster: onoff.ClusterID, Command: onoff.CmdOffWithEffect}, onoffdef.OffWithEffectRequest{}, interaction.InvokeOptions{})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("forAddress returns the same node for concurrent calls", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		p := site.CommissionedPair(fabrictest.PairOptions{})
		reg := p.Controller.Registry()
		var wg sync.WaitGroup
		nodes := make([]*client.Node, 2)
		for i := range nodes {
			wg.Add(1)
			go func() {
				defer wg.Done()
				nodes[i], _ = reg.ForAddress(p.Address, "")
			}()
		}
		wg.Wait()
		if nodes[0] == nil || nodes[0] != nodes[1] || reg.Size() != 1 {
			t.Fatalf("nodes %p %p, registry size %d", nodes[0], nodes[1], reg.Size())
		}
	})

	t.Run("reads a changes-omitted attribute from the peer while subscribed", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		path := attributePath(0, gendiag.ClusterID, gendiag.AttrUpTime)
		cached, ok := imageValue(cp.node, path)
		if !ok {
			t.Fatal("no UpTime in the image")
		}
		cp.advance(t, 100*time.Second)
		var got spec.Node
		cp.run(t, func(ctx context.Context) error {
			r, err := cp.node.Read(ctx, client.ReadRequest{ReadRequest: interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{clusterPath(0, gendiag.ClusterID)}}})
			if err != nil {
				return err
			}
			got, _ = r.Value(0, gendiag.ClusterID, gendiag.AttrUpTime)
			return nil
		})
		if got.El.Uint < cached.El.Uint+100 {
			t.Fatalf("UpTime %d after 100 s, cached %d", got.El.Uint, cached.El.Uint)
		}
	})
}

// otherDevice is a second device of the site, its discriminator not the
// first device's — matter.js addDevice({ index: 3 }) gives each device its
// own.
func otherDevice(site *fabrictest.Site) *fabrictest.Device {
	return site.AddDevice(fabrictest.Topology{Configure: func(c *node.Config) { c.Commissioning.Discriminator = 0x203 }})
}

// newParityLight is an OnOffLight on the site's clock.
func newParityLight(t *testing.T, site *fabrictest.Site, key string) node.Endpoint {
	t.Helper()
	l, err := newLight(site, key)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestClientNodeForwardsModelInvalidWriteParity(t *testing.T) {
	t.Parallel()
	t.Run("device rejects the value and the local cache keeps its value", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		path := cp.lightPath(onoff.ClusterID, onoff.AttrStartUpOnOff)
		before := deviceValue(t, cp, path)
		err := write(t, cp, interaction.WriteRequest{Path: path, Value: uint8Val(99)})
		if statusOf(err) != im.StatusConstraintError {
			t.Fatalf("write of StartUpOnOff 99: %v, want CONSTRAINT_ERROR from the device", err)
		}
		if after := deviceValue(t, cp, path); !reflect.DeepEqual(after.El, before.El) {
			t.Fatalf("device StartUpOnOff %+v, want %+v", after.El, before.El)
		}
		if v, _ := imageValue(cp.node, path); !reflect.DeepEqual(v.El, before.El) {
			t.Fatalf("image StartUpOnOff %+v, want %+v", v.El, before.El)
		}
	})
}

func TestClientNodeSendsWritesParity(t *testing.T) {
	t.Parallel()
	vendor := attributePath(0, basicinfo.ClusterID, basicinfo.AttrVendorName)

	t.Run("declines a write of a read-only fixed attribute and leaves the cache intact", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		original, _ := imageValue(cp.node, vendor)
		if err := write(t, cp, interaction.WriteRequest{Path: vendor, Value: strVal("Overwritten Vendor")}); statusOf(err) != im.StatusUnsupportedWrite {
			t.Fatalf("write of VendorName: %v, want UNSUPPORTED_WRITE", err)
		}
		if v, _ := imageValue(cp.node, vendor); v.El.String != original.El.String {
			t.Fatalf("image vendor %q, want %q", v.El.String, original.El.String)
		}
		if v := deviceValue(t, cp, vendor); v.El.String != original.El.String {
			t.Fatalf("device vendor %q, want %q", v.El.String, original.El.String)
		}
	})

	t.Run("declines a changes-omitted attribute, which no report would ever correct", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		path := attributePath(0, gendiag.ClusterID, gendiag.AttrUpTime)
		val := interaction.Raw(spec.Node{El: tlv.Element{Type: tlv.TypeUnsignedInt8, Uint: 999999}})
		if err := write(t, cp, interaction.WriteRequest{Path: path, Value: val}); statusOf(err) != im.StatusUnsupportedWrite {
			t.Fatalf("write of UpTime: %v, want UNSUPPORTED_WRITE", err)
		}
		if v, _ := imageValue(cp.node, path); v.El.Uint == 999999 {
			t.Fatal("the image took the refused UpTime")
		}
	})
}

func TestClientNodeSendsUnchangedWriteParity(t *testing.T) {
	t.Parallel()
	t.Run("fails an unchanged write of a read-only attribute with the device's status", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		vendor := attributePath(0, basicinfo.ClusterID, basicinfo.AttrVendorName)
		original, _ := imageValue(cp.node, vendor)
		if err := write(t, cp, interaction.WriteRequest{Path: vendor, Value: strVal(original.El.String)}); statusOf(err) != im.StatusUnsupportedWrite {
			t.Fatalf("unchanged write of VendorName: %v, want UNSUPPORTED_WRITE", err)
		}
		if v, _ := imageValue(cp.node, vendor); v.El.String != original.El.String {
			t.Fatalf("image vendor %q", v.El.String)
		}
	})
}

func TestClientNodeAcceptedWriteParity(t *testing.T) {
	t.Parallel()

	t.Run("with an active subscription", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		path := cp.lightPath(onoff.ClusterID, onoff.AttrStartUpOnOff)
		if err := write(t, cp, interaction.WriteRequest{Path: path, Value: uint8Val(2)}); err != nil {
			t.Fatal(err)
		}
		// The Go image takes the value from the report that follows.
		cp.eventually(t, "StartUpOnOff Toggle in the image", func() bool {
			v, ok := imageValue(cp.node, path)
			return ok && !v.El.IsNull && v.El.Uint == 2
		})
		if v := deviceValue(t, cp, path); v.El.IsNull || v.El.Uint != 2 {
			t.Fatalf("device StartUpOnOff %+v, want Toggle", v.El)
		}
	})

	t.Run("keeps the old local value when the peer never answers", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		path := cp.lightPath(onoff.ClusterID, onoff.AttrStartUpOnOff)
		before, _ := imageValue(cp.node, path)
		if err := cp.pair.Device.Stop(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if err := write(t, cp, interaction.WriteRequest{Path: path, Value: uint8Val(2)}); err == nil {
			t.Fatal("the write to a stopped device succeeded")
		}
		if v, _ := imageValue(cp.node, path); !reflect.DeepEqual(v.El, before.El) {
			t.Fatalf("image StartUpOnOff %+v, want %+v", v.El, before.El)
		}
	})

	t.Run("but not a declined one, with no subscription to repair the cache", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{commission: client.CommissionOptions{AutoSubscribe: boolPtr(false)}})
		vendor := attributePath(0, basicinfo.ClusterID, basicinfo.AttrVendorName)
		original, _ := imageValue(cp.node, vendor)
		if err := write(t, cp, interaction.WriteRequest{Path: vendor, Value: strVal("Overwritten Vendor")}); statusOf(err) != im.StatusUnsupportedWrite {
			t.Fatalf("write of VendorName: %v, want UNSUPPORTED_WRITE", err)
		}
		if v, _ := imageValue(cp.node, vendor); v.El.String != original.El.String {
			t.Fatalf("image vendor %q, want %q", v.El.String, original.El.String)
		}
	})
}

func TestClientNodeFabricIndexParity(t *testing.T) {
	t.Parallel()
	t.Run("captures fabricIndexOnPeer at commissioning", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		if fi := cp.node.Record().FabricIndexOnPeer; fi == 0 || fi == 255 {
			t.Fatalf("FabricIndexOnPeer %d, want a fabric index", fi)
		}
	})
}

func TestClientNodeOrphanedPeerCleanupParity(t *testing.T) {
	t.Parallel()

	// discovered is the registry's node of the device the controller
	// discovers — matter.js peers.discover, which creates the node.
	discovered := func(t *testing.T, c *fabrictest.Controller, d *fabrictest.Device) *client.Node {
		t.Helper()
		found, err := c.Discover(parityCtx(t), discovery.ByLongDiscriminator(d.Discriminator()), 90*time.Second)
		if err != nil || len(found) != 1 {
			t.Fatalf("discovered %+v, %v", found, err)
		}
		n, err := c.Registry().ForDevice(parityCtx(t), found[0])
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("uncommissioned node from incomplete commissioning persists across restart", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		p := site.UncommissionedPair(fabrictest.PairOptions{})
		n := discovered(t, p.Controller, p.Device)
		if p.Controller.Registry().Size() != 1 || n.IsCommissioned() || n.Record().DiscoveredAt.IsZero() {
			t.Fatalf("size %d, commissioned %v, discovered at %v", p.Controller.Registry().Size(), n.IsCommissioned(), n.Record().DiscoveredAt)
		}
		id := n.ID()
		if err := p.Controller.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		reg := p.Controller.Registry()
		r := reg.Get(id)
		if reg.Size() != 1 || r == nil || r.IsCommissioned() || r.Record().DiscoveredAt.IsZero() {
			t.Fatalf("restored size %d, node %v", reg.Size(), r)
		}
	})

	t.Run("culls uncommissioned nodes after TTL expires even when commissioned nodes are present", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		second := otherDevice(cp.site)
		n2 := discovered(t, cp.pair.Controller, second)
		reg := cp.pair.Controller.Registry()
		if n2.IsCommissioned() || reg.Size() != 2 {
			t.Fatalf("commissioned %v, size %d", n2.IsCommissioned(), reg.Size())
		}
		// The expiry timer runs every minute; the default TTL is 15 min.
		for i := 0; i < 20 && reg.Get(n2.ID()) != nil; i++ {
			cp.advance(t, client.ExpirationInterval)
		}
		if reg.Size() != 1 || reg.Get(cp.node.ID()) == nil || reg.Get(n2.ID()) != nil {
			t.Fatalf("size %d, commissioned kept %v, uncommissioned culled %v", reg.Size(), reg.Get(cp.node.ID()) != nil, reg.Get(n2.ID()) == nil)
		}
	})

	t.Run("does not cull an uncommissioned node while runCommissioning is in flight", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		second := otherDevice(cp.site)
		n2 := discovered(t, cp.pair.Controller, second)
		reg := cp.pair.Controller.Registry()
		client.UpdateRecord(n2, func(r *client.Record) { r.DiscoveredAt = cp.site.Clock().Now().Add(-20 * time.Minute) })
		release, err := client.HoldOperation(reg, n2, "commission")
		if err != nil {
			t.Fatal(err)
		}
		cp.advance(t, 2*time.Minute)
		if reg.Get(n2.ID()) == nil {
			t.Fatal("culled while a fabric operation held it")
		}
		release()
		for i := 0; i < 3 && reg.Get(n2.ID()) != nil; i++ {
			cp.advance(t, client.ExpirationInterval)
		}
		if reg.Get(n2.ID()) != nil {
			t.Fatal("not culled once released")
		}
	})

	t.Run("rejects parallel runCommissioning on the same node", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		second := otherDevice(cp.site)
		n2 := discovered(t, cp.pair.Controller, second)
		reg := cp.pair.Controller.Registry()
		release, err := client.HoldOperation(reg, n2, "commission")
		if err != nil {
			t.Fatal(err)
		}
		o := client.CommissionOptions{}
		o.Passcode = second.Passcode()
		if err := reg.Commission(parityCtx(t), n2, o); !errors.Is(err, client.ErrFabricOperationInProgress) {
			t.Fatalf("a second commission: %v, want ErrFabricOperationInProgress", err)
		}
		release()
		release2, err := client.HoldOperation(reg, n2, "commission")
		if err != nil {
			t.Fatalf("an operation after the first ended: %v", err)
		}
		release2()
	})

	t.Run("skips reuse of nodes whose construction is closed", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		second := otherDevice(cp.site)
		n2 := discovered(t, cp.pair.Controller, second)
		reg := cp.pair.Controller.Registry()
		d := discovery.Device{InstanceID: n2.Record().DeviceIdentifier}
		if found, err := reg.ForDevice(parityCtx(t), d); err != nil || found != n2 {
			t.Fatalf("ForDevice %p, %v; want the discovered node %p", found, err, n2)
		}
		cp.run(t, func(ctx context.Context) error { return reg.Decommission(ctx, n2) })
		if found, err := reg.ForDevice(parityCtx(t), d); err != nil || found == n2 {
			t.Fatalf("ForDevice after the delete returned the deleted node (%v)", err)
		}
	})

	t.Run("prunes expired addresses from commissioned nodes", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		original := cp.node.Record().Addresses
		if len(original) != 1 {
			t.Fatalf("addresses %v", original)
		}
		stale := client.NetworkAddress{Port: 5678, DiscoveredAt: cp.site.Clock().Now().Add(-20 * time.Minute)}
		stale.IP = mustAddr("192.168.99.99")
		client.UpdateRecord(cp.node, func(r *client.Record) { r.Addresses = append(r.Addresses, stale) })
		if n := len(cp.node.Record().Addresses); n != 2 {
			t.Fatalf("%d addresses after adding the stale one", n)
		}
		cp.advance(t, 2*time.Minute)
		got := cp.node.Record().Addresses
		if len(got) != 1 || got[0].IP != original[0].IP {
			t.Fatalf("addresses %v, want the original %v", got, original)
		}
	})
}

func TestClientNodePeerAddressResilienceParity(t *testing.T) {
	t.Parallel()
	t.Run("peerAddress survives ServerNode shutdown via cache", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		before, ok := cp.node.PeerAddress()
		if !ok {
			t.Fatal("no peer address")
		}
		cp.pair.Controller.Close()
		after, ok := cp.node.PeerAddress()
		if !ok || after != before {
			t.Fatalf("peer address %v %v after the close, want %v", after, ok, before)
		}
		_ = cp.node.ID()
	})
}
