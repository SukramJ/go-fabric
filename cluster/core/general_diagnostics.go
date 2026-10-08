// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// NetworkInterfaceStruct mirrors Matter §11.12.4.1 (NetworkInterface
// struct) — the per-interface entry the GeneralDiagnostics
// `NetworkInterfaces` (0x0000) attribute exposes. Apple Home's
// `HMMTRAccessoryServerBrowser` reads this list to build its internal
// "topology dictionary"; without at least one entry it logs
// "No enumeration/topology dictionary found" + "Nil supported link
// layer types" and aborts the pair via RemoveFabric ~5 s after
// Subscribe-Initial. matter.js HEAD shape:
// packages/types/src/clusters/general-diagnostics.ts:NetworkInterface.
type NetworkInterfaceStruct struct {
	// Name is a non-empty UTF-8 label (max 32 chars). Matches the OS
	// interface name ("en0", "eth0", …).
	Name string
	// IsOperational is true when the interface is up and carrying
	// traffic.
	IsOperational bool
	// OffPremiseServicesReachableIPv4 / IPv6 are nullable booleans
	// (nil = "unknown" / "no DNS/HTTP probe done"). go-fabric does
	// not probe off-premise reachability — both stay nil.
	OffPremiseServicesReachableIPv4 *bool
	OffPremiseServicesReachableIPv6 *bool
	// HardwareAddress is the EUI-48 (6-byte) or EUI-64 (8-byte) MAC.
	// Empty for loopback / synthetic interfaces.
	HardwareAddress []byte
	// IPv4Addresses + IPv6Addresses carry the raw 4-byte / 16-byte
	// address bytes for every active address on the interface.
	IPv4Addresses [][]byte
	IPv6Addresses [][]byte
	// InterfaceType is the matter.js InterfaceTypeEnum:
	// 0 Unspecified, 1 WiFi, 2 Ethernet, 3 Cellular, 4 Thread.
	InterfaceType uint8
}

// InterfaceType enum values per Matter §11.12.5.1.
const (
	InterfaceTypeUnspecified uint8 = 0
	InterfaceTypeWiFi        uint8 = 1
	InterfaceTypeEthernet    uint8 = 2
	InterfaceTypeCellular    uint8 = 3
	InterfaceTypeThread      uint8 = 4
)

// GeneralDiagnostics implements the Matter GeneralDiagnostics cluster
// (0x0033) per Matter Core Specification 1.5.1 §11.12. Mandatory on
// the Root endpoint. Reports basic runtime diagnostics: uptime,
// reboot reason, hardware faults, network faults.
//
// go-fabric emits stub values for the fault lists (the bridge
// itself does not surface hardware-fault events to Matter); UpTime
// and TotalOperationalHours come from a monotonic clock.
type GeneralDiagnostics struct {
	mu sync.RWMutex

	startTime  time.Time
	bootReason uint8

	// Persistence-seeded counters; populated by [SetPersistedCounters].
	// rebootCount survives across daemon restarts (incremented on each
	// fresh boot before being seeded into the cluster).
	// baseOperationalTime is the operational time accumulated by prior
	// process lifetimes, kept at the resolution matter.js persists
	// (totalOperationalHoursCounter, milliseconds); the attribute adds the
	// current process's uptime and floors the sum to whole hours.
	rebootCount         uint16
	baseOperationalTime time.Duration
	persistedSeeded     bool

	// dataVersion tracks the per-cluster monotonic counter per Matter
	// §10.6.5. Bumped at construction and when persisted counters are seeded
	// via [SetPersistedCounters]. Runtime attributes (UpTime,
	// TotalOperationalHours) change continuously but DataVersion is not bumped
	// per-second — controllers that cache UpTime do not need sub-second
	// invalidation. Satisfies [contract.ClusterDataVersion].
	// Mirrors chip's ember dirty-marking in
	// src/app/clusters/general-diagnostics-server/.
	dataVersion cluster.DataVersionTracker

	// testEnableKey and testTrigger are set by [EnableTestEventTriggers]:
	// the device's 16-byte test enable key and the host's trigger handler.
	// Without them TestEventTriggersEnabled reads false and every
	// TestEventTrigger fails the enable-key check.
	testEnableKey []byte
	testTrigger   TestEventTriggerHandler

	// deviceLoad supplies DeviceLoadStatus for an accessing fabric; nil
	// reports the zeroed struct matter.js reports before its interaction
	// server is online.
	deviceLoad func(fabricIndex uint8) DeviceLoadStruct

	// upTimeHighWater is the highest UpTime (in nanoseconds) reported since
	// startTime; see upTime.
	upTimeHighWater atomic.Int64

	// utcClock is the node's TimeSynchronization UTC time; nil when the
	// node has no TimeSynchronization cluster (see SetUTCClock).
	utcClock func() (time.Time, bool)

	// Event emitter + endpoint; wired by the bridge topology assembler
	// via [SetMatterEventEmitter] + [SetEndpoint] so [EmitBootReason]
	// can fire the §11.12.8.1 BootReason event.
	endpoint uint16
	emitter  contract.EventEmitter
}

