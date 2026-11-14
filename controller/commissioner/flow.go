// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	netcomm "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	otareq "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	timesync "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/im"
)

// Timing of the flow. Mirrors matter.js
// packages/protocol/src/peer/ControllerCommissioningFlow.ts:289
// (DEFAULT_FAILSAFE_TIME = 1 min), :295 (CASE_RECONNECT_TIMEOUT = 5 min),
// :298 (FAILSAFE_PROCESSING_MARGIN = 10 s) and :301
// (FAILSAFE_MAX_SEND_TIME = 10 s) at 0ceb202d.
const (
	DefaultFailSafeTime      = time.Minute
	CASEReconnectTimeout     = 5 * time.Minute
	FailSafeProcessingMargin = 10 * time.Second
	FailSafeMaxSendTime      = 10 * time.Second
)

// abortDisarmTimeout bounds the ArmFailSafe with expiry 0 the flow sends
// when the caller's context ended after an ArmFailSafe was started
// (flow.disarmAfterAbort): the caller gave the commissioning up, so the
// wait for the device is short and the flow's own. The value is this
// module's choice — neither matter.js nor chip has such a wait — and is
// what the flow counts for the sending of one command, FailSafeMaxSendTime
// (FAILSAFE_MAX_SEND_TIME, ControllerCommissioningFlow.ts:302 at db021da0).
const abortDisarmTimeout = FailSafeMaxSendTime

// rootEndpoint is RootEndpointNumber (:307).
const rootEndpoint uint16 = 0

// initialBreadcrumb is the breadcrumb a flow starts with, lastBreadcrumb's
// initial value (ControllerCommissioningFlow.ts:328 at db021da0).
const initialBreadcrumb uint64 = 1

// attestationNonceSize and csrNonceSize are the 32 random bytes of the
// AttestationRequest and the CSRRequest (:1247, :1394).
const (
	attestationNonceSize = 32
	csrNonceSize         = 32
)

// errUnexpectedResponse is a response the flow cannot use — matter.js
// UnexpectedDataError (:1401-1403), or a missing response payload, which
// matter.js's destructuring of undefined fails on. Neither is a
// CommissioningError, so the failsafe stays armed (:463-465).
var errUnexpectedResponse = errors.New("commissioner: unexpected response from the device")

// stepResult is CommissioningStepResultCode (:164-169).
type stepResult int

const (
	stepSuccess stepResult = iota
	stepFailure
	stepSkipped
	stepStop
)

// step is one CommissioningStep (:184-199). Its reArmFailsafe flag, set
// only on the network steps (:603-620), is the flow's reArmBefore set.
type step struct {
	number, sub int
	name        string
	run         func(ctx context.Context) (stepResult, error)
}

// networkFeature is one NetworkCommissioning instance's FeatureMap.
type networkFeature struct {
	endpoint   uint16
	featureMap uint32
}

// networkStatus is one NetworkCommissioning instance's Networks.
type networkStatus struct {
	endpoint uint16
	networks []netcomm.NetworkInfoStruct
}

// collectedData is CollectedCommissioningData (:202-221) without the OTA
// requestor list, which only the OTA provider step this port leaves out
// reads.
type collectedData struct {
	basicCommissioningInfo       *gencomm.BasicCommissioningInfo
	productName                  string
	networkFeatures              []networkFeature // nil until collected
	networkStatus                []networkStatus  // nil until collected
	successfullyConnected        bool
	rootPartsList                []uint16
	rootServerList               []uint32
	vendorID, productID          uint16
	supportsConcurrentConnection *bool
	fabricIndex                  uint8
	// terms is the device's terms-and-conditions requirement, nil when
	// its root GeneralCommissioning has no TC feature (readTerms).
	terms *termsRequirement
}

// termsRequirement is what the root GeneralCommissioning's TC attributes
// say of the device's terms and conditions: TCMinRequiredVersion,
// TCAcknowledgementsRequired (true while no acceptance satisfies the
// requirement) and TCUpdateDeadline (nil when null or absent).
type termsRequirement struct {
	minVersion uint16
	required   bool
	deadline   *uint32
}

// transitionFunc is the flow's transitionToCase callback (:316-319).
type transitionFunc func(ctx context.Context, addr handshake.PeerAddress, concurrent bool) (*interaction.Interaction, error)

// flow is ControllerCommissioningFlow (:312-2141).
type flow struct {
	c          *Commissioner
	log        *slog.Logger
	ia         *interaction.Interaction
	opts       Options
	fabric     *Fabric
	address    handshake.PeerAddress
	challenge  []byte
	transition transitionFunc
	// hint hands addresses the reconnect probe learned to the running
	// operational connection (peer.Peer.HintAddresses); nil hands none.
	hint    func([]netip.AddrPort)
	steps   []step
	results map[[2]int]stepResult
	// reArmBefore are the steps with reArmFailsafe (:196-198).
	reArmBefore map[[2]int]bool
	// ble is the commissioning channel's type: BLE (Options.Channel) or IP
	// (interaction.channelType, :596).
	ble bool

	// mu guards what the BLE re-arm timer shares with the steps: the
	// failsafe end, the cumulative expiry, the breadcrumb, the
	// interaction and the timer itself.
	mu       sync.Mutex
	armTimer *armInterval

	commissioningExpiry time.Time // zero: undefined
	failSafeEnd         time.Time // zero: undefined
	// armStarted is set from the flow's start — the device armed its
	// fail-safe when the PASE session the flow starts on was established
	// (newFlow) — and when the flow starts an ArmFailSafe on the
	// commissioning session, and cleared when the device confirms
	// CommissioningComplete or a disarm: while it is set the device may
	// hold a fail-safe, whether or not it has answered. failSafeEnd, set
	// by the answer, is what matter.js's bookkeeping reads; this is what
	// the disarm after an abort or a failure reads (disarmAfterAbort,
	// disarmAfterFailure).
	armStarted      bool
	defaultFailSafe time.Duration
	// abortDisarm is abortDisarmTimeout; a test shortens it.
	abortDisarm    time.Duration
	dacKey         *ecdsa.PublicKey
	lastBreadcrumb uint64
	data           collectedData
}

func newFlow(c *Commissioner, ia *interaction.Interaction, opts Options, address handshake.PeerAddress, challenge []byte, transition transitionFunc) *flow {
	f := &flow{
		c: c, log: c.log, ia: ia, opts: opts, fabric: opts.Fabric, address: address,
		challenge: append([]byte(nil), challenge...), transition: transition,
		results:         map[[2]int]stepResult{},
		reArmBefore:     map[[2]int]bool{},
		ble:             opts.Channel != nil,
		defaultFailSafe: DefaultFailSafeTime,
		abortDisarm:     abortDisarmTimeout,
		lastBreadcrumb:  initialBreadcrumb,
		// The flow starts on the PASE session, and the device armed its
		// fail-safe when that session was established (matter.js
		// packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts:55-65
		// at db021da0, "As required by Commissioning Flows any new PASE
		// session needs to arm the failsafe for 60s"; connectedhomeip
		// src/app/server/CommissioningWindowManager.cpp:226-236 at
		// 6170af84, OnSessionEstablished): from here a commissioning that
		// gives up owes the device a disarm, and the disarm ends the PASE
		// session on the device, which accepts no new PASE exchange while
		// it exists (PaseServer.ts:80-82 at db021da0).
		armStarted: true,
	}
	f.initializeSteps()
	return f
}

