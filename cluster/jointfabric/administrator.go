// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jointfabric

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/client/jcm"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	jfadef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricadministrator"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/mattercert"
	"github.com/SukramJ/go-fabric/store"
)

// AdministratorClusterID is the JointFabricAdministrator cluster id.
const AdministratorClusterID = jfadef.ClusterID

// Generated types the administrator speaks.
type (
	// AdministratorPort holds the administrator key: its ICAC CSR.
	AdministratorPort = jfadef.AdministratorPort
	// CommissionerReach reads the announcing commissioner.
	CommissionerReach = jfadef.CommissionerReach
	// ICACCSRStatus is an ICACCSRRequest or
	// OpenJointCommissioningWindow cluster status.
	ICACCSRStatus = jfadef.ICACCSRResponseStatusCodeEnum
	// ICACStatus is an AddICAC response status.
	ICACStatus = jfadef.ICACResponseStatusEnum
)

// FabricTable is the node's fabrics and its operational identity on each:
// what the administrator reads of the accessing fabric (connectedhomeip's
// FabricTable). store.Port is one; the node attaches it
// ([Administrator.AttachNode]).
type FabricTable interface {
	GetFabric(ctx context.Context, fabricIndex uint8) (store.FabricRecord, error)
	ListFabrics(ctx context.Context) ([]store.FabricRecord, error)
	GetIdentity(ctx context.Context, fabricIndex uint8) (store.IdentityRecord, error)
}

// The window bounds of a commissioned node
// (CommissioningWindowManager.h:70-74, .cpp:318-330 at 6170af84) and the
// PAKE bounds (kSpake2p_Min/Max_PBKDF_Iterations,
// kSpake2p_Min/Max_PBKDF_Salt_Length, kSpake2p_VerifierSerialized_Length,
// kMaxDiscriminatorValue).
const (
	minWindowSeconds  = 3 * 60
	maxWindowSeconds  = 15 * 60
	minIterations     = 1000
	maxIterations     = 100000
	minSaltLength     = 16
	maxSaltLength     = 32
	verifierLength    = 97
	maxDiscriminator  = 0xFFF
	invalidFabricByte = 0
)

// clusterStatus is a FAILURE carrying a JointFabricAdministrator cluster
// status (AddClusterSpecificFailure,
// joint-fabric-administrator-server.cpp:223-226, :346-349).
type clusterStatus struct {
	code ICACCSRStatus
	msg  string
}

func (e clusterStatus) Error() string                 { return "JointFabricAdministrator: " + e.msg }
func (clusterStatus) MatterStatusCode() im.StatusCode { return im.StatusFailure }
func (e clusterStatus) MatterClusterStatus() uint8    { return uint8(e.code) }
func statusErr(s im.StatusCode, format string, args ...any) error {
	return spec.Errorf(s, "JointFabricAdministrator: "+format, args...)
}

