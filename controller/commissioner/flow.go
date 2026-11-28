// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/client/handshake"
	"thingfabric.dev/go-fabric/client/interaction"
	"thingfabric.dev/go-fabric/client/peer"
	"thingfabric.dev/go-fabric/cluster/spec"
	basicinfo "thingfabric.dev/go-fabric/cluster/spec/basicinformation"
	"thingfabric.dev/go-fabric/cluster/spec/descriptor"
	gencomm "thingfabric.dev/go-fabric/cluster/spec/generalcommissioning"
	netcomm "thingfabric.dev/go-fabric/cluster/spec/networkcommissioning"
	opcredsdef "thingfabric.dev/go-fabric/cluster/spec/operationalcredentials"
	otareq "thingfabric.dev/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	timesync "thingfabric.dev/go-fabric/cluster/spec/timesynchronization"
	"thingfabric.dev/go-fabric/controller/attestation"
	"thingfabric.dev/go-fabric/im"
)

// Timing of the flow. Mirrors matter.js
// packages/protocol/src/peer/ControllerCommissioningFlow.ts:290
// (DEFAULT_FAILSAFE_TIME = 1 min), :296 (CASE_RECONNECT_TIMEOUT = 5 min),
// :299 (FAILSAFE_PROCESSING_MARGIN = 10 s) and :302
// (FAILSAFE_MAX_SEND_TIME = 10 s) at db021da0.
const (
	DefaultFailSafeTime      = time.Minute
	CASEReconnectTimeout     = 5 * time.Minute
	FailSafeProcessingMargin = 10 * time.Second
	FailSafeMaxSendTime      = 10 * time.Second
)

// canceledExpiryWait is how long the fail-safe expiry after a cancel
// waits for the device's answer, counted from the cancel: a device that
// does not answer in time ends its fail-safe itself when it expires.
// Mirrors matter.js CANCELED_EXPIRY_WAIT = 2 s
// (packages/protocol/src/peer/ControllerCommissioningFlow.ts:323 at
// db91886c), started by the cancel in #expireFailsafe (:1166-1173).
// Commissioner.disarmPASE, this module's own disarm of a PASE session
// given up before its flow, takes the same bound once its context ended.
const canceledExpiryWait = 2 * time.Second

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

// errUnexpectedResponse is a response the flow cannot use: one this
// module's decoders refuse, or a missing response payload, which
// matter.js's destructuring of undefined fails on. It is not a
// CommissioningError (:463-465). An empty CSRResponse is not one: it is
// the NOCSR check ResponseEmpty, a commissioning error
// (NocsrValidator.ts:86-91 at db91886c).
var errUnexpectedResponse = errors.New("commissioner: unexpected response from the device")

// errNoDACKey is the CSRResponse step reached without the DAC key the
// attestation step keeps — matter.js's InternalError
// (ControllerCommissioningFlow.ts:1450-1452 at b25ed960), a defect of the
// flow and not a CommissioningError. No path of this flow reaches it: the
// attestation step (10.1) runs before the CSR step (11.1), and it sets the
// key or fails the flow.
var errNoDACKey = errors.New("commissioner: internal error: CSRResponse received before the device attestation step set the DAC public key")

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
	basicCommissioningInfo *gencomm.BasicCommissioningInfo
	productName            string
	networkFeatures        []networkFeature // nil until collected
	networkStatus          []networkStatus  // nil until collected
	successfullyConnected  bool
	rootPartsList          []uint16
	rootServerList         []uint32
	// vendorID and productID are BasicInformation's, nil when the device
	// did not report them (collectedCommissioningData, its vendorId and
	// productId optional: DeviceAttestationValidator.ts:239-243 at
	// db91886c).
	vendorID, productID          *uint16
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
	// armStarted is #failSafeMayBeArmed (ControllerCommissioningFlow.ts:354-355
	// at db91886c): set from the flow's start — the device armed its
	// fail-safe when the PASE session the flow starts on was established
	// (newFlow) — and when the flow starts an ArmFailSafe on the
	// commissioning session, and cleared when the device confirms
	// CommissioningComplete (:2193) or, this module's addition, a disarm:
	// while it is set the device may hold a fail-safe, whether or not it
	// has answered. failSafeEnd, set by the answer, is what the step
	// bookkeeping reads; this is what expireFailsafe reads.
	armStarted      bool
	defaultFailSafe time.Duration
	// expiryWait is canceledExpiryWait; a test shortens it.
	expiryWait time.Duration
	// committed is #commitStarted (:357, set at :2176): CommissioningComplete
	// was started, and from there the caller's context no longer ends a
	// request or a step (requestContext). Read and written on the flow's
	// goroutine only.
	committed bool
	dacKey    *ecdsa.PublicKey
	// attested is the BasicInformation vendor and product id attestation
	// validated, nil while it has not ([Result.AttestedVendorID]).
	attested       *[2]uint16
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
		expiryWait:      canceledExpiryWait,
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