// BootReason values per Matter §11.12.5.4.
const (
	BootReasonUnspecified      uint8 = 0
	BootReasonPowerOnReboot    uint8 = 1
	BootReasonBrownOutReset    uint8 = 2
	BootReasonSoftwareWatchdog uint8 = 3
	BootReasonHardwareWatchdog uint8 = 4
	BootReasonSoftwareUpdate   uint8 = 5
	BootReasonSoftwareReset    uint8 = 6
)

// Cluster ID + revision per Matter §11.12.
const (
	gendiagClusterID       uint32 = 0x0033
	gendiagClusterRevision uint16 = 3 // matter.js HEAD general-diagnostics.element.ts:21 default=3

	gendiagAttrNetworkInterfaces        uint32 = 0x0000
	gendiagAttrRebootCount              uint32 = 0x0001
	gendiagAttrUpTime                   uint32 = 0x0002
	gendiagAttrTotalOperationalHours    uint32 = 0x0003
	gendiagAttrBootReason               uint32 = 0x0004
	gendiagAttrActiveHardwareFaults     uint32 = 0x0005
	gendiagAttrActiveRadioFaults        uint32 = 0x0006
	gendiagAttrActiveNetworkFaults      uint32 = 0x0007
	gendiagAttrTestEventTriggersEnabled uint32 = 0x0008
	// gendiagAttrDeviceLoadStatus is DeviceLoadStatus (0x000A, conformance
	// "Rev >= v3", quality C — general-diagnostics.element.ts:49-50):
	// mandatory at the revision this server advertises.
	gendiagAttrDeviceLoadStatus uint32 = 0x000A

	// Commands per Matter §11.12.7.
	gendiagCmdTestEventTrigger uint32 = 0x0000
	gendiagCmdTimeSnapshot     uint32 = 0x0001
	gendiagCmdTimeSnapshotResp uint32 = 0x0002
	// PayloadTestRequest / PayloadTestResponse carry conformance DMTEST
	// (general-diagnostics.element.ts:110-119).
	gendiagCmdPayloadTestRequest  uint32 = 0x0003
	gendiagCmdPayloadTestResponse uint32 = 0x0004

	// gendiagFeatureDataModelTest is the DMTEST bit (element :24,
	// constraint "0"). Mandatory above a MaxPathsPerInvoke of one
	// (Matter 1.6.1 Core §11.12.4.1, matter.js
	// GeneralDiagnosticsServer.ts #assertDataModelTest) — and this module's
	// BasicInformation advertises im.DefaultMaxPathsPerInvoke (10).
	gendiagFeatureDataModelTest uint32 = 1 << 0

	// payloadTestMaxCount is PayloadTestRequest.Count's constraint "max 2048".
	payloadTestMaxCount = 2048
	// payloadTestMaxResponse bounds the Payload a response can carry in
	// one unsegmented message: matter.js answers ResourceExhausted when the
	// encoded response exceeds the exchange's maximum payload
	// (GeneralDiagnosticsServer.ts payloadTestRequest), which for UDP is
	// 1280 minus the message and security overhead.
	payloadTestMaxResponse = 1100

	// Events per Matter §11.12.8 / matter.js general-diagnostics.element.ts:74-79.
	// BootReason is event 0x03; the lower three (HardwareFaultChange,
	// RadioFaultChange, NetworkFaultChange) are optional and not emitted
	// by the bridge.
	gendiagEventBootReason uint32 = 0x0003
)

// NewGeneralDiagnostics returns the cluster with startTime captured
// at construction. bootReason is supplied by the daemon's bootstrap
// (typically [BootReasonPowerOnReboot] for cold start, or
// [BootReasonSoftwareUpdate] after an OTA).
func NewGeneralDiagnostics(bootReason uint8) *GeneralDiagnostics {
	g := &GeneralDiagnostics{
		startTime:  time.Now(),
		bootReason: bootReason,
	}
	// Seed DataVersion at a non-zero sentinel so DataVersionFilter=0 does not
	// produce false-positive cache hits.
	g.dataVersion.Bump()
	return g
}

// UpTimeSeconds returns the seconds elapsed since the cluster was
// constructed (= daemon start). Daemon shutdown hooks read it to
// compute the operational-hours delta to persist back to the store.
func (g *GeneralDiagnostics) UpTimeSeconds() uint64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return uint64(time.Since(g.startTime).Seconds())
}

// SetPersistedCounters seeds RebootCount and the
// pre-current-process TotalOperationalHours from external storage —
// the daemon's bootstrap loads the values from a SQLite row before
// the cluster is mounted, then bumps RebootCount and persists the
// updated state. Without seeding, the cluster reports a hardcoded
// RebootCount=1 placeholder and TotalOperationalHours == this
// process's uptime.
//
// Mirrors matter.js packages/node/src/behaviors/general-diagnostics/
// GeneralDiagnosticsServer.ts where bootReason / rebootCount are
// stamped from persistent state at construction. The wiring side is
// the daemon's responsibility; this method makes the cluster
// persistence-aware without forcing the wiring to land in lockstep.
//
// baseOperationalHours seeds the operational time in whole hours, the
// resolution a host persisted before [GeneralDiagnostics.SetPersistedOperationalTime]
// existed; a host that persists [GeneralDiagnostics.TotalOperationalTime]
// seeds it with that method afterwards instead.
func (g *GeneralDiagnostics) SetPersistedCounters(rebootCount uint16, baseOperationalHours uint32) {
	g.mu.Lock()
	g.rebootCount = rebootCount
	// time.Duration holds ~2.56 million hours; a larger count saturates.
	g.baseOperationalTime = time.Duration(min(int64(baseOperationalHours), int64(math.MaxInt64/time.Hour))) * time.Hour
	g.persistedSeeded = true
	g.mu.Unlock()
	// Bump DataVersion after counter seed so subscribers that cached the
	// pre-seed values get a version change notification.
	g.dataVersion.Bump()
}

