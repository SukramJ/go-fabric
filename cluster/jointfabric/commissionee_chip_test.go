// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// JCMCommissionee: a port of connectedhomeip
// src/app/clusters/joint-fabric-administrator-server/tests/TestJCMCommissionee.cpp
// at 6170af84 (13 TEST_F), by meaning. chip's ProgressionJCMCommissionee
// (:102-134) is a commissionee whose perform records the stages;
// SingleStageJCMCommissionee (:139-345) runs one stage on its overridden
// reads, here a fakeReach; the use-after-free cases (:684-838) become the
// verification's context, which the fail-safe ends.
package jointfabric

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/client/jcm"
)

// progression is ProgressionJCMCommissionee (:102-134).
type progression struct {
	c      *commissionee
	stages []jcm.Stage
}

func newProgression(t *testing.T, endpoint uint16) *progression {
	t.Helper()
	fx := newAdminFixture(t, AdministratorConfig{})
	p := &progression{c: fx.a.newCommissionee(3, endpoint)}
	p.c.perform = func(_ context.Context, s jcm.Stage) { p.stages = append(p.stages, s) }
	return p
}

// singleStage is SingleStageJCMCommissionee (:139-167): it runs one stage.
func singleStage(fx *adminFixture, endpoint uint16, stage jcm.Stage) *commissionee {
	c := fx.a.newCommissionee(3, endpoint)
	c.next = func(s jcm.Stage) jcm.Stage {
		if s == stage {
			return jcm.StageComplete
		}
		return stage
	}
	return c
}

// stageResult runs c and returns the result its one stage finished with.
func stageResult(t *testing.T, c *commissionee) jcm.Result {
	t.Helper()
	return c.run(t.Context())
}

// TestNextStageFollowsExpectedOrder is :382-408.
func TestNextStageFollowsExpectedOrder(t *testing.T) {
	order := []jcm.Stage{jcm.StageIdle, jcm.StageStoringEndpointID, jcm.StageReadingCommissionerAdminFabricIndex, jcm.StagePerformingVendorIDVerification, jcm.StageCrossCheckingAdministratorIDs, jcm.StageComplete}
	for i := 0; i+1 < len(order); i++ {
		if got := nextCommissioneeStage(order[i]); got != order[i+1] {
			t.Errorf("after %s: %s, want %s", order[i], got, order[i+1])
		}
	}
}

// TestSuccessfulProgressionAdvancesAllStages is :410-452.
func TestSuccessfulProgressionAdvancesAllStages(t *testing.T) {
	p := newProgression(t, 2)
	p.c.start(t.Context())
	want := []jcm.Stage{jcm.StageStoringEndpointID}
	for _, s := range []jcm.Stage{jcm.StageStoringEndpointID, jcm.StageReadingCommissionerAdminFabricIndex, jcm.StagePerformingVendorIDVerification, jcm.StageCrossCheckingAdministratorIDs} {
		if !slices.Equal(p.stages, want) {
			t.Fatalf("stages %v, want %v", p.stages, want)
		}
		p.c.machine.Finished(s, jcm.Success)
		want = append(want, nextCommissioneeStage(s))
	}
	p.c.machine.Finished(jcm.StageComplete, jcm.Success)
	if !p.c.done || p.c.result != jcm.Success {
		t.Fatalf("completion %v %v", p.c.done, p.c.result)
	}
}

// TestErrorDuringStagePropagatesToCompletion is :454-479.
func TestErrorDuringStagePropagatesToCompletion(t *testing.T) {
	p := newProgression(t, 3)
	p.c.start(t.Context())
	p.c.machine.Finished(jcm.StageStoringEndpointID, jcm.InternalError)
	if !p.c.done || p.c.result != jcm.InternalError {
		t.Fatalf("completion %v %v", p.c.done, p.c.result)
	}
}

// TestStoreEndpointIdSuccess is :481-504.
func TestStoreEndpointIdSuccess(t *testing.T) {
	fx := newAdminFixture(t, AdministratorConfig{})
	if r := stageResult(t, singleStage(fx, 55, jcm.StageStoringEndpointID)); r != jcm.Success {
		t.Fatalf("result %s", r)
	}
	if got := fx.a.PeerAdministratorEndpoint(); got != 55 {
		t.Fatalf("peer endpoint %d", got)
	}
}

