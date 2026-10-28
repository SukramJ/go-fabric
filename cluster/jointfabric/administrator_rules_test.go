// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// JointFabricAdministrator rules: connectedhomeip has no unit test of the
// server file (joint-fabric-administrator-server.cpp at 6170af84); its
// statuses are the ones the Python cases TC_JFADMIN_1_x and 2_x check.
package jointfabric

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/client/jcm"
	jfadef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricadministrator"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// AdministratorCmdAnnounce is AnnounceJointFabricAdministrator's id.
const AdministratorCmdAnnounce = jfadef.CmdAnnounceJointFabricAdministrator

func announce(endpoint uint16) jfadef.AnnounceJointFabricAdministratorRequest {
	return jfadef.AnnounceJointFabricAdministratorRequest{EndpointId: endpoint}
}

func clusterCode(err error) (uint8, bool) {
	var cs im.MatterClusterStatusError
	if errors.As(err, &cs) {
		return cs.MatterClusterStatus(), true
	}
	return 0, false
}

func ojcw() jfadef.OpenJointCommissioningWindowRequest {
	return jfadef.OpenJointCommissioningWindowRequest{CommissioningTimeout: 180, PakePasscodeVerifier: make([]byte, 97), Discriminator: 3840, Iterations: 1000, Salt: make([]byte, 16)}
}

func setAdminIndex(t *testing.T, a *Administrator, idx uint8) {
	t.Helper()
	if err := a.SetAttributes(map[uint32]any{jfadef.AttrAdministratorFabricIndex: idx}); err != nil {
		t.Fatal(err)
	}
}

// TestOpenJointCommissioningWindowStatuses is HandleOJCW (:173-237).
func TestOpenJointCommissioningWindowStatuses(t *testing.T) {
	fx := newAdminFixture(t, AdministratorConfig{})
	ctx := caseCtx(t.Context(), 3)
	call := func(ctx context.Context, req jfadef.OpenJointCommissioningWindowRequest) error {
		_, err := fx.a.MatterInvoke(ctx, jfadef.CmdOpenJointCommissioningWindow, req)
		return err
	}
	if code, ok := clusterCode(call(caseCtx(t.Context(), 9), ojcw())); !ok || code != uint8(jfadef.ICACCSRResponseStatusCodePakeParameterError) { // :197
		t.Errorf("unknown fabric: %d %v", code, ok)
	}
	if code, _ := clusterCode(call(ctx, ojcw())); code != uint8(jfadef.ICACCSRResponseStatusCodeInvalidAdministratorFabricIndex) { // :201-202
		t.Errorf("null AdministratorFabricIndex: %d", code)
	}
	setAdminIndex(t, fx.a, 3)
	fx.arm(true)
	if code, _ := clusterCode(call(ctx, ojcw())); code != uint8(jfadef.ICACCSRResponseStatusCodeBusy) { // :203
		t.Errorf("armed fail-safe: %d", code)
	}
	fx.arm(false)
	for name, edit := range map[string]func(*jfadef.OpenJointCommissioningWindowRequest){
		"iterations":    func(r *jfadef.OpenJointCommissioningWindowRequest) { r.Iterations = 999 },
		"salt":          func(r *jfadef.OpenJointCommissioningWindowRequest) { r.Salt = make([]byte, 33) },
		"timeout":       func(r *jfadef.OpenJointCommissioningWindowRequest) { r.CommissioningTimeout = 901 },
		"short timeout": func(r *jfadef.OpenJointCommissioningWindowRequest) { r.CommissioningTimeout = 179 },
		"discriminator": func(r *jfadef.OpenJointCommissioningWindowRequest) { r.Discriminator = 0x1000 },
		"verifier":      func(r *jfadef.OpenJointCommissioningWindowRequest) { r.PakePasscodeVerifier = make([]byte, 96) },
	} {
		req := ojcw()
		edit(&req)
		if got := statusOf(call(ctx, req)); got != im.StatusInvalidCommand { // :206-216
			t.Errorf("%s: %#x", name, got)
		}
	}
	if err := call(ctx, ojcw()); err != nil {
		t.Fatalf("open: %v", err)
	}
	if p := fx.window.opened; len(p) != 1 || !p[0].JointFabric || p[0].AdminFabricIndex != 3 || p[0].AdminVendorID != fx.f.vendorID {
		t.Fatalf("window opened with %+v", p)
	}
	if code, _ := clusterCode(call(ctx, ojcw())); code != uint8(jfadef.ICACCSRResponseStatusCodeBusy) { // :205
		t.Errorf("window open: %d", code)
	}
}

