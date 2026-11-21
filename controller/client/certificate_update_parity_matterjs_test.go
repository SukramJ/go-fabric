// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The facade cases of matter.js packages/node/test/node/OperationalCertificateUpdateTest.ts
// at b25ed960: what CommissioningClient.updateOperationalCertificate
// (packages/node/src/behavior/system/commissioning/CommissioningClient.ts:519-570)
// does with the node's record — the refusal of an uncommissioned node and
// the fabric index read before the update, stored also when the update
// fails. The update's own cases are ported in
// controller/commissioner/operational_certificate_update_parity_matterjs_test.go;
// these need a record this package can set (client.UpdateRecord), as
// matter.js sets CommissioningClient's state.
package client_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// staleFabricIndex is STALE_FABRIC_INDEX (:104): an index the device never
// assigns, stored before an update to show whether the update replaces
// it.
const staleFabricIndex = 200

// updateCertificate runs the registry's update of the pair's node.
func (cp *clientPair) updateCertificate(t *testing.T) error {
	t.Helper()
	var uerr error
	cp.run(t, func(ctx context.Context) error {
		uerr = cp.pair.Controller.Registry().UpdateOperationalCertificate(ctx, cp.node, client.CertificateUpdateOptions{})
		return nil
	})
	return uerr
}

// deviceFabricIndex is the device's index of the fabric (deviceFabric).
func (cp *clientPair) deviceFabricIndex(t *testing.T) uint8 {
	t.Helper()
	fs, err := cp.pair.Device.Node().FabricInfos(parityCtx(t))
	if err != nil || len(fs) != 1 {
		t.Fatalf("device fabrics %+v (%v)", fs, err)
	}
	return fs[0].FabricIndex
}

func (cp *clientPair) storeFabricIndex(i uint8) {
	client.UpdateRecord(cp.node, func(r *client.Record) { r.FabricIndexOnPeer = i })
}

// busyBreadcrumb is device.setStateOf(GeneralCommissioningServer,
// {breadcrumb: 7}): the update refuses to arm.
func (cp *clientPair) busyBreadcrumb(t *testing.T) {
	t.Helper()
	if err := cp.pair.Device.Node().Root().GeneralCommissioning.MatterWrite(context.Background(), gencomm.AttrBreadcrumb, uint64(7)); err != nil {
		t.Fatal(err)
	}
}

// warnings records the messages of the warnings logged.
type warnings struct {
	mu   sync.Mutex
	msgs []string
}

func (w *warnings) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelWarn }

func (w *warnings) Handle(_ context.Context, r slog.Record) error {
	w.mu.Lock()
	w.msgs = append(w.msgs, r.Message)
	w.mu.Unlock()
	return nil
}
func (w *warnings) WithAttrs([]slog.Attr) slog.Handler { return w }
func (w *warnings) WithGroup(string) slog.Handler      { return w }

func (w *warnings) has(msg string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, m := range w.msgs {
		if m == msg {
			return true
		}
	}
	return false
}

// failingStore fails every save while failing is set — matter.js
// FabricIndexFailingStorage (:146-170), which fails the write of the
// fabric index.
type failingStore struct {
	client.Store
	failing atomic.Bool
}

func (s *failingStore) Save(id string, ep uint16, cluster uint32, values map[uint32][]byte, version *uint32) error {
	if s.failing.Load() {
		return errors.New("simulated storage failure")
	}
	return s.Store.Save(id, ep, cluster, values, version)
}

func TestOperationalCertificateUpdateFacade(t *testing.T) {
	t.Parallel()

	t.Run("throws the update error unchanged when storing the CurrentFabricIndex read before fails", func(t *testing.T) {
		t.Parallel()
		w := &warnings{}
		st := &failingStore{Store: &client.MemoryStore{}}
		cp := newClientPair(t, clientPairOptions{controller: fabrictest.ControllerOptions{
			Logger:   slog.New(w),
			Registry: func(c *client.RegistryConfig) { c.Store = st },
		}})
		cp.busyBreadcrumb(t)
		idx := cp.deviceFabricIndex(t)
		st.failing.Store(true)
		err := cp.updateCertificate(t)
		st.failing.Store(false)
		var ue *commissioner.CertificateUpdateError
		if !errors.As(err, &ue) || !errors.Is(err, commissioner.ErrCertificateUpdateBusy) || !strings.Contains(err.Error(), "Breadcrumb is 7, so another operation") {
			t.Fatalf("got %v", err)
		}
		if ue.FabricIndexOnPeer != idx {
			t.Fatalf("index %d, want %d", ue.FabricIndexOnPeer, idx)
		}
		if !w.has("matter.controller.client.store_write_failed") {
			t.Fatal("the failed store was not logged")
		}
	})

	t.Run("refuses an uncommissioned node", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		client.UpdateRecord(cp.node, func(r *client.Record) { r.PeerAddress = nil })
		err := cp.updateCertificate(t)
		if !errors.Is(err, client.ErrNotCommissioned) || !strings.Contains(err.Error(), "not commissioned") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("stores the device's fabric index when none is stored", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		cp.storeFabricIndex(0)
		if err := cp.updateCertificate(t); err != nil {
			t.Fatal(err)
		}
		if got, want := cp.node.Record().FabricIndexOnPeer, cp.deviceFabricIndex(t); got != want {
			t.Fatalf("stored %d, want %d", got, want)
		}
	})

	t.Run("replaces a wrong stored fabric index with the CurrentFabricIndex read before the update", func(t *testing.T) {
		t.Parallel()
		w := &warnings{}
		cp := newClientPair(t, clientPairOptions{controller: fabrictest.ControllerOptions{Logger: slog.New(w)}})
		cp.storeFabricIndex(staleFabricIndex)
		if err := cp.updateCertificate(t); err != nil {
			t.Fatal(err)
		}
		if got, want := cp.node.Record().FabricIndexOnPeer, cp.deviceFabricIndex(t); got != want {
			t.Fatalf("stored %d, want %d", got, want)
		}
		if !w.has("matter.controller.client.registry.fabric_index_replaced") {
			t.Fatal("no warning of the replaced index")
		}
	})
}

// TestCertificateUpdateStoresIndexOfFailedUpdate: the index read before an
// update that then fails replaces a stale stored one (matter.js
// expectFabricIndexReported after updateFailure, :128-136 with :104-121).
// A Go case, not a port: the facade half of the rejected updates.
func TestCertificateUpdateStoresIndexOfFailedUpdate(t *testing.T) {
	t.Parallel()
	cp := newClientPair(t, clientPairOptions{})
	cp.busyBreadcrumb(t)
	cp.storeFabricIndex(staleFabricIndex)
	err := cp.updateCertificate(t)
	var ue *commissioner.CertificateUpdateError
	if !errors.As(err, &ue) || ue.Outcome != commissioner.CertificateUpdatePreviousKept {
		t.Fatalf("got %v", err)
	}
	if got, want := cp.node.Record().FabricIndexOnPeer, cp.deviceFabricIndex(t); got != want || ue.FabricIndexOnPeer != want {
		t.Fatalf("stored %d, reported %d, want %d", got, ue.FabricIndexOnPeer, want)
	}
}