// SetPersistedOperationalTime seeds the operational time earlier process
// lifetimes accumulated, at full resolution, so a node restarting more often
// than hourly still accrues TotalOperationalHours. A host persists
// [GeneralDiagnostics.TotalOperationalTime] and hands it back here at the
// next start; negative values are taken as zero.
//
// Mirrors matter.js packages/node/src/behaviors/general-diagnostics/
// GeneralDiagnosticsServer.ts: the persisted totalOperationalHoursCounter
// (milliseconds, updated every 5 min and on going offline) from which the
// totalOperationalHours getter derives Hours.of(counter + elapsed).
func (g *GeneralDiagnostics) SetPersistedOperationalTime(d time.Duration) {
	g.mu.Lock()
	g.baseOperationalTime = max(d, 0)
	g.mu.Unlock()
}

// TotalOperationalTime is the node's operational time: the persisted base
// plus this process's uptime. TotalOperationalHours is its floor in hours
// (matter.js Hours.of). Persist it, not the attribute, to keep the
// part-hour across a restart.
func (g *GeneralDiagnostics) TotalOperationalTime() time.Duration {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.baseOperationalTime + time.Since(g.startTime)
}

// BootReasonEvent is the payload for the Matter §11.12.8.1 BootReason
// event (id 0x0000, priority Critical). Mirrors matter.js
// packages/model/src/standard/elements/general-diagnostics.element.ts:74-79.
type BootReasonEvent struct {
	// BootReason carries the BootReasonEnum value that caused the
	// current boot (conformance M, field id 0x0).
	BootReason uint8
}

// Compile-time assertions: GeneralDiagnostics satisfies MatterClusterServer,
// the attribute-lister capability, the event-lister capability, the
// event-receiver (emitter wiring) capability, the command-lister capability,
// and MatterClusterDataVersion.
var (
	_ contract.ClusterServer                 = (*GeneralDiagnostics)(nil)
	_ contract.ClusterAttributeLister        = (*GeneralDiagnostics)(nil)
	_ contract.ClusterEventLister            = (*GeneralDiagnostics)(nil)
	_ contract.EventReceiver                 = (*GeneralDiagnostics)(nil)
	_ contract.ClusterCommandLister          = (*GeneralDiagnostics)(nil)
	_ contract.ClusterDataVersion            = (*GeneralDiagnostics)(nil)
	_ contract.ClusterCommandInvokePrivilege = (*GeneralDiagnostics)(nil)
)

// MatterDataVersion implements [contract.ClusterDataVersion].
// Returns the per-cluster monotonic counter seeded at construction.
// Mirrors chip's ember dirty-marking in
// src/app/clusters/general-diagnostics-server/ and matter.js behavior
// layer auto-tracking.
func (g *GeneralDiagnostics) MatterDataVersion() uint32 {
	return g.dataVersion.Current()
}

// MatterClusterID implements [contract.ClusterServer].
func (g *GeneralDiagnostics) MatterClusterID() uint32 { return gendiagClusterID }

// MinInvokePrivilege implements [contract.ClusterCommandInvokePrivilege].
// TestEventTrigger requires Manage (4) per Matter §11.12 (access "M").
// Mirrors matter.js packages/model/src/standard/elements/
// general-diagnostics.element.ts:90.
func (g *GeneralDiagnostics) MinInvokePrivilege(cmdID uint32) uint8 {
	switch cmdID {
	case gendiagCmdTestEventTrigger:
		return 4 // Manage
	default:
		return 3 // Operate — standard default
	}
}

