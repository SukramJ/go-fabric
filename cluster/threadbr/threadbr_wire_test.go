// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package threadbr_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/core"
	gcdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	ncdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	tbrm "github.com/SukramJ/go-fabric/cluster/spec/threadborderroutermanagement"
	tnd "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics"
	"github.com/SukramJ/go-fabric/cluster/threadbr"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/thread/otbr"
	"github.com/SukramJ/go-fabric/thread/otbr/otbrtest"
)

// TestThreadBorderRouterOverTheWire runs the three servers on one fake OTBR
// on a commissioned pair (ADR 0027 decision 7): the controller reads the
// empty active dataset, is refused an untimed and an unarmed
// SetActiveDatasetRequest, arms the fail-safe over CASE and activates
// chip's dataset; the border router attaches, the breadcrumb is written,
// the timestamps and the diagnostics follow, and CommissioningComplete
// commits. NetworkCommissioning's ScanNetworks then answers the border
// router's own network. The site runs on the system clock: the adapter
// waits for the attach in real time.
func TestThreadBorderRouterOverTheWire(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	fake := otbrtest.New(otbrtest.Options{AttachPolls: 2})
	defer fake.Close()
	c, err := otbr.NewClient(otbr.Config{Host: fake.Host(), Port: fake.Port(), Timeout: 2 * time.Second})
	mustT(t, err)
	br, err := otbr.NewBorderRouter(c, otbr.BorderRouterConfig{Name: "wire OTBR", ThreadVersion: 5, PollInterval: 5 * time.Millisecond})
	mustT(t, err)
	mgmt, err := threadbr.NewManagement(ctx, threadbr.ManagementConfig{BorderRouter: br, Features: tbrm.FeaturePanChange})
	mustT(t, err)
	diag, err := threadbr.NewDiagnostics(ctx, threadbr.DiagnosticsConfig{BorderRouter: br, Features: threadbr.DiagnosticsFeatures})
	mustT(t, err)
	nc, err := core.NewThreadNetworkCommissioning(ctx, core.ThreadNetworkCommissioningConfig{BorderRouter: br})
	mustT(t, err)

	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}})
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Bridged: []node.Endpoint{fabrictest.Endpoint("tbr", 0x0091, mgmt, diag, nc)},
	}})
	ep, ok := pair.Device.EndpointOf("tbr")
	if !ok {
		t.Fatal("no tbr endpoint")
	}

	res, err := fabrictest.Invoke(ctx, pair, client.CommandAt(ep, tbrm.ClusterID, tbrm.CmdGetActiveDatasetRequest), tbrm.GetActiveDatasetRequestRequest{})
	mustT(t, err)
	var ds tbrm.DatasetResponse
	mustT(t, ds.DecodeTLV(res.Fields))
	if len(ds.Dataset) != 0 {
		t.Errorf("active dataset %x, want empty", ds.Dataset)
	}

	set := client.CommandAt(ep, tbrm.ClusterID, tbrm.CmdSetActiveDatasetRequest)
	crumb := uint64(7)
	req := tbrm.SetActiveDatasetRequestRequest{ActiveDataset: chipDataset, Breadcrumb: &crumb}
	var se *fabrictest.StatusError
	if _, err := fabrictest.InvokeUntimed(ctx, pair, set, req); !errors.As(err, &se) || se.Status != im.StatusNeedsTimedInteraction {
		t.Errorf("untimed SetActiveDatasetRequest: %v, want NEEDS_TIMED_INTERACTION", err)
	}
	if _, err := fabrictest.InvokeTimed(ctx, pair, set, req); !errors.As(err, &se) || se.Status != im.StatusFailsafeRequired {
		t.Errorf("unarmed SetActiveDatasetRequest: %v, want FAILSAFE_REQUIRED", err)
	}

	_, err = fabrictest.Invoke(ctx, pair, client.CommandAt(0, gcdef.ClusterID, gcdef.CmdArmFailSafe), gcdef.ArmFailSafeRequest{ExpiryLengthSeconds: 60, Breadcrumb: 1})
	mustT(t, err)
	if _, err := fabrictest.InvokeTimed(ctx, pair, set, req); err != nil {
		t.Fatalf("armed SetActiveDatasetRequest: %v", err)
	}
	if !bytes.Equal(fake.Active(), chipDataset) || fake.State() != "leader" {
		t.Errorf("border router: %x, %s", fake.Active(), fake.State())
	}
	values, err := fabrictest.ReadCluster(ctx, pair, 0, gcdef.ClusterID)
	mustT(t, err)
	if st, _ := gcdef.StateFrom(values); st.Breadcrumb != 7 {
		t.Errorf("Breadcrumb %d, want 7", st.Breadcrumb)
	}
	values, err = fabrictest.ReadCluster(ctx, pair, ep, tbrm.ClusterID)
	mustT(t, err)
	if st, _ := tbrm.StateFrom(values); st.ActiveDatasetTimestamp == nil || *st.ActiveDatasetTimestamp != 0 || !st.InterfaceEnabled || st.BorderRouterName != "wire OTBR" {
		t.Errorf("management %+v", st)
	}
	// A second activation finds the dataset configured (:223-230).
	if _, err := fabrictest.InvokeTimed(ctx, pair, set, req); !errors.As(err, &se) || se.Status != im.StatusInvalidInState {
		t.Errorf("second SetActiveDatasetRequest: %v, want INVALID_IN_STATE", err)
	}

	_, err = fabrictest.Invoke(ctx, pair, client.CommandAt(0, gcdef.ClusterID, gcdef.CmdCommissioningComplete), gcdef.CommissioningCompleteRequest{})
	mustT(t, err)

	mustT(t, diag.Refresh(ctx))
	values, err = fabrictest.ReadCluster(ctx, pair, ep, tnd.ClusterID)
	mustT(t, err)
	if st, _ := tnd.StateFrom(values); st.Channel == nil || *st.Channel != 11 || st.NetworkName == nil || *st.NetworkName != "OpenThread" ||
		st.Rloc16 == nil || *st.Rloc16 != 0x7400 || st.RoutingRole == nil || *st.RoutingRole != tnd.RoutingRoleLeader || len(st.RouteTable) != 5 {
		t.Errorf("diagnostics %+v", st)
	}

	res, err = fabrictest.Invoke(ctx, pair, client.CommandAt(ep, ncdef.ClusterID, ncdef.CmdScanNetworks), ncdef.ScanNetworksRequest{})
	mustT(t, err)
	var scan ncdef.ScanNetworksResponse
	mustT(t, scan.DecodeTLV(res.Fields))
	if scan.ThreadScanResults == nil || len(*scan.ThreadScanResults) != 1 || (*scan.ThreadScanResults)[0].NetworkName != "OpenThread" {
		t.Errorf("scan %+v", scan)
	}
}