// AdministratorConfig carries the construction parameters of an
// [Administrator].
type AdministratorConfig struct {
	// Port holds the administrator key; without one ICACCSRRequest
	// answers FAILURE and AddICAC's public key check fails, as
	// connectedhomeip without a delegate (:339-340, :421).
	Port AdministratorPort
	// Reach reads the announcing commissioner; without one
	// AnnounceJointFabricAdministrator fails.
	Reach CommissionerReach
	// TrustAnchors finds the globally trusted root of a vendor (the DCL's
	// Operational Trust Anchors). nil is connectedhomeip's placeholder,
	// the commissioner's own RCAC (JCMCommissionee.cpp:71-81, "temporarily
	// return the already known remote admin trusted root certificate").
	TrustAnchors jcm.TrustAnchorLookup
	// Datastore is the endpoint's JointFabricDatastore: when a fabric
	// whose NOC carries the administrator and anchor CATs completes
	// commissioning, the administrator takes it as its
	// AdministratorFabricIndex and the datastore records the anchor.
	Datastore *Datastore
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// Administrator implements [contract.ClusterServer] for
// JointFabricAdministrator: AdministratorFabricIndex (set by the host,
// or taken when the anchor fabric completes commissioning), the joint
// commissioning window, the ICAC CSR and the ICAC check under the
// fail-safe, the trust verification of a commissioner that announces
// itself, and the anchor transfer connectedhomeip answers without effect.
// The rules are connectedhomeip's joint-fabric-administrator-server.cpp
// (:172-449) and JointFabricAdministrator.h at 6170af84; matter.js's
// JointFabricAdministratorServer is the generated behaviour
// (JointFabricAdministratorServer.ts:14 at 862ad75f).
type Administrator struct {
	*spec.Instance
	spec.SelfHeld

	srv          *spec.Server
	port         AdministratorPort
	reach        CommissionerReach
	trustAnchors jcm.TrustAnchorLookup
	datastore    *Datastore

	// mu guards what follows.
	mu sync.Mutex
	// fabrics and window are the node's, attached as the topology is
	// assembled; armed reports the node's fail-safe.
	fabrics FabricTable
	window  wire.WindowController
	armed   func() bool
	// vidVerified is the fabric whose VID verification succeeded
	// (JointFabricAdministrator.h:54-70), 0 none.
	vidVerified uint8
	// peerEndpoint is the announcing commissioner's
	// JointFabricAdministrator endpoint (:49-52, :80).
	peerEndpoint uint16
	// addICACInvoked is the fail-safe's AddICAC flag
	// (FailSafeContext::AddICACCommandHasBeenInvoked).
	addICACInvoked bool
	// announce cancels the AnnounceJointFabricAdministrator in progress
	// (mActiveCommissionee, :102-103).
	announce context.CancelFunc
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                 = (*Administrator)(nil)
	_ contract.ClusterDataVersion            = (*Administrator)(nil)
	_ contract.ClusterAttributeLister        = (*Administrator)(nil)
	_ contract.ClusterCommandLister          = (*Administrator)(nil)
	_ contract.ClusterCommandInvokePrivilege = (*Administrator)(nil)
	_ contract.AttributeChangeNotifier       = (*Administrator)(nil)
	_ spec.AttributeSetter                   = (*Administrator)(nil)
	_ spec.CommandRegistrar                  = (*Administrator)(nil)
)

// NewAdministrator builds a JointFabricAdministrator server.
func NewAdministrator(cfg AdministratorConfig) (*Administrator, error) {
	a := &Administrator{port: cfg.Port, reach: cfg.Reach, trustAnchors: cfg.TrustAnchors, datastore: cfg.Datastore, peerEndpoint: jcm.InvalidEndpointID}
	handlers := map[uint32]spec.CommandHandler{
		jfadef.CmdOpenJointCommissioningWindow:     a.openJointCommissioningWindow,
		jfadef.CmdAnnounceJointFabricAdministrator: a.announceJointFabricAdministrator,
		jfadef.CmdIcaccsrRequest:                   a.icacCSRRequest,
		jfadef.CmdAddIcac:                          a.addICAC,
		jfadef.CmdTransferAnchorRequest:            a.transferAnchorRequest,
		jfadef.CmdTransferAnchorComplete:           a.transferAnchorComplete,
	}
	srv, err := spec.NewServer(jfadef.Definition, spec.Options{}, spec.ServerConfig{Handlers: handlers, DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("jointfabric: %w", err)
	}
	a.srv, a.Instance, a.SelfHeld = srv, srv.Instance, spec.NewSelfHeld(srv)
	return a, nil
}

// AttachNode hands the administrator the node's fabric table and its
// commissioning window; the node calls it as the endpoint joins the
// topology.
func (a *Administrator) AttachNode(fabrics FabricTable, window wire.WindowController) {
	a.mu.Lock()
	a.fabrics, a.window = fabrics, window
	a.mu.Unlock()
}

// AttachFailSafe hands the administrator the node's fail-safe state; the
// node calls it as the endpoint joins the topology. The Breadcrumb is not
// the administrator's to write.
func (a *Administrator) AttachFailSafe(armed func() bool, _ func(uint64)) {
	a.mu.Lock()
	a.armed = armed
	a.mu.Unlock()
}

// OnFailSafeExpired ends an AnnounceJointFabricAdministrator in progress
// and forgets the VID verification, the commissioner's endpoint and the
// AddICAC of the fail-safe. Mirrors OnFailSafeTimerExpired (:298-303) and
// OnPlatformEventHandler (:109-117).
func (a *Administrator) OnFailSafeExpired(context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.announce != nil {
		a.announce()
		a.announce = nil
	}
	a.vidVerified, a.peerEndpoint, a.addICACInvoked = 0, jcm.InvalidEndpointID, false
}

// OnCommissioningComplete ends the fail-safe's AddICAC; a fabric whose
// NOC carries the administrator and anchor CATs, while no
// AdministratorFabricIndex is set, becomes the administrator's fabric
// and, with a Datastore, its anchor: the fabric index, root certificate,
// node id and vendor id. Mirrors the administrator application's
// HandleCommissioningCompleteEvent
// (examples/jf-admin-app/linux/JFAManager.cpp:95-157 at 6170af84); the
// rest of its setup (friendly name, status, default groups, key set and
// admin entry, :159-205)
// stays the host's.
func (a *Administrator) OnCommissioningComplete(ctx context.Context) {
	a.mu.Lock()
	a.addICACInvoked = false
	fabrics := a.fabrics
	a.mu.Unlock()
	if fabrics == nil || a.administratorFabricIndex() != 0 {
		return
	}
	list, err := fabrics.ListFabrics(ctx)
	if err != nil {
		return
	}
	for _, f := range list {
		id, err := fabrics.GetIdentity(ctx, f.FabricIndex)
		if err != nil {
			continue
		}
		cats := nocCATs(id.NOC)
		if !hasCATIdentifier(cats, AdminCATIdentifier) || !hasCATIdentifier(cats, AnchorCATIdentifier) { // :105
			continue
		}
		_ = a.srv.SetAttributes(map[uint32]any{jfadef.AttrAdministratorFabricIndex: f.FabricIndex}) // :107-108
		if d := a.datastore; d != nil {
			_ = d.SetAnchorFabricIndex(ctx, f.FabricIndex) // :113
			_ = d.SetAnchorRootCA(ctx, f.RootCert)         // :117-122
			if f.NodeID != 0 {
				_ = d.SetAnchorNodeID(ctx, f.NodeID) // :131-136
			}
			if f.VendorID != 0 {
				_ = d.SetAnchorVendorID(ctx, f.VendorID) // :144-149
			}
		}
		return
	}
}

// OnFabricRemoved forgets the VID verification of the removed fabric
// (JointFabricAdministrator.h:60-66) and hands the removal to the
// Datastore.
func (a *Administrator) OnFabricRemoved(ctx context.Context, fabricIndex uint8) {
	a.mu.Lock()
	if a.vidVerified == fabricIndex {
		a.vidVerified = 0
	}
	a.mu.Unlock()
	if a.datastore != nil {
		a.datastore.OnFabricRemoved(ctx, fabricIndex)
	}
}

// nocCATs are the CASE Authenticated Tags of a NOC's subject.
func nocCATs(noc []byte) []uint32 {
	c, err := mattercert.Decode(noc)
	if err != nil {
		return nil
	}
	return c.Subject.CASEAuthTags
}

// hasCATIdentifier is CATValues::ContainsIdentifier (CASEAuthTag.h:158-169).
func hasCATIdentifier(cats []uint32, identifier uint16) bool {
	return slices.ContainsFunc(cats, func(c uint32) bool { return c>>16 == uint32(identifier) })
}

// PeerAdministratorEndpoint returns the announcing commissioner's
// JointFabricAdministrator endpoint, jcm.InvalidEndpointID when none
// (GetPeerJFAdminClusterEndpointId, JointFabricAdministrator.h:80).
func (a *Administrator) PeerAdministratorEndpoint() uint16 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.peerEndpoint
}

// VIDVerifiedFabric returns the fabric whose VID verification succeeded,
// 0 when none (WasVidVerificationExecutedForFabric, :67-70).
func (a *Administrator) VIDVerifiedFabric() uint8 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.vidVerified
}

