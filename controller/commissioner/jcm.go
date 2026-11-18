// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
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
// (ControllerCommissioningFlow.ts:536-656 at db021da0). The trust
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
	// ContinueAfterUserConsent :63-73), an error with InternalError and
	// the error as the step's cause.
	//
	// It may take as long as a person needs: the flow extends the
	// fail-safe before the call and keeps re-arming it while Consent runs
	// (every [BTPIdleAliveInterval], or half the device's fail-safe when
	// that is shorter). At the device's cumulative fail-safe limit or
	// after a re-arm fails, the stage fails at once with that cause.
	// ctx is Commission's: when it ends the stage fails
	// at once with the context's error, whether or not Consent has
	// returned; a Consent that ignores ctx runs on until it returns and
	// its answer is dropped (review II, R2-P6-fsm-1860, owner decision
	// 215).
	Consent func(ctx context.Context, info TrustInfo) (bool, error)
	// TrustAnchors finds the globally trusted root of the administrator's
	// vendor (the DCL's Operational Trust Anchors). nil fails the Vendor ID
	// Verification (VENDOR_ID_VERIFICATION_FAILED, cause
	// [jcm.ErrNoTrustAnchor]) and Consent is not asked: a missing port
	// must not turn the verification into the device vouching for itself.
	// [jcm.SelfAnchored] is that vouching as an explicit opt-in — the
	// administrator's own RCAC, connectedhomeip's example placeholder
	// (examples/jf-control-app/commands/pairing/PairingCommand.cpp:935-946
	// at 6170af84) — for tests and the example controller (review II,
	// R2-P6-sec-1850, owner decision 190).
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
	t := &jcmTrust{opts: f.opts.JointFabric, info: jcm.NewInfo(), challenge: f.challenge, signer: flowSigner{f}, keepArmed: f.keepFailsafeArmed, log: f.log}
	if err := t.parse(report); err != nil {
		return 0, newError(ErrJointFabric, err.Error(), err)
	}
	if r := t.run(ctx); r.Result != jcm.Success {
		return 0, newError(ErrJointFabric, r.Error(), errors.Join(r, t.cause))
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
	// cause is the error behind a failed stage, joined to the step's
	// error.
	cause error
	// keepArmed keeps the fail-safe armed while Consent runs; stop ends
	// that, and lapsed receives the cause when the fail-safe can be kept
	// no longer (the cumulative limit, a failed re-arm). nil keeps nothing
	// armed (a test of the stages).
	keepArmed func(ctx context.Context) (stop func(), lapsed <-chan error, err error)
	// log receives a reported value skipped for its shape; nil discards.
	log  *slog.Logger
	done bool
	// perform replaces a stage in tests.
	perform func(ctx context.Context, stage jcm.Stage)
}

// errJCMParse is ParseExtraCommissioningInfo's refusal: chip's
// CHIP_ERROR_NOT_FOUND, KEY_NOT_FOUND, CERT_NOT_FOUND and CANCELLED.
var errJCMParse = errors.New("commissioner: the device's administrator information is incomplete")

// logger is t.log, or a discarding one.
func (t *jcmTrust) logger() *slog.Logger {
	if t.log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return t.log
}