// TestAnnounceJointFabricAdministrator is :239-290: the trust
// verification of the announcing commissioner marks the accessing
// fabric's VID verified; an invalid endpoint is a ConstraintError, a
// failing verification FAILURE.
func TestAnnounceJointFabricAdministrator(t *testing.T) {
	fx := newAdminFixture(t, AdministratorConfig{})
	ctx := caseCtx(t.Context(), 3)
	if _, err := fx.a.MatterInvoke(ctx, AdministratorCmdAnnounce, announce(jcm.InvalidEndpointID)); statusOf(err) != im.StatusConstraintError { // :272
		t.Fatalf("invalid endpoint: %v", err)
	}
	if _, err := fx.a.MatterInvoke(ctx, AdministratorCmdAnnounce, announce(1)); err != nil {
		t.Fatalf("announce: %v", err)
	}
	if fx.a.VIDVerifiedFabric() != 3 || fx.a.PeerAdministratorEndpoint() != 1 {
		t.Fatalf("verified %d, endpoint %d", fx.a.VIDVerifiedFabric(), fx.a.PeerAdministratorEndpoint())
	}
	// Another fabric's administrator over this fabric: the cross-check
	// fails (CrossCheckAdministratorIds :325-340).
	other := newJFFabric(t, 3, adminCAT)
	fx.a.reach = &fakeReach{f: other, adminIndex: 3}
	if _, err := fx.a.MatterInvoke(ctx, AdministratorCmdAnnounce, announce(1)); statusOf(err) != im.StatusFailure {
		t.Fatalf("mismatched administrator: %v", err)
	}
	if fx.a.VIDVerifiedFabric() != 0 { // :249-250
		t.Fatal("the failed announcement kept the VID verified")
	}
	// Without a reach, the announcement fails.
	fx.a.reach = nil
	if _, err := fx.a.MatterInvoke(ctx, AdministratorCmdAnnounce, announce(1)); statusOf(err) != im.StatusFailure {
		t.Fatalf("no reach: %v", err)
	}
}

// TestAnnounceBusy: a second announcement while one runs is Busy (:276).
func TestAnnounceBusy(t *testing.T) {
	fx := newAdminFixture(t, AdministratorConfig{})
	reach := &fakeReach{f: fx.f, adminIndex: 3, block: true, waiting: make(chan struct{})}
	fx.a.reach = reach
	ctx, cancel := context.WithCancel(caseCtx(context.Background(), 3))
	done := make(chan struct{})
	go func() { _, _ = fx.a.MatterInvoke(ctx, AdministratorCmdAnnounce, announce(1)); close(done) }()
	<-reach.waiting
	if _, err := fx.a.MatterInvoke(caseCtx(t.Context(), 3), AdministratorCmdAnnounce, announce(1)); statusOf(err) != im.StatusBusy {
		t.Errorf("second announcement: %v", err)
	}
	cancel()
	<-done
}