// TestThreadBorderRouterFailSafeExpiryOverTheWire: an activation that does
// not attach before the fail-safe expires is answered TIMEOUT and the
// border router reverted (:301-317) — the node's fail-safe reaches the
// bridged endpoint's server.
func TestThreadBorderRouterFailSafeExpiryOverTheWire(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	fake := otbrtest.New(otbrtest.Options{AttachPolls: 1 << 30})
	defer fake.Close()
	c, err := otbr.NewClient(otbr.Config{Host: fake.Host(), Port: fake.Port(), Timeout: 2 * time.Second})
	mustT(t, err)
	br, err := otbr.NewBorderRouter(c, otbr.BorderRouterConfig{Name: "wire OTBR", ThreadVersion: 5, PollInterval: 20 * time.Millisecond})
	mustT(t, err)
	mgmt, err := threadbr.NewManagement(ctx, threadbr.ManagementConfig{BorderRouter: br})
	mustT(t, err)
	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}})
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Bridged: []node.Endpoint{fabrictest.Endpoint("tbr", 0x0091, mgmt)},
	}})
	ep, _ := pair.Device.EndpointOf("tbr")
	_, err = fabrictest.Invoke(ctx, pair, client.CommandAt(0, gcdef.ClusterID, gcdef.CmdArmFailSafe), gcdef.ArmFailSafeRequest{ExpiryLengthSeconds: 1})
	mustT(t, err)
	_, err = fabrictest.InvokeTimed(ctx, pair, client.CommandAt(ep, tbrm.ClusterID, tbrm.CmdSetActiveDatasetRequest), tbrm.SetActiveDatasetRequestRequest{ActiveDataset: chipDataset})
	var se *fabrictest.StatusError
	if !errors.As(err, &se) || se.Status != im.StatusTimeout {
		t.Errorf("SetActiveDatasetRequest past the fail-safe: %v, want TIMEOUT", err)
	}
	if fake.Active() != nil || fake.State() != "disabled" {
		t.Errorf("not reverted: %x, %s", fake.Active(), fake.State())
	}
}