// administratorFabricIndex is the attribute, 0 when null.
func (a *Administrator) administratorFabricIndex() uint8 {
	v, ok := a.srv.MatterRead(jfadef.AttrAdministratorFabricIndex)
	if !ok || v == nil {
		return invalidFabricByte
	}
	i, _ := v.(uint8)
	return i
}

// accessing is the accessing fabric and whether the invoke arrived over
// CASE.
func accessing(ctx context.Context) (fabricIndex uint8, isCASE bool) {
	_, fabricIndex = im.FabricFilterFromContext(ctx)
	return fabricIndex, fabricIndex != 0 && !im.IsPASEFromContext(ctx)
}

func (a *Administrator) node() (FabricTable, wire.WindowController, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	armed := a.armed != nil && a.armed()
	return a.fabrics, a.window, armed
}

// windowOpen reports whether a commissioning window is open.
func windowOpen(w wire.WindowController) bool {
	if r, ok := w.(wire.AnyWindowReporter); ok {
		return r.IsOpen()
	}
	return w.CurrentWindow().Status != wire.WindowStatusClosed
}

// openJointCommissioningWindow opens the joint commissioning window: the
// accessing fabric must be one of the node's (PAKEParameterError), the
// AdministratorFabricIndex set (InvalidAdministratorFabricIndex), the
// fail-safe disarmed and no window open (Busy); the timeout, the
// discriminator and the PAKE parameters inside their bounds
// (INVALID_COMMAND). Mirrors HandleOJCW (:173-237) and
// CommissioningWindowManager::OpenJointCommissioningWindow (:413-419).
func (a *Administrator) openJointCommissioningWindow(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[jfadef.OpenJointCommissioningWindowRequest](fields)
	if !ok {
		return nil, statusErr(im.StatusInvalidCommand, "OpenJointCommissioningWindow payload %T", fields)
	}
	fabricIndex, _ := accessing(ctx)
	fabrics, window, armed := a.node()
	var fabric store.FabricRecord
	var err error
	if fabrics == nil || fabricIndex == 0 {
		err = errors.New("jointfabric: no fabric")
	} else {
		fabric, err = fabrics.GetFabric(ctx, fabricIndex)
	}
	switch {
	case err != nil: // :197
		return nil, clusterStatus{jfadef.ICACCSRResponseStatusCodePakeParameterError, "the accessing fabric is not the node's"}
	case a.administratorFabricIndex() == 0: // :198-202
		return nil, clusterStatus{jfadef.ICACCSRResponseStatusCodeInvalidAdministratorFabricIndex, "AdministratorFabricIndex is null"}
	case armed: // :203
		return nil, clusterStatus{jfadef.ICACCSRResponseStatusCodeBusy, "the fail-safe is armed"}
	case window == nil:
		return nil, statusErr(im.StatusFailure, "no commissioning window")
	case windowOpen(window): // :205
		return nil, clusterStatus{jfadef.ICACCSRResponseStatusCodeBusy, "a commissioning window is open"}
	case req.Iterations < minIterations || req.Iterations > maxIterations, // :206-207
		len(req.Salt) < minSaltLength || len(req.Salt) > maxSaltLength,                             // :208-209
		req.CommissioningTimeout > maxWindowSeconds || req.CommissioningTimeout < minWindowSeconds, // :211-212
		req.Discriminator > maxDiscriminator,                                                       // :213
		len(req.PakePasscodeVerifier) != verifierLength:                                            // :215-216
		return nil, statusErr(im.StatusInvalidCommand, "OpenJointCommissioningWindow parameters out of bounds")
	}
	if err := window.OpenWindow(ctx, wire.OpenWindowParams{ // :217-219
		CommissioningTimeoutSeconds: req.CommissioningTimeout, PAKEPasscodeVerifier: req.PakePasscodeVerifier,
		Discriminator: req.Discriminator, Iterations: req.Iterations, Salt: req.Salt,
		AdminFabricIndex: fabricIndex, AdminVendorID: fabric.VendorID, JointFabric: true,
	}); err != nil {
		return nil, statusErr(im.StatusInvalidCommand, "OpenJointCommissioningWindow: %v", err)
	}
	return nil, nil
}

