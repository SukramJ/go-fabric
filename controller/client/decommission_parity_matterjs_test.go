// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/DecommissionTest.ts at
// 862ad75f: the controller decommissioning a peer — without a structure
// read or subscription, with the fabric index recorded or read, with
// RemoveFabric never acknowledged, one fabric operation at a time, a node
// deleted or deleted by a leave, and nothing left in the store.
//
// matter.js patches the peer's OperationalCredentials client so that
// removeFabric throws a chosen error or emits a leave first
// (patchRemoveFabric, :36-47); the port reaches the same outcomes on the
// wire where it can — RemoveFabric never acknowledged is every datagram to
// the device dropped — and leaves the patched cases missing. matter.js's
// node delete is the registry's Decommission in Go (a deleted node is
// closed: client.ErrClosed where matter.js throws DestroyedDependencyError
// "is closing or being deleted"); a held commission or decommission is
// client.HoldOperation, matter.js's peers.runCommissioning.
package client_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// opCredsFabricsPath is OperationalCredentials.Fabrics.
var opCredsFabricsPath = attributePath(0, 0x003E, 0x0001)

// decommission runs the registry's Decommission of the pair's node.
func (cp *clientPair) decommission(t *testing.T) error {
	t.Helper()
	var derr error
	if err := cp.site.Run(parityCtx(t), func(ctx context.Context) error {
		derr = cp.pair.Controller.Registry().Decommission(ctx, cp.node)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return derr
}

// deviceCommissioned is matter.js device.lifecycle.isCommissioned.
func (cp *clientPair) deviceCommissioned(t *testing.T) bool {
	t.Helper()
	f, err := cp.pair.Device.Node().FabricInfos(parityCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	return len(f) > 0
}

// unstructured is DecommissionTest.ts:56-79 unstructuredPeer: commissioned
// without the initial read and without a subscription.
func unstructured(t *testing.T, def *client.SubscribeOptions) *clientPair {
	t.Helper()
	return newClientPair(t, clientPairOptions{commission: client.CommissionOptions{
		AutoSubscribe: boolPtr(false), NoInitialRead: true, DefaultSubscription: def,
	}})
}

// expectRemoved asserts the node and the device's fabric are gone.
func (cp *clientPair) expectRemoved(t *testing.T) {
	t.Helper()
	if n := cp.pair.Controller.Registry().Size(); n != 0 {
		t.Fatalf("registry size %d, want 0", n)
	}
	if cp.deviceCommissioned(t) {
		t.Fatal("the device is still commissioned")
	}
}

func TestDecommissionUnstructuredParity(t *testing.T) {
	t.Parallel()

	t.Run("removes it although it holds no OperationalCredentials or BasicInformation behavior", func(t *testing.T) {
		t.Parallel()
		cp := unstructured(t, nil)
		if root := cp.node.Endpoint(0); root != nil && (root.Cluster(0x003E) != nil || root.Cluster(0x0028) != nil) {
			t.Fatal("the image holds OperationalCredentials or BasicInformation")
		}
		if err := cp.decommission(t); err != nil {
			t.Fatal(err)
		}
		cp.expectRemoved(t)
	})

	t.Run("removes it when its default subscription is not fabric filtered", func(t *testing.T) {
		t.Parallel()
		cp := unstructured(t, &client.SubscribeOptions{NoFabricFilter: true, Attributes: []im.ConcreteAttributePath{{}}})
		if err := cp.decommission(t); err != nil {
			t.Fatal(err)
		}
		cp.expectRemoved(t)
	})

	// readFabricsOnly is DecommissionTest.ts:81-94: only
	// OperationalCredentials.Fabrics read, so the image has the cluster but
	// no CurrentFabricIndex.
	readFabricsOnly := func(t *testing.T, cp *clientPair) {
		t.Helper()
		cp.run(t, func(ctx context.Context) error {
			_, err := cp.node.Read(ctx, client.ReadRequest{ReadRequest: interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{opCredsFabricsPath}}})
			return err
		})
		c := cp.node.Endpoint(0).Cluster(0x003E)
		if c == nil {
			t.Fatal("no OperationalCredentials after the read")
		}
		if _, ok := c.Value(0x0005); ok {
			t.Fatal("the image holds CurrentFabricIndex")
		}
	}

	t.Run("removes the fabric recorded at commissioning when the peer holds no current fabric index", func(t *testing.T) {
		t.Parallel()
		cp := unstructured(t, nil)
		readFabricsOnly(t, cp)
		if err := cp.decommission(t); err != nil {
			t.Fatal(err)
		}
		cp.expectRemoved(t)
	})

	t.Run("reads the current fabric index when none was recorded at commissioning", func(t *testing.T) {
		t.Parallel()
		cp := unstructured(t, nil)
		readFabricsOnly(t, cp)
		client.UpdateRecord(cp.node, func(r *client.Record) { r.FabricIndexOnPeer = 0 })
		if err := cp.decommission(t); err != nil {
			t.Fatal(err)
		}
		cp.expectRemoved(t)
	})
}

func TestDecommissionParity(t *testing.T) {
	t.Parallel()

	// unacknowledged drops every datagram to the device: RemoveFabric is
	// never acknowledged.
	unacknowledged := func(cp *clientPair) {
		dev := cp.pair.Device.Addr().String()
		cp.site.Net().Filter(func(d fabrictest.Datagram) bool { return d.To.String() != dev })
	}
	expectKept := func(t *testing.T, cp *clientPair, err error) {
		t.Helper()
		var pu *exchange.PeerUnresponsiveError
		if !errors.As(err, &pu) {
			t.Fatalf("decommission: %v, want PeerUnresponsiveError", err)
		}
		addr, _ := cp.node.PeerAddress()
		if cp.pair.Controller.Registry().Size() != 1 || cp.pair.Controller.Peers().Get(addr) == nil || !cp.node.IsCommissioned() {
			t.Fatal("the node or its peer was not kept")
		}
	}

	t.Run("keeps the node when removeFabric is never acknowledged", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		unacknowledged(cp)
		expectKept(t, cp, cp.decommission(t))
	})

	t.Run("accepts another decommission after a failed one", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		unacknowledged(cp)
		expectKept(t, cp, cp.decommission(t))
		expectKept(t, cp, cp.decommission(t))
	})

	t.Run("rejects a decommission while one is in progress", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		release, err := client.HoldOperation(cp.pair.Controller.Registry(), cp.node, "decommission")
		if err != nil {
			t.Fatal(err)
		}
		err = cp.decommission(t)
		if !errors.Is(err, client.ErrFabricOperationInProgress) || !strings.Contains(err.Error(), "a decommission attempt is already in progress") {
			t.Fatalf("second decommission: %v", err)
		}
		release()
		if err := cp.decommission(t); err != nil {
			t.Fatal(err)
		}
		cp.expectRemoved(t)
	})

	t.Run("rejects a decommission queued behind a leave that deletes the node", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		fi := cp.node.Record().FabricIndexOnPeer
		if err := cp.pair.Device.Act(func(n *node.Node) error {
			n.Root().BasicInformation.EmitLeave(fi)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		cp.eventually(t, "the node deleted by the leave", func() bool { return cp.pair.Controller.Registry().Size() == 0 })
		if err := cp.decommission(t); !errors.Is(err, client.ErrClosed) {
			t.Fatalf("decommission after the leave: %v, want ErrClosed", err)
		}
	})

	t.Run("rejects a decommission of a deleted node", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		if err := cp.decommission(t); err != nil {
			t.Fatal(err)
		}
		if err := cp.decommission(t); !errors.Is(err, client.ErrClosed) {
			t.Fatalf("decommission of the deleted node: %v, want ErrClosed", err)
		}
	})

	t.Run("rejects a commission while a decommission is in progress", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		reg := cp.pair.Controller.Registry()
		release, err := client.HoldOperation(reg, cp.node, "decommission")
		if err != nil {
			t.Fatal(err)
		}
		o := client.CommissionOptions{}
		o.Passcode = cp.pair.Device.Passcode()
		err = reg.Commission(parityCtx(t), cp.node, o)
		if !errors.Is(err, client.ErrFabricOperationInProgress) || !strings.Contains(err.Error(), "a decommission attempt is already in progress") {
			t.Fatalf("commission during a decommission: %v", err)
		}
		release()
		if err := cp.decommission(t); err != nil {
			t.Fatal(err)
		}
		cp.expectRemoved(t)
	})

	t.Run("waits for a running decommission before handing out a node (kept)", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		reg := cp.pair.Controller.Registry()
		id := cp.node.Record().DeviceIdentifier
		if id == "" {
			t.Fatal("no device identifier")
		}
		release, err := client.HoldOperation(reg, cp.node, "decommission")
		if err != nil {
			t.Fatal(err)
		}
		found := make(chan *client.Node, 1)
		go func() {
			n, _ := reg.ForDevice(parityCtx(t), discovery.Device{InstanceID: id})
			found <- n
		}()
		select {
		case <-found:
			t.Fatal("ForDevice handed out the node during the decommission")
		default:
		}
		release()
		if n := <-found; n != cp.node || reg.Size() != 1 {
			t.Fatalf("found %p, want the kept node %p; size %d", n, cp.node, reg.Size())
		}
	})

	t.Run("rejects a decommission while a commission is in progress", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		release, err := client.HoldOperation(cp.pair.Controller.Registry(), cp.node, "commission")
		if err != nil {
			t.Fatal(err)
		}
		err = cp.decommission(t)
		if !errors.Is(err, client.ErrFabricOperationInProgress) || !strings.Contains(err.Error(), "a commission attempt is already in progress") {
			t.Fatalf("decommission during a commission: %v", err)
		}
		release()
		if !cp.node.IsCommissioned() || cp.pair.Controller.Registry().Get(cp.node.ID()) == nil {
			t.Fatal("the node did not survive")
		}
	})

	t.Run("rejects a commission of a deleted node", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		if err := cp.decommission(t); err != nil {
			t.Fatal(err)
		}
		o := client.CommissionOptions{}
		o.Passcode = cp.pair.Device.Passcode()
		if err := cp.pair.Controller.Registry().Commission(parityCtx(t), cp.node, o); !errors.Is(err, client.ErrClosed) {
			t.Fatalf("commission of the deleted node: %v, want ErrClosed", err)
		}
	})

	t.Run("leaves no storage behind when the default subscription delivers the leave that deletes the node", func(t *testing.T) {
		t.Parallel()
		st := &client.MemoryStore{}
		errs := &errorLog{}
		log := slog.New(errs)
		cp := newClientPair(t, clientPairOptions{controller: fabrictest.ControllerOptions{Registry: func(rc *client.RegistryConfig) {
			rc.Store, rc.Logger, rc.Node.Logger = st, log, log
		}}})
		id := cp.node.ID()
		fi := cp.node.Record().FabricIndexOnPeer
		if err := cp.pair.Device.Act(func(n *node.Node) error {
			n.Root().BasicInformation.EmitLeave(fi)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		cp.eventually(t, "the node deleted by the leave", func() bool { return cp.pair.Controller.Registry().Size() == 0 })
		cp.advance(t, 5*time.Second)
		ids, err := st.NodeIDs()
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range ids {
			if n == id {
				t.Fatalf("the store keeps node %q", id)
			}
		}
		if e := errs.take(); len(e) != 0 {
			t.Fatalf("errors logged: %v", e)
		}
	})
}

// errorLog is a slog handler keeping the messages of ERROR records.
type errorLog struct {
	mu   sync.Mutex
	msgs []string
}

func (h *errorLog) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelError }
func (h *errorLog) WithAttrs([]slog.Attr) slog.Handler           { return h }
func (h *errorLog) WithGroup(string) slog.Handler                { return h }
func (h *errorLog) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.msgs = append(h.msgs, r.Message)
	h.mu.Unlock()
	return nil
}

func (h *errorLog) take() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.msgs
}
