// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"testing"
	"testing/synctest"
	"time"

	netcommdef "thingfabric.dev/go-fabric/cluster/spec/networkcommissioning"
	"thingfabric.dev/go-fabric/im"
)

// gatedBorderRouter holds the first SetActiveDataset of the dataset hold
// until release is closed, and says when it entered. It ignores its ctx,
// as a border router whose HTTP request is already on the wire.
type gatedBorderRouter struct {
	*simBorderRouter
	hold    []byte
	entered chan struct{}
	release chan struct{}
	used    bool
}

func (g *gatedBorderRouter) SetActiveDataset(ctx context.Context, ds []byte) error {
	g.mu.Lock()
	gate := !g.used && bytes.Equal(ds, g.hold)
	if gate {
		g.used = true
	}
	g.mu.Unlock()
	if gate {
		close(g.entered)
		<-g.release
	}
	return g.simBorderRouter.SetActiveDataset(ctx, ds)
}

// A ConnectNetwork whose attach outlives the fail-safe that admitted it:
// the expiry reverts the border router, then the held attach lands. The
// border router ends on the reverted dataset, no Breadcrumb is written
// after the expiry and the late result moves no attribute (review II,
// R2-P6-fsm-1001; the probe of phase 6).
func TestThreadConnectOutlivesFailSafe(t *testing.T) {
	t.Parallel()
	old := otherDataset()
	oldXP, _ := extPANIDOf(old)
	sim := &simBorderRouter{active: old, attached: true, enabled: true}
	br := &gatedBorderRouter{simBorderRouter: sim, hold: threadDataset, entered: make(chan struct{}), release: make(chan struct{})}
	armed := true
	var crumbs []uint64
	n, err := NewThreadNetworkCommissioning(context.Background(), ThreadNetworkCommissioningConfig{
		BorderRouter: br, IsFailSafeArmed: func() bool { return armed },
		SetBreadcrumb: func(v uint64) { crumbs = append(crumbs, v) },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := n.MatterInvoke(ctx, netcommdef.CmdRemoveNetwork, netcommdef.RemoveNetworkRequest{NetworkId: oldXP}); err != nil {
		t.Fatal(err)
	}
	if _, err := n.MatterInvoke(ctx, netcommdef.CmdAddOrUpdateThreadNetwork, netcommdef.AddOrUpdateThreadNetworkRequest{OperationalDataset: threadDataset}); err != nil {
		t.Fatal(err)
	}
	statusBefore, _ := n.MatterRead(netcommAttrLastNetworkingStatus)
	done := make(chan struct{})
	var resp any
	var respErr error
	go func() {
		defer close(done)
		resp, respErr = n.MatterInvoke(ctx, netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: threadExtPANID, Breadcrumb: bc(7)})
	}()
	<-br.entered
	armed = false
	n.OnFailSafeExpired(ctx)
	crumbsAtExpiry := len(crumbs)
	close(br.release)
	<-done
	// Answered with nothing, as chip (R2-P6-fsm-1001, F-db021da0-10).
	if _, ok := resp.(im.NoResponse); !ok || respErr != nil {
		t.Errorf("the abandoned ConnectNetwork answered %#v, %v; want im.NoResponse", resp, respErr)
	}
	sim.mu.Lock()
	final := bytes.Clone(sim.active)
	sim.mu.Unlock()
	if !bytes.Equal(final, old) {
		t.Errorf("border router after the abandoned ConnectNetwork returned: on the reverted dataset = %v, on the rolled-back one = %v",
			bytes.Equal(final, old), bytes.Equal(final, threadDataset))
	}
	if len(crumbs) > crumbsAtExpiry {
		t.Errorf("Breadcrumb %v written after the fail-safe expired", crumbs[crumbsAtExpiry:])
	}
	if last, _ := n.MatterRead(netcommAttrLastNetworkID); last != nil {
		t.Errorf("LastNetworkID = %x after the abandoned ConnectNetwork, want it untouched (null)", last)
	}
	if st, _ := n.MatterRead(netcommAttrLastNetworkingStatus); st != statusBefore {
		t.Errorf("LastNetworkingStatus = %v after the abandoned ConnectNetwork, want %v", st, statusBefore)
	}
	nets := networksOf(t, n)
	if len(nets) != 1 || !bytes.Equal(nets[0].NetworkId, oldXP) || !nets[0].Connected {
		t.Errorf("Networks = %+v, want the reverted network, connected", nets)
	}
}

// The Breadcrumb is written without the server's lock: a listener that
// reads the server from the write returns (review II, R2-P6-conc-1011).
func TestThreadBreadcrumbListenerMayReadTheServer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		sim := &simBorderRouter{}
		n, err := NewThreadNetworkCommissioning(context.Background(), ThreadNetworkCommissioningConfig{
			BorderRouter: sim, IsFailSafeArmed: func() bool { return true },
		})
		if err != nil {
			t.Fatal(err)
		}
		n.AttachFailSafe(nil, func(uint64) { n.MatterRead(netcommAttrNetworks) })
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = n.MatterInvoke(context.Background(), netcommdef.CmdAddOrUpdateThreadNetwork, netcommdef.AddOrUpdateThreadNetworkRequest{OperationalDataset: threadDataset, Breadcrumb: bc(3)})
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("AddOrUpdateThreadNetwork did not return: the Breadcrumb listener's read waits for the server's lock")
		}
	})
}

// Two ConnectNetworks at once: the second is Busy, as the check and the
// claim of the command's turn are one step (review II, R2-P6-conc-1026).
func TestThreadSecondConnectIsBusy(t *testing.T) {
	t.Parallel()
	sim := &simBorderRouter{}
	br := &gatedBorderRouter{simBorderRouter: sim, hold: threadDataset, entered: make(chan struct{}), release: make(chan struct{})}
	n, err := NewThreadNetworkCommissioning(context.Background(), ThreadNetworkCommissioningConfig{
		BorderRouter: br, IsFailSafeArmed: func() bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := n.MatterInvoke(ctx, netcommdef.CmdAddOrUpdateThreadNetwork, netcommdef.AddOrUpdateThreadNetworkRequest{OperationalDataset: threadDataset}); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = n.MatterInvoke(ctx, netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: threadExtPANID})
	}()
	<-br.entered
	_, err = n.MatterInvoke(ctx, netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: threadExtPANID})
	close(br.release)
	<-done
	if got := threadStatusOf(t, err); got != im.StatusBusy {
		t.Errorf("second ConnectNetwork while one runs: %v, want Busy", got)
	}
	if _, err := n.MatterInvoke(ctx, netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: threadExtPANID}); threadStatusOf(t, err) != im.StatusSuccess {
		t.Errorf("ConnectNetwork after the first returned: %v, want it served", err)
	}
}
