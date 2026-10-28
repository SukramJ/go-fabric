// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	timesyncdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/clockvar"
)

// TimeSynchronization is the Matter TimeSynchronization cluster (0x0038).
// [NewTimeSynchronization] serves UTCTime and Granularity, with no
// feature; [NewTimeSynchronizationWith] adds the features TZ, NTPC, NTPS
// and TSC after connectedhomeip (time_synchronization_features.go).
// UTCTime is the host clock in either case.
//
// chip-tool reads UTCTime + Granularity during ReadCommissioningInfo;
// returning sensible values keeps the commissioning flow clean
// instead of producing 0xC3 UnsupportedCluster errors.
type TimeSynchronization struct {
	// clock is the UTC source (see [TimeSynchronization.SetClock]).
	clock clockvar.Var
	// inst is the definition bound to the selection; cfg the features'
	// configuration.
	inst *spec.Instance
	cfg  TimeSynchronizationConfig
	// mu guards state.
	mu      sync.Mutex
	state   timeSyncState
	changes cluster.AttributeChanges
	events  spec.Events
}

// SetClock sets the clock UTCTime is read from; nil is [clock.System].
func (t *TimeSynchronization) SetClock(c clock.Clock) { t.clock.Store(c) }

const (
	timeSyncClusterID       = timesyncdef.ClusterID
	timeSyncClusterRevision = timesyncdef.Revision

	timeSyncAttrUTCTime     = timesyncdef.AttrUtcTime
	timeSyncAttrGranularity = timesyncdef.AttrGranularity
)

// timeSyncInst is the definition bound to no feature
// (cluster/spec/timesynchronization, ADR 0013, from matter.js
// time-synchronization.element.ts) — UTCTime, Granularity and SetUTCTime;
// a server's own instance is bound to its selection (t.inst).
var timeSyncInst = mustInstance(timesyncdef.Definition, spec.Options{})

// GranularityEnum values per Matter §11.16.5.1.
const (
	GranularityNoTime          uint8 = 0
	GranularityMinutesGran     uint8 = 1
	GranularitySecondsGran     uint8 = 2
	GranularityMillisecGran    uint8 = 3
	GranularityMicrosecondGran uint8 = 4
)

// matterEpochOffsetSec is the Matter epoch (2000-01-01 00:00:00 UTC, §A.2)
// in Unix seconds: 30 years + 7 leap days.
const matterEpochOffsetSec int64 = 946684800

// UTC returns the node's UTC time — the host clock — and false while the
// UTCTime attribute is null (a host clock before the Matter epoch). It is
// the clock [GeneralDiagnostics.SetUTCClock] takes, so TimeSnapshot
// carries a PosixTimeMs exactly when UTCTime is not null.
func (t *TimeSynchronization) UTC() (time.Time, bool) {
	now := t.clock.Load().Now()
	if now.Unix() < matterEpochOffsetSec {
		return time.Time{}, false
	}
	return now, true
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister]:
// SetUTCTime (0x00, conformance M). Without it the dispatcher answered an
// empty AcceptedCommandList for a mandatory command (TC-IDM-10.x).
// The features add SetTrustedTimeSource (TSC), SetTimeZone and
// SetDSTOffset (TZ) and SetDefaultNTP (NTPC), as chip's AcceptedCommands
// does (TimeSynchronizationCluster.cpp:1066-1094 at 6170af84).
func (t *TimeSynchronization) MatterAcceptedCommands() []uint32 {
	return t.instance().MatterAcceptedCommands()
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister]:
// SetTimeZoneResponse with TZ (:1096-1107), none otherwise.
func (t *TimeSynchronization) MatterGeneratedCommands() []uint32 {
	if g := t.instance().MatterGeneratedCommands(); len(g) > 0 {
		return g
	}
	return []uint32{}
}

// MatterEvents implements [contract.ClusterEventLister]: TimeFailure, and
// the events of the selection's features.
func (t *TimeSynchronization) MatterEvents() []uint32 { return t.instance().MatterEvents() }

// SetMatterEventEmitter implements [contract.EventReceiver].
func (t *TimeSynchronization) SetMatterEventEmitter(emitter contract.EventEmitter) {
	t.events.SetEmitter(emitter)
}

// OnAttributeChange implements [contract.AttributeChangeNotifier]: the
// features' attributes a command or the host changed.
func (t *TimeSynchronization) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return t.changes.OnAttributeChange(cb)
}

// instance is the server's bound definition; the zero server's is
// timeSyncInst.
func (t *TimeSynchronization) instance() *spec.Instance {
	if t.inst == nil {
		return timeSyncInst
	}
	return t.inst
}

// NewTimeSynchronization returns the cluster server with no feature:
// UTCTime from the clock, Granularity fixed.
func NewTimeSynchronization() *TimeSynchronization { return &TimeSynchronization{} }

var (
	_ contract.ClusterServer           = (*TimeSynchronization)(nil)
	_ contract.ClusterAttributeLister  = (*TimeSynchronization)(nil)
	_ contract.ClusterCommandLister    = (*TimeSynchronization)(nil)
	_ contract.ClusterEventLister      = (*TimeSynchronization)(nil)
	_ contract.EventReceiver           = (*TimeSynchronization)(nil)
	_ contract.AttributeChangeNotifier = (*TimeSynchronization)(nil)
)