// MatterRead implements [contract.ClusterServer].
func (g *GeneralDiagnostics) MatterRead(attrID uint32) (any, bool) {
	if attrID == gendiagAttrDeviceLoadStatus {
		// Without an accessing fabric; the provider runs outside g.mu.
		return g.MatterReadFiltered(context.Background(), attrID)
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	switch attrID {
	case gendiagAttrNetworkInterfaces:
		// Apple Home's HMMTRAccessoryServerBrowser reads this list to
		// build its "topology dictionary". An empty list (the prior
		// stub value) makes Apple log "No enumeration/topology dictionary
		// found" + "Nil supported link layer types" and tear the
		// fabric down via RemoveFabric ~5 s after Subscribe-Initial.
		// Enumerate every operational non-loopback interface and emit
		// at least one entry so HAP-ServiceMapper can resolve the link
		// layer type. Mirrors matter.js
		// packages/node/src/behaviors/general-diagnostics/
		// GeneralDiagnosticsServer.ts:networkInterfaces.
		return enumerateNetworkInterfaces(), true
	case gendiagAttrRebootCount:
		// Seeded by the daemon via [SetPersistedCounters]; falls back
		// to 1 (= "first boot") when persistence is not wired.
		if g.persistedSeeded {
			return g.rebootCount, true
		}
		return uint16(1), true
	case gendiagAttrUpTime:
		return uint64(g.upTime().Seconds()), true
	case gendiagAttrTotalOperationalHours:
		// Hours.of(base + uptime): the floor of the whole operational
		// time, so the part-hours of earlier runs add up (matter.js
		// GeneralDiagnosticsServer.ts totalOperationalHours getter).
		total := g.baseOperationalTime + time.Since(g.startTime)
		return uint32(total / time.Hour), true //nolint:gosec // 2^32 hours is ~490,000 years
	// BootReason / ActiveHardwareFaults / ActiveRadioFaults /
	// ActiveNetworkFaults are OPTIONAL on GeneralDiagnostics. matter.js's
	// `examples/device-bridge-onoff` Sample (Apple-pair-success byte-
	// dump) does NOT emit these four — Apple's MTRDevice-
	// Cache appears to treat the presence of empty fault-list arrays
	// as "unexpected schema" and refuses to persist the cluster.
	// BootReason still surfaces via the §11.12.8.1 BootReason event
	// (id 0x0000) on Subscribe-Initial, which Apple parses into
	// `estimated start time forward to ...`. The attribute itself is
	// not needed when the event flows.
	case gendiagAttrBootReason:
		return nil, false
	case gendiagAttrActiveHardwareFaults:
		return nil, false
	case gendiagAttrActiveRadioFaults:
		return nil, false
	case gendiagAttrActiveNetworkFaults:
		return nil, false
	case gendiagAttrTestEventTriggersEnabled:
		return g.testTrigger != nil, true

	case cluster.AttrGlobalFeatureMap:
		return gendiagFeatureDataModelTest, true
	case cluster.AttrGlobalClusterRevision:
		return gendiagClusterRevision, true
	}
	return nil, false
}

// MatterWrite always rejects — GeneralDiagnostics is read-only.
func (g *GeneralDiagnostics) MatterWrite(_ context.Context, attrID uint32, _ any) error {
	return fmt.Errorf("matter: GeneralDiagnostics is read-only (got attr 0x%04X)", attrID)
}

// MatterInvoke handles GeneralDiagnostics commands per Matter §11.12.7.
//
// Implemented:
//   - 0x01 TimeSnapshot — returns SystemTimeMs (monotonic since boot)
//     and PosixTimeMs (wall clock; null when the bridge is pre-time-sync).
//
// TestEventTrigger (0x00, conformance M) is enumerated but always fails
// enable-key validation with ConstraintError (see the handler); the bridge
// configures no test-event enable key. PayloadTestRequest (0x03, DMTEST) is
// not implemented.
func (g *GeneralDiagnostics) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	switch cmdID {
	case gendiagCmdTimeSnapshot:
		systemMs := uint64(time.Since(g.startTime).Milliseconds()) //nolint:gosec // G115: wall-clock millis are non-negative for any valid host time; see #20
		// PosixTimeMs only when the node's TimeSynchronization cluster holds
		// a UTC time: matter.js GeneralDiagnosticsServer.ts timeSnapshot
		// (agent.has(TimeSynchronizationBehavior) && utcTime !== null), and
		// TC-DGGEN-2.4 fails a node that reports one without it.
		resp := TimeSnapshotResponse{SystemTimeMs: systemMs}
		g.mu.RLock()
		clock := g.utcClock
		g.mu.RUnlock()
		if clock != nil {
			if now, ok := clock(); ok && now.UnixMilli() >= 0 {
				posix := uint64(now.UnixMilli()) //nolint:gosec // non-negative, checked above
				resp.PosixTimeMs = &posix
			}
		}
		return resp, nil
	case gendiagCmdPayloadTestRequest:
		return g.payloadTestRequest(fields)
	case gendiagCmdTestEventTrigger:
		return nil, g.testEventTrigger(ctx, fields)
	}
	return nil, im.UnsupportedCommandf("matter: GeneralDiagnostics command 0x%02X not supported", cmdID)
}

// DeviceLoadStruct is DeviceLoadStatus' value
// (general-diagnostics.element.ts:202-207): the node's subscription and
// Interaction Model message load, as matter.js computes it from its
// InteractionServer counters and SessionManager
// (GeneralDiagnosticsServer.ts deviceLoadStatus).
type DeviceLoadStruct struct {
	CurrentSubscriptions                  uint16
	CurrentSubscriptionsForFabric         uint16
	TotalSubscriptionsEstablished         uint32
	TotalInteractionModelMessagesSent     uint32
	TotalInteractionModelMessagesReceived uint32
}

