// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"fmt"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// TimeSynchronization implements the minimum required surface of the
// Matter TimeSynchronization cluster (0x0038) per Matter Core
// Specification 1.5.1 §11.16. The bridge exposes UTCTime + Granularity
// only — feature flags (TZ, NTPC, NTPS, TSC) all stay off, so the
// optional attributes (TimeSource, TrustedTimeSource, DefaultNTP,
// TimeZone, DSTOffset, etc.) intentionally surface as
// UnsupportedAttribute.
//
// chip-tool reads UTCTime + Granularity during ReadCommissioningInfo;
// returning sensible values keeps the commissioning flow clean
// instead of producing 0xC3 UnsupportedCluster errors.
type TimeSynchronization struct{}

const (
	timeSyncClusterID       uint32 = 0x0038
	timeSyncClusterRevision uint16 = 2 // Matter 1.5.1 §11.16

	timeSyncAttrUTCTime     uint32 = 0x0000
	timeSyncAttrGranularity uint32 = 0x0001
)

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
	now := time.Now()
	if now.Unix() < matterEpochOffsetSec {
		return time.Time{}, false
	}
	return now, true
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister]:
// SetUTCTime (0x00, conformance M). Without it the dispatcher answered an
// empty AcceptedCommandList for a mandatory command (TC-IDM-10.x).
func (t *TimeSynchronization) MatterAcceptedCommands() []uint32 {
	return []uint32{timeSyncCmdSetUTCTime}
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister]:
// SetUTCTime has no response command; the other commands that do belong
// to features the server does not advertise.
func (t *TimeSynchronization) MatterGeneratedCommands() []uint32 { return []uint32{} }

// NewTimeSynchronization returns the cluster server. Stateless —
// every read is computed at call time from `time.Now`.
func NewTimeSynchronization() *TimeSynchronization { return &TimeSynchronization{} }

var (
	_ contract.ClusterServer          = (*TimeSynchronization)(nil)
	_ contract.ClusterAttributeLister = (*TimeSynchronization)(nil)
	_ contract.ClusterCommandLister   = (*TimeSynchronization)(nil)
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
	case cluster.AttrGlobalFeatureMap:
		return uint32(0), true // no optional features advertised
	case cluster.AttrGlobalClusterRevision:
		return timeSyncClusterRevision, true
	}
	return nil, false
}

// MatterWrite implements [contract.ClusterServer]. Every
// attribute is read-only on the bridge — clients that try to set
// TimeSource etc. get UnsupportedWrite.
func (t *TimeSynchronization) MatterWrite(_ context.Context, attrID uint32, _ any) error {
	return fmt.Errorf("matter: TimeSynchronization attribute 0x%04X is read-only", attrID)
}

// timeSyncCmdSetUTCTime is the SetUTCTime command ID (Matter §11.16.9.1).
// Mirrors matter.js packages/model/src/standard/elements/time-synchronization.element.ts
// command id 0x00.
const timeSyncCmdSetUTCTime uint32 = 0x00

// MatterInvoke implements [contract.ClusterServer].
// SetUTCTime (0x00) is a mandatory command per Matter §11.16.9.1 when the
// UTC feature bit is advertised. The bridge does not adjust the host clock,
// so the command is accepted and returns Success without acting —
// controllers that send SetUTCTime receive a well-formed response instead
// of UnsupportedCommand, which some implementations treat as a fatal
// commissioning error.
// All other commands require feature flags the bridge does not advertise;
// the IM dispatcher rejects them at the path level.
func (t *TimeSynchronization) MatterInvoke(_ context.Context, cmdID uint32, _ any) (any, error) {
	if cmdID == timeSyncCmdSetUTCTime {
		// Accept the command and return Success. The bridge's clock is
		// managed by the host OS; no adjustment is applied here.
		// Mirrors matter.js TimeSynchronizationServer.ts::setUtcTime which
		// stores the value — we omit the store because the bridge is not a
		// time-coordinator.
		return nil, nil
	}
	return nil, im.UnsupportedCommandf("matter: TimeSynchronization command 0x%02X not supported", cmdID)
}

// MatterReportable lists subscribe-able attributes.
func (t *TimeSynchronization) MatterReportable() []uint32 {
	return []uint32{timeSyncAttrUTCTime, timeSyncAttrGranularity}
}

// MatterAttributes lists every TimeSynchronization (0x0038) attribute
// the server implements via MatterRead. Apple Home's HAP service
// rebuild reads the full attribute set; without this the dispatcher
// falls back to MatterReportable's two-attribute surface.
func (t *TimeSynchronization) MatterAttributes() []uint32 {
	return []uint32{timeSyncAttrUTCTime, timeSyncAttrGranularity}
}
