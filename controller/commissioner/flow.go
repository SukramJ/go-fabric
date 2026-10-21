// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
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
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	netcomm "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	otareq "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	timesync "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/controller/interaction"
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

// rootEndpoint is RootEndpointNumber (:307).
const rootEndpoint uint16 = 0

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
	steps      []step
	results    map[[2]int]stepResult
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
	defaultFailSafe     time.Duration
	dacKey              *ecdsa.PublicKey
	lastBreadcrumb      uint64
	data                collectedData
}

func newFlow(c *Commissioner, ia *interaction.Interaction, opts Options, address handshake.PeerAddress, challenge []byte, transition transitionFunc) *flow {
	f := &flow{
		c: c, log: c.log, ia: ia, opts: opts, fabric: opts.Fabric, address: address,
		challenge: append([]byte(nil), challenge...), transition: transition,
		results:         map[[2]int]stepResult{},
		reArmBefore:     map[[2]int]bool{},
		ble:             opts.Channel != nil,
		defaultFailSafe: DefaultFailSafeTime,
		lastBreadcrumb:  1, // :327
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
		{10, 1, "OperationalCredentials.DeviceAttestation", f.deviceAttestation},
		{11, 1, "OperationalCredentials.Certificates", f.certificates},
		{15, 1, "AccessControl", f.configureAccessControlLists},
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
// error is returned as is (:463-465). No retries: a TODO upstream (:443-451).
func (f *flow) execute(ctx context.Context) error {
	f.sortSteps()
	// The BLE re-arm timer ends with the flow (close, :2138-2140).
	defer f.stopArmInterval()
	rearmedAfterPrevious := false
	for _, s := range f.steps {
		f.log.Info("matter.controller.commissioning.step", slog.String("step", fmt.Sprintf("%d.%d", s.number, s.sub)), slog.String("name", s.name))
		// A step with reArmFailsafe re-arms first, unless the previous
		// step's bookkeeping just did or the BLE re-arm timer runs
		// (:385-393).
		var err error
		if f.reArmBefore[[2]int{s.number, s.sub}] && !rearmedAfterPrevious && !f.armIntervalRunning() {
			f.log.Debug("matter.controller.commissioning.rearm_before_step", slog.String("name", s.name))
			_, err = f.armFailsafe(ctx, 0)
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
				if res == stepStop {
					break
				}
				continue
			}
		}
		if ce, ok := err.(*CommissioningError); ok && ce.Kind == errRecoverable { //nolint:errorlint // instanceof RecoverableCommissioningError (:431)
			f.log.Warn("matter.controller.commissioning.step_failed_recoverable", slog.String("name", s.name), slog.String("err", err.Error()))
			continue
		}
		if isCommissioningError(err) || isStatusError(err) {
			f.log.Error("matter.controller.commissioning.step_failed", slog.String("name", s.name), slog.String("err", err.Error()))
			f.resetFailsafeTimer(ctx)
			// StatusResponseError.accept rethrows anything else as is
			// (:459, general/src/MatterError.ts:87-92); a status error
			// itself is repacked (:462, util/Error.ts:88-104).
			if se, ok := err.(*interaction.StatusError); ok { //nolint:errorlint // the top-level instanceof of :459
				return newError(ErrCommissioning, se.Error(), se)
			}
			return err
		}
		return err
	}
	return nil
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
		if _, err := f.armFailsafe(ctx, 0); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
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

// ensureGeneralCommissioningSuccess is :712-719.
func (f *flow) ensureGeneralCommissioningSuccess(what string, code gencomm.CommissioningErrorEnum, debugText string) error {
	f.log.Debug("matter.controller.commissioning.general_commissioning_result", slog.String("context", what), slog.Int("code", int(code)), slog.String("debug", debugText))
	if code == gencomm.CommissioningErrorOk {
		return nil
	}
	msg := fmt.Sprintf("Commission error for %q: %s (%d)", what, enumName(gencomm.CommissioningErrorEnumDef, uint64(code)), code)
	if debugText != "" {
		msg += ", " + debugText
	}
	return newError(ErrCommissioning, msg, nil)
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
		return newError(ErrFabricLabelConflict, fmt.Sprintf("Cannot set fabric label to %q because another fabric on this device already uses this name. Please adjust fabric labels to be unique.", f.fabric.Label), nil)
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
// not run).
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
	)
	if err != nil {
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
	return stepSuccess, nil
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

// resetFailsafeTimer is :1077-1093: ArmFailSafe with 0 when a failsafe is
// armed, the response not checked, a failure only logged.
func (f *flow) resetFailsafeTimer(ctx context.Context) {
	if end, _ := f.failsafeTimes(); end.IsZero() {
		return
	}
	if _, err := f.invoke(ctx, gencomm.ClusterID, gencomm.CmdArmFailSafe, gencomm.ArmFailSafeRequest{Breadcrumb: f.breadcrumb(), ExpiryLengthSeconds: 0}, false); err != nil {
		f.log.Error("matter.controller.commissioning.reset_failsafe_failed", slog.String("err", err.Error()))
		return
	}
	f.setFailSafeEnd(time.Time{})
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
				return 0, newError(ErrCommissioning, reason, baseErr)
			}
			return 0, baseErr
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
// the CSR's key (:1418-1419; the echoed nonce is not compared, as matter.js
// does not); AddTrustedRootCertificate, its answer unused (:1421-1433); the
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
	der, err := decodeCertSigningRequest(csr.elements)
	if err != nil {
		return 0, fmt.Errorf("%w: NOCSRElements: %w", errUnexpectedResponse, err)
	}
	operationalKey, err := publicKeyFromCSR(der)
	if err != nil {
		return 0, err
	}
	if _, err := f.invoke(ctx, opCredsCluster, cmdAddTrustedRootCertificate, octetsRequest(f.fabric.CA.RootCert()), true); err != nil {
		return 0, err
	}
	noc, err := f.fabric.CA.GenerateNOC(operationalKey, f.fabric.FabricID, f.address.NodeID, nil)
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
func (f *flow) reconnectWithDevice(ctx context.Context) (stepResult, error) {
	concurrent := f.data.supportsConcurrentConnection == nil || *f.data.supportsConcurrentConnection
	f.log.Debug("matter.controller.commissioning.reconnecting", slog.Bool("concurrent", concurrent))
	if concurrent {
		if err := f.ensureFailsafeTimerFor(ctx, CASEReconnectTimeout); err != nil {
			f.log.Info("matter.controller.commissioning.rearm_before_reconnect_failed", slog.String("err", err.Error()))
		}
	} else {
		f.stopArmInterval()
	}
	next, err := f.transition(ctx, f.address, concurrent)
	// The re-arm timer ends with the transition (:2031-2033).
	f.stopArmInterval()
	if err != nil {
		return 0, newError(ErrOperativeConnectionFailed, "Operative reconnection with device failed", err)
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
	f.setFailSafeEnd(time.Time{})
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
	res, err := f.invoke(ctx, opCredsCluster, cmdUpdateFabricLabel, updateFabricLabelRequest(f.fabric.Label), false)
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