// TestStoreEndpointIdError is :506-529.
func TestStoreEndpointIdError(t *testing.T) {
	fx := newAdminFixture(t, AdministratorConfig{})
	_ = stageResult(t, singleStage(fx, 55, jcm.StageStoringEndpointID))
	if r := stageResult(t, singleStage(fx, jcm.InvalidEndpointID, jcm.StageStoringEndpointID)); r != jcm.InvalidAdministratorEndpointID {
		t.Fatalf("result %s", r)
	}
	if got := fx.a.PeerAdministratorEndpoint(); got != 55 {
		t.Fatalf("peer endpoint %d, want it kept", got)
	}
}

// TestReadCommissionerAdminFabricIndexSuccess is :531-545.
func TestReadCommissionerAdminFabricIndexSuccess(t *testing.T) {
	fx := newAdminFixture(t, AdministratorConfig{})
	fx.a.reach = &fakeReach{f: fx.f, adminIndex: 1}
	c := singleStage(fx, 77, jcm.StageReadingCommissionerAdminFabricIndex)
	if r := stageResult(t, c); r != jcm.Success || c.info.AdminFabricIndex != 1 {
		t.Fatalf("result %s, index %d", r, c.info.AdminFabricIndex)
	}
	// A failed read and a null index are ReadAdminAttributeFailed, index 0
	// InvalidAdministratorFabricIndex (:185-211).
	for _, r := range []*fakeReach{{f: fx.f, readErr: errors.New("read")}, {f: fx.f, null: true}} {
		fx.a.reach = r
		if got := stageResult(t, singleStage(fx, 77, jcm.StageReadingCommissionerAdminFabricIndex)); got != jcm.ReadAdminAttributeFailed {
			t.Errorf("result %s", got)
		}
	}
	fx.a.reach = &fakeReach{f: fx.f, adminIndex: 0}
	if got := stageResult(t, singleStage(fx, 77, jcm.StageReadingCommissionerAdminFabricIndex)); got != jcm.InvalidAdministratorFabricIndex {
		t.Errorf("index 0: %s", got)
	}
}

// TestReadAdminFabricsPopulatesCommissionerInfo is :547-578.
func TestReadAdminFabricsPopulatesCommissionerInfo(t *testing.T) {
	fx := newAdminFixture(t, AdministratorConfig{})
	c := fx.a.newCommissionee(3, 78)
	c.info.AdminFabricIndex = 3
	if err := c.readAdminFabrics(t.Context()); err != nil {
		t.Fatal(err)
	}
	if c.info.AdminFabricID != fx.f.fabricID || c.info.AdminVendorID != fx.f.vendorID || string(c.info.RootPublicKey) != string(fx.f.rootPub) {
		t.Fatalf("info %+v", c.info)
	}
	c.info.AdminFabricIndex = 9
	if err := c.readAdminFabrics(t.Context()); !errors.Is(err, errCommissionerInfo) {
		t.Fatalf("another index: %v", err)
	}
}

// TestReadAdminCertsPopulatesCommissionerRcac is :580-607.
func TestReadAdminCertsPopulatesCommissionerRcac(t *testing.T) {
	fx := newAdminFixture(t, AdministratorConfig{})
	c := fx.a.newCommissionee(3, 79)
	c.info.RootPublicKey = fx.f.rootPub
	if err := c.readAdminCerts(t.Context()); err != nil {
		t.Fatal(err)
	}
	if string(c.info.AdminRCAC) != string(fx.f.rcac) {
		t.Fatal("RCAC not taken")
	}
	c.info.RootPublicKey = newJFFabric(t, 1).rootPub
	if err := c.readAdminCerts(t.Context()); !errors.Is(err, errCommissionerInfo) {
		t.Fatalf("another root: %v", err)
	}
}

// TestReadAdminNOCsPopulatesCommissionerCerts is :609-641.
func TestReadAdminNOCsPopulatesCommissionerCerts(t *testing.T) {
	fx := newAdminFixture(t, AdministratorConfig{})
	c := fx.a.newCommissionee(3, 80)
	c.info.AdminFabricIndex = 3
	if err := c.readAdminNOCs(t.Context()); err != nil {
		t.Fatal(err)
	}
	if string(c.info.AdminNOC) != string(fx.f.noc) || string(c.info.AdminICAC) != string(fx.f.icac) {
		t.Fatal("NOC and ICAC not taken")
	}
}