// announceJointFabricAdministrator verifies the trust of the commissioner
// that announces its JointFabricAdministrator endpoint
// ([commissionee]): its success marks the accessing fabric's VID
// verified and answers SUCCESS, its failure FAILURE. The VID
// verification is forgotten first; the invalid endpoint is a
// ConstraintError, an announcement in progress Busy. Mirrors
// HandleAnnounceJointFabricAdministrator (:239-290) and CleanupAnnounceJFA
// (:292-296).
func (a *Administrator) announceJointFabricAdministrator(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[jfadef.AnnounceJointFabricAdministratorRequest](fields)
	if !ok {
		return nil, statusErr(im.StatusInvalidCommand, "AnnounceJointFabricAdministrator payload %T", fields)
	}
	fabricIndex, _ := accessing(ctx)
	a.mu.Lock()
	a.vidVerified = 0                            // :249-250
	if req.EndpointId == jcm.InvalidEndpointID { // :272
		a.mu.Unlock()
		return nil, statusErr(im.StatusConstraintError, "AnnounceJointFabricAdministrator: invalid endpoint")
	}
	if a.announce != nil { // :276
		a.mu.Unlock()
		return nil, statusErr(im.StatusBusy, "an announcement is in progress")
	}
	run, cancel := context.WithCancel(ctx)
	a.announce = cancel
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		if a.announce != nil {
			a.announce()
			a.announce = nil
		}
		a.mu.Unlock()
	}()
	c := a.newCommissionee(fabricIndex, req.EndpointId)
	result := c.run(run)
	if result != jcm.Success || run.Err() != nil { // :256-266
		return nil, statusErr(im.StatusFailure, "trust verification: %s", result)
	}
	a.mu.Lock()
	a.vidVerified = fabricIndex // :259
	a.mu.Unlock()
	return nil, nil
}