// upTime is the time since the node came up. Mirrors matter.js
// GeneralDiagnosticsServer.ts upTime (c0a7978d, #4614): the larger of the
// elapsed time on the monotonic clock and on the wall clock — Go's
// monotonic reading, like Time.nowUs, stops while the host is suspended,
// and the wall clock folds the suspend back in — never below the highest
// value already reported, so a backward wall-clock step (an NTP correction)
// cannot lower it, and never negative.
func (g *GeneralDiagnostics) upTime() time.Duration {
	return g.upTimeFrom(time.Since(g.startTime), time.Now().Round(0).Sub(g.startTime.Round(0)))
}

// upTimeFrom folds the elapsed monotonic and wall-clock times into UpTime
// against the high-water mark.
func (g *GeneralDiagnostics) upTimeFrom(mono, wall time.Duration) time.Duration {
	up := max(mono, wall, 0)
	for {
		hw := g.upTimeHighWater.Load()
		if int64(up) <= hw {
			return time.Duration(hw)
		}
		if g.upTimeHighWater.CompareAndSwap(hw, int64(up)) {
			return up
		}
	}
}

// SetUTCClock couples TimeSnapshot to the node's TimeSynchronization
// cluster: clock returns its UTC time, false while it is null. Without a
// clock (no TimeSynchronization on the node) PosixTimeMs stays null, as
// matter.js GeneralDiagnosticsServer.timeSnapshot leaves it. Pass
// [TimeSynchronization.UTC] when both are mounted.
func (g *GeneralDiagnostics) SetUTCClock(clock func() (time.Time, bool)) {
	g.mu.Lock()
	g.utcClock = clock
	g.mu.Unlock()
}

// SetDeviceLoadProvider wires the source of DeviceLoadStatus. The bridge
// installs its own counters when the cluster is attached to the root
// (bridge.AttachRootClusters); a host only calls this to override them.
func (g *GeneralDiagnostics) SetDeviceLoadProvider(fn func(fabricIndex uint8) DeviceLoadStruct) {
	g.mu.Lock()
	g.deviceLoad = fn
	g.mu.Unlock()
}

// MatterReadFiltered implements [contract.FabricScopedReader] for
// DeviceLoadStatus, whose CurrentSubscriptionsForFabric counts the
// accessing fabric's subscriptions; every other attribute reads as
// MatterRead does.
func (g *GeneralDiagnostics) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	if attrID != gendiagAttrDeviceLoadStatus {
		return g.MatterRead(attrID) //nolint:contextcheck // MatterRead is the contract's context-free read; no request state applies
	}
	_, fabricIndex := im.FabricFilterFromContext(ctx)
	g.mu.RLock()
	fn := g.deviceLoad
	g.mu.RUnlock()
	if fn == nil {
		return DeviceLoadStruct{}, true
	}
	return fn(fabricIndex), true
}

// TestEventTriggerHandler performs one test event trigger (Matter §11.12.7.1).
// It returns an error for a trigger the host does not support; a plain
// error answers InvalidCommand — matter.js GeneralDiagnosticsServer.ts
// triggerTestEvent's default — and an [im.StatusCodeError] answers its own
// status.
type TestEventTriggerHandler func(ctx context.Context, eventTrigger uint64) error

// TestEnableKeySize is the length of the EnableKey field and the device's
// test enable key (general-diagnostics.element.ts, "16" octets).
const TestEnableKeySize = 16

// EnableTestEventTriggers arms TestEventTrigger: TestEventTriggersEnabled
// reads true, and a TestEventTrigger carrying key reaches handler. A device
// in the field never calls this — the specification reserves test event
// triggers for certification and development (§11.12.6.9), which is why
// matter.js leaves them off unless a deviceTestEnableKey is configured
// (GeneralDiagnosticsServer.ts initialize). key must be 16 bytes and not
// all zero, the value matter.js treats as "not enabled".
func (g *GeneralDiagnostics) EnableTestEventTriggers(key []byte, handler TestEventTriggerHandler) error {
	if len(key) != TestEnableKeySize {
		return fmt.Errorf("matter: test enable key is %d bytes, want %d", len(key), TestEnableKeySize)
	}
	if allZero(key) {
		return errors.New("matter: an all-zero test enable key means test event triggers are disabled")
	}
	if handler == nil {
		return errors.New("matter: test event triggers need a handler")
	}
	g.mu.Lock()
	g.testEnableKey = append([]byte(nil), key...)
	g.testTrigger = handler
	g.mu.Unlock()
	g.dataVersion.Bump()
	return nil
}

// testEventTrigger validates the enable key and runs the trigger. Mirrors
// matter.js GeneralDiagnosticsServer.ts #validateTestEnabledKey and
// testEventTrigger: an all-zero or mismatching key answers ConstraintError,
// an unsupported trigger InvalidCommand.
func (g *GeneralDiagnostics) testEventTrigger(ctx context.Context, fields any) error {
	key, trigger, ok := decodeTestEventTrigger(fields)
	if !ok {
		return gendiagStatusErr{im.StatusInvalidCommand, "TestEventTrigger: malformed fields"}
	}
	if len(key) != TestEnableKeySize {
		return gendiagStatusErr{im.StatusConstraintError, "TestEventTrigger: EnableKey must be 16 bytes"}
	}
	if allZero(key) {
		return gendiagStatusErr{im.StatusConstraintError, "TestEventTrigger: invalid test enable key, all zeros"}
	}
	g.mu.RLock()
	want, handler := g.testEnableKey, g.testTrigger
	g.mu.RUnlock()
	if handler == nil || subtle.ConstantTimeCompare(key, want) != 1 {
		return gendiagStatusErr{im.StatusConstraintError, "TestEventTrigger: invalid test enable key"}
	}
	if err := handler(ctx, trigger); err != nil {
		if _, ok := errors.AsType[im.StatusCodeError](err); ok {
			return err
		}
		return gendiagStatusErr{im.StatusInvalidCommand, fmt.Sprintf("TestEventTrigger 0x%016X: %v", trigger, err)}
	}
	return nil
}

