// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jointfabric

import (
	"bytes"
	"context"
	"errors"
	"slices"

	"github.com/SukramJ/go-fabric/client/jcm"
	opcreds "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/secure/mattercert"
)

// errCommissionerInfo is a commissioner whose administrator data does not
// name its administrator fabric, root or chain — connectedhomeip's
// CHIP_ERROR_NOT_FOUND, CHIP_ERROR_CERT_NOT_FOUND and CHIP_ERROR_INTERNAL of
// ReadAdminFabrics, ReadAdminCerts and ReadAdminNOCs.
var errCommissionerInfo = errors.New("jointfabric: the commissioner's administrator data is incomplete")

// commissionee is the administrator's trust verification of a commissioner
// that announced itself: it stores the commissioner's
// JointFabricAdministrator endpoint, reads its AdministratorFabricIndex,
// verifies the vendor id of that fabric — its Fabrics, TrustedRootCertificates
// and NOCs read, the Vendor ID Verification signed — and cross-checks the
// fabric against the accessing one. Mirrors connectedhomeip
// JCMCommissionee.{h,cpp} at 6170af84.
type commissionee struct {
	a               *Administrator
	accessingFabric uint8
	// peer is the commissioner the reach reads (the invoking session);
	// reach is the administrator's at the announcement.
	peer    CommissionerPeer
	reach   CommissionerReach
	info    jcm.Info
	machine *jcm.Machine
	result  jcm.Result
	done    bool

	// next and perform replace the stage order and a stage in tests, as
	// chip's test subclasses override them (TestJCMCommissionee.cpp:102-169).
	next    func(jcm.Stage) jcm.Stage
	perform func(ctx context.Context, stage jcm.Stage)
}

// newCommissionee is the JCMCommissionee constructor (JCMCommissionee.h:54-66).
func (a *Administrator) newCommissionee(accessingFabric uint8, endpointID uint16) *commissionee {
	c := &commissionee{a: a, accessingFabric: accessingFabric, info: jcm.NewInfo(), peer: CommissionerPeer{FabricIndex: accessingFabric}, reach: a.reachPort()}
	c.info.AdminEndpointID = endpointID
	return c
}

// run performs the trust verification to its end and returns its result
// (VerifyTrustAgainstCommissionerAdmin :41-44, OnTrustVerificationComplete
// :153-167).
func (c *commissionee) run(ctx context.Context) jcm.Result {
	c.start(ctx)
	if !c.done {
		return jcm.InternalError
	}
	return c.result
}

func (c *commissionee) start(ctx context.Context) {
	next := c.next
	if next == nil {
		next = nextCommissioneeStage
	}
	c.machine = &jcm.Machine{
		Next:     next,
		Complete: func(r jcm.Result) { c.done, c.result = true, r },
	}
	c.machine.Perform = func(stage jcm.Stage) {
		if c.perform != nil {
			c.perform(ctx, stage)
			return
		}
		c.performStage(ctx, stage)
	}
	c.machine.Start()
}

// nextCommissioneeStage is GetNextTrustVerificationStage (:90-118):
// StoringEndpointID, ReadingCommissionerAdminFabricIndex,
// PerformingVendorIDVerification, CrossCheckingAdministratorIDs, Complete.
func nextCommissioneeStage(s jcm.Stage) jcm.Stage {
	switch s {
	case jcm.StageIdle:
		return jcm.StageStoringEndpointID
	case jcm.StageStoringEndpointID:
		return jcm.StageReadingCommissionerAdminFabricIndex
	case jcm.StageReadingCommissionerAdminFabricIndex:
		return jcm.StagePerformingVendorIDVerification
	case jcm.StagePerformingVendorIDVerification:
		return jcm.StageCrossCheckingAdministratorIDs
	case jcm.StageCrossCheckingAdministratorIDs:
		return jcm.StageComplete
	default:
		return jcm.StageError
	}
}