// icacCSRRequest answers the administrator key's CSR: over CASE only
// (INVALID_COMMAND), under the fail-safe (FAILSAFE_REQUIRED), before
// AddICAC (CONSTRAINT_ERROR), after the accessing fabric's VID
// verification (VIDNotVerified) and with an AdministratorFabricIndex
// (InvalidAdministratorFabricIndex). Mirrors HandleICACCSRRequest
// (:305-354).
func (a *Administrator) icacCSRRequest(ctx context.Context, _ any) (any, error) {
	fabricIndex, isCASE := accessing(ctx)
	_, _, armed := a.node()
	a.mu.Lock()
	invoked, verified := a.addICACInvoked, a.vidVerified == fabricIndex && fabricIndex != 0
	a.mu.Unlock()
	switch {
	case !isCASE: // :322-323
		return nil, statusErr(im.StatusInvalidCommand, "ICACCSRRequest outside CASE")
	case !armed: // :325-326
		return nil, statusErr(im.StatusFailsafeRequired, "ICACCSRRequest without the fail-safe")
	case invoked: // :328
		return nil, statusErr(im.StatusConstraintError, "AddICAC was invoked")
	case !verified: // :330-331
		return nil, clusterStatus{jfadef.ICACCSRResponseStatusCodeVidNotVerified, "the accessing fabric's VID is not verified"}
	case a.administratorFabricIndex() == 0: // :333-337
		return nil, clusterStatus{jfadef.ICACCSRResponseStatusCodeInvalidAdministratorFabricIndex, "AdministratorFabricIndex is null"}
	case a.port == nil: // :339
		return nil, statusErr(im.StatusFailure, "no AdministratorPort")
	}
	csr, err := a.port.IcacCsr(ctx) // :340
	if err != nil {
		return nil, statusErr(im.StatusFailure, "IcacCsr: %v", err)
	}
	return jfadef.IcaccsrResponse{StatusCode: jfadef.ICACCSRResponseStatusCodeOk, Icaccsr: &csr}, nil // :342-343
}