// initializeSteps is #initializeCommissioningSteps (:536-656): the
// NetworkCommissioning steps on a BLE channel (:595-620) — validation,
// then Wi-Fi and Thread when their credentials are given, each re-arming
// the failsafe first — and, unlike matter.js, also on an IP channel when
// Wi-Fi or Thread credentials are given (a by_design candidate: the
// steps are testable without a radio, and a device that advertises
// neither feature skips them). The OTA provider step only comes with its
// option, which this port does not offer (:648-655).
func (f *flow) initializeSteps() {
	f.steps = []step{
		{0, 1, "GetInitialData", f.getInitialData},
		{7, 1, "GeneralCommissioning.ArmFailsafe", func(ctx context.Context) (stepResult, error) { return f.armFailsafe(ctx, 0) }},
		{8, 1, "GeneralCommissioning.ConfigureRegulatoryInformation", f.configureRegulatoryInformation},
		{8, 2, "TimeSynchronization.SynchronizeTime", f.synchronizeTime},
		{8, 3, StepTermsAndConditions, f.configureTermsAndConditions},
		{10, 1, "OperationalCredentials.DeviceAttestation", f.deviceAttestation},
		{11, 1, "OperationalCredentials.Certificates", f.certificates},
		{15, 1, "AccessControl", f.configureAccessControlLists},
	}
	if f.opts.JointFabric != nil { // jcm.go
		f.steps = append(f.steps, step{10, 2, StepJointFabric, f.jointFabricTrustVerification})
	}
	if f.ble || f.opts.WiFiNetwork != nil || f.opts.ThreadNetwork != nil {
		f.steps = append(f.steps, step{16, 1, "NetworkCommissioning.Validate", f.validateNetwork})
		if f.opts.WiFiNetwork != nil {
			f.steps = append(f.steps, step{16, 2, "NetworkCommissioning.Wifi", f.configureNetworkWifi})
			f.reArmBefore[[2]int{16, 2}] = true
		}
		if f.opts.ThreadNetwork != nil {
			f.steps = append(f.steps, step{16, 3, "NetworkCommissioning.Thread", f.configureNetworkThread})
			f.reArmBefore[[2]int{16, 3}] = true
		}
	} else {
		f.log.Info("matter.controller.commissioning.skipping_network_commissioning", slog.String("reason", "the device is already on IP network (udp)"))
	}
	f.steps = append(f.steps, []step{
		{18, 1, "Reconnect", f.reconnectWithDevice},
		{20, 1, "GeneralCommissioning.Complete", f.completeCommissioning},
		{98, 1, "OperationalCredentials.UpdateFabricLabel", f.updateFabricLabel},
	}...)
}

// sortSteps is #sortSteps (:658-663).
func (f *flow) sortSteps() {
	slices.SortStableFunc(f.steps, func(a, b step) int {
		if a.number != b.number {
			return a.number - b.number
		}
		return a.sub - b.sub
	})
}

func (f *flow) now() time.Time { return f.c.ctx.Clock.Now() }

// execute is executeCommissioning (:378-468): each step in order; after a
// successful step with a failsafe armed, a total past the device's maximum
// cumulative failsafe is a CommissioningTimeoutError (:397-407) and less
// than half the default failsafe left re-arms (:409-423); Stop ends the
// loop (:426-428). A recoverable error is logged and the loop continues
// (:431-436); a CommissioningError or an IM status error disarms the
// failsafe and is returned as a CommissioningError (:437-462); any other
// error is returned as is (:463-465). Ahead of matter.js, a step that
// failed because the caller's context ended (callerAborted) — whichever
// of the two classes its error is — has the fail-safe disarmed on the
// flow's own bounded context (disarmAfterAbort) and its error returned as
// it is; and every other failed step disarms too (disarmAfterFailure),
// whether or not the flow has armed: matter.js disarms only after an
// ArmFailSafe of its own was answered and only for the two classes. No
// retries: a TODO upstream (:443-451).
func (f *flow) execute(ctx context.Context) error {
	f.sortSteps()
	// The BLE re-arm timer ends with the flow (close, :2138-2140).
	defer f.stopArmInterval()
	rearmedAfterPrevious := false
	for _, s := range f.steps {
		f.log.Info("matter.controller.commissioning.step", slog.String("step", fmt.Sprintf("%d.%d", s.number, s.sub)), slog.String("name", s.name))
		f.report(s, StepStarted, nil)
		// A step with reArmFailsafe re-arms first, unless the previous
		// step's bookkeeping just did or the BLE re-arm timer runs
		// (:385-393).
		var err error
		if f.reArmBefore[[2]int{s.number, s.sub}] && !rearmedAfterPrevious && !f.armIntervalRunning() {
			f.log.Debug("matter.controller.commissioning.rearm_before_step", slog.String("name", s.name))
			_, err = f.reArmFailsafeBetweenSteps(ctx)
		}
		rearmedAfterPrevious = false
		var res stepResult
		if err == nil {
			res, err = s.run(ctx)
		}
		if err == nil {
			f.results[[2]int{s.number, s.sub}] = res
			rearmedAfterPrevious, err = f.afterStep(ctx, s)
			if err == nil {
				f.report(s, phaseOf(res), nil)
				if res == stepStop {
					break
				}
				continue
			}
		}
		if ce, ok := err.(*CommissioningError); ok && ce.Kind == errRecoverable { //nolint:errorlint // instanceof RecoverableCommissioningError (:431)
			f.log.Warn("matter.controller.commissioning.step_failed_recoverable", slog.String("name", s.name), slog.Any("err", err))
			f.report(s, StepFailed, err)
			continue
		}
		classed := isCommissioningError(err) || isStatusError(err)
		switch {
		case callerAborted(ctx, err, classed):
			// Ahead of matter.js, whose flow takes no abort signal and
			// rethrows a bare error here (:463-465): the caller gave up.
			// The step's error is returned as it is.
			f.log.Warn("matter.controller.commissioning.aborted", slog.String("name", s.name), slog.Any("err", err))
			f.disarmAfterAbort(ctx)
		case classed:
			f.log.Error("matter.controller.commissioning.step_failed", slog.String("name", s.name), slog.Any("err", err))
			f.disarmAfterFailure(ctx)
			// StatusResponseError.accept rethrows anything else as is
			// (:459, general/src/MatterError.ts:87-92); a status error
			// itself is repacked (:462, util/Error.ts:88-104).
			if se, ok := err.(*interaction.StatusError); ok { //nolint:errorlint // the top-level instanceof of :459
				err = newError(ErrCommissioning, se.Error(), se)
			}
		default:
			// matter.js rethrows without a reset (:463-465); the device
			// would hold its fail-safe, and with it the PASE session, until
			// the fail-safe expires (disarmAfterFailure).
			f.disarmAfterFailure(ctx)
		}
		f.report(s, StepFailed, err)
		return err
	}
	return nil
}

// callerAborted reports whether a step failed because the caller's
// context ended. ctx is the context execute was given, and it has to be
// done: an error that only looks like a context's — a wait of the
// connection's or the device's own that ran out — on a live context is
// the step's failure. With ctx done, an error matter.js's flow would
// rethrow bare (not classed: neither a CommissioningError nor a status
// error) is the abort whatever it is, as before; a classed one is the
// abort when it carries ctx's error — the Reconnect step wraps the
// operational connection's in ErrOperativeConnectionFailed
// (reconnectWithDevice), and that connection gives its wait up with the
// caller's context (peer.Peer.Connect) — and the step's own failure when
// it does not.
func callerAborted(ctx context.Context, err error, classed bool) bool {
	ended := ctx.Err()
	if ended == nil {
		return false
	}
	return !classed || errors.Is(err, ended)
}

// report reports the step through Options.Progress ([Step]); a failed
// step's err is the error the flow returns or, recoverable, goes on after.
func (f *flow) report(s step, phase StepPhase, err error) {
	f.opts.report(uint8(s.number), uint8(s.sub), s.name, phase, err) //nolint:gosec // step numbers are 0-99 (:536-656)
}

// phaseOf is the phase a step's result reports: Stop as Succeeded.
func phaseOf(res stepResult) StepPhase {
	switch res {
	case stepFailure:
		return StepFailed
	case stepSkipped:
		return StepSkipped
	case stepSuccess, stepStop:
	}
	return StepSucceeded
}