// execute is executeCommissioning. Mirrors matter.js
// packages/protocol/src/peer/ControllerCommissioningFlow.ts:406-495 at
// db91886c: each step in order, the caller's cancel checked at its entry
// before the re-arm (:413); a step with reArmFailsafe re-arms first
// (:414-421); Stop ends the loop as soon as the result is recorded
// (:425-427), before the bookkeeping; after a successful step with a
// failsafe armed, a total past the device's maximum cumulative failsafe is
// a CommissioningTimeoutError (:429-440) and less than half the default
// failsafe left re-arms (:447-455). A recoverable error is logged and the
// loop continues — unless the caller canceled (:458-466). Every other
// failure ends the fail-safe (expireFailsafe, :477); then a cancel —
// one before the failure or one that came during the cleanup — is what
// returns (cancelError, :478-487), else a CommissioningError or an IM
// status error is returned as a CommissioningError and any other error
// as it is (:489-493). No retries.
//
// The caller's cancel is ctx ending, the commissioner's abort signal
// (#cancel, :385; #requestAbort, :557-559): it reaches every request
// until CommissioningComplete is started (committed), and from there no
// longer (requestContext).
func (f *flow) execute(ctx context.Context) error {
	f.sortSteps()
	// The BLE re-arm timer ends with the flow (close, :2256-2259).
	defer f.stopArmInterval()
	rearmedAfterPrevious := false
	for _, s := range f.steps {
		f.log.Info("matter.controller.commissioning.step", slog.String("step", fmt.Sprintf("%d.%d", s.number, s.sub)), slog.String("name", s.name))
		f.report(s, StepStarted, nil)
		var err error
		if f.canceled(ctx) {
			err = cancelError(ctx)
		}
		// A step with reArmFailsafe re-arms first, unless the previous
		// step's bookkeeping just did or the BLE re-arm timer runs
		// (:414-421).
		if err == nil && f.reArmBefore[[2]int{s.number, s.sub}] && !rearmedAfterPrevious && !f.armIntervalRunning() {
			f.log.Debug("matter.controller.commissioning.rearm_before_step", slog.String("name", s.name))
			_, err = f.reArmFailsafeBetweenSteps(f.requestContext(ctx))
		}
		rearmedAfterPrevious = false
		var res stepResult
		if err == nil {
			res, err = s.run(f.requestContext(ctx))
		}
		if err == nil {
			f.results[[2]int{s.number, s.sub}] = res
			if res == stepStop {
				f.report(s, phaseOf(res), nil)
				break
			}
			rearmedAfterPrevious, err = f.afterStep(f.requestContext(ctx), s)
			if err == nil {
				f.report(s, phaseOf(res), nil)
				continue
			}
		}
		canceledBeforeCleanup := f.canceled(ctx)
		if ce, ok := err.(*CommissioningError); ok && ce.Kind == errRecoverable && !canceledBeforeCleanup { //nolint:errorlint // instanceof RecoverableCommissioningError (:459)
			f.log.Warn("matter.controller.commissioning.step_failed_recoverable", slog.String("name", s.name), slog.Any("err", err))
			f.report(s, StepFailed, err)
			continue
		}
		classed := isCommissioningError(err) || isStatusError(err)
		if classed && !canceledBeforeCleanup {
			f.log.Warn("matter.controller.commissioning.step_failed", slog.String("name", s.name), slog.Any("err", err))
		}
		f.expireFailsafe(ctx, err)
		switch {
		case f.canceled(ctx):
			// A cancel that came during the cleanup decides the outcome as
			// well (:478-487).
			f.log.Info("matter.controller.commissioning.aborted", slog.String("name", s.name), slog.Any("err", err))
			err = cancelError(ctx)
		case classed:
			// StatusResponseError.accept rethrows anything else as is
			// (general/src/MatterError.ts:87-92; util/Error.ts:88-104).
			if se, ok := err.(*interaction.StatusError); ok { //nolint:errorlint // the top-level instanceof of :490
				err = newError(ErrCommissioning, se.Error(), se)
			}
		}
		f.report(s, StepFailed, err)
		return err
	}
	return nil
}