// TestValidateAdministratorIdsMatch is :643-682.
func TestValidateAdministratorIdsMatch(t *testing.T) {
	fx := newAdminFixture(t, AdministratorConfig{})
	c := fx.a.newCommissionee(3, 92)
	const fabricID = 0x0123456789ABCDEF
	key := make([]byte, 65)
	key[0] = 0x04
	for i := 1; i < 65; i++ {
		key[i] = byte(i)
	}
	c.info.AdminFabricID, c.info.RootPublicKey = fabricID, key
	if r := c.validateAdministratorIDsMatch(fabricID, key); r != jcm.Success {
		t.Errorf("matching: %s", r)
	}
	other := slices.Clone(key)
	other[64] ^= 0xFF
	if r := c.validateAdministratorIDsMatch(fabricID, other); r != jcm.AdministratorIDMismatched {
		t.Errorf("other key: %s", r)
	}
	if r := c.validateAdministratorIDsMatch(0xFEDCBA9876543210, key); r != jcm.AdministratorIDMismatched {
		t.Errorf("other fabric: %s", r)
	}
	c.info.RootPublicKey = key[:64]
	if r := c.validateAdministratorIDsMatch(fabricID, key); r != jcm.InternalError {
		t.Errorf("short key: %s", r)
	}
}

// TestPerformVendorIdVerificationCompletesOnceOnReadFailure is :684-743: a
// failed commissioner read completes the stage once, with
// VendorIDVerificationFailed, and does not go on to the signing.
func TestPerformVendorIdVerificationCompletesOnceOnReadFailure(t *testing.T) {
	fx := newAdminFixture(t, AdministratorConfig{})
	reach := &fakeReach{f: fx.f, adminIndex: 3, fabricsErr: errors.New("read")}
	fx.a.reach = reach
	completions := 0
	c := singleStage(fx, 41, jcm.StagePerformingVendorIDVerification)
	c.start(t.Context())
	c.machine.Complete = func(jcm.Result) { completions++ }
	if !c.done || c.result != jcm.VendorIDVerificationFailed {
		t.Fatalf("result %v %s", c.done, c.result)
	}
	c.machine.Finished(jcm.StagePerformingVendorIDVerification, jcm.Success)
	if completions != 0 || reach.signs != 0 {
		t.Fatalf("completed again (%d) or signed (%d)", completions, reach.signs)
	}
}

// TestPerformVendorIdVerificationDoesNotUseFreedCommissioneeOnReadFailure
// is :745-765: after the failure nothing of the verification runs on — the
// commissioner is not asked to sign.
func TestPerformVendorIdVerificationDoesNotUseFreedCommissioneeOnReadFailure(t *testing.T) {
	fx := newAdminFixture(t, AdministratorConfig{})
	reach := &fakeReach{f: fx.f, adminIndex: 3, fabricsErr: errors.New("read")}
	fx.a.reach = reach
	if r := stageResult(t, singleStage(fx, 41, jcm.StagePerformingVendorIDVerification)); r != jcm.VendorIDVerificationFailed || reach.signs != 0 {
		t.Fatalf("result %s, signs %d", r, reach.signs)
	}
}

// TestFailSafeAsyncReadUseAfterFree is :767-838: the fail-safe ends an
// announcement whose read is in flight; the late answer completes nothing
// and the fabric's VID stays unverified.
func TestFailSafeAsyncReadUseAfterFree(t *testing.T) {
	fx := newAdminFixture(t, AdministratorConfig{})
	reach := &fakeReach{f: fx.f, adminIndex: 3, block: true, waiting: make(chan struct{})}
	fx.a.reach = reach
	done := make(chan error, 1)
	go func() {
		_, err := fx.a.MatterInvoke(caseCtx(context.Background(), 3), AdministratorCmdAnnounce, announce(1))
		done <- err
	}()
	<-reach.waiting
	fx.a.OnFailSafeExpired(t.Context())
	if err := <-done; statusOf(err) != 0x01 {
		t.Fatalf("announce = %v, want FAILURE", err)
	}
	if fx.a.VIDVerifiedFabric() != 0 {
		t.Fatal("the cancelled verification marked the VID verified")
	}
}