// afterStep is the failsafe bookkeeping after a successful step
// (:397-424); it reports whether it re-armed.
func (f *flow) afterStep(ctx context.Context, s step) (bool, error) {
	end, expiry := f.failsafeTimes()
	if end.IsZero() {
		return false, nil
	}
	if !expiry.IsZero() && f.now().After(expiry) {
		f.log.Error("matter.controller.commissioning.too_long", slog.String("name", s.name))
		msg := "Commissioning time exceeds the maximum timeframe"
		if i := f.data.basicCommissioningInfo; i != nil && i.MaxCumulativeFailsafeSeconds != 0 {
			msg += fmt.Sprintf(" of %ds", i.MaxCumulativeFailsafeSeconds)
		}
		return false, newError(ErrCommissioningTimeout, msg, nil)
	}
	if left := end.Sub(f.now()); left < f.defaultFailSafe/2 {
		f.log.Info("matter.controller.commissioning.rearm_failsafe", slog.String("after", s.name), slog.Duration("left", left))
		return f.reArmFailsafeBetweenSteps(ctx)
	}
	return false, nil
}

// reArmFailsafeBetweenSteps re-arms the failsafe between two steps and
// reports whether it did. It skips the re-arm once the commissioning
// session is closed — for example after the device left BLE for its
// operational network, the Reconnect step then continuing over CASE.
// Mirrors matter.js ControllerCommissioningFlow.ts:835-849
// (#reArmFailsafeBetweenSteps), called before a step with reArmFailsafe
// (:385-393) and after a step that left less than half the failsafe
// (:409-423) at db021da0.
func (f *flow) reArmFailsafeBetweenSteps(ctx context.Context) (bool, error) {
	if s := f.interaction().Session(); s != nil && s.IsClosed() {
		f.log.Debug("matter.controller.commissioning.rearm_skipped", slog.String("reason", "session closed"))
		return false, nil
	}
	if _, err := f.armFailsafe(ctx, 0); err != nil {
		return false, err
	}
	return true, nil
}

// failsafeTimes are the failsafe's end and the cumulative expiry.
func (f *flow) failsafeTimes() (end, expiry time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failSafeEnd, f.commissioningExpiry
}

func (f *flow) setFailSafeEnd(t time.Time) {
	f.mu.Lock()
	f.failSafeEnd = t
	f.mu.Unlock()
}

// markArmStarted records that an ArmFailSafe is about to be started on
// the commissioning session (armStarted).
func (f *flow) markArmStarted() {
	f.mu.Lock()
	f.armStarted = true
	f.mu.Unlock()
}

// failsafeMayBeArmed reports whether the device may hold a fail-safe of
// this flow: an ArmFailSafe was started and the device has confirmed
// neither CommissioningComplete nor a disarm since (armStarted).
func (f *flow) failsafeMayBeArmed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.armStarted
}

// failsafeEnded records that the device confirmed the fail-safe's end,
// by CommissioningComplete or by a zero expiry. matter.js then holds no
// end time (ControllerCommissioningFlow.ts:2093 and :1104 at db021da0),
// and an abort has nothing to disarm.
func (f *flow) failsafeEnded() {
	f.mu.Lock()
	f.failSafeEnd = time.Time{}
	f.armStarted = false
	f.mu.Unlock()
}

// breadcrumb is lastBreadcrumb.
func (f *flow) breadcrumb() uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastBreadcrumb
}

// nextBreadcrumb is `this.lastBreadcrumb++`: the current value, then
// incremented.
func (f *flow) nextBreadcrumb() uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := f.lastBreadcrumb
	f.lastBreadcrumb++
	return b
}

// interaction is the current interaction (PASE, then CASE).
func (f *flow) interaction() *interaction.Interaction {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ia
}

// invoke is #invokeCommand (:506-530) on the root endpoint.
func (f *flow) invoke(ctx context.Context, cluster, command uint32, fields spec.Encodable, extended bool) (interaction.Result, error) {
	return f.invokeWith(ctx, cluster, command, fields, interaction.InvokeOptions{ExtendedFailSafeTimeout: extended})
}

// invokeWith is #invokeCommand with the request options (:506-530).
func (f *flow) invokeWith(ctx context.Context, cluster, command uint32, fields spec.Encodable, opts interaction.InvokeOptions) (interaction.Result, error) {
	return f.interaction().Invoke(ctx, im.ConcreteCommandPath{Endpoint: rootEndpoint, Cluster: cluster, Command: command}, fields, opts)
}

// response decodes the fields of a command's response into v.
func response(res interaction.Result, what string, decode func(spec.Node) error) error {
	if !res.HasFields {
		return fmt.Errorf("%w: %s without response fields", errUnexpectedResponse, what)
	}
	if err := decode(res.Fields); err != nil {
		return fmt.Errorf("%w: %s: %w", errUnexpectedResponse, what, err)
	}
	return nil
}

// readValues is #readConcreteAttributeValues (:474-503): one read of root
// endpoint attributes, the values in request order, absent ones not ok.
func (f *flow) readValues(ctx context.Context, paths ...im.ConcreteAttributePath) ([]spec.Node, []bool, error) {
	report, err := f.interaction().Read(ctx, paths...)
	if err != nil {
		return nil, nil, err
	}
	values, ok := make([]spec.Node, len(paths)), make([]bool, len(paths))
	for i, p := range paths {
		values[i], ok[i] = report.Value(p.Endpoint, p.Cluster, p.Attribute)
	}
	return values, ok, nil
}