// MatterClusterID implements [contract.ClusterServer].
func (t *TimeSynchronization) MatterClusterID() uint32 { return timeSyncClusterID }

// MatterRead implements [contract.ClusterServer]. UTCTime is
// reported as Matter's epoch_us (microseconds since 2000-01-01 UTC,
// per §A.2). Granularity is fixed at MILLISECONDS_GRANULARITY since
// the bridge syncs from the host clock (typically NTP-disciplined).
func (t *TimeSynchronization) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case timeSyncAttrUTCTime:
		now, ok := t.UTC()
		if !ok {
			return nil, true // null per spec when host clock is pre-Matter-epoch
		}
		// Matter §A.2: epoch is 2000-01-01 00:00:00 UTC.
		return uint64(now.UnixMicro() - matterEpochOffsetSec*1_000_000), true //nolint:gosec // non-negative, UTC checked it
	case timeSyncAttrGranularity:
		return GranularityMillisecGran, true
	case cluster.AttrGlobalFeatureMap, cluster.AttrGlobalClusterRevision:
		return t.instance().ReadGlobal(attrID)
	}
	if !t.instance().Serves(attrID) {
		return nil, false
	}
	if attrID == timesyncdef.AttrLocalTime {
		if v, ok := t.localTime(); ok {
			return v, true
		}
		return nil, true // null without a DST list, as chip answers it (:309-317)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.readFeatureLocked(attrID)
}

// MatterWrite implements [contract.ClusterServer]. Every
// attribute is read-only on the bridge — clients that try to set
// TimeSource etc. get UnsupportedWrite.
func (t *TimeSynchronization) MatterWrite(_ context.Context, attrID uint32, value any) error {
	if !t.instance().Serves(attrID) {
		_, err := t.instance().ValidateWrite(attrID, value, nil)
		return err
	}
	return timeSyncReadOnlyErr{fmt.Sprintf("core: TimeSynchronization attribute 0x%04X is read-only", attrID)}
}

// timeSyncReadOnlyErr answers a write UNSUPPORTED_WRITE, as the comment
// above promised; a plain error read as FAILURE.
type timeSyncReadOnlyErr struct{ msg string }

func (e timeSyncReadOnlyErr) Error() string                 { return e.msg }
func (timeSyncReadOnlyErr) MatterStatusCode() im.StatusCode { return im.StatusUnsupportedWrite }

// timeSyncCmdSetUTCTime is the SetUTCTime command ID (Matter §11.16.9.1).
// Mirrors matter.js packages/model/src/standard/elements/time-synchronization.element.ts
// command id 0x00.
const timeSyncCmdSetUTCTime = timesyncdef.CmdSetUtcTime

// MatterInvoke implements [contract.ClusterServer].
// SetUTCTime (0x00) is a mandatory command per Matter §11.16.9.1 when the
// UTC feature bit is advertised. The bridge does not adjust the host clock,
// so the command is accepted and returns Success without acting —
// controllers that send SetUTCTime receive a well-formed response instead
// of UnsupportedCommand, which some implementations treat as a fatal
// commissioning error.
// All other commands require feature flags the bridge does not advertise;
// the IM dispatcher rejects them at the path level.
func (t *TimeSynchronization) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	if cmdID != timeSyncCmdSetUTCTime && t.instance().Accepts(cmdID) {
		// chip's InvokeCommand (:1030-1064).
		switch cmdID {
		case timesyncdef.CmdSetTrustedTimeSource:
			return nil, t.handleSetTrustedTimeSource(ctx, fields)
		case timesyncdef.CmdSetTimeZone:
			return t.handleSetTimeZone(ctx, fields)
		case timesyncdef.CmdSetDstOffset:
			return nil, t.handleSetDSTOffset(ctx, fields)
		case timesyncdef.CmdSetDefaultNtp:
			return nil, t.handleSetDefaultNTP(ctx, fields)
		}
	}
	if cmdID == timeSyncCmdSetUTCTime {
		return nil, handleSetUTCTime(fields)
	}
	return nil, im.UnsupportedCommandf("core: TimeSynchronization command 0x%02X not supported", cmdID)
}

// MatterReportable lists subscribe-able attributes: UTCTime, Granularity
// and the features' attributes but the fixed ones.
func (t *TimeSynchronization) MatterReportable() []uint32 {
	out := []uint32{timeSyncAttrUTCTime, timeSyncAttrGranularity}
	for _, id := range t.instance().MatterAttributes() {
		switch id {
		case timesyncdef.AttrTrustedTimeSource, timesyncdef.AttrDefaultNtp, timesyncdef.AttrTimeZone,
			timesyncdef.AttrDstOffset, timesyncdef.AttrLocalTime, timesyncdef.AttrNtpServerAvailable:
			out = append(out, id)
		}
	}
	return out
}

// MatterAttributes lists every TimeSynchronization (0x0038) attribute
// the server implements via MatterRead. Apple Home's HAP service
// rebuild reads the full attribute set; without this the dispatcher
// falls back to MatterReportable's two-attribute surface.
func (t *TimeSynchronization) MatterAttributes() []uint32 {
	return t.instance().MatterAttributes()
}