// TestICACCSRRequestStatuses is HandleICACCSRRequest (:305-354).
func TestICACCSRRequestStatuses(t *testing.T) {
	fx := newAdminFixture(t, AdministratorConfig{})
	ctx := caseCtx(t.Context(), 3)
	call := func(ctx context.Context) (any, error) {
		return fx.a.MatterInvoke(ctx, jfadef.CmdIcaccsrRequest, jfadef.IcaccsrRequestRequest{})
	}
	if _, err := call(im.WithAuthModePASE(ctx)); statusOf(err) != im.StatusInvalidCommand { // :322
		t.Errorf("PASE: %v", err)
	}
	if _, err := call(ctx); statusOf(err) != im.StatusFailsafeRequired { // :325
		t.Errorf("unarmed: %v", err)
	}
	fx.arm(true)
	if _, err := call(ctx); err == nil {
		t.Error("an unverified fabric got the CSR")
	} else if code, _ := clusterCode(err); code != uint8(jfadef.ICACCSRResponseStatusCodeVidNotVerified) { // :330-331
		t.Errorf("unverified: %d", code)
	}
	if _, err := fx.a.MatterInvoke(ctx, AdministratorCmdAnnounce, announce(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := call(ctx); err == nil {
		t.Error("CSR without AdministratorFabricIndex")
	} else if code, _ := clusterCode(err); code != uint8(jfadef.ICACCSRResponseStatusCodeInvalidAdministratorFabricIndex) { // :336-337
		t.Errorf("null index: %d", code)
	}
	setAdminIndex(t, fx.a, 3)
	resp, err := call(ctx)
	if err != nil {
		t.Fatalf("CSR: %v", err)
	}
	if r, ok := resp.(jfadef.IcaccsrResponse); !ok || r.StatusCode != jfadef.ICACCSRResponseStatusCodeOk || r.Icaccsr == nil || len(*r.Icaccsr) == 0 {
		t.Fatalf("response %+v", resp)
	}
	fx.a.port = &fakePort{err: errors.New("no key")}
	if _, err := call(ctx); statusOf(err) != im.StatusFailure { // :340
		t.Errorf("port failure: %v", err)
	}
}

// TestAddICAC is HandleAddICAC (:356-391): the ICAC of the accessing
// fabric for the CSR's key passes, once per fail-safe.
func TestAddICAC(t *testing.T) {
	fx := newAdminFixture(t, AdministratorConfig{})
	ctx := caseCtx(t.Context(), 3)
	call := func(icac []byte) (any, error) {
		return fx.a.MatterInvoke(ctx, jfadef.CmdAddIcac, jfadef.AddIcacRequest{IcacValue: icac})
	}
	if _, err := fx.a.MatterInvoke(im.WithAuthModePASE(ctx), jfadef.CmdAddIcac, jfadef.AddIcacRequest{}); statusOf(err) != im.StatusInvalidCommand {
		t.Errorf("PASE: %v", err)
	}
	if _, err := call(fx.f.icac); statusOf(err) != im.StatusFailsafeRequired {
		t.Errorf("unarmed: %v", err)
	}
	fx.arm(true)
	if resp, err := call(fx.f.icac); err != nil || resp != nil {
		t.Fatalf("AddICAC = %v, %v; want SUCCESS without a response", resp, err)
	}
	if _, err := call(fx.f.icac); statusOf(err) != im.StatusConstraintError { // :372
		t.Errorf("second AddICAC: %v", err)
	}
	fx.a.OnCommissioningComplete(t.Context())
	other := newJFFabric(t, 3)
	for name, c := range map[string]struct {
		icac []byte
		want ICACStatus
	}{
		"another root":  {other.icac, jfadef.ICACResponseStatusInvalidIcac},
		"not decodable": {[]byte("icac"), jfadef.ICACResponseStatusInvalidIcac},
		"a NOC":         {fx.f.noc, jfadef.ICACResponseStatusInvalidIcac},
	} {
		resp, err := call(c.icac)
		r, ok := resp.(jfadef.IcacResponse)
		if err != nil || !ok || r.StatusCode != c.want {
			t.Errorf("%s: %v %v", name, resp, err)
		}
		fx.a.OnFailSafeExpired(t.Context())
		fx.arm(true)
	}
	fx.a.port = &fakePort{csr: other.csr(t)}
	if resp, _ := call(fx.f.icac); resp != (jfadef.IcacResponse{StatusCode: jfadef.ICACResponseStatusInvalidPublicKey}) { // :378
		t.Errorf("another key: %v", resp)
	}
}

// TestTransferAnchorAnswersSuccess: connectedhomeip's handlers are empty
// (:439-449).
func TestTransferAnchorAnswersSuccess(t *testing.T) {
	fx := newAdminFixture(t, AdministratorConfig{})
	for _, cmd := range []uint32{jfadef.CmdTransferAnchorRequest, jfadef.CmdTransferAnchorComplete} {
		if resp, err := fx.a.MatterInvoke(caseCtx(t.Context(), 3), cmd, struct{}{}); err != nil || resp != nil {
			t.Errorf("command %#x: %v %v", cmd, resp, err)
		}
	}
}

// TestAnchorOnCommissioningComplete: a fabric with the administrator and
// anchor CATs becomes AdministratorFabricIndex and the datastore's anchor
// (JFAManager.cpp:95-157); its removal wipes the datastore.
func TestAnchorOnCommissioningComplete(t *testing.T) {
	ctx := t.Context()
	d := newStore(t, nil)
	fx := newAdminFixture(t, AdministratorConfig{Datastore: d})
	anchor := newJFFabric(t, 4, adminCAT, anchorCAT)
	fx.a.AttachNode(&fakeFabrics{
		fabrics: []store.FabricRecord{fx.f.record(), anchor.record()},
		ids:     map[uint8]store.IdentityRecord{3: {NOC: fx.f.noc}, 4: {NOC: anchor.noc}},
	}, fx.window)
	fx.a.OnCommissioningComplete(ctx)
	if got := fx.a.administratorFabricIndex(); got != 4 {
		t.Fatalf("AdministratorFabricIndex %d", got)
	}
	if d.AnchorFabricIndex() != 4 || d.AnchorNodeID() != anchor.nodeID || d.AnchorVendorID() != anchor.vendorID || !bytes.Equal(d.AnchorRootCA(), anchor.rcac) {
		t.Fatalf("anchor %d %#x %#x", d.AnchorFabricIndex(), d.AnchorNodeID(), d.AnchorVendorID())
	}
	mustOK(t, d.AddPendingNode(ctx, 9, "n"))
	fx.a.OnFabricRemoved(ctx, 4)
	if len(d.NodeList()) != 0 || d.AnchorFabricIndex() != 0 {
		t.Fatal("the anchor fabric's removal left the datastore")
	}
}
