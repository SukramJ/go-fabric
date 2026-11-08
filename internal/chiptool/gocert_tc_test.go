// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"context"
	"errors"
	"fmt"
	"time"

	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	gocontroller "github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/im"
)

// The terms-and-conditions acceptance of issue 449: the module's
// controller as DUT commissions the reference daemon with --tc-required
// (GeneralCommissioning's TermsAndConditions feature, version 1 with
// acknowledgement bit 1 required, examples/reference-bridge/surface.go
// termsAndConditions — the daemon of the CGENTC family). Without an
// acknowledgement the commissioning ends right after the information step
// with a *commissioner.TermsRequiredError of version 1, before
// ArmFailSafe, so the same commissioning window still takes the second
// commissioning, which acknowledges the terms and succeeds (register row
// BD-Matter-Commissioner-TermsRequirementRead). Neither matter.js nor
// connectedhomeip has a case for it: neither reads the requirement. The
// family key CGENTC-GO names this module's own acceptance, no chip case
// prefix.

func init() { registerGoCert(caseTermsGo) }

// termsGoOrigin is the case's golden source.
const termsGoOrigin = "the terms-and-conditions acceptance of issue 449: the commissioner reads the daemon's --tc-required requirement and fails early without the acknowledgement (gocert_tc_test.go)"

// termsRun is the case's state across its steps.
type termsRun struct {
	ctrl *gocontroller.Controller
	n    *client.Node
}

func (r *termsRun) pair(th *goTH, tc *commissioner.TermsAndConditionsAcknowledgement) (*client.Node, error) {
	ctx, cancel := context.WithTimeout(th.ctx, 2*time.Minute)
	defer cancel()
	off := false
	return r.ctrl.Pair(ctx, th.dut.info.manualCode, gocontroller.PairOptions{
		AutoSubscribe: &off, OnAttestationFailure: attestation.Accept(), TermsAndConditions: tc,
	})
}

// refused is step 1: the Pair without an acknowledgement.
func (r *termsRun) refused(th *goTH) error {
	var err error
	if r.ctrl, err = th.controller("dut"); err != nil {
		return err
	}
	_, err = r.pair(th, nil)
	var te *commissioner.TermsRequiredError
	if !errors.As(err, &te) {
		return fmt.Errorf("Pair without the acknowledgement: %w, want a *commissioner.TermsRequiredError", err)
	}
	if te.Version != 1 || te.Deadline != nil {
		return fmt.Errorf("the requirement version %d, deadline %v; want version 1, no deadline", te.Version, te.Deadline)
	}
	return nil
}

// accepted is step 2: the Pair with the acknowledgement.
func (r *termsRun) accepted(th *goTH) error {
	if r.ctrl == nil {
		return errors.New("step 1 built no DUT controller")
	}
	n, err := r.pair(th, &commissioner.TermsAndConditionsAcknowledgement{Version: 1, Acknowledgements: 1})
	if err != nil {
		return fmt.Errorf("Pair with TermsAndConditions{1, 1}: %w", err)
	}
	r.n = n
	return nil
}

// readBack is step 3: the daemon holds the acceptance.
func (r *termsRun) readBack(th *goTH) error {
	if r.n == nil {
		return errors.New("step 2 commissioned nothing")
	}
	ctx, cancel := context.WithTimeout(th.ctx, 30*time.Second)
	defer cancel()
	attrs := []uint32{gencomm.AttrTcAcceptedVersion, gencomm.AttrTcAcknowledgements, gencomm.AttrTcAcknowledgementsRequired}
	req := client.ReadRequest{IncludeKnownVersions: true}
	for _, a := range attrs {
		req.Attributes = append(req.Attributes, im.ConcreteAttributePath{
			Cluster: gencomm.ClusterID, Attribute: a, HasEndpoint: true, HasCluster: true, HasAttribute: true,
		})
	}
	rep, err := r.n.Read(ctx, req)
	if err != nil {
		return err
	}
	v, ok := rep.Value(0, gencomm.ClusterID, gencomm.AttrTcAcceptedVersion)
	if !ok || v.El.Uint != 1 {
		return fmt.Errorf("TCAcceptedVersion = %v (%v), want 1", v.El.Uint, ok)
	}
	if v, ok = rep.Value(0, gencomm.ClusterID, gencomm.AttrTcAcknowledgements); !ok || v.El.Uint != 1 {
		return fmt.Errorf("TCAcknowledgements = %v (%v), want 1", v.El.Uint, ok)
	}
	if v, ok = rep.Value(0, gencomm.ClusterID, gencomm.AttrTcAcknowledgementsRequired); !ok || v.El.Bool {
		return fmt.Errorf("TCAcknowledgementsRequired = %v (%v), want false", v.El.Bool, ok)
	}
	return nil
}

// caseTermsGo is the acceptance.
func caseTermsGo() goCertCase {
	r := &termsRun{}
	return goCertCase{
		tc: "CGENTC-GO-1.1", role: roleDUT, origin: termsGoOrigin, daemonArgs: []string{"--tc-required"},
		finalize: func(th *goTH) {
			if r.n != nil {
				_ = r.ctrl.Registry().Decommission(th.ctx, r.n)
			}
		},
		steps: []goStep{
			{n: "1", title: "DUT commissions the TC daemon without an acknowledgement: TermsRequiredError of version 1 after the information step, before ArmFailSafe", run: r.refused},
			{n: "2", title: "DUT commissions it again in the same window with TermsAndConditions{1, 1}: the commissioning succeeds", run: r.accepted},
			{n: "3", title: "DUT reads the acceptance back: TCAcceptedVersion 1, TCAcknowledgements 1, TCAcknowledgementsRequired false", run: r.readBack},
		},
	}
}