func rootAttr(cluster, attribute uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: rootEndpoint, Cluster: cluster, Attribute: attribute, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

// ensureGeneralCommissioningSuccess is :712-719, the refusal typed as a
// [*GeneralCommissioningError] that keeps matter.js's message.
func (f *flow) ensureGeneralCommissioningSuccess(what string, code gencomm.CommissioningErrorEnum, debugText string) error {
	f.log.Debug("matter.controller.commissioning.general_commissioning_result", slog.String("context", what), slog.Int("code", int(code)), slog.String("debug", debugText))
	if code == gencomm.CommissioningErrorOk {
		return nil
	}
	return &GeneralCommissioningError{Command: what, Code: code, DebugText: debugText}
}

// ensureOperationalCredentialsSuccess is :674-709.
func (f *flow) ensureOperationalCredentialsSuccess(what string, r nocResponse) error {
	suffix := ""
	if r.debugText != nil && *r.debugText != "" {
		suffix += ", " + *r.debugText
	}
	if r.fabricIndex != nil {
		suffix += fmt.Sprintf(", fabricIndex: %d", *r.fabricIndex)
	}
	f.log.Debug("matter.controller.commissioning.operational_credentials_result", slog.String("context", what), slog.String("status", nocStatusNames[r.status]))
	if r.status == nocStatusOk {
		return nil
	}
	switch {
	case what == "addNoc" && r.status == nocStatusFabricConflict:
		return newError(ErrDeviceAlreadyCommissionedToThisFabric, "Commission error: This device is already commissioned into this fabric. You cannot commission it again.", nil)
	case what == "addNoc" && r.status == nocStatusTableFull:
		return newError(ErrMaximumCommissionedFabricsReached, "Commission error: This device reached the maximum number of fabrics it can be part of. Please remove a fabric before trying to add another one.", nil)
	case what == "updateFabricLabel" && r.status == nocStatusLabelConflict:
		return newError(ErrFabricLabelConflict, fmt.Sprintf("Cannot set fabric label to %q because another fabric on this device already uses this name. Please adjust fabric labels to be unique.", f.fabric.CurrentLabel()), nil)
	}
	return newError(ErrCommissioning, fmt.Sprintf("Commission error for %q: %s (%d)%s", what, nocStatusNames[r.status], r.status, suffix), nil)
}

// enumName is the matter.js enum lookup Enum[value].
func enumName(e *spec.Enum, v uint64) string {
	for i := range e.Values {
		if e.Values[i].Value == v {
			return e.Values[i].Name
		}
	}
	return "undefined"
}

// getInitialData is step 0.1 (:724-833): one read of the fabrics, the root
// descriptor, the vendor and product and the concurrent-connection flag;
// a device without room for a fabric is refused (:759-763); then the
// NetworkCommissioning FeatureMap of every endpoint (the wildcard read of
// :773-784, which also names networks and the OTA requestor's default
// providers — read for the wire's sake, used by steps the IP channel does
// not run). That second read also carries the root GeneralCommissioning's
// FeatureMap and its terms-and-conditions requirement (readTerms), and a
// requirement the options do not meet ends the step with a
// [*TermsRequiredError] — before the flow arms, so the device is left as
// it was found once the failure's disarm has expired the fail-safe the
// PASE session armed (checkTerms, disarmAfterFailure).
func (f *flow) getInitialData(ctx context.Context) (stepResult, error) {
	v, ok, err := f.readValues(ctx,
		rootAttr(opCredsCluster, opCredsAttrSupportedFabrics),
		rootAttr(opCredsCluster, opCredsAttrCommissionedFabrics),
		rootAttr(descriptor.ClusterID, descriptor.AttrPartsList),
		rootAttr(descriptor.ClusterID, descriptor.AttrServerList),
		rootAttr(basicinfo.ClusterID, basicinfo.AttrVendorId),
		rootAttr(basicinfo.ClusterID, basicinfo.AttrProductId),
		rootAttr(basicinfo.ClusterID, basicinfo.AttrProductName),
		rootAttr(gencomm.ClusterID, gencomm.AttrSupportsConcurrentConnection),
	)
	if err != nil {
		return 0, err
	}
	if ok[0] && ok[1] {
		supported, err1 := spec.DecodeUint[uint8](8)(v[0])
		commissioned, err2 := spec.DecodeUint[uint8](8)(v[1])
		if err := errors.Join(err1, err2); err != nil {
			return 0, fmt.Errorf("%w: fabrics: %w", errUnexpectedResponse, err)
		}
		if commissioned >= supported {
			return 0, newError(ErrMaximumCommissionedFabricsReached, fmt.Sprintf("Commissioned fabrics (%d) exceed supported fabrics (%d). Please remove some fabrics before commissioning.", commissioned, supported), nil)
		}
	}
	d := &f.data
	var errs []error
	if ok[2] {
		d.rootPartsList, err = spec.DecodeList(spec.DecodeUint[uint16](16))(v[2])
		errs = append(errs, err)
	}
	if ok[3] {
		d.rootServerList, err = spec.DecodeList(spec.DecodeUint[uint32](32))(v[3])
		errs = append(errs, err)
	}
	if ok[4] {
		d.vendorID, err = spec.DecodeUint[uint16](16)(v[4])
		errs = append(errs, err)
	}
	if ok[5] {
		d.productID, err = spec.DecodeUint[uint16](16)(v[5])
		errs = append(errs, err)
	}
	if ok[6] {
		d.productName, err = spec.DecodeString(v[6])
		errs = append(errs, err)
	}
	if ok[7] {
		var b bool
		b, err = spec.DecodeBool(v[7])
		d.supportsConcurrentConnection = &b
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return 0, fmt.Errorf("%w: initial data: %w", errUnexpectedResponse, err)
	}

	report, err := f.ia.Read(ctx,
		im.ConcreteAttributePath{Cluster: netcomm.ClusterID, Attribute: spec.AttrFeatureMap, HasCluster: true, HasAttribute: true},
		im.ConcreteAttributePath{Cluster: netcomm.ClusterID, Attribute: netcomm.AttrNetworks, HasCluster: true, HasAttribute: true},
		im.ConcreteAttributePath{Cluster: otareq.ClusterID, Attribute: otareq.AttrDefaultOtaProviders, HasCluster: true, HasAttribute: true},
		rootAttr(gencomm.ClusterID, spec.AttrFeatureMap),
		rootAttr(gencomm.ClusterID, gencomm.AttrTcMinRequiredVersion),
		rootAttr(gencomm.ClusterID, gencomm.AttrTcAcknowledgementsRequired),
		rootAttr(gencomm.ClusterID, gencomm.AttrTcUpdateDeadline),
	)
	if err != nil {
		return 0, err
	}
	if d.terms, err = readTerms(report); err != nil {
		return 0, err
	}
	d.networkFeatures = []networkFeature{}
	d.networkStatus = []networkStatus{}
	for i := range report.Attributes {
		a := &report.Attributes[i]
		if a.Path.Cluster != netcomm.ClusterID {
			continue
		}
		switch a.Path.Attribute {
		case spec.AttrFeatureMap:
			fm, err := spec.DecodeUint[uint32](32)(a.Value)
			if err != nil {
				return 0, fmt.Errorf("%w: NetworkCommissioning FeatureMap: %w", errUnexpectedResponse, err)
			}
			d.networkFeatures = append(d.networkFeatures, networkFeature{endpoint: a.Path.Endpoint, featureMap: fm})
		case netcomm.AttrNetworks:
			nets, err := decodeNetworks(a.Value)
			if err != nil {
				return 0, fmt.Errorf("%w: NetworkCommissioning Networks: %w", errUnexpectedResponse, err)
			}
			d.networkStatus = append(d.networkStatus, networkStatus{endpoint: a.Path.Endpoint, networks: nets})
		}
	}
	if err := f.checkTerms(); err != nil {
		return 0, err
	}
	return stepSuccess, nil
}

// readTerms is the device's terms-and-conditions requirement from the
// second read of step 0.1: nil without the root GeneralCommissioning's
// TC feature, whose attributes a device without it does not serve.
// Neither matter.js nor connectedhomeip reads these: matter.js's flow
// leaves the consent a TODO (ControllerCommissioningFlow.ts:570 at
// db021da0), and connectedhomeip's kReadCommissioningInfo reads
// GeneralCommissioning's SupportsConcurrentConnection, Breadcrumb,
// BasicCommissioningInfo, RegulatoryConfig, LocationCapability and
// IsCommissioningWithoutPower and no TC attribute
// (CHIPDeviceController.cpp:2592-2603 at 6170af84) — register row
// BD-Matter-Commissioner-TermsRequirementRead. The four paths join the
// three of the wildcard read, seven in all: a server has to accept no
// more than nine per read (CHIPDeviceController.cpp:2581-2582), which
// the first read's eight would pass.
func readTerms(report *interaction.Report) (*termsRequirement, error) {
	fmNode, ok := report.Value(rootEndpoint, gencomm.ClusterID, spec.AttrFeatureMap)
	if !ok {
		return nil, nil
	}
	fm, err := spec.DecodeUint[uint32](32)(fmNode)
	if err != nil {
		return nil, fmt.Errorf("%w: GeneralCommissioning FeatureMap: %w", errUnexpectedResponse, err)
	}
	if fm&uint32(gencomm.FeatureTermsAndConditions) == 0 {
		return nil, nil
	}
	t := &termsRequirement{required: true}
	var errs []error
	if v, ok := report.Value(rootEndpoint, gencomm.ClusterID, gencomm.AttrTcMinRequiredVersion); ok {
		t.minVersion, err = spec.DecodeUint[uint16](16)(v)
		errs = append(errs, err)
	}
	if v, ok := report.Value(rootEndpoint, gencomm.ClusterID, gencomm.AttrTcAcknowledgementsRequired); ok {
		t.required, err = spec.DecodeBool(v)
		errs = append(errs, err)
	}
	if v, ok := report.Value(rootEndpoint, gencomm.ClusterID, gencomm.AttrTcUpdateDeadline); ok {
		var n spec.Nullable[uint32]
		n, err = spec.DecodeNullable(spec.DecodeUint[uint32](32))(v)
		if err == nil && !n.Null {
			t.deadline = &n.Value
		}
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("%w: GeneralCommissioning terms and conditions: %w", errUnexpectedResponse, err)
	}
	return t, nil
}

// checkTerms fails the commissioning early when the device requires
// terms the options do not acknowledge: its TCAcknowledgementsRequired is
// true — no acceptance it holds satisfies its requirement — and
// [Options.TermsAndConditions] is nil or below TCMinRequiredVersion. The
// device's own verdict would come only at SetTCAcknowledgements or at
// CommissioningComplete (TCAcknowledgementsNotReceived), after AddNOC.
// An acknowledgement at or above the version passes: the bits the device
// requires are not on the wire, so a missing bit is the device's
// RequiredTCNotAccepted at step 8.3, a [*GeneralCommissioningError].
func (f *flow) checkTerms() error {
	t := f.data.terms
	if t == nil || !t.required {
		return nil
	}
	tc := f.opts.TermsAndConditions
	if tc != nil && tc.Version >= t.minVersion {
		return nil
	}
	return &TermsRequiredError{Version: t.minVersion, Deadline: t.deadline}
}

// armFailsafe is step 7.1 and the re-arm (:844-886): on the first arm the
// device's BasicCommissioningInfo gives the default failsafe time and
// starts the cumulative budget (:845-861); the length is the requested
// time, else the default, at most the maximum cumulative failsafe
// (:863-868); ArmFailSafe with the current breadcrumb; the end time kept
// (:881). d 0 is "no time requested".
func (f *flow) armFailsafe(ctx context.Context, d time.Duration) (stepResult, error) {
	if f.data.basicCommissioningInfo == nil {
		v, ok, err := f.readValues(ctx, rootAttr(gencomm.ClusterID, gencomm.AttrBasicCommissioningInfo))
		if err != nil {
			return 0, err
		}
		if !ok[0] {
			return 0, fmt.Errorf("%w: no BasicCommissioningInfo", errUnexpectedResponse)
		}
		var info gencomm.BasicCommissioningInfo
		if err := info.DecodeTLV(v[0]); err != nil {
			return 0, fmt.Errorf("%w: BasicCommissioningInfo: %w", errUnexpectedResponse, err)
		}
		f.data.basicCommissioningInfo = &info
		f.defaultFailSafe = time.Duration(info.FailSafeExpiryLengthSeconds) * time.Second
		started := f.now()
		f.mu.Lock()
		f.commissioningExpiry = started.Add(time.Duration(info.MaxCumulativeFailsafeSeconds) * time.Second)
		f.mu.Unlock()
	}
	maxExpiry := f.defaultFailSafe
	if i := f.data.basicCommissioningInfo; i != nil {
		maxExpiry = time.Duration(i.MaxCumulativeFailsafeSeconds) * time.Second
	}
	length := d
	if length == 0 {
		length = f.defaultFailSafe
	}
	length = min(length, maxExpiry)
	// From here the device may arm whatever becomes of the invoke — its
	// request queued, retransmitted, answered late or not at all — so an
	// abort owes it a disarm (disarmAfterAbort). The read above sends no
	// command: an abort during it owes nothing.
	f.markArmStarted()
	res, err := f.invoke(ctx, gencomm.ClusterID, gencomm.CmdArmFailSafe, gencomm.ArmFailSafeRequest{
		Breadcrumb: f.breadcrumb(), ExpiryLengthSeconds: uint16(length / time.Second), //nolint:gosec // bounded by MaxCumulativeFailsafeSeconds, a uint16
	}, false)
	if err != nil {
		return 0, err
	}
	var resp gencomm.ArmFailSafeResponse
	if err := response(res, "ArmFailSafeResponse", resp.DecodeTLV); err != nil {
		return 0, err
	}
	if err := f.ensureGeneralCommissioningSuccess("armFailSafe", resp.ErrorCode, resp.DebugText); err != nil {
		return 0, err
	}
	f.setFailSafeEnd(f.now().Add(length))
	return stepSuccess, nil
}

// failSafeTimeLeft is :888-893.
func (f *flow) failSafeTimeLeft() time.Duration {
	end, _ := f.failsafeTimes()
	if end.IsZero() {
		return 0
	}
	return max(end.Sub(f.now()), 0)
}

// ensureFailsafeTimerFor is :895-951: the failsafe must outlive the wait
// for a response of maxProcessingTime — the interaction's maximum peer
// response time with the peer's sending time, at most maxProcessingTime
// plus FailSafeMaxSendTime — plus FailSafeProcessingMargin; on BLE the
// re-arm timer restarts (:905-938); with less left it re-arms for that,
// at least the default failsafe time.
func (f *flow) ensureFailsafeTimerFor(ctx context.Context, maxProcessingTime time.Duration) error {
	responseWait := min(f.interaction().MaximumPeerResponseTime(maxProcessingTime, true), maxProcessingTime+FailSafeMaxSendTime)
	minFailsafe := responseWait + FailSafeProcessingMargin
	if f.ble {
		f.startArmInterval(ctx)
	}
	if left := f.failSafeTimeLeft(); left < minFailsafe {
		f.log.Debug("matter.controller.commissioning.failsafe_short", slog.Duration("left", left), slog.Duration("rearm_for", minFailsafe))
		_, err := f.armFailsafe(ctx, max(minFailsafe, f.defaultFailSafe))
		return err
	}
	f.log.Debug("matter.controller.commissioning.failsafe_long_enough", slog.Duration("left", f.failSafeTimeLeft()))
	return nil
}

// resetFailsafeTimer is #resetFailsafeTimer (:1092-1108 at db021da0):
// ArmFailSafe with 0 when a failsafe end time is known — the device
// answered an ArmFailSafe — the response not checked, a failure only
// logged.
func (f *flow) resetFailsafeTimer(ctx context.Context) {
	if end, _ := f.failsafeTimes(); end.IsZero() {
		return
	}
	f.expireFailsafe(ctx)
}

// expireFailsafe sends #resetFailsafeTimer's request (:1095-1103 at
// db021da0): ArmFailSafe with expiry 0 and the current breadcrumb, the
// response not checked, a failure only logged (:1105-1107) — the flow
// then still counts the fail-safe.
func (f *flow) expireFailsafe(ctx context.Context) {
	if _, err := f.invoke(ctx, gencomm.ClusterID, gencomm.CmdArmFailSafe, gencomm.ArmFailSafeRequest{Breadcrumb: f.breadcrumb(), ExpiryLengthSeconds: 0}, false); err != nil {
		f.log.Error("matter.controller.commissioning.reset_failsafe_failed", slog.Any("err", err))
		return
	}
	f.failsafeEnded()
}

// disarmAfterAbort expires the fail-safe when the caller's context ended —
// cancelled or past its deadline — while the device may hold one of this
// flow's (from the PASE session's establishment on, newFlow) and the
// session still stands; without it the device refuses a new commissioning
// until the fail-safe runs out. Ahead
// of matter.js: its flow takes no abort signal (the constructor,
// packages/protocol/src/peer/ControllerCommissioningFlow.ts:333-351 at
// db021da0; the commissioner hands its AbortSignal to the PASE attempts
// only, ControllerCommissioner.ts:194-195, :277) and rethrows an error
// that is neither a CommissioningError nor a status error without the
// reset (:463-465). Mirrors connectedhomeip
// src/controller/CHIPDeviceController.cpp:2219-2247 at 6170af84
// (DeviceCommissioner::CleanupCommissioning: on CHIP_ERROR_CANCELLED,
// which StopPairing reports at :1225-1226, ArmFailSafe with
// expiryLengthSeconds 0, :2164-2170, over the session the commissioning
// holds, :2224-2225, a failure ignored, :2233-2236).
//
// The disarm is owed from the PASE session's establishment, which armed
// the device's fail-safe, and for an ArmFailSafe that was started, not
// only for one that was answered (armStarted, set in newFlow and in
// armFailsafe before the invoke):
// the caller's context ends in a wait, and the wait for that answer is
// one — the request may have armed the device, may still be on its way,
// or may never have left, and the flow cannot tell which. A device that
// holds no fail-safe answers the disarm with success and no effect
// (matter.js
// packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts:96-98
// at db021da0). connectedhomeip does not ask either: its branch sends
// whatever stage was cancelled (:2219 tests the completion status alone,
// and a failed stage goes to kCleanup whatever it was,
// src/controller/AutoCommissioner.cpp:465-468).
//
// Two differences from chip, all this module's. chip sends in the
// background and returns; here the send
// is awaited, because Commission closes the PASE session as soon as the
// flow returns and the command would not leave. It is awaited on a
// context of the flow's own — the caller's has ended — for at most
// abortDisarmTimeout. And the request is the one #resetFailsafeTimer
// sends (:1095-1103), with the current breadcrumb where chip sends 0.
//
// The BLE re-arm timer stops first, or its next period would arm the
// fail-safe again behind the disarm. Once the device has confirmed
// CommissioningComplete or a disarm nothing is owed and nothing is sent
// (failsafeEnded); on a closed session nothing can be.
//
// What it does not cover: the aborted ArmFailSafe is not retransmitted
// (exchange.Exchange.Send ends the retransmissions with its context), but
// a datagram of it still on its way can reach the device behind the
// disarm and arm it; so can a re-arm of the BLE timer already in flight,
// which runs beside the steps on a context the abort does not end.
func (f *flow) disarmAfterAbort(ctx context.Context) {
	own, cancel := context.WithTimeout(context.WithoutCancel(ctx), f.abortDisarm)
	defer cancel()
	f.disarmIfMayBeArmed(own)
}

// disarmAfterFailure expires the fail-safe after a step that failed for a
// reason of its own — the caller's context did not end it (callerAborted)
// — and so ends the PASE session on the device: the device's rollback
// closes it (matter.js packages/protocol/src/common/FailsafeContext.ts:277-279
// at db021da0), and until then the device accepts no new PASE exchange
// (PaseServer.ts:80-82 at db021da0). With a fail-safe end known — the
// device answered an ArmFailSafe of the flow's — it is matter.js's
// #resetFailsafeTimer (:457, :1092-1108), on a context that outlives
// ctx: matter.js's reset has no abort signal (#resetFailsafeTimer,
// #invokeCommand), and a step that failed for a reason of its own while
// ctx ended would otherwise leave the device holding its fail-safe until
// it expires; the exchange's own response timeout bounds it.
//
// Ahead of matter.js otherwise, which resets only with that end known and
// only after a CommissioningError or a status error, and rethrows any
// other error without it (:463-465): while the device may hold a
// fail-safe (failsafeMayBeArmed — from the PASE session's establishment
// on), the flow disarms after a failure of either kind, as connectedhomeip
// does after a failure before its network setup stages
// (src/controller/CHIPDeviceController.cpp:2272-2277 at 6170af84, "we
// also need to force a reset on the failsafe so we can start fresh on the
// next attempt"); matter.js's flow notes that a commissioner restarting
// from step 2 may expire the fail-safe at once or else waits for it to
// expire before the device accepts PASE again
// (ControllerCommissioningFlow.ts:453-456 at db021da0). That disarm is bounded by ctx and the exchange's response
// timeout, or — ctx having ended — by the flow's own context as after an
// abort; it is skipped on a closed session.
func (f *flow) disarmAfterFailure(ctx context.Context) {
	if end, _ := f.failsafeTimes(); !end.IsZero() {
		f.resetFailsafeTimer(context.WithoutCancel(ctx))
		return
	}
	if ctx.Err() != nil {
		f.disarmAfterAbort(ctx)
		return
	}
	f.disarmIfMayBeArmed(ctx)
}

// disarmIfMayBeArmed is the disarm of disarmAfterAbort and
// disarmAfterFailure on ctx: the BLE re-arm timer stopped first, nothing
// sent while the device cannot hold a fail-safe of this flow or on a
// closed session.
func (f *flow) disarmIfMayBeArmed(ctx context.Context) {
	f.stopArmInterval()
	if !f.failsafeMayBeArmed() {
		return
	}
	if s := f.interaction().Session(); s != nil && s.IsClosed() {
		f.log.Debug("matter.controller.commissioning.abort_disarm_skipped", slog.String("reason", "session closed"))
		return
	}
	f.expireFailsafe(ctx)
}

// configureRegulatoryInformation is step 8.1 (:1102-1188): only when a
// NetworkCommissioning instance has the Wi-Fi or Thread feature
// (:1109-1112); the device's LocationCapability, the option when it is
// IndoorOutdoor (:1115-1133); SetRegulatoryConfig with the breadcrumb,
// incremented after (:1141); a ValueOutsideRange for a country other than
// "XX" retried once with "XX" (:1150-1175).
func (f *flow) configureRegulatoryInformation(ctx context.Context) (stepResult, error) {
	if f.data.networkFeatures == nil {
		return 0, newError(ErrCommissioning, "Initial information collection failed. No network features collected. This should never happen.", nil)
	}
	radio := slices.ContainsFunc(f.data.networkFeatures, func(n networkFeature) bool {
		return n.featureMap&uint32(netcomm.FeatureWiFiNetworkInterface|netcomm.FeatureThreadNetworkInterface) != 0
	})
	if !radio {
		return stepSkipped, nil
	}
	v, ok, err := f.readValues(ctx, rootAttr(gencomm.ClusterID, gencomm.AttrLocationCapability))
	if err != nil {
		return 0, err
	}
	if !ok[0] {
		return 0, fmt.Errorf("%w: no LocationCapability", errUnexpectedResponse)
	}
	capability, err := spec.DecodeUint[uint8](8)(v[0])
	if err != nil {
		return 0, fmt.Errorf("%w: LocationCapability: %w", errUnexpectedResponse, err)
	}
	location := gencomm.RegulatoryLocationTypeEnum(capability)
	if location == gencomm.RegulatoryLocationTypeIndoorOutdoor {
		location = *f.opts.RegulatoryLocation
	} else {
		f.log.Debug("matter.controller.commissioning.fixed_regulatory_location", slog.Int("location", int(location)))
	}
	country := f.opts.RegulatoryCountryCode
	breadcrumb := f.nextBreadcrumb()
	res, err := f.invoke(ctx, gencomm.ClusterID, gencomm.CmdSetRegulatoryConfig, gencomm.SetRegulatoryConfigRequest{
		Breadcrumb: breadcrumb, NewRegulatoryConfig: location, CountryCode: country,
	}, true)
	if err != nil {
		return 0, err
	}
	var resp gencomm.SetRegulatoryConfigResponse
	if err := response(res, "SetRegulatoryConfigResponse", resp.DecodeTLV); err != nil {
		return 0, err
	}
	if resp.ErrorCode == gencomm.CommissioningErrorValueOutsideRange && country != "XX" {
		f.log.Debug("matter.controller.commissioning.country_code_fallback", slog.String("country", country))
		country = "XX"
		res, err = f.invoke(ctx, gencomm.ClusterID, gencomm.CmdSetRegulatoryConfig, gencomm.SetRegulatoryConfigRequest{
			Breadcrumb: f.breadcrumb(), NewRegulatoryConfig: location, CountryCode: country,
		}, true)
		if err != nil {
			return 0, err
		}
		if err := response(res, "SetRegulatoryConfigResponse", resp.DecodeTLV); err != nil {
			return 0, err
		}
	}
	if err := f.ensureGeneralCommissioningSuccess("setRegulatoryConfig", resp.ErrorCode, resp.DebugText); err != nil {
		return 0, err
	}
	return stepSuccess, nil
}

// StepTermsAndConditions is the step that acknowledges the device's terms
// and conditions ([Options.TermsAndConditions]); it has no matter.js
// counterpart.
const StepTermsAndConditions = "GeneralCommissioning.ConfigureTCAcknowledgements"

// configureTermsAndConditions is connectedhomeip's
// kConfigureTCAcknowledgments stage (AutoCommissioner.cpp:485-487, between
// the regulatory configuration and the UTC time; CHIPDeviceController.cpp:3946-3970
// at 6170af84) — numbered 8.3 here because 8.2 keeps matter.js's
// SynchronizeTime stub, which does nothing: skipped without the acknowledgement, otherwise
// SetTCAcknowledgements with the acknowledged version and bits, whose
// error code fails the commissioning as any GeneralCommissioning
// command's does.
func (f *flow) configureTermsAndConditions(ctx context.Context) (stepResult, error) {
	tc := f.opts.TermsAndConditions
	if tc == nil {
		return stepSkipped, nil
	}
	res, err := f.invoke(ctx, gencomm.ClusterID, gencomm.CmdSetTcAcknowledgements, gencomm.SetTcAcknowledgementsRequest{
		TcVersion: tc.Version, TcUserResponse: tc.Acknowledgements,
	}, false)
	if err != nil {
		return 0, err
	}
	var resp gencomm.SetTcAcknowledgementsResponse
	if err := response(res, "SetTCAcknowledgementsResponse", resp.DecodeTLV); err != nil {
		return 0, err
	}
	if err := f.ensureGeneralCommissioningSuccess("setTCAcknowledgements", resp.ErrorCode, ""); err != nil {
		return 0, err
	}
	return stepSuccess, nil
}

// synchronizeTime is step 8.2 (:1199-1211), a stub upstream: Skipped.
func (f *flow) synchronizeTime(context.Context) (stepResult, error) {
	if slices.Contains(f.data.rootServerList, timesync.ClusterID) {
		f.log.Debug("matter.controller.commissioning.time_synchronization_supported")
	}
	return stepSkipped, nil
}

// configureAccessControlLists is step 15.1 (:1515-1522): the entry AddNOC
// installs is enough; Skipped.
func (f *flow) configureAccessControlLists(context.Context) (stepResult, error) {
	return stepSkipped, nil
}

// random returns n random bytes.
func (f *flow) random(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := io.ReadFull(f.c.ctx.Rand, b)
	return b, err
}

// deviceAttestation is step 10.1 (:1218-1338): the DAC and the PAI
// (CertificateChainRequest), the AttestationRequest with a 32-byte nonce,
// attestation.Validate; a validation error becomes one error finding — the
// attestation.Error's check, else ValidationError (:1286-1303); the
// policy decides (:1305-1319); the DAC key kept, parsed from the DAC when
// the policy accepted a failed validation (:1321-1331).
func (f *flow) deviceAttestation(ctx context.Context) (stepResult, error) {
	chain := make([][]byte, 2)
	for i, kind := range []uint8{certificateTypeDAC, certificateTypePAI} {
		res, err := f.invoke(ctx, opCredsCluster, cmdCertificateChainRequest, certificateChainRequest(kind), true)
		if err != nil {
			return 0, err
		}
		if err := response(res, "CertificateChainResponse", func(n spec.Node) (err error) {
			chain[i], err = decodeCertificateChainResponse(n)
			return err
		}); err != nil {
			return 0, err
		}
	}
	nonce, err := f.random(attestationNonceSize)
	if err != nil {
		return 0, err
	}
	res, err := f.invoke(ctx, opCredsCluster, cmdAttestationRequest, octetsRequest(nonce), true)
	if err != nil {
		return 0, err
	}
	var att signedElements
	if err := response(res, "AttestationResponse", func(n spec.Node) (err error) {
		att, err = decodeSignedElements(n, "AttestationResponse")
		return err
	}); err != nil {
		return 0, err
	}

	var findings []attestation.Finding
	var baseErr error
	result, err := attestation.Validate(
		attestation.Context{Challenge: f.challenge, Store: f.c.ctx.Attestation},
		attestation.Data{
			DAC: chain[0], PAI: chain[1], Elements: att.elements, Signature: att.signature, Nonce: nonce,
			VendorID: f.data.vendorID, ProductID: f.data.productID,
		},
	)
	if err != nil {
		baseErr = err
		var ae *attestation.Error
		if errors.As(err, &ae) {
			findings = []attestation.Finding{{Level: attestation.LevelError, Check: ae.Check, Message: ae.Message}}
		} else {
			findings = []attestation.Finding{{Level: attestation.LevelError, Check: attestation.ValidationError, Message: err.Error()}}
		}
	} else {
		findings = result.Findings
		f.dacKey = result.DACPublicKey
	}

	if len(findings) > 0 {
		proceed, reason, perr := f.opts.OnAttestationFailure.Resolve(findings, f.log)
		if perr != nil {
			return 0, perr
		}
		if !proceed {
			if baseErr == nil {
				baseErr = newError(ErrCommissioning, fmt.Sprintf("Device attestation produced %d finding(s) and was rejected by policy", len(findings)), nil)
			}
			if reason != "" {
				return 0, attestationRejected{newError(ErrCommissioning, reason, baseErr)}
			}
			return 0, attestationRejected{baseErr}
		}
		f.log.Info("matter.controller.commissioning.attestation_verified", slog.Int("accepted_findings", len(findings)))
	} else {
		f.log.Info("matter.controller.commissioning.attestation_verified")
	}

	if f.dacKey == nil {
		key, err := dacKeyOf(chain[0])
		if err != nil {
			return 0, newError(ErrCommissioning, "Device attestation was accepted but the DAC cannot be parsed, so the CSR signature cannot be verified", err)
		}
		f.dacKey = key
	}
	return stepSuccess, nil
}

// dacKeyOf reads the DAC's EC P-256 key (Dac.fromAsn1, :1324).
func dacKeyOf(der []byte) (*ecdsa.PublicKey, error) {
	c, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	k, ok := c.PublicKey.(*ecdsa.PublicKey)
	if !ok || k.Curve != elliptic.P256() {
		return nil, errors.New("commissioner: DAC public key must be EC P-256")
	}
	return k, nil
}

// certificates is step 11.1 (:1388-1468): the CSRRequest with a 32-byte
// nonce; empty elements or signature is unexpected data (:1401-1403); the
// signature over elements ‖ challenge against the DAC key (:1405-1416);
// the CSR's key (:1418-1419); AddTrustedRootCertificate, its answer unused
// (:1421-1433); the
// NOC (:1435-1439); AddNOC (:1441-1457) checked (:1459); the fabric index
// kept (:1461-1462).
func (f *flow) certificates(ctx context.Context) (stepResult, error) {
	nonce, err := f.random(csrNonceSize)
	if err != nil {
		return 0, err
	}
	res, err := f.invoke(ctx, opCredsCluster, cmdCSRRequest, octetsRequest(nonce), true)
	if err != nil {
		return 0, err
	}
	var csr signedElements
	if err := response(res, "CsrResponse", func(n spec.Node) (err error) {
		csr, err = decodeSignedElements(n, "CsrResponse")
		return err
	}); err != nil {
		return 0, err
	}
	if len(csr.elements) == 0 || len(csr.signature) == 0 {
		return 0, fmt.Errorf("%w: Invalid response from device", errUnexpectedResponse)
	}
	if f.dacKey != nil && !verifyRaw(f.dacKey, slices.Concat(csr.elements, f.challenge), csr.signature) {
		return 0, newError(ErrCommissioning, "CSR signature verification failed against DAC public key", nil)
	}
	der, echoed, err := decodeCertSigningRequest(csr.elements)
	if err != nil {
		return 0, fmt.Errorf("%w: NOCSRElements: %w", errUnexpectedResponse, err)
	}
	// Ahead of matter.js: the echoed nonce must be the one sent. matter.js
	// draws the nonce (ControllerCommissioningFlow.ts:1412 at db021da0)
	// and never reads it back; connectedhomeip refuses a mismatch
	// (DefaultDeviceAttestationVerifier.cpp:868-870 at 6170af84,
	// VerifyNodeOperationalCSRInformation). See notes/parity/by_design.md
	// BD-chip-Commissioner-CsrNonceEchoed.
	if !bytes.Equal(echoed, nonce) {
		return 0, newError(ErrCommissioning, "CSR nonce mismatch: the device echoed a nonce other than the one sent", nil)
	}
	operationalKey, err := publicKeyFromCSR(der)
	if err != nil {
		return 0, err
	}
	if _, err := f.invoke(ctx, opCredsCluster, cmdAddTrustedRootCertificate, octetsRequest(f.fabric.CA.RootCert()), true); err != nil {
		return 0, err
	}
	noc, err := f.fabric.CA.GenerateNOC(operationalKey, f.fabric.FabricID, f.address.NodeID, f.opts.CATs)
	if err != nil {
		return 0, err
	}
	res, err = f.invoke(ctx, opCredsCluster, cmdAddNOC, addNOCRequest{
		noc: noc, icac: f.fabric.CA.ICACCert(), ipk: f.fabric.IPK,
		caseAdminSubject: f.fabric.RootNodeID, adminVendorID: f.fabric.RootVendorID,
	}, true)
	if err != nil {
		return 0, err
	}
	var nr nocResponse
	if err := response(res, "NocResponse", func(n spec.Node) (err error) {
		nr, err = decodeNOCResponse(n)
		return err
	}); err != nil {
		return 0, err
	}
	if err := f.ensureOperationalCredentialsSuccess("addNoc", nr); err != nil {
		return 0, err
	}
	if nr.fabricIndex != nil {
		f.data.fabricIndex = *nr.fabricIndex
	}
	return stepSuccess, nil
}

// publicKeyFromCSR is Certificate.getPublicKeyFromCsr
// (certificate/kinds/Certificate.ts:782-843): version 0, an EC P-256 key,
// ecdsa-with-SHA256, the CSR's own signature verified; the key as its
// 65-byte uncompressed point.
func publicKeyFromCSR(der []byte) ([]byte, error) {
	req, err := x509.ParseCertificateRequest(der)
	if err != nil {
		return nil, fmt.Errorf("%w: CSR: %w", errUnexpectedResponse, err)
	}
	if req.Version != 0 {
		return nil, fmt.Errorf("%w: unsupported CSR version %d", errUnexpectedResponse, req.Version)
	}
	key, ok := req.PublicKey.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("%w: unsupported public key in CSR", errUnexpectedResponse)
	}
	if req.SignatureAlgorithm != x509.ECDSAWithSHA256 {
		return nil, fmt.Errorf("%w: unsupported signature algorithm in CSR", errUnexpectedResponse)
	}
	if err := req.CheckSignature(); err != nil {
		return nil, fmt.Errorf("%w: CSR signature: %w", errUnexpectedResponse, err)
	}
	return key.Bytes()
}