// performStage is PerformTrustVerificationStage (:120-151).
func (c *commissionee) performStage(ctx context.Context, stage jcm.Stage) {
	c.machine.Run(stage, func() jcm.Result {
		if ctx.Err() != nil { // the fail-safe ended the verification
			return jcm.InternalError
		}
		switch stage {
		case jcm.StageStoringEndpointID:
			return c.storeEndpointID()
		case jcm.StageReadingCommissionerAdminFabricIndex:
			return c.readCommissionerAdminFabricIndex(ctx)
		case jcm.StagePerformingVendorIDVerification:
			return c.performVendorIDVerification(ctx)
		case jcm.StageCrossCheckingAdministratorIDs:
			return c.crossCheckAdministratorIDs(ctx)
		case jcm.StageComplete:
			return jcm.Success
		default:
			return jcm.InternalError
		}
	})
}

// storeEndpointID keeps the commissioner's endpoint; the invalid one is
// InvalidAdministratorEndpointID (StoreEndpointId :169-181).
func (c *commissionee) storeEndpointID() jcm.Result {
	if c.info.AdminEndpointID == jcm.InvalidEndpointID {
		return jcm.InvalidAdministratorEndpointID
	}
	c.a.mu.Lock()
	c.a.peerEndpoint = c.info.AdminEndpointID
	c.a.mu.Unlock()
	return jcm.Success
}

// readCommissionerAdminFabricIndex reads the commissioner's
// AdministratorFabricIndex: a failed read or a null one is
// ReadAdminAttributeFailed, an index outside 1-254
// InvalidAdministratorFabricIndex (ReadCommissionerAdminFabricIndex
// :183-224).
func (c *commissionee) readCommissionerAdminFabricIndex(ctx context.Context) jcm.Result {
	if c.reach == nil {
		return jcm.ReadAdminAttributeFailed
	}
	idx, null, err := c.reach.ReadAdministratorFabricIndex(ctx, c.peer, c.info.AdminEndpointID)
	switch {
	case err != nil, null: // :186-191, :207-211
		return jcm.ReadAdminAttributeFailed
	case idx == 0 || idx == 0xFF: // :195-200, IsValidFabricIndex
		return jcm.InvalidAdministratorFabricIndex
	}
	c.info.AdminFabricIndex = idx
	return jcm.Success
}

// readAdminFabrics takes the root public key, fabric id and vendor id of
// the commissioner's administrator fabric (ReadAdminFabrics :226-261).
func (c *commissionee) readAdminFabrics(ctx context.Context) error {
	fabrics, err := c.reach.ReadFabrics(ctx, c.peer)
	if err != nil {
		return err
	}
	for _, f := range fabrics {
		if f.FabricIndex == c.info.AdminFabricIndex {
			c.info.RootPublicKey = slices.Clone(f.RootPublicKey)
			c.info.AdminFabricID, c.info.AdminVendorID = f.FabricId, f.VendorId
			return nil
		}
	}
	return errCommissionerInfo // :249-253
}

// readAdminCerts takes the trusted root whose public key is the
// administrator fabric's (ReadAdminCerts :342-408).
func (c *commissionee) readAdminCerts(ctx context.Context) error {
	roots, err := c.reach.ReadTrustedRootCertificates(ctx, c.peer)
	if err != nil {
		return err
	}
	for _, r := range roots {
		cert, err := mattercert.Decode(r)
		if err != nil { // :355-360
			return err
		}
		if len(r) > 0 && bytes.Equal(cert.PublicKey, c.info.RootPublicKey) { // :367-377
			c.info.AdminRCAC = slices.Clone(r)
			return nil
		}
	}
	return errCommissionerInfo // :390-394
}