// PayloadTestResponse is the DMTEST response: Payload is Count copies of
// Value (general-diagnostics.element.ts:119).
type PayloadTestResponse struct {
	Payload []byte
}

// payloadTestRequest implements PayloadTestRequest, mirroring matter.js
// GeneralDiagnosticsServer.ts payloadTestRequest: the enable key is checked
// as for TestEventTrigger, test event triggers must be enabled
// (ConstraintError otherwise), and a payload too large for one message is
// ResourceExhausted. Fields: [0] EnableKey octets, [1] Value uint8,
// [2] Count uint16 (max 2048).
func (g *GeneralDiagnostics) payloadTestRequest(fields any) (any, error) {
	m, ok := fields.(map[uint8]any)
	if !ok {
		return nil, gendiagStatusErr{im.StatusInvalidCommand, "PayloadTestRequest: malformed fields"}
	}
	key, _ := m[0].([]byte)
	value, ok1 := m[1].(uint64)
	count, ok2 := m[2].(uint64)
	if !ok1 || !ok2 || value > 0xFF {
		return nil, gendiagStatusErr{im.StatusInvalidCommand, "PayloadTestRequest: malformed fields"}
	}
	if count > payloadTestMaxCount {
		return nil, gendiagStatusErr{im.StatusConstraintError, "PayloadTestRequest: Count above 2048"}
	}
	if len(key) != TestEnableKeySize || allZero(key) {
		return nil, gendiagStatusErr{im.StatusConstraintError, "PayloadTestRequest: invalid test enable key"}
	}
	g.mu.RLock()
	want, enabled := g.testEnableKey, g.testTrigger != nil
	g.mu.RUnlock()
	if !enabled || subtle.ConstantTimeCompare(key, want) != 1 {
		return nil, gendiagStatusErr{im.StatusConstraintError, "PayloadTestRequest: test event triggers are disabled or the key does not match"}
	}
	if count > payloadTestMaxResponse {
		return nil, gendiagStatusErr{im.StatusResourceExhausted, "PayloadTestRequest: response too large"}
	}
	return PayloadTestResponse{Payload: bytes.Repeat([]byte{byte(value)}, int(count))}, nil
}

// TestEventTriggerRequest is the decoded TestEventTrigger command
// (general-diagnostics.element.ts: EnableKey tag 0, EventTrigger tag 1).
type TestEventTriggerRequest struct {
	EnableKey    []byte
	EventTrigger uint64
}

