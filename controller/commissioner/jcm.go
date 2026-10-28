// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/jcm"
	"github.com/SukramJ/go-fabric/cluster/spec"
	jfadef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricadministrator"
	opcredsdef "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/mattercert"
)

// Joint Commissioning (JCM): when the device is a Joint Fabric
// administrator of its own fabric, the commissioner verifies that
// administrator before it issues the device's NOC — its
// AdministratorFabricIndex and endpoint, that fabric's root, NOC and
// ICAC, the administrator CAT, its vendor id — and asks the host for
// consent. A port of connectedhomeip src/controller/jcm
// (DeviceCommissioner.cpp, AutoCommissioner.{h,cpp}) at 6170af84;
// matter.js's commissioning flow has no such step
// (ControllerCommissioningFlow.ts:536-656 at 862ad75f). The trust
// verification's stage machine and the Vendor ID Verification are
// client/jcm's.

// TrustInfo is what the trust verification found about the device's
// administrator (connectedhomeip TrustVerificationInfo).
type TrustInfo = jcm.Info

// JointFabricOptions turns on Joint Commissioning ([Options].JointFabric).
type JointFabricOptions struct {
	// Consent is asked, after the administrator and its vendor id
	// verified, whether to commission it onto the fabric; false aborts
	// with UserDeniedConsent, nil with TrustVerificationDelegateNotSet
	// (AskUserForConsent, DeviceCommissioner.cpp:430-441;
	// ContinueAfterUserConsent :63-73). It must not block long: the
	// fail-safe runs.
	Consent func(info TrustInfo) bool
	// TrustAnchors finds the globally trusted root of the administrator's
	// vendor (the DCL's Operational Trust Anchors). nil is the example
	// controller's placeholder, the administrator's own RCAC
	// (examples/jf-control-app/commands/pairing/PairingCommand.cpp:935-946).
	TrustAnchors jcm.TrustAnchorLookup
	// Progress hears each trust verification stage's result
	// (TrustVerificationDelegate::OnProgressUpdate).
	Progress func(stage jcm.Stage, result jcm.Result)
}

// ErrJointFabric is a Joint Commissioning that failed: the
// administrator's information did not parse, or a trust verification
// stage failed (a [jcm.Failure] in its chain).
var ErrJointFabric = errors.New("commissioner: joint commissioning failed")

// StepJointFabric is the JCM step's name; it runs as 10.2, after
// "OperationalCredentials.DeviceAttestation" 10.1 and before
// "OperationalCredentials.Certificates" 11.1, where connectedhomeip runs
// kJCMTrustVerification after kAttestationRevocationCheck and before
// kSendOpCertSigningRequest (AutoCommissioner.cpp:522-531).
const StepJointFabric = "JointFabric.TrustVerification"

// kAdminCATIdentifier (CASEAuthTag.h:35).
const adminCATIdentifier uint16 = 0xFFFF

// The read paths of a JCM commissioning (jcm/AutoCommissioner.h:56-66):
// AdministratorFabricIndex on every endpoint, the root's Fabrics, NOCs
// and TrustedRootCertificates.
func jcmReadPaths() []im.ConcreteAttributePath {
	return []im.ConcreteAttributePath{
		{Cluster: jfadef.ClusterID, Attribute: jfadef.AttrAdministratorFabricIndex, HasCluster: true, HasAttribute: true},
		rootAttr(opCredsCluster, opcredsdef.AttrFabrics),
		rootAttr(opCredsCluster, opcredsdef.AttrNocs),
		rootAttr(opCredsCluster, opcredsdef.AttrTrustedRootCertificates),
	}
}

// jointFabricTrustVerification is the JCM step: the read, the parse
// (ParseExtraCommissioningInfo, DeviceCommissioner.cpp:330-365) and the
// trust verification (StartJCMTrustVerification :45-61,
// OnTrustVerificationComplete :503-520).
func (f *flow) jointFabricTrustVerification(ctx context.Context) (stepResult, error) {
	report, err := f.interaction().ReadWith(ctx, interaction.ReadRequest{Attributes: jcmReadPaths(), NoFabricFilter: true})
	if err != nil {
		return 0, err
	}
	t := &jcmTrust{opts: f.opts.JointFabric, info: jcm.NewInfo(), challenge: f.challenge, signer: flowSigner{f}}
	if err := t.parse(report); err != nil {
		return 0, newError(ErrJointFabric, err.Error(), err)
	}
	if r := t.run(ctx); r.Result != jcm.Success {
		return 0, newError(ErrJointFabric, r.Error(), r)
	}
	return stepSuccess, nil
}