// requestContext is the context of the flow's requests and steps —
// #requestAbort (ControllerCommissioningFlow.ts:557-559 at db91886c): the
// caller's ctx until CommissioningComplete is started, then one the
// caller cannot end, so that the device's answer decides from there
// (:2176). It keeps ctx's values. It drops ctx's deadline as well: the
// bound of each request from there is the exchange's own response
// timeout, as matter.js's requests carry no signal then.
func (f *flow) requestContext(ctx context.Context) context.Context {
	if f.committed {
		return context.WithoutCancel(ctx)
	}
	return ctx
}

// canceled reports whether the caller canceled before the commit cut-off:
// `this.#requestAbort?.aborted` (:458, :480-481).
func (f *flow) canceled(ctx context.Context) bool {
	return !f.committed && ctx.Err() != nil
}

// cancelError is the error a canceled flow returns: the cancel's reason
// (`throw asError(cancel.reason)`, ControllerCommissioningFlow.ts:486 at
// db91886c) — context.Cause(ctx) — which also matches ctx.Err() with
// errors.Is: a reason that matches ctx.Err() already (ctx.Err() itself, or
// a reason that wraps it, such as fmt.Errorf("user quit: %w",
// context.Canceled)) is returned as it is, one that does not is joined
// with it ([canceledError]). A step's own error that came with the cancel
// is not returned.
func cancelError(ctx context.Context) error {
	err := ctx.Err()
	if err == nil {
		return nil
	}
	cause := context.Cause(ctx)
	if cause == nil {
		return err
	}
	if errors.Is(cause, err) {
		return cause
	}
	return &canceledError{cause: cause, ctxErr: err}
}

// canceledError is a cancel's reason that is not the context's error
// itself (context.WithCancelCause, WithTimeoutCause): its message is the
// reason's, and it matches both the reason and the context's error.
// matter.js rejects with the bare reason; the context's error is this
// module's addition, so that errors.Is(err, ctx.Err()) holds for every
// canceled commissioning.
type canceledError struct {
	cause, ctxErr error
}

func (e *canceledError) Error() string   { return e.cause.Error() }
func (e *canceledError) Unwrap() []error { return []error{e.cause, e.ctxErr} }

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
// (:429-455 at db91886c); it reports whether it re-armed.
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
// this flow: the PASE session was established or an ArmFailSafe was
// started, and the device has confirmed neither CommissioningComplete nor
// a disarm since (armStarted, #failSafeMayBeArmed).
func (f *flow) failsafeMayBeArmed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.armStarted
}

// failsafeEnded records that the device confirmed the fail-safe's end,
// by CommissioningComplete or by a zero expiry. matter.js then holds no
// end time (ControllerCommissioningFlow.ts:2093 and :1104 at db021da0),
// and a failure or a cancel owes no expiry.
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
	ia := f.interaction()
	res, err := ia.Invoke(ctx, im.ConcreteCommandPath{Endpoint: rootEndpoint, Cluster: cluster, Command: command}, fields, opts)
	return res, f.channelLoss(ia, err)
}