// reconnectWithDevice is step 18.1 (:2000-2052): a concurrent flow first
// makes the failsafe last the CASE reconnect, a failure only logged
// (:2011-2017); the transition (:2022-2033), its error an
// OperativeConnectionFailedError; none — FinalizeCommissioning took over —
// stops the flow (:2035-2041); else the CASE interaction replaces the PASE
// one (:2043-2044).
//
// Beside the transition of a concurrent flow the device is probed over the
// commissioning session (reconnect_probe.go): its network state logged,
// its routable addresses handed to the operational connection, and a
// failure naming its last report — ahead of matter.js, whose step goes
// from the re-arm straight to transitionToCase
// (ControllerCommissioningFlow.ts:2029-2046 at db021da0).
func (f *flow) reconnectWithDevice(ctx context.Context) (stepResult, error) {
	concurrent := f.data.supportsConcurrentConnection == nil || *f.data.supportsConcurrentConnection
	f.log.Debug("matter.controller.commissioning.reconnecting", slog.Bool("concurrent", concurrent))
	if concurrent {
		if err := f.ensureFailsafeTimerFor(ctx, CASEReconnectTimeout); err != nil {
			f.log.Info("matter.controller.commissioning.rearm_before_reconnect_failed", slog.Any("err", err))
		}
	} else {
		f.stopArmInterval()
	}
	// With the commissioning session open beside the transition, the
	// device is asked what its network state is (reconnect_probe.go).
	var probe *reconnectProbe
	if concurrent {
		probe = f.startReconnectProbe(ctx, f.interaction())
		// A transition that panics does not leave the probe reading.
		defer probe.stop()
	}
	next, err := f.transition(ctx, f.address, concurrent)
	// The re-arm timer ends with the transition (:2031-2033).
	f.stopArmInterval()
	last := probe.stop()
	if err != nil {
		msg := "Operative reconnection with device failed"
		if last != nil {
			msg += " (" + last.summary() + ")"
		}
		return 0, newError(ErrOperativeConnectionFailed, msg, err)
	}
	if next == nil {
		f.log.Debug("matter.controller.commissioning.finalized_externally")
		return stepStop, nil
	}
	f.mu.Lock()
	f.ia = next
	f.mu.Unlock()
	f.log.Debug("matter.controller.commissioning.reconnected")
	return stepSuccess, nil
}