// jcmTrust is the commissioner's trust verification: chip's
// JCM::DeviceCommissioner as a TrustVerificationStateMachine.
type jcmTrust struct {
	opts      *JointFabricOptions
	info      jcm.Info
	challenge []byte
	signer    jcm.Signer
	machine   *jcm.Machine
	failure   jcm.Failure
	done      bool
	// perform replaces a stage in tests.
	perform func(ctx context.Context, stage jcm.Stage)
}

// errJCMParse is ParseExtraCommissioningInfo's refusal: chip's
// CHIP_ERROR_NOT_FOUND, KEY_NOT_FOUND, CERT_NOT_FOUND and CANCELLED.
var errJCMParse = errors.New("commissioner: the device's administrator information is incomplete")

// parse is ParseExtraCommissioningInfo's JCM half (:343-362).
func (t *jcmTrust) parse(r *interaction.Report) error {
	if err := t.parseAdminFabricIndexAndEndpointID(r); err != nil {
		return err
	}
	if err := t.parseOperationalCredentials(r); err != nil {
		return err
	}
	return t.parseTrustedRoot(r)
}

// parseAdminFabricIndexAndEndpointID takes the endpoint whose
// AdministratorFabricIndex is set; a null or undefined one refuses
// (ParseAdminFabricIndexAndEndpointId :87-136).
func (t *jcmTrust) parseAdminFabricIndexAndEndpointID(r *interaction.Report) error {
	for _, a := range r.Attributes {
		if a.Path.Cluster != jfadef.ClusterID || a.Path.Attribute != jfadef.AttrAdministratorFabricIndex {
			continue
		}
		idx, err := spec.DecodeNullable(spec.DecodeUint[uint8](8))(a.Value)
		if err != nil {
			return fmt.Errorf("%w: AdministratorFabricIndex: %w", errJCMParse, err)
		}
		if idx.Null || idx.Value == 0 { // :99-109
			return fmt.Errorf("%w: AdministratorFabricIndex is null", errJCMParse)
		}
		t.info.AdminFabricIndex, t.info.AdminEndpointID = idx.Value, a.Path.Endpoint
	}
	if t.info.AdminFabricIndex == 0 || t.info.AdminEndpointID == jcm.InvalidEndpointID { // :121-131
		return fmt.Errorf("%w: no JointFabricAdministrator with an AdministratorFabricIndex", errJCMParse)
	}
	return nil
}