// addICAC checks the ICAC the commissioner issued for the administrator
// key: over CASE only (INVALID_COMMAND), under the fail-safe
// (FAILSAFE_REQUIRED), once per fail-safe (CONSTRAINT_ERROR); an ICAC that
// does not chain to the accessing fabric's root, or does not decode, is
// InvalidICAC, one for another key InvalidPublicKey; an ICAC that passes
// answers SUCCESS. Mirrors HandleAddICAC (:356-391) and its checks
// (:393-437). connectedhomeip keeps nothing of the ICAC; neither does the
// server — the host's application installs it.
func (a *Administrator) addICAC(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[jfadef.AddIcacRequest](fields)
	if !ok {
		return nil, statusErr(im.StatusInvalidCommand, "AddICAC payload %T", fields)
	}
	fabricIndex, isCASE := accessing(ctx)
	fabrics, _, armed := a.node()
	switch {
	case !isCASE: // :366-367
		return nil, statusErr(im.StatusInvalidCommand, "AddICAC outside CASE")
	case !armed: // :369-370
		return nil, statusErr(im.StatusFailsafeRequired, "AddICAC without the fail-safe")
	}
	a.mu.Lock()
	if a.addICACInvoked { // :372
		a.mu.Unlock()
		return nil, statusErr(im.StatusConstraintError, "AddICAC was invoked")
	}
	a.addICACInvoked = true // :373
	a.mu.Unlock()
	respond := func(s ICACStatus) (any, error) { return jfadef.IcacResponse{StatusCode: s}, nil }
	var rcac []byte
	if fabrics != nil {
		if f, err := fabrics.GetFabric(ctx, fabricIndex); err == nil {
			rcac = f.RootCert
		}
	}
	if err := verifyICACChain(rcac, req.IcacValue); err != nil { // :375-376
		return respond(jfadef.ICACResponseStatusInvalidIcac)
	}
	var csr []byte
	if a.port != nil {
		csr, _ = a.port.IcacCsr(ctx)
	}
	if err := verifyICACPublicKey(csr, req.IcacValue); err != nil { // :378
		return respond(jfadef.ICACResponseStatusInvalidPublicKey)
	}
	if _, err := mattercert.Decode(req.IcacValue); err != nil { // :380, :433-437
		return respond(jfadef.ICACResponseStatusInvalidIcac)
	}
	return nil, nil // :390
}

// transferAnchorRequest answers as connectedhomeip: SUCCESS without effect
// or response (HandleTransferAnchorRequest :439-443, an empty body).
func (a *Administrator) transferAnchorRequest(context.Context, any) (any, error) { return nil, nil }

// transferAnchorComplete answers SUCCESS without effect
// (HandleTransferAnchorComplete :445-449, an empty body).
func (a *Administrator) transferAnchorComplete(context.Context, any) (any, error) { return nil, nil }

// SetAttributes implements [spec.AttributeSetter]: the host's
// AdministratorFabricIndex.
func (a *Administrator) SetAttributes(values map[uint32]any) error {
	return a.srv.SetAttributes(values)
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (a *Administrator) MatterDataVersion() uint32 { return a.srv.MatterDataVersion() }

// OnAttributeChange implements [contract.AttributeChangeNotifier].
func (a *Administrator) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return a.srv.OnAttributeChange(cb)
}

// MatterRead resolves an attribute.
func (a *Administrator) MatterRead(attrID uint32) (any, bool) { return a.srv.MatterRead(attrID) }

// MatterWrite refuses a write: AdministratorFabricIndex is read-only.
func (a *Administrator) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return a.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke dispatches the commands; a host's handler runs in its
// place ([spec.SelfHeld.Handle]).
func (a *Administrator) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &a.SelfHeld, cmdID, fields)
}