// completeCommissioning is step 20.1 (:2061-2081): CommissioningComplete
// over CASE, checked, the failsafe end cleared.
func (f *flow) completeCommissioning(ctx context.Context) (stepResult, error) {
	res, err := f.invoke(ctx, gencomm.ClusterID, gencomm.CmdCommissioningComplete, gencomm.CommissioningCompleteRequest{}, true)
	if err != nil {
		return 0, err
	}
	var resp gencomm.CommissioningCompleteResponse
	if err := response(res, "CommissioningCompleteResponse", resp.DecodeTLV); err != nil {
		return 0, err
	}
	if err := f.ensureGeneralCommissioningSuccess("commissioningComplete", resp.ErrorCode, resp.DebugText); err != nil {
		return 0, err
	}
	f.failsafeEnded()
	return stepSuccess, nil
}

// updateFabricLabel is step 98.1 (:1477-1506): UpdateFabricLabel when the
// fabric index is known (else Failure, :1479-1485); any error is
// recoverable (:1498-1500).
func (f *flow) updateFabricLabel(ctx context.Context) (stepResult, error) {
	if f.data.fabricIndex == 0 {
		f.log.Error("matter.controller.commissioning.no_fabric_index")
		return stepFailure, nil
	}
	res, err := f.invoke(ctx, opCredsCluster, cmdUpdateFabricLabel, updateFabricLabelRequest(f.fabric.CurrentLabel()), false)
	if err == nil {
		var nr nocResponse
		err = response(res, "NocResponse", func(n spec.Node) (err error) {
			nr, err = decodeNOCResponse(n)
			return err
		})
		if err == nil {
			err = f.ensureOperationalCredentialsSuccess("updateFabricLabel", nr)
		}
	}
	if err != nil {
		return 0, newError(errRecoverable, err.Error(), err)
	}
	return stepSuccess, nil
}

// verifyRaw verifies a raw 64-byte r‖s ECDSA signature over SHA-256 of msg
// (matter.js crypto.verifyEcdsa with an EcdsaSignature in its default
// form, :1408-1412).
func verifyRaw(key *ecdsa.PublicKey, msg, sig []byte) bool {
	if len(sig) != 64 {
		return false
	}
	digest := sha256.Sum256(msg)
	return ecdsa.Verify(key, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:]))
}