// parse is ParseExtraCommissioningInfo's JCM half (:343-362). Every
// reported value is decoded by its shape, one that does not decode absent
// (F-db021da0-15, reportedValue); a value the trust cannot do without is
// then the parse's own "no …" refusal.
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
	for i := range r.Attributes {
		a := &r.Attributes[i]
		if a.Path.Cluster != jfadef.ClusterID || a.Path.Attribute != jfadef.AttrAdministratorFabricIndex {
			continue
		}
		idx, isNull, ok := reportedUint[uint8](t.logger(), "AdministratorFabricIndex", 8, a.Value)
		if !ok {
			continue
		}
		if isNull || idx == 0 { // :99-109
			return fmt.Errorf("%w: AdministratorFabricIndex is null", errJCMParse)
		}
		t.info.AdminFabricIndex, t.info.AdminEndpointID = idx, a.Path.Endpoint
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
	log := t.logger()
	fabricDef := opcredsdef.FabricDescriptorStructDef
	v, ok := r.Value(rootEndpoint, opCredsCluster, opcredsdef.AttrFabrics)
	var fabrics []map[uint32]any
	if ok {
		fabrics, ok = reportedStructList(log, "Fabrics", fabricDef, v)
	}
	if ok {
		found := false
		for _, fd := range fabrics {
			if vvs, has := field(fabricDef, fd, "VidVerificationStatement"); has && vvs != nil { // :158-164
				return fmt.Errorf("%w: a VID Verification Statement is not supported, Joint Fabric requires an ICA", errJCMParse)
			}
			if idx, has := fieldUint[uint8](fabricDef, fd, "FabricIndex"); has && idx == t.info.AdminFabricIndex { // :166-181
				rootKey, _ := fieldBytes(fabricDef, fd, "RootPublicKey")
				if len(rootKey) != 65 {
					return fmt.Errorf("%w: root public key of %d bytes", errJCMParse, len(rootKey))
				}
				t.info.RootPublicKey = slices.Clone(rootKey)
				t.info.AdminVendorID, _ = fieldUint[uint16](fabricDef, fd, "VendorId")
				t.info.AdminFabricID, _ = fieldUint[uint64](fabricDef, fd, "FabricId")
				found = true
				break
			}
		}
		if !found { // :183-186
			return fmt.Errorf("%w: no fabric %d", errJCMParse, t.info.AdminFabricIndex)
		}
	}
	nocDef := opcredsdef.NOCStructDef
	v, ok = r.Value(rootEndpoint, opCredsCluster, opcredsdef.AttrNocs)
	var nocs []map[uint32]any
	if ok {
		nocs, ok = reportedStructList(log, "NOCs", nocDef, v)
	}
	if ok {
		for _, n := range nocs { // :195-217
			if idx, has := fieldUint[uint8](nocDef, n, "FabricIndex"); !has || idx != t.info.AdminFabricIndex {
				continue
			}
			noc, _ := fieldBytes(nocDef, n, "Noc")
			t.info.AdminNOC = slices.Clone(noc)
			icac, has := field(nocDef, n, "Icac")
			if has && icac == nil {
				return fmt.Errorf("%w: the administrator fabric has no ICAC", errJCMParse)
			}
			b, _ := icac.([]byte)
			t.info.AdminICAC = slices.Clone(b)
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
	v, ok := r.Value(rootEndpoint, opCredsCluster, opcredsdef.AttrTrustedRootCertificates)
	var roots []any
	if ok {
		var list any
		list, ok = reportedValue(t.logger(), "TrustedRootCertificates", spec.Type{Kind: spec.KindList, Entry: &spec.Field{Type: spec.Type{Kind: spec.KindBytes}}}, v)
		roots, _ = list.([]any)
	}
	if ok {
		for _, entry := range roots {
			root, _ := entry.([]byte)
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
	default:
		return jcm.StageError
	}
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
			return t.askUserForConsent(ctx)
		case jcm.StageComplete:
			return jcm.Success
		default:
			return jcm.InternalError
		}
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
	if err != nil || !slices.ContainsFunc(noc.Subject.CASEAuthTags, func(c uint32) bool { return c>>16 == uint32(adminCATIdentifier) }) {
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
		// Fail closed, before the device is asked to sign (R2-P6-sec-1850).
		t.cause = jcm.ErrNoTrustAnchor
		return jcm.VendorIDVerificationFailed
	}
	if err := jcm.VerifyVendorID(ctx, t.signer, &t.info, lookup, nil); err != nil {
		t.cause = err
		return jcm.VendorIDVerificationFailed
	}
	return jcm.Success
}

// askUserForConsent asks the host (AskUserForConsent :430-441,
// ContinueAfterUserConsent :63-73).
func (t *jcmTrust) askUserForConsent(ctx context.Context) jcm.Result {
	if t.opts == nil || t.opts.Consent == nil {
		return jcm.TrustVerificationDelegateNotSet
	}
	var lapsed <-chan error
	if t.keepArmed != nil {
		stop, l, err := t.keepArmed(ctx)
		if err != nil {
			t.cause = err
			return jcm.InternalError
		}
		defer stop()
		lapsed = l
	}
	// Consent runs on a goroutine of its own, so the stage ends with ctx
	// even when the host's function does not; its late answer is dropped.
	type answer struct {
		ok  bool
		err error
	}
	answered := make(chan answer, 1)
	go func() {
		ok, err := t.opts.Consent(ctx, t.info)
		answered <- answer{ok, err}
	}()
	select {
	case a := <-answered:
		switch {
		case a.err != nil:
			t.cause = a.err
			return jcm.InternalError
		case !a.ok:
			return jcm.UserDeniedConsent
		}
		return jcm.Success
	case <-ctx.Done():
		t.cause = ctx.Err()
		return jcm.InternalError
	case err := <-lapsed:
		// The fail-safe can be kept no longer: the stage fails now, not a
		// later step (Fable read F5).
		t.cause = err
		return jcm.InternalError
	}
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

// keepFailsafeArmed keeps the fail-safe armed while the host's Consent
// runs: the fail-safe extended first, as the network scan does
// (ensureFailsafeTimerFor), then re-armed periodically by the re-arm timer
// of a BLE commissioning (armInterval: every BTPIdleAliveInterval or half
// the default fail-safe, up to the cumulative limit). On BLE that timer
// runs already and stays after the wait; on IP stop ends it and awaits a
// re-arm in flight. lapsed says why the timer ended on its own. A flow
// that holds no fail-safe (none armed yet — the flow arms at 7.1, before
// this step) keeps nothing (review II, R2-P6-fsm-1860, Fable read F5, F6).
func (f *flow) keepFailsafeArmed(ctx context.Context) (stop func(), lapsed <-chan error, err error) {
	if end, _ := f.failsafeTimes(); end.IsZero() {
		return func() {}, nil, nil
	}
	if err := f.ensureFailsafeTimerFor(ctx, f.defaultFailSafe); err != nil {
		return nil, nil, err
	}
	if ai := f.currentArmInterval(); ai != nil {
		return func() {}, ai.lapsed, nil
	}
	ai := f.startArmInterval(ctx)
	return f.stopArmInterval, ai.lapsed, nil
}