// readAdminNOCs takes the NOC and ICAC of the administrator fabric; an
// empty NOC or a missing ICAC is refused (ReadAdminNOCs :410-479).
func (c *commissionee) readAdminNOCs(ctx context.Context) error {
	nocs, err := c.reach.ReadNOCs(ctx, c.peer)
	if err != nil {
		return err
	}
	for _, n := range nocs {
		if n.FabricIndex != c.info.AdminFabricIndex {
			continue
		}
		if len(n.Noc) == 0 || n.Icac.Null || len(n.Icac.Value) == 0 { // :428-450
			return errCommissionerInfo
		}
		c.info.AdminNOC, c.info.AdminICAC = slices.Clone(n.Noc), slices.Clone(n.Icac.Value)
		return nil
	}
	return errCommissionerInfo // :462-467
}

// fetchCommissionerInfo reads the fabrics, the roots and the NOCs in turn
// (FetchCommissionerInfo :263-295).
func (c *commissionee) fetchCommissionerInfo(ctx context.Context) error {
	if c.reach == nil {
		return errCommissionerInfo
	}
	if err := c.readAdminFabrics(ctx); err != nil {
		return err
	}
	if err := c.readAdminCerts(ctx); err != nil {
		return err
	}
	return c.readAdminNOCs(ctx)
}

// performVendorIDVerification reads the commissioner's administrator data
// and verifies its vendor id; a failure of either is
// VendorIDVerificationFailed, and a failed read stops before the
// verification (PerformVendorIdVerification :297-323,
// OnVendorIdVerificationComplete :83-88).
func (c *commissionee) performVendorIDVerification(ctx context.Context) jcm.Result {
	if err := c.fetchCommissionerInfo(ctx); err != nil {
		return jcm.VendorIDVerificationFailed // :300-308
	}
	lookup := c.a.trustAnchors
	if lookup == nil { // OnLookupOperationalTrustAnchor :71-81
		rcac := c.info.AdminRCAC
		lookup = func(context.Context, uint16, []byte) ([]byte, error) { return rcac, nil }
	}
	if err := jcm.VerifyVendorID(ctx, peerSigner{c.reach, c.peer}, &c.info, lookup, nil); err != nil { // :310-319
		return jcm.VendorIDVerificationFailed
	}
	return jcm.Success
}

// crossCheckAdministratorIDs compares the commissioner's administrator
// fabric with the accessing fabric (CrossCheckAdministratorIds :325-340).
func (c *commissionee) crossCheckAdministratorIDs(ctx context.Context) jcm.Result {
	fabrics, _, _ := c.a.node()
	if fabrics == nil {
		return jcm.InternalError
	}
	f, err := fabrics.GetFabric(ctx, c.accessingFabric) // :330-331
	if err != nil {
		return jcm.InternalError
	}
	return c.validateAdministratorIDsMatch(f.FabricID, f.RootPublicKey)
}

// validateAdministratorIDsMatch: the fabric ids differ, or the root keys
// do, is AdministratorIDMismatched; a key not of 65 bytes InternalError
// (ValidateAdministratorIdsMatch :481-505).
func (c *commissionee) validateAdministratorIDsMatch(accessingFabricID uint64, accessingRootPublicKey []byte) jcm.Result {
	if accessingFabricID != c.info.AdminFabricID { // :484-488
		return jcm.AdministratorIDMismatched
	}
	if len(accessingRootPublicKey) != 65 || len(c.info.RootPublicKey) != 65 { // :490-496
		return jcm.InternalError
	}
	if !bytes.Equal(c.info.RootPublicKey, accessingRootPublicKey) { // :498-502
		return jcm.AdministratorIDMismatched
	}
	return jcm.Success
}

// peerSigner is the reach's SignVIDVerification bound to the peer, the
// jcm.Signer VerifyVendorID sends the request through.
type peerSigner struct {
	reach CommissionerReach
	peer  CommissionerPeer
}

func (s peerSigner) SignVIDVerification(ctx context.Context, fabricIndex uint8, clientChallenge []byte) (opcreds.SignVidVerificationResponse, []byte, error) {
	return s.reach.SignVIDVerification(ctx, s.peer, fabricIndex, clientChallenge)
}