// parseOperationalCredentials takes the administrator fabric's root
// public key, vendor id and fabric id from Fabrics and its NOC and ICAC
// from NOCs; a VIDVerificationStatement is refused, as is a fabric
// without an ICAC (ParseOperationalCredentials :138-256).
func (t *jcmTrust) parseOperationalCredentials(r *interaction.Report) error {
	if v, ok := r.Value(rootEndpoint, opCredsCluster, opcredsdef.AttrFabrics); ok {
		fabrics, err := spec.DecodeList(spec.DecodeStruct[opcredsdef.FabricDescriptorStruct])(v)
		if err != nil {
			return fmt.Errorf("%w: Fabrics: %w", errJCMParse, err)
		}
		found := false
		for _, fd := range fabrics {
			if fd.VidVerificationStatement != nil { // :158-164
				return fmt.Errorf("%w: a VID Verification Statement is not supported, Joint Fabric requires an ICA", errJCMParse)
			}
			if fd.FabricIndex == t.info.AdminFabricIndex { // :166-181
				if len(fd.RootPublicKey) != 65 {
					return fmt.Errorf("%w: root public key of %d bytes", errJCMParse, len(fd.RootPublicKey))
				}
				t.info.RootPublicKey = slices.Clone(fd.RootPublicKey)
				t.info.AdminVendorID, t.info.AdminFabricID = fd.VendorId, fd.FabricId
				found = true
				break
			}
		}
		if !found { // :183-186
			return fmt.Errorf("%w: no fabric %d", errJCMParse, t.info.AdminFabricIndex)
		}
	}
	if v, ok := r.Value(rootEndpoint, opCredsCluster, opcredsdef.AttrNocs); ok {
		nocs, err := spec.DecodeList(spec.DecodeStruct[opcredsdef.NOCStruct])(v)
		if err != nil {
			return fmt.Errorf("%w: NOCs: %w", errJCMParse, err)
		}
		for _, n := range nocs { // :195-217
			if n.FabricIndex != t.info.AdminFabricIndex {
				continue
			}
			t.info.AdminNOC = slices.Clone(n.Noc)
			if n.Icac.Null {
				return fmt.Errorf("%w: the administrator fabric has no ICAC", errJCMParse)
			}
			t.info.AdminICAC = slices.Clone(n.Icac.Value)
			break
		}
	}
	switch { // :235-253
	case len(t.info.RootPublicKey) == 0:
		return fmt.Errorf("%w: no root public key", errJCMParse)
	case len(t.info.AdminNOC) == 0:
		return fmt.Errorf("%w: no NOC", errJCMParse)
	case len(t.info.AdminICAC) == 0:
		return fmt.Errorf("%w: no ICAC", errJCMParse)
	}
	return nil
}

// parseTrustedRoot takes the trusted root whose public key is the
// administrator fabric's (ParseTrustedRoot :258-328).
func (t *jcmTrust) parseTrustedRoot(r *interaction.Report) error {
	if v, ok := r.Value(rootEndpoint, opCredsCluster, opcredsdef.AttrTrustedRootCertificates); ok {
		roots, err := spec.DecodeList(spec.DecodeBytes)(v)
		if err != nil {
			return fmt.Errorf("%w: TrustedRootCertificates: %w", errJCMParse, err)
		}
		for _, root := range roots {
			c, err := mattercert.Decode(root) // :279
			if err != nil {
				return fmt.Errorf("%w: trusted root: %w", errJCMParse, err)
			}
			if len(root) > 0 && bytes.Equal(c.PublicKey, t.info.RootPublicKey) { // :291-297
				t.info.AdminRCAC = slices.Clone(root)
				break
			}
		}
	}
	if len(t.info.AdminRCAC) == 0 { // :315-320
		t.info.AdminFabricIndex = 0
		return fmt.Errorf("%w: no trusted root of the administrator fabric", errJCMParse)
	}
	return nil
}

// run performs the stages to their end (StartJCMTrustVerification
// :45-61) and returns the failure, Success when none.
func (t *jcmTrust) run(ctx context.Context) jcm.Failure {
	t.start(ctx, jcm.StageIdle)
	if !t.done {
		return jcm.Failure{Stage: jcm.StageError, Result: jcm.InternalError}
	}
	return t.failure
}

// start builds the machine and finishes the completed stage — chip's
// TrustVerificationStageFinished(kIdle) at the start (:53).
func (t *jcmTrust) start(ctx context.Context, completed jcm.Stage) {
	last := completed
	t.machine = &jcm.Machine{
		Next: nextJCMStage,
		Progress: func(s jcm.Stage, r jcm.Result) {
			last = s
			if t.opts != nil && t.opts.Progress != nil {
				t.opts.Progress(s, r)
			}
		},
		Complete: func(r jcm.Result) { t.done, t.failure = true, jcm.Failure{Stage: last, Result: r} },
	}
	t.machine.Perform = func(s jcm.Stage) {
		if t.perform != nil {
			t.perform(ctx, s)
			return
		}
		t.performStage(ctx, s)
	}
	t.machine.Finished(completed, jcm.Success)
}

// nextJCMStage is GetNextTrustVerificationStage (:476-501).
func nextJCMStage(s jcm.Stage) jcm.Stage {
	switch s {
	case jcm.StageIdle:
		return jcm.StageVerifyingAdministratorInformation
	case jcm.StageVerifyingAdministratorInformation:
		return jcm.StagePerformingVendorIDVerification
	case jcm.StagePerformingVendorIDVerification:
		return jcm.StageAskingUserForConsent
	case jcm.StageAskingUserForConsent:
		return jcm.StageComplete
	}
	return jcm.StageError
}