// channelLoss is err of an interaction over the commissioning channel's
// PASE session with the channel's close as its cause, when the channel
// closed and err is the closed session's: the session the channel's loss
// force-closed (establishOverChannel) refuses a new exchange with
// exchange.ErrSessionClosed alone, and the flow reads the cause as a
// commissioning-channel drop (isChannelDrop). matter.js force-closes the
// PASE session with a BleChannelClosedError so that its pending exchanges
// reject with it (packages/protocol/src/peer/ControllerCommissioner.ts:523-558
// at b25ed960, #closeOnBleLoss); an exchange that starts after the close
// is this module's addition — what matter.js reports there was not
// established. Interactions over another manager — the CASE session after the
// Reconnect step — are left as they are.
func (f *flow) channelLoss(ia *interaction.Interaction, err error) error {
	ch := f.opts.Channel
	if err == nil || ch == nil || !errors.Is(err, exchange.ErrSessionClosed) || ia.Provider().Manager() != ch.Exchanges() {
		return err
	}
	select {
	case <-ch.Closed():
	default:
		return err
	}
	if cause := ch.ClosedCause(); cause != nil && !errors.Is(err, cause) {
		return fmt.Errorf("%w: %w", err, cause)
	}
	return err
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
	ia := f.interaction()
	report, err := ia.Read(ctx, paths...)
	if err != nil {
		return nil, nil, f.channelLoss(ia, err)
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
		suffix += ", " + deviceText(*r.debugText)
	}
	if r.fabricIndex != nil {
		suffix += fmt.Sprintf(", fabricIndex: %d", *r.fabricIndex)
	}
	f.log.Debug("matter.controller.commissioning.operational_credentials_result", slog.String("context", what), slog.String("status", nocStatusName(r.status)))
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
	return newError(ErrCommissioning, fmt.Sprintf("Commission error for %q: %s (%d)%s", what, nocStatusName(r.status), r.status, suffix), nil)
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
// PASE session armed (checkTerms, expireFailsafe).
func (f *flow) getInitialData(ctx context.Context) (stepResult, error) {
	v, ok, err := f.readValues(ctx,
		rootAttr(opCredsCluster, opcredsdef.AttrSupportedFabrics),
		rootAttr(opCredsCluster, opcredsdef.AttrCommissionedFabrics),
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
		d.vendorID, err = spec.DecodeOptional(spec.DecodeUint[uint16](16))(v[4])
		errs = append(errs, err)
	}
	if ok[5] {
		d.productID, err = spec.DecodeOptional(spec.DecodeUint[uint16](16))(v[5])
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
		return 0, f.channelLoss(f.ia, err)
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
				// A value whose shape does not decode is skipped and the
				// read goes on (F-db021da0-15, reportedValue).
				f.log.Warn("matter.controller.commissioning.reported_value_skipped", slog.String("attribute", "NetworkCommissioning Networks"), slog.Int("endpoint", int(a.Path.Endpoint)), slog.Any("err", err))
				continue
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
		// By shape, a malformed value absent (F-db021da0-15, reportedValue).
		def := gencomm.BasicCommissioningInfoDef
		var fields map[uint32]any
		if ok[0] {
			fields, ok[0] = reportedStruct(f.log, "BasicCommissioningInfo", def, v[0])
		}
		if !ok[0] {
			return 0, fmt.Errorf("%w: no BasicCommissioningInfo", errUnexpectedResponse)
		}
		var info gencomm.BasicCommissioningInfo
		var has bool
		// A value without FailSafeExpiryLengthSeconds is refused: this
		// module's rule — the flow cannot arm without it (F-db021da0-15).
		if info.FailSafeExpiryLengthSeconds, has = fieldUint[uint16](def, fields, "FailSafeExpiryLengthSeconds"); !has {
			return 0, fmt.Errorf("%w: no BasicCommissioningInfo FailSafeExpiryLengthSeconds", errUnexpectedResponse)
		}
		// A missing MaxCumulativeFailsafeSeconds leaves the commissioning
		// expiry undefined (zero), as an unset field.
		maxCumulative, hasMax := fieldUint[uint16](def, fields, "MaxCumulativeFailsafeSeconds")
		info.MaxCumulativeFailsafeSeconds = maxCumulative
		f.data.basicCommissioningInfo = &info
		f.defaultFailSafe = time.Duration(info.FailSafeExpiryLengthSeconds) * time.Second
		started := f.now()
		if hasMax {
			f.mu.Lock()
			f.commissioningExpiry = started.Add(time.Duration(maxCumulative) * time.Second)
			f.mu.Unlock()
		}
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
	// request queued, retransmitted, answered late or not at all — so a
	// failure or a cancel owes it the expiry (expireFailsafe). The fail-safe
	// the PASE session armed is owed one already (newFlow).
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

// sendFailsafeExpiry is #expireFailsafe's request
// (ControllerCommissioningFlow.ts:1174-1197 at db91886c): ArmFailSafe
// with expiry 0 and the current breadcrumb, over an existing session only
// (ClientRequest.RequireExistingSession — the flow's interactions are
// bound to their session, so a closed one fails at once and none is
// established), the response checked (#ensureGeneralCommissioningSuccess);
// on success the flow counts no fail-safe any more, a failure is only
// logged — the device ends the fail-safe itself when it expires. ctx is
// the expiry's own (expireFailsafe), never the caller's.
func (f *flow) sendFailsafeExpiry(ctx context.Context) {
	res, err := f.invokeWith(ctx, gencomm.ClusterID, gencomm.CmdArmFailSafe,
		gencomm.ArmFailSafeRequest{Breadcrumb: f.breadcrumb(), ExpiryLengthSeconds: 0},
		interaction.InvokeOptions{ClientRequest: interaction.ClientRequest{RequireExistingSession: true}})
	if err == nil {
		var resp gencomm.ArmFailSafeResponse
		if err = response(res, "ArmFailSafeResponse", resp.DecodeTLV); err == nil {
			err = f.ensureGeneralCommissioningSuccess("armFailSafe", resp.ErrorCode, resp.DebugText)
		}
	}
	if err != nil {
		f.log.Info("matter.controller.commissioning.reset_failsafe_failed", slog.Any("err", err))
		return
	}
	f.failsafeEnded()
}

// expireFailsafe is #expireFailsafe, the end of the fail-safe after a
// failed or canceled step, failure the step's error. Mirrors matter.js
// packages/protocol/src/peer/ControllerCommissioningFlow.ts:1153-1200 at
// db91886c, in its order: the BLE re-arm timer stopped (:1154-1155);
// nothing sent while the device cannot hold a fail-safe of this flow —
// #failSafeMayBeArmed, true from the PASE session's establishment on
// (armStarted, :354-355, :1157) — or when the device stopped responding
// (deviceStoppedResponding, :1158-1161), a re-arm in flight then
// cancelled, not awaited; else a re-arm already sent awaited (`await
// this.#periodicArm`, :1163), which the caller's cancel ends, as it ends
// matter.js's re-arm (#armFailsafe sends with #requestAbort); then
// sendFailsafeExpiry.
//
// The expiry runs on a context of its own, never ended by the caller
// (expiryWait, :1164, sent with it at :1188). Until CommissioningComplete
// is started the caller's cancel bounds it: canceledExpiryWait from the
// cancel — at once when ctx has already ended, else when it ends during
// the send (`new Abort({ abort: this.#requestAbort, handler })`,
// :1166-1173; Abort runs the handler at once for a signal already
// aborted, packages/general/src/util/Abort.ts:133-138). Without a cancel
// the exchange's response timeout is the bound.
//
// Not covered: the aborted ArmFailSafe is not retransmitted
// (exchange.Exchange.Send ends the retransmissions with its context), but
// a datagram of it still on its way can reach the device behind the
// expiry and arm it.
func (f *flow) expireFailsafe(ctx context.Context, failure error) {
	ai := f.detachArmInterval()
	if !f.expiryOwed(failure) {
		cancelArmInterval(ai)
		return
	}
	awaitArmInterval(f.requestContext(ctx), ai)
	expiry, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	if !f.committed {
		defer f.boundAfterCancel(ctx, cancel)()
	}
	f.sendFailsafeExpiry(expiry)
}

// boundAfterCancel starts f.expiryWait on the flow's clock once ctx ends
// — at once when it has ended — and calls end when it runs out: the
// handler of :1166-1173. The returned stop ends the watch and the timer
// (`canceledDeadline?.stop()`, :1198).
func (f *flow) boundAfterCancel(ctx context.Context, end context.CancelFunc) (stop func()) {
	var (
		mu      sync.Mutex
		stopped bool
		timer   exchange.Timer
	)
	unwatch := context.AfterFunc(ctx, func() {
		mu.Lock()
		defer mu.Unlock()
		if !stopped {
			timer = f.c.ctx.Clock.AfterFunc(f.expiryWait, end)
		}
	})
	return func() {
		unwatch()
		mu.Lock()
		defer mu.Unlock()
		stopped = true
		if timer != nil {
			timer.Stop()
		}
	}
}

// expiryOwed reports whether expireFailsafe sends: matter.js's two
// returns (:1157, :1158-1161). A closed session is a shortcut of this
// module's: matter.js sends with requireExistingSession there and logs
// the failure (:1188, :1192-1197), the same wire result.
func (f *flow) expiryOwed(failure error) bool {
	if !f.failsafeMayBeArmed() {
		return false
	}
	if s := f.interaction().Session(); s != nil && s.IsClosed() {
		f.log.Debug("matter.controller.commissioning.abort_disarm_skipped", slog.String("reason", "session closed"))
		return false
	}
	if deviceStoppedResponding(failure) {
		f.log.Debug("matter.controller.commissioning.failsafe_left_to_expire", slog.String("reason", "device stopped responding"))
		return false
	}
	return true
}

// deviceStoppedResponding is #deviceStoppedResponding
// (ControllerCommissioningFlow.ts:996-1003 at b25ed960): the failure is a
// commissioning-channel drop (isChannelDrop), so a further command would
// only wait for its own timeout — but not a failed operational reconnect,
// which reports that CASE could not be established while the
// commissioning channel may still work (the top-level instanceof of
// :1002).
func deviceStoppedResponding(failure error) bool {
	if ce, ok := failure.(*CommissioningError); ok && ce.Kind == ErrOperativeConnectionFailed { //nolint:errorlint // the top-level instanceof of :1002
		return false
	}
	return isChannelDrop(failure)
}

// validateAttestation is attestation.Validate, whose wait ends with ctx:
// the validation takes no context — a host's PAAStore may look a root or
// a revocation up on the network — so only the wait for it is canceled,
// and the validation runs on and its result is dropped. Mirrors matter.js
// `Abort.attempt(this.#requestAbort, DeviceAttestationValidator.validate(…))`
// (packages/protocol/src/peer/ControllerCommissioningFlow.ts:1375-1377 at
// db91886c, "The DCL lookups take no cancel signal, so only the wait for
// them can be canceled"); the caller reads ctx after it, as Abort.attempt
// throws for a signal aborted by then (packages/general/src/util/Abort.ts:366-373).
func (f *flow) validateAttestation(ctx context.Context, actx attestation.Context, data attestation.Data) (attestation.Result, error) {
	type outcome struct {
		res attestation.Result
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		// A panic in the host's store ends the validation with an error,
		// which becomes the ValidationError finding the policy judges
		// (review II, P10-err-6202); the value is named by type only.
		defer func() {
			if r := recover(); r != nil {
				done <- outcome{err: fmt.Errorf("commissioner: attestation validation panicked (%T)", r)}
			}
		}()
		res, err := attestation.Validate(actx, data)
		done <- outcome{res, err}
	}()
	select {
	case o := <-done:
		return o.res, o.err
	case <-ctx.Done():
		return attestation.Result{}, cancelError(ctx)
	}
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
	var capability uint8
	if ok[0] {
		// By shape, a malformed value absent (F-db021da0-15, reportedValue).
		var isNull bool
		capability, isNull, ok[0] = reportedUint[uint8](f.log, "LocationCapability", 8, v[0])
		ok[0] = ok[0] && !isNull
	}
	if !ok[0] {
		return 0, fmt.Errorf("%w: no LocationCapability", errUnexpectedResponse)
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

// deviceAttestation is step 10.1: the DAC and the PAI
// (CertificateChainRequest), the AttestationRequest with a 32-byte nonce,
// attestation.Validate; the policy decides (:1347-1364 at b25ed960); the
// DAC key kept, parsed from the DAC when the policy accepted a failed
// validation (:1366-1380 at b25ed960). A hard failure hands the policy the findings collected
// before it and then its own error finding, the failure last; any other
// validation error is one ValidationError finding; a rejection without a
// failure names every error finding. Mirrors matter.js
// packages/protocol/src/peer/ControllerCommissioningFlow.ts:1257-1386 at
// b25ed960 (#deviceAttestation).
func (f *flow) deviceAttestation(ctx context.Context) (stepResult, error) {
	// An earlier run's pair never outlives this run: a run that fails or
	// withholds the pair leaves none (review II, R2-P6-fsm-1811).
	f.attested = nil
	chain := make([][]byte, 2)
	for i, kind := range []opcredsdef.CertificateChainTypeEnum{opcredsdef.CertificateChainTypeDacCertificate, opcredsdef.CertificateChainTypePaiCertificate} {
		res, err := f.invoke(ctx, opCredsCluster, opcredsdef.CmdCertificateChainRequest, opcredsdef.CertificateChainRequestRequest{CertificateType: kind}, true)
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
	res, err := f.invoke(ctx, opCredsCluster, opcredsdef.CmdAttestationRequest, opcredsdef.AttestationRequestRequest{AttestationNonce: nonce}, true)
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
	result, err := f.validateAttestation(ctx,
		attestation.Context{Challenge: f.challenge, Store: f.c.ctx.Attestation, Clock: f.c.ctx.Clock},
		attestation.Data{
			DAC: chain[0], PAI: chain[1], Elements: att.elements, Signature: att.signature, Nonce: nonce,
			VendorID: f.data.vendorID, ProductID: f.data.productID,
		},
	)
	if ctxErr := cancelError(ctx); ctxErr != nil {
		return 0, ctxErr
	}
	if err != nil {
		baseErr = err
		var ae *attestation.Error
		if errors.As(err, &ae) {
			// The findings collected before the failure, then the failure
			// (ControllerCommissioningFlow.ts:1332-1336 at b25ed960).
			findings = append(slices.Clone(ae.PrecedingFindings), attestation.Finding{Level: attestation.LevelError, Check: ae.Check, Message: ae.Message})
		} else {
			findings = []attestation.Finding{{Level: attestation.LevelError, Check: attestation.ValidationError, Message: err.Error()}}
		}
	} else {
		findings = result.Findings
		f.dacKey = result.DACPublicKey
		// No error from Validate no longer means the Certification
		// Declaration names this vendor and product: a mismatch with the
		// BasicInformation VendorID or ProductID, or a value not reported,
		// is the LevelError finding CertificationDeclarationFieldMismatch
		// (DeviceAttestationValidator.ts:579-595 at db91886c). No error
		// and no error finding is what says it does (decision 105). The
		// pair is recorded only when the chain was verified as well and no
		// error finding stands (owner decision 119): a finding that a check
		// could not run (no trust store, DclServiceUnavailable) or a
		// LevelError one the policy accepts below leaves it unattested,
		// with one log line (attestedPairWithheld); the pair needs both
		// values reported.
		if why, withheld := attestedPairWithheld(result); withheld {
			f.log.Info("matter.controller.commissioning.attested_identity_not_recorded", slog.String("reason", why))
		} else if f.data.vendorID != nil && f.data.productID != nil {
			f.attested = &[2]uint16{*f.data.vendorID, *f.data.productID}
		}
	}

	if len(findings) > 0 {
		proceed, reason, perr := f.opts.OnAttestationFailure.Resolve(findings, f.log)
		if perr != nil {
			return 0, perr
		}
		if !proceed {
			if baseErr == nil {
				// The rejection names every error finding
				// (ControllerCommissioningFlow.ts:1350-1355 at b25ed960).
				baseErr = newError(ErrCommissioning, rejectedByPolicy(findings), nil)
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

// rejectedByPolicy is the message of a rejection of collected findings
// (ControllerCommissioningFlow.ts:1350-1355 at b25ed960): the error
// findings' messages, else the number of findings.
func rejectedByPolicy(findings []attestation.Finding) string {
	var msgs []string
	for _, fd := range findings {
		if fd.Level == attestation.LevelError {
			msgs = append(msgs, fd.Message)
		}
	}
	if len(msgs) > 0 {
		return "Device attestation rejected by policy: " + strings.Join(msgs, "; ")
	}
	return fmt.Sprintf("Device attestation produced %d finding(s) and was rejected by policy", len(findings))
}

// attestedPairWithheld reports why an attestation that returned no error
// still records no attested pair: a chain that ends in a test root or a
// declaration the test CD signer signed ([attestation.Result].TestPAA,
// TestCDSigner; decision 119 extended by decision 192), a check that did
// not run because no trust store (DCL service) was available — the DAC
// chain, or the Certification Declaration's authorized_paa_list
// ([attestation.DclServiceUnavailable]) — or an error finding
// ([attestation.LevelError]), which a policy may accept.
func attestedPairWithheld(result attestation.Result) (string, bool) {
	findings := result.Findings
	// A test root or the test CD signer: trusted as such by a store that
	// accepts test certificates, without a finding, and their keys are
	// public — the pair is anyone's to claim (R2-P6-sec-1803, decision
	// 214).
	switch {
	case result.TestPAA:
		return "the attestation chain ends in a test root", true
	case result.TestCDSigner:
		return "the Certification Declaration is signed by the Matter test CD signer", true
	}
	for _, fd := range findings {
		if fd.Check == attestation.DclServiceUnavailable {
			return "the attestation chain was not verified: " + fd.Message, true
		}
	}
	for _, fd := range findings {
		if fd.Level == attestation.LevelError {
			return "an error finding was accepted: " + string(fd.Check) + ": " + fd.Message, true
		}
	}
	return "", false
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

// certificates is step 11.1: the CSRRequest with a 32-byte nonce; the CSRResponse validated by attestation.ValidateNOCSR against
// the DAC key the attestation step kept, the nonce sent and the session's
// challenge, which returns the CSR's key — every failure a commissioning
// error, which no attestation policy accepts; AddTrustedRootCertificate,
// its answer unused (:1463-1475); the NOC (:1477-1481); AddNOC
// (:1483-1499) checked (:1501); the fabric index kept (:1503-1504), lines
// at b25ed960. Mirrors matter.js
// packages/protocol/src/peer/ControllerCommissioningFlow.ts:1436-1510 at
// b25ed960 (#certificates).
func (f *flow) certificates(ctx context.Context) (stepResult, error) {
	nonce, err := f.random(csrNonceSize)
	if err != nil {
		return 0, err
	}
	res, err := f.invoke(ctx, opCredsCluster, opcredsdef.CmdCsrRequest, opcredsdef.CsrRequestRequest{CsrNonce: nonce}, true)
	if err != nil {
		return 0, err
	}
	var csr attestation.NOCSRResponse
	if err := response(res, "CsrResponse", func(n spec.Node) (err error) {
		csr, err = decodeCSRResponse(n)
		return err
	}); err != nil {
		return 0, err
	}
	// The DAC key is required: the attestation step sets it or fails, so
	// its absence is a defect of the flow, not of the device's data
	// (ControllerCommissioningFlow.ts:1450-1452 at b25ed960, an
	// InternalError).
	if f.dacKey == nil {
		return 0, errNoDACKey
	}
	operationalKey, err := attestation.ValidateNOCSR(
		attestation.NOCSRContext{Challenge: f.challenge, CSRNonce: nonce, DACPublicKey: f.dacKey},
		csr,
	)
	if err != nil {
		var ne *attestation.NOCSRError
		if errors.As(err, &ne) {
			// NocsrError extends CommissioningError (NocsrValidator.ts:39
			// at db91886c).
			return 0, newError(ErrCommissioning, ne.Message, ne)
		}
		return 0, err
	}
	if _, err := f.invoke(ctx, opCredsCluster, opcredsdef.CmdAddTrustedRootCertificate, opcredsdef.AddTrustedRootCertificateRequest{RootCaCertificate: f.fabric.CA.RootCert()}, true); err != nil {
		return 0, err
	}
	noc, err := f.fabric.CA.GenerateNOC(operationalKey, f.fabric.FabricID, f.address.NodeID, f.opts.CATs)
	if err != nil {
		return 0, err
	}
	res, err = f.invoke(ctx, opCredsCluster, opcredsdef.CmdAddNoc,
		addNOCRequest(noc, f.fabric.CA.ICACCert(), f.fabric.IPK, f.fabric.RootNodeID, f.fabric.RootVendorID), true)
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

// completeCommissioning is step 20.1: CommissioningComplete over CASE,
// checked, the failsafe end cleared. Mirrors matter.js
// packages/protocol/src/peer/ControllerCommissioningFlow.ts:2175-2199 at
// db91886c (#completeCommissioning): its first act is the commit cut-off
// (`this.#commitStarted = true`, :2176) — from here the caller's cancel
// reaches no request and no step (requestContext), so the device's answer
// decides, and a failure is reported as itself — and the command is sent
// on the "unlimited" network profile, which no exchange slot throttles
// (`{ network: "unlimited" }`, :2188-2189), with the extended response
// timeout (:2185-2187).
func (f *flow) completeCommissioning(ctx context.Context) (stepResult, error) {
	f.committed = true
	ctx = f.requestContext(ctx)
	res, err := f.invokeWith(ctx, gencomm.ClusterID, gencomm.CmdCommissioningComplete, gencomm.CommissioningCompleteRequest{},
		interaction.InvokeOptions{ExtendedFailSafeTimeout: true, ClientRequest: interaction.ClientRequest{Network: peer.NetworkUnlimited}})
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
	res, err := f.invoke(ctx, opCredsCluster, opcredsdef.CmdUpdateFabricLabel, opcredsdef.UpdateFabricLabelRequest{Label: f.fabric.CurrentLabel()}, false)
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