// decodeTestEventTrigger accepts the typed request or the generic
// tag-keyed map the bridge's fields reader produces for it.
func decodeTestEventTrigger(fields any) (enableKey []byte, trigger uint64, ok bool) {
	switch f := fields.(type) {
	case TestEventTriggerRequest:
		return f.EnableKey, f.EventTrigger, true
	case *TestEventTriggerRequest:
		if f == nil {
			return nil, 0, false
		}
		return f.EnableKey, f.EventTrigger, true
	case map[uint8]any:
		key, ok1 := f[0].([]byte)
		trigger, ok2 := f[1].(uint64)
		return key, trigger, ok1 && ok2
	}
	return nil, 0, false
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// gendiagStatusErr answers a GeneralDiagnostics command with a specific
// Interaction Model status.
type gendiagStatusErr struct {
	code im.StatusCode
	msg  string
}

func (e gendiagStatusErr) Error() string                   { return "matter: GeneralDiagnostics " + e.msg }
func (e gendiagStatusErr) MatterStatusCode() im.StatusCode { return e.code }

var _ im.StatusCodeError = gendiagStatusErr{}

// TimeSnapshotResponse mirrors Matter §11.12.7.3.
// Mirrors matter.js packages/model/src/standard/elements/
// general-diagnostics.element.ts:99-102.
type TimeSnapshotResponse struct {
	// SystemTimeMs is the bridge's monotonic time since boot,
	// expressed in milliseconds.
	SystemTimeMs uint64
	// PosixTimeMs is wall-clock time (Unix epoch ms), nullable. Set
	// to nil when the bridge has no synchronised wall clock yet.
	PosixTimeMs *uint64
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister].
// Lists the command IDs the server handles via MatterInvoke.
// Mirrors matter.js packages/model/src/standard/elements/
// general-diagnostics.element.ts accepted commands.
//
// TestEventTrigger (0x00) is mandatory (conformance M) and is enumerated;
// the handler always rejects it with ConstraintError (no enable key). Only
// PayloadTestRequest (0x03, DMTEST) stays unlisted.
func (g *GeneralDiagnostics) MatterAcceptedCommands() []uint32 {
	return []uint32{
		gendiagCmdTestEventTrigger,   // 0x00
		gendiagCmdTimeSnapshot,       // 0x01
		gendiagCmdPayloadTestRequest, // 0x03, DMTEST
	}
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister].
// Lists the response command IDs this server may emit.
// Mirrors matter.js packages/model/src/standard/elements/
// general-diagnostics.element.ts generated commands.
func (g *GeneralDiagnostics) MatterGeneratedCommands() []uint32 {
	return []uint32{
		gendiagCmdTimeSnapshotResp,    // 0x02
		gendiagCmdPayloadTestResponse, // 0x04, DMTEST
	}
}

// MatterReportable returns the subscribe-able attributes.
func (g *GeneralDiagnostics) MatterReportable() []uint32 {
	return []uint32{gendiagAttrUpTime, gendiagAttrBootReason}
}

// MatterAttributes lists every GeneralDiagnostics (0x0033) attribute
// the server implements via MatterRead. Apple Home's HAP service
// rebuild reads the full attribute set; without this the dispatcher
// falls back to MatterReportable's two-attribute surface.
func (g *GeneralDiagnostics) MatterAttributes() []uint32 {
	// BootReason + 3× ActiveFaults are OPTIONAL per Matter Core
	// §11.12.6 and matter.js's bridge sample does not advertise them
	// (verified via Apple-pair-success byte-dump). BootReason
	// surfaces via the §11.12.8.1 BootReason *event* which we emit on
	// Subscribe-Initial via EmitBootReason() — Apple parses that event
	// into the `estimated start time` log line and the BootReason
	// attribute on the wire is redundant.
	return []uint32{
		gendiagAttrNetworkInterfaces,
		gendiagAttrRebootCount,
		gendiagAttrUpTime,
		gendiagAttrTotalOperationalHours,
		gendiagAttrTestEventTriggersEnabled,
		gendiagAttrDeviceLoadStatus,
	}
}

// MatterEvents implements [contract.ClusterEventLister] so the
// dispatcher synthesises the global EventList (0xFFFA) attribute
// correctly for this cluster.
func (g *GeneralDiagnostics) MatterEvents() []uint32 {
	return []uint32{gendiagEventBootReason}
}

// SetMatterEventEmitter implements [contract.EventReceiver].
// Called by the bridge during topology assembly so [EmitBootReason]
// can fire the §11.12.8.1 BootReason event without the cluster holding
// a direct reference to the bridge. Idempotent — re-wiring during
// topology rebuild replaces the emitter cleanly.
func (g *GeneralDiagnostics) SetMatterEventEmitter(emitter contract.EventEmitter) {
	g.mu.Lock()
	g.emitter = emitter
	g.mu.Unlock()
}

// SetEndpoint stamps the endpoint id this GeneralDiagnostics server is
// mounted on. Matter events carry the (endpoint, cluster, event) triple
// so the commissioner can fan them out to the right subscription path.
// The root endpoint is always 0 in standard topologies, but the bridge
// injects the real value here so the cluster does not hard-code it.
func (g *GeneralDiagnostics) SetEndpoint(endpoint uint16) {
	g.mu.Lock()
	g.endpoint = endpoint
	g.mu.Unlock()
}

// EmitBootReason fires the Matter §11.12.8.1 BootReason event (id
// 0x0000, priority Critical) via the wired [contract.EventEmitter].
// No-op when the emitter has not been wired yet — the daemon calls this
// once at startup after topology assembly. Mirrors matter.js
// packages/node/src/behaviors/general-diagnostics/
// GeneralDiagnosticsServer.ts where the BootReason event is emitted
// on startup with the persisted boot-reason value.
func (g *GeneralDiagnostics) EmitBootReason() {
	g.mu.RLock()
	emitter := g.emitter
	endpoint := g.endpoint
	bootReason := g.bootReason
	g.mu.RUnlock()
	if emitter == nil {
		slog.Default().Warn("matter.general_diagnostics.emit_bootreason_skipped",
			slog.String("reason", "emitter nil — wiring race"))
		return
	}
	slog.Default().Info("matter.general_diagnostics.emit_bootreason",
		slog.Any("endpoint", endpoint), slog.Any("boot_reason", bootReason))
	emitter.MatterEmitEvent(endpoint, gendiagClusterID, gendiagEventBootReason,
		BootReasonEvent{BootReason: bootReason},
		contract.EventPriorityCritical)
}

// enumerateNetworkInterfaces walks net.Interfaces() and projects every
// non-loopback interface that has at least one assigned address into a
// [NetworkInterfaceStruct]. The returned slice is what
// `NetworkInterfaces` reports on the wire — guaranteed non-empty in
// practice (any host that can talk Matter has at least one routable
// interface). Loopback interfaces are excluded so they cannot
// accidentally satisfy the HAP-ServiceMapper's "at least one
// non-loopback layer" check.
//
// Heuristics:
//
//   - InterfaceType is Ethernet for any name starting with "en" or
//     "eth"; WiFi for "wl" prefixes; Thread for "tr"; Unspecified
//     otherwise. The bridge does not probe link technology so a
//     macOS "en0" (which can be either WiFi or Ethernet) defaults to
//     Ethernet — Apple's mapper treats both equivalently for a
//     non-Thread / non-Cellular accessory.
//   - HardwareAddress is canonicalised to 6 or 8 bytes. Synthetic
//     interfaces with empty MACs return an empty slice, which Matter
//     allows.
//   - IPv4 addresses are emitted as the raw 4-byte form. IPv6
//     addresses are emitted as the raw 16-byte form. Link-local
//     (fe80::/10) addresses are kept — Apple's commissioner uses
//     them to route inside the home subnet.
//
// Apple HAP-mapper hard limits — picked empirically from observed
// Apple Home iOS 17+ behaviour. Exceeding either makes Apple log
// "No known schema for decoding attribute value" + HAPErrorDomain
// Code=14 and tear the pair down.
const (
	maxIfacesForApple = 4 // matter.js spec has no overall cap; Apple's mapper does
	maxIPv4PerIface   = 4 // matter.js: list constraint "max 4"
	maxIPv6PerIface   = 8 // matter.js: list constraint "max 8"
)

func enumerateNetworkInterfaces() []NetworkInterfaceStruct {
	ifaces, err := net.Interfaces()
	if err != nil || len(ifaces) == 0 {
		return []NetworkInterfaceStruct{syntheticEthernetIface()}
	}
	out := make([]NetworkInterfaceStruct, 0, len(ifaces))
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		// matter.js NetworkInterface struct field 4 (HardwareAddress)
		// is `type: "hwadr"` with exact constraint 6 or 8 bytes.
		// Apple's IM-decoder rejects the whole struct (and on cascade
		// the entire Subscribe-Initial) with "No known schema for
		// decoding attribute value" when HardwareAddress is empty or
		// odd length. Synthetic / virtual interfaces (utun, awdl, ...)
		// often have either no MAC or a 0-byte MAC — skip them.
		if len(iface.HardwareAddr) != 6 && len(iface.HardwareAddr) != 8 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil || len(addrs) == 0 {
			continue
		}
		entry := NetworkInterfaceStruct{
			Name:            iface.Name,
			IsOperational:   iface.Flags&net.FlagUp != 0,
			InterfaceType:   classifyInterface(iface.Name),
			HardwareAddress: append([]byte(nil), iface.HardwareAddr...),
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if v4 := ipnet.IP.To4(); v4 != nil {
				if len(entry.IPv4Addresses) < maxIPv4PerIface {
					entry.IPv4Addresses = append(entry.IPv4Addresses, append([]byte(nil), v4...))
				}
			} else if v6 := ipnet.IP.To16(); v6 != nil {
				if len(entry.IPv6Addresses) < maxIPv6PerIface {
					entry.IPv6Addresses = append(entry.IPv6Addresses, append([]byte(nil), v6...))
				}
			}
		}
		out = append(out, entry)
		if len(out) >= maxIfacesForApple {
			break
		}
	}
	if len(out) == 0 {
		// No real interface qualifies (loopback-only host, no MACs,
		// no addresses). Fall back to one synthetic Ethernet entry —
		// matter.js spec accepts hardware-address fixed at zeros for
		// synthetic accessories, and Apple's HAP mapper just needs ONE
		// entry to build the topology dictionary.
		return []NetworkInterfaceStruct{syntheticEthernetIface()}
	}
	return out
}