// performStage is PerformTrustVerificationStage (:443-474).
func (t *jcmTrust) performStage(ctx context.Context, s jcm.Stage) {
	t.machine.Run(s, func() jcm.Result {
		switch s {
		case jcm.StageVerifyingAdministratorInformation:
			return t.verifyAdministratorInformation()
		case jcm.StagePerformingVendorIDVerification:
			return t.performVendorIDVerification(ctx)
		case jcm.StageAskingUserForConsent:
			return t.askUserForConsent()
		case jcm.StageComplete:
			return jcm.Success
		}
		return jcm.InternalError
	})
}

// verifyAdministratorInformation: a valid endpoint and fabric index and a
// NOC with the administrator CAT (VerifyAdministratorInformation
// :367-398).
func (t *jcmTrust) verifyAdministratorInformation() jcm.Result {
	switch {
	case t.info.AdminEndpointID == jcm.InvalidEndpointID: // :371-375
		return jcm.InvalidAdministratorEndpointID
	case t.info.AdminFabricIndex == 0: // :377-381
		return jcm.InvalidAdministratorFabricIndex
	}
	noc, err := mattercert.Decode(t.info.AdminNOC) // :383-390
	if err != nil || !slices.ContainsFunc(noc.Subject.CASEAuthTags, func(c uint32) bool { return uint16(c>>16) == adminCATIdentifier }) {
		return jcm.InvalidAdministratorCAT
	}
	return jcm.Success
}

// performVendorIDVerification verifies the administrator fabric's vendor
// id (PerformVendorIDVerificationProcedure :416-428,
// ContinueAfterVendorIDVerification :75-85).
func (t *jcmTrust) performVendorIDVerification(ctx context.Context) jcm.Result {
	var lookup jcm.TrustAnchorLookup
	if t.opts != nil {
		lookup = t.opts.TrustAnchors
	}
	if lookup == nil {
		rcac := t.info.AdminRCAC
		lookup = func(context.Context, uint16, []byte) ([]byte, error) { return rcac, nil }
	}
	if err := jcm.VerifyVendorID(ctx, t.signer, &t.info, lookup, nil); err != nil {
		return jcm.VendorIDVerificationFailed
	}
	return jcm.Success
}

// askUserForConsent asks the host (AskUserForConsent :430-441,
// ContinueAfterUserConsent :63-73).
func (t *jcmTrust) askUserForConsent() jcm.Result {
	if t.opts == nil || t.opts.Consent == nil {
		return jcm.TrustVerificationDelegateNotSet
	}
	if !t.opts.Consent(t.info) {
		return jcm.UserDeniedConsent
	}
	return jcm.Success
}

// flowSigner sends SignVIDVerificationRequest over the commissioning
// session; the session's attestation challenge is the flow's
// (VendorIdVerificationClient.cpp:145-162, :178-179).
type flowSigner struct{ f *flow }

// SignVIDVerification implements [jcm.Signer].
func (s flowSigner) SignVIDVerification(ctx context.Context, fabricIndex uint8, challenge []byte) (opcredsdef.SignVidVerificationResponse, []byte, error) {
	res, err := s.f.invoke(ctx, opCredsCluster, opcredsdef.CmdSignVidVerificationRequest, opcredsdef.SignVidVerificationRequestRequest{FabricIndex: fabricIndex, ClientChallenge: challenge}, false)
	if err != nil {
		return opcredsdef.SignVidVerificationResponse{}, nil, err
	}
	var resp opcredsdef.SignVidVerificationResponse
	if err := response(res, "SignVIDVerificationResponse", func(n spec.Node) (err error) {
		resp, err = spec.DecodeStruct[opcredsdef.SignVidVerificationResponse](n)
		return err
	}); err != nil {
		return opcredsdef.SignVidVerificationResponse{}, nil, err
	}
	return resp, s.f.challenge, nil
}