// syntheticEthernetIface returns a fallback NetworkInterface struct
// when host enumeration produces nothing valid. All-zero MAC keeps
// the matter.js `hwadr` constraint (exact 6 bytes); zero address
// lists keep the spec's max-4 / max-8 constraints trivially.
func syntheticEthernetIface() NetworkInterfaceStruct {
	return NetworkInterfaceStruct{
		Name:            "eth0",
		IsOperational:   true,
		HardwareAddress: make([]byte, 6),
		InterfaceType:   InterfaceTypeEthernet,
	}
}

// classifyInterface maps an OS interface name to a Matter
// InterfaceType enum. Defaults to Ethernet for the common Linux/macOS
// prefixes (en0, eth0) — Apple's HAP mapper treats Ethernet + WiFi
// identically for non-Thread accessories, so the heuristic miss is
// harmless. A loopback name should never reach this function (the
// caller filters it out), but guard with Unspecified just in case.
func classifyInterface(name string) uint8 {
	n := strings.ToLower(name)
	switch {
	case strings.HasPrefix(n, "lo"):
		return InterfaceTypeUnspecified
	case strings.HasPrefix(n, "wl") || strings.HasPrefix(n, "wlan") || strings.HasPrefix(n, "wifi"):
		return InterfaceTypeWiFi
	case strings.HasPrefix(n, "tr") || strings.HasPrefix(n, "thread"):
		return InterfaceTypeThread
	case strings.HasPrefix(n, "en") || strings.HasPrefix(n, "eth"):
		return InterfaceTypeEthernet
	}
	return InterfaceTypeUnspecified
}
