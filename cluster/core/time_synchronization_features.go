// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"strings"

	"github.com/SukramJ/go-fabric/cluster/spec"
	timesyncdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// The TimeSynchronization features TZ, NTPC, NTPS and TSC, served after
// connectedhomeip: matter.js's TimeSynchronizationServer is the generated
// behaviour with no command handlers (TimeSynchronizationServer.ts:14 at
// 862ad75f), so chip's src/app/clusters/time-synchronization-server at
// 6170af84 is the authority — the attribute gating
// (TimeSynchronizationCluster.cpp:186-248), the reads (:250-331), the
// stored state (:333-353, TimeSyncDataProvider.h:58-75), the four command
// handlers (:1138-1275) with the setters they call (:571-830) and the
// state updates LocalTime runs (:874-1007), and the fabric removal
// (:1020-1028). UTCTime stays the node's clock: chip's SetUTCTime sets the
// system clock (:74-86, :856-872); this server keeps answering it without
// effect (time_synchronization.go), and its Granularity is the host
// clock's. chip's trusted-time-source client (reading UTCTime from the
// trusted node, :379-556) is not ported: the trusted time source is
// stored, reported and announced, the node's clock stays its UTC.

// TimeSynchronizationConfig selects the features a [TimeSynchronization]
// serves and what each needs of the host.
type TimeSynchronizationConfig struct {
	// Features is the FeatureMap selection: [timesyncdef.FeatureTimeZone]
	// (TZ), [timesyncdef.FeatureNtpClient] (NTPC),
	// [timesyncdef.FeatureNtpServer] (NTPS) and
	// [timesyncdef.FeatureTimeSyncClient] (TSC). Zero serves UTCTime and
	// Granularity only.
	Features timesyncdef.Feature
	// TimeZoneDatabase is the TimeZoneDatabase attribute (TZ): what the
	// node knows of time-zone names; nil is None, chip's
	// StartupConfiguration default (TimeSynchronizationCluster.h:93).
	TimeZoneDatabase *timesyncdef.TimeZoneDatabaseEnum
	// DSTOffsetsFor answers SetTimeZone from the node's database when
	// TimeZoneDatabase is not None (TZ): the DST offsets of the time zone
	// named, true when it has them; SetTimeZoneResponse then states that
	// no DSTOffset is required and the offsets are stored. chip's
	// Delegate::HandleUpdateDSTOffset (TimeSynchronizationCluster.cpp:
	// 1191-1199); nil knows none.
	DSTOffsetsFor func(name string) ([]timesyncdef.DSTOffsetStruct, bool)
	// SupportsDNSResolve is the SupportsDNSResolve attribute (NTPC): the
	// node resolves a DefaultNTP domain name; without it SetDefaultNTP
	// refuses a domain (:1259-1265).
	SupportsDNSResolve bool
	// NTPServerAvailable is the NTPServerAvailable attribute (NTPS): the
	// node runs an NTP server ([TimeSynchronization.SetNTPServerAvailable]
	// changes it).
	NTPServerAvailable bool
	// Store keeps TrustedTimeSource, DefaultNTP, TimeZone and DSTOffset
	// across restarts (chip's TimeSyncDataProvider); nil keeps them in
	// memory.
	Store store.Settings
	// Logger logs a store failure; nil is slog.Default().
	Logger *slog.Logger
}

// The list capacities chip configures (CHIPConfig.h:1683, :1696 at
// 6170af84, CHIP_CONFIG_TIME_ZONE_LIST_MAX_SIZE and
// CHIP_CONFIG_DST_OFFSET_LIST_MAX_SIZE) and the bounds the handlers hold
// (TimeSynchronizationCluster.cpp:658, :678, TimeSyncDataProvider.h:37;
// DefaultNTP's "max 128").
const (
	timeZoneListMaxSize  = 2
	dstOffsetListMaxSize = 2
	timeZoneOffsetMin    = -43200
	timeZoneOffsetMax    = 50400
	timeZoneNameMaxLen   = 64
	defaultNTPMaxLen     = 128
)

// The store keys (chip's TimeSyncDataProvider keys, TimeSyncDataProvider.cpp).
const (
	timeSyncKeyTrustedTimeSource = "timesync.trusted_time_source"
	timeSyncKeyDefaultNTP        = "timesync.default_ntp"
	timeSyncKeyTimeZone          = "timesync.time_zone"
	timeSyncKeyDSTOffset         = "timesync.dst_offset"
)

// timeState is chip's TimeState (TimeSynchronizationCluster.h).
type timeState int

const (
	timeStateInvalid timeState = iota
	timeStateActive
	timeStateChanged
	timeStateStopped
)

// timeSyncState is the features' state; mu of [TimeSynchronization]
// guards it.
type timeSyncState struct {
	trusted    *timesyncdef.TrustedTimeSourceStruct
	defaultNTP *string
	timeZone   []timesyncdef.TimeZoneStruct
	dstOffset  []timesyncdef.DSTOffsetStruct
	ntpServer  bool
}

// NewTimeSynchronizationWith returns the server with cfg's features, its
// stored state loaded as chip's Startup loads it (:333-353): the trusted
// time source, the time zones (else the one default entry, offset 0) and
// the DST offsets (else none).
func NewTimeSynchronizationWith(cfg TimeSynchronizationConfig) (*TimeSynchronization, error) {
	known := timesyncdef.FeatureTimeZone | timesyncdef.FeatureNtpClient | timesyncdef.FeatureNtpServer | timesyncdef.FeatureTimeSyncClient
	if cfg.Features&^known != 0 {
		return nil, fmt.Errorf("core: TimeSynchronization feature 0x%X is not one of TZ, NTPC, NTPS, TSC", uint32(cfg.Features&^known))
	}
	inst, err := spec.New(timesyncdef.Definition, spec.Options{Features: uint32(cfg.Features)})
	if err != nil {
		return nil, fmt.Errorf("core: TimeSynchronization: %w", err)
	}
	t := &TimeSynchronization{inst: inst, cfg: cfg}
	t.state.ntpServer = cfg.NTPServerAvailable
	t.state.timeZone = defaultTimeZone()
	ctx := context.Background()
	var tts timesyncdef.TrustedTimeSourceStruct
	if ok := t.load(ctx, timeSyncKeyTrustedTimeSource, &tts); ok {
		t.state.trusted = &tts
	}
	var ntp string
	if ok := t.load(ctx, timeSyncKeyDefaultNTP, &ntp); ok {
		t.state.defaultNTP = &ntp
	}
	var tz []timesyncdef.TimeZoneStruct
	if ok := t.load(ctx, timeSyncKeyTimeZone, &tz); ok && len(tz) > 0 {
		t.state.timeZone = tz
	}
	var dst []timesyncdef.DSTOffsetStruct
	if ok := t.load(ctx, timeSyncKeyDSTOffset, &dst); ok {
		t.state.dstOffset = dst
	}
	return t, nil
}

// defaultTimeZone is chip's InitTimeZone (:608-617): one entry, offset 0,
// valid at 0, no name.
func defaultTimeZone() []timesyncdef.TimeZoneStruct {
	return []timesyncdef.TimeZoneStruct{{}}
}

func (t *TimeSynchronization) has(f timesyncdef.Feature) bool { return t.cfg.Features&f != 0 }

// timeZoneDatabase is the configured database, None when unset.
func (t *TimeSynchronization) timeZoneDatabase() timesyncdef.TimeZoneDatabaseEnum {
	if t.cfg.TimeZoneDatabase == nil {
		return timesyncdef.TimeZoneDatabaseNone
	}
	return *t.cfg.TimeZoneDatabase
}

func (t *TimeSynchronization) logger() *slog.Logger {
	if t.cfg.Logger != nil {
		return t.cfg.Logger
	}
	return slog.Default()
}

// load reads one stored value; false when none is stored or it does not
// decode.
func (t *TimeSynchronization) load(ctx context.Context, key string, into any) bool {
	if t.cfg.Store == nil {
		return false
	}
	raw, ok, err := t.cfg.Store.GetSetting(ctx, key)
	if err != nil || !ok {
		return false
	}
	return json.Unmarshal([]byte(raw), into) == nil
}

// save stores one value; nil v clears it (stored as JSON null).
func (t *TimeSynchronization) save(ctx context.Context, key string, v any) error {
	if t.cfg.Store == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return t.cfg.Store.SetSetting(ctx, key, string(raw))
}

// emit emits one of the cluster's events on the root endpoint, as chip's
// helpers do (kRootEndpointId, :91-168); an event outside the selection's
// EventList is dropped (the helpers' feature checks).
func (t *TimeSynchronization) emit(eventID uint32, data any) {
	if err := t.events.Emit(t.inst, 0, eventID, data); err != nil && !errors.Is(err, spec.ErrNotEmitted) {
		t.logger().Warn("matter.timesync.event_failed", slog.String("reason", err.Error()))
	}
}

// pending is what a call emits once it let go of the lock.
type pending struct {
	events []func()
	attrs  []uint32
}

func (p *pending) event(fn func())   { p.events = append(p.events, fn) }
func (p *pending) changed(id uint32) { p.attrs = append(p.attrs, id) }
func (t *TimeSynchronization) flush(origin contract.Origin, p *pending) {
	if len(p.attrs) > 0 {
		t.changes.NotifyFrom(origin, p.attrs...)
	}
	for _, fn := range p.events {
		fn()
	}
}

// chipEpochMicros is the node's UTC in Matter epoch microseconds; false
// before the epoch (chip's UnixEpochToChipEpochMicros failing).
func (t *TimeSynchronization) chipEpochMicros() (uint64, bool) {
	now, ok := t.UTC()
	if !ok {
		return 0, false
	}
	return uint64(now.UnixMicro() - matterEpochOffsetSec*1_000_000), true //nolint:gosec // non-negative, UTC checked it
}

// updateTimeZoneStateLocked is chip's UpdateTimeZoneState (:913-949): an
// entry whose validAt has passed becomes the active one (its validAt 0)
// and the entries before it leave the list, which is stored.
func (t *TimeSynchronization) updateTimeZoneStateLocked(ctx context.Context) timeState {
	now, ok := t.chipEpochMicros()
	tz := t.state.timeZone
	if !ok || len(tz) == 0 {
		return timeStateInvalid
	}
	active := 0
	for i := range tz {
		if tz[i].ValidAt != 0 && tz[i].ValidAt <= now {
			tz[i].ValidAt = 0
			active = i
		}
	}
	if active == 0 {
		return timeStateActive
	}
	t.state.timeZone = slices.Clone(tz[active:])
	if err := t.save(ctx, timeSyncKeyTimeZone, t.state.timeZone); err != nil {
		return timeStateInvalid
	}
	return timeStateChanged
}

// updateDSTOffsetStateLocked is chip's UpdateDSTOffsetState (:951-1007):
// the DST offset in effect now, the expired ones dropped; the last one
// expiring clears the list (DSTTableEmpty), one expiring before the next
// starts takes offset 0 in memory.
func (t *TimeSynchronization) updateDSTOffsetStateLocked(ctx context.Context, p *pending) timeState {
	now, ok := t.chipEpochMicros()
	dst := t.state.dstOffset
	if !ok || len(dst) == 0 {
		return timeStateInvalid
	}
	active, stopped := 0, true
	for i := range dst {
		if dst[i].ValidStarting <= now {
			active, stopped = i, false
		}
	}
	if stopped {
		return timeStateStopped
	}
	cur := &dst[active]
	if cur.Offset == 0 && cur.ValidUntil.Null {
		return timeStateStopped
	}
	if !cur.ValidUntil.Null && cur.ValidUntil.Value <= now {
		if active+1 >= len(dst) {
			t.clearDSTOffsetLocked(ctx, p)
			return timeStateInvalid
		}
		previous := cur.Offset
		cur.Offset = 0 // not stored, as chip keeps it in memory (:991)
		if previous == 0 {
			return timeStateStopped
		}
		return timeStateChanged
	}
	if active > 0 {
		t.state.dstOffset = slices.Clone(dst[active:])
		if err := t.save(ctx, timeSyncKeyDSTOffset, t.state.dstOffset); err != nil {
			return timeStateInvalid
		}
		return timeStateChanged
	}
	return timeStateActive
}

// clearDSTOffsetLocked is chip's ClearDSTOffset (:824-830): the list
// emptied and stored, and DSTTableEmpty.
func (t *TimeSynchronization) clearDSTOffsetLocked(ctx context.Context, p *pending) {
	t.state.dstOffset = nil
	if err := t.save(ctx, timeSyncKeyDSTOffset, []timesyncdef.DSTOffsetStruct{}); err != nil {
		t.logger().Warn("matter.timesync.store_failed", slog.String("what", "DSTOffset"), slog.String("reason", err.Error()))
		return
	}
	p.changed(timesyncdef.AttrDstOffset)
	p.event(func() { t.emit(timesyncdef.EventDstTableEmpty, timesyncdef.DstTableEmptyEvent{}) })
}

// timeZoneStatusEvent is EmitTimeZoneStatusEvent (:124-141): the active
// time zone's offset and name.
func (t *TimeSynchronization) timeZoneStatusEventLocked(p *pending) {
	if len(t.state.timeZone) == 0 {
		return
	}
	tz := t.state.timeZone[0]
	ev := timesyncdef.TimeZoneStatusEvent{Offset: tz.Offset}
	if tz.Name != nil {
		name := *tz.Name
		ev.Name = &name
	}
	p.event(func() { t.emit(timesyncdef.EventTimeZoneStatus, ev) })
}

func (t *TimeSynchronization) dstStatusEvent(p *pending, active bool) {
	p.event(func() { t.emit(timesyncdef.EventDstStatus, timesyncdef.DstStatusEvent{DstOffsetActive: active}) })
}

// localTime is chip's GetLocalTime (:874-911): UTC with the active time
// zone's offset and, when the first DST offset has started, its offset;
// null without a DST list. It may emit TimeZoneStatus and DSTStatus.
func (t *TimeSynchronization) localTime() (uint64, bool) {
	ctx := context.Background()
	var p pending
	t.mu.Lock()
	v, ok := t.localTimeLocked(ctx, &p)
	t.mu.Unlock()
	t.flush(contract.OriginHostSet, &p)
	return v, ok
}

func (t *TimeSynchronization) localTimeLocked(ctx context.Context, p *pending) (uint64, bool) {
	state := t.updateDSTOffsetStateLocked(ctx, p)
	if state == timeStateInvalid {
		return 0, false
	}
	now, ok := t.chipEpochMicros()
	if !ok {
		return 0, false
	}
	if t.updateTimeZoneStateLocked(ctx) == timeStateChanged {
		t.timeZoneStatusEventLocked(p)
	}
	if len(t.state.timeZone) == 0 || len(t.state.dstOffset) == 0 {
		return 0, false
	}
	tzOffset := int64(t.state.timeZone[0].Offset)
	var dstOffset int64
	if dst := t.state.dstOffset[0]; dst.ValidStarting <= now {
		dstOffset = int64(dst.Offset)
	}
	rem := now % 1_000_000
	sec := int64(now/1_000_000) + tzOffset + dstOffset //nolint:gosec // seconds since 2000 fit int64
	if state == timeStateChanged {
		t.dstStatusEvent(p, dstOffset != 0)
	}
	return uint64(sec)*1_000_000 + rem, true //nolint:gosec // a local time after the epoch
}

// readFeatureLocked reads the features' attributes (:250-331); false for
// any other attribute.
func (t *TimeSynchronization) readFeatureLocked(attrID uint32) (any, bool) {
	switch attrID {
	case timesyncdef.AttrTrustedTimeSource:
		if t.state.trusted == nil {
			return nil, true
		}
		return *t.state.trusted, true
	case timesyncdef.AttrDefaultNtp:
		if t.state.defaultNTP == nil {
			return nil, true
		}
		return *t.state.defaultNTP, true
	case timesyncdef.AttrTimeZone:
		return spec.List[timesyncdef.TimeZoneStruct](slices.Clone(t.state.timeZone)), true
	case timesyncdef.AttrDstOffset:
		return spec.List[timesyncdef.DSTOffsetStruct](slices.Clone(t.state.dstOffset)), true
	case timesyncdef.AttrTimeZoneDatabase:
		return t.timeZoneDatabase(), true
	case timesyncdef.AttrNtpServerAvailable:
		return t.state.ntpServer, true
	case timesyncdef.AttrTimeZoneListMaxSize:
		return uint8(timeZoneListMaxSize), true
	case timesyncdef.AttrDstOffsetListMaxSize:
		return uint8(dstOffsetListMaxSize), true
	case timesyncdef.AttrSupportsDnsResolve:
		return t.cfg.SupportsDNSResolve, true
	}
	return nil, false
}

// setTrustedTimeSource is chip's SetTrustedTimeSource (:571-593): the
// source stored (null clears it) and reported.
func (t *TimeSynchronization) setTrustedTimeSourceLocked(ctx context.Context, tts *timesyncdef.TrustedTimeSourceStruct, p *pending) error {
	t.state.trusted = tts
	p.changed(timesyncdef.AttrTrustedTimeSource)
	return t.save(ctx, timeSyncKeyTrustedTimeSource, tts)
}

// handleSetTrustedTimeSource is chip's HandleSetTrustedTimeSource
// (:1138-1158): the source with the accessing fabric, or null, which
// emits MissingTrustedTimeSource; SUCCESS, a store failure ignored.
func (t *TimeSynchronization) handleSetTrustedTimeSource(ctx context.Context, fields any) error {
	req, ok := requestFields[timesyncdef.SetTrustedTimeSourceRequest](fields)
	if !ok {
		return spec.Errorf(im.StatusInvalidCommand, "core: SetTrustedTimeSource carried %T", fields)
	}
	var p pending
	var tts *timesyncdef.TrustedTimeSourceStruct
	if req.TrustedTimeSource.Null {
		p.event(func() { t.emit(timesyncdef.EventMissingTrustedTimeSource, timesyncdef.MissingTrustedTimeSourceEvent{}) })
	} else {
		_, fabricIndex := im.FabricFilterFromContext(ctx)
		tts = &timesyncdef.TrustedTimeSourceStruct{FabricIndex: fabricIndex, NodeId: req.TrustedTimeSource.Value.NodeId, Endpoint: req.TrustedTimeSource.Value.Endpoint}
	}
	t.mu.Lock()
	err := t.setTrustedTimeSourceLocked(ctx, tts, &p)
	t.mu.Unlock()
	if err != nil {
		t.logger().Warn("matter.timesync.store_failed", slog.String("what", "TrustedTimeSource"), slog.String("reason", err.Error()))
	}
	t.flush(contract.OriginFrom(ctx), &p)
	return nil
}

// FabricRemoved is chip's OnFabricRemoved (:1020-1028): a trusted time
// source of the removed fabric is cleared, with MissingTrustedTimeSource.
func (t *TimeSynchronization) FabricRemoved(ctx context.Context, fabricIndex uint8) {
	if !t.has(timesyncdef.FeatureTimeSyncClient) {
		return
	}
	var p pending
	t.mu.Lock()
	if t.state.trusted == nil || t.state.trusted.FabricIndex != fabricIndex {
		t.mu.Unlock()
		return
	}
	if err := t.setTrustedTimeSourceLocked(ctx, nil, &p); err != nil {
		t.logger().Warn("matter.timesync.store_failed", slog.String("what", "TrustedTimeSource"), slog.String("reason", err.Error()))
	}
	t.mu.Unlock()
	p.event(func() { t.emit(timesyncdef.EventMissingTrustedTimeSource, timesyncdef.MissingTrustedTimeSourceEvent{}) })
	t.flush(contract.OriginHostSet, &p)
}

// errTooMany is chip's CHIP_ERROR_BUFFER_TOO_SMALL from a setter, which
// the handlers answer RESOURCE_EXHAUSTED; errInvalid its malformed data
// (CONSTRAINT_ERROR).
var (
	errTimeSyncTooMany = errors.New("list longer than the node holds")
	errTimeSyncInvalid = errors.New("malformed list")
)

// statusOf is the handlers' answer to a setter's error (:1168-1178,
// :1225-1235): RESOURCE_EXHAUSTED for too many entries, CONSTRAINT_ERROR
// otherwise.
func timeSyncStatusOf(what string, err error) error {
	if errors.Is(err, errTimeSyncTooMany) {
		return spec.Errorf(im.StatusResourceExhausted, "core: %s: %v", what, err)
	}
	return spec.Errorf(im.StatusConstraintError, "core: %s: %v", what, err)
}

// setTimeZoneLocked is chip's SetTimeZone (:619-730): at most two
// entries, the first valid at 0 and every later one at a set time, each
// offset within -43200 to 50400 and each name at most 64 bytes; none is
// the default entry. It reports whether the active time zone's offset or
// name changed (the TimeZoneStatus flag). A refused list changes nothing.
func (t *TimeSynchronization) setTimeZoneLocked(ctx context.Context, list []timesyncdef.TimeZoneStruct, p *pending) (statusChanged bool, err error) {
	if len(list) > timeZoneListMaxSize {
		return false, errTimeSyncTooMany
	}
	if len(list) == 0 {
		t.state.timeZone = defaultTimeZone()
		p.changed(timesyncdef.AttrTimeZone)
		return false, t.save(ctx, timeSyncKeyTimeZone, t.state.timeZone)
	}
	lastState := t.updateTimeZoneStateLocked(ctx)
	var last timesyncdef.TimeZoneStruct
	if lastState != timeStateInvalid {
		last = t.state.timeZone[0]
	}
	next := make([]timesyncdef.TimeZoneStruct, 0, len(list))
	for i, tz := range list {
		switch {
		case tz.Offset < timeZoneOffsetMin || tz.Offset > timeZoneOffsetMax:
			return false, fmt.Errorf("%w: offset %d outside %d to %d", errTimeSyncInvalid, tz.Offset, timeZoneOffsetMin, timeZoneOffsetMax)
		case i == 0 && tz.ValidAt != 0:
			return false, fmt.Errorf("%w: the first time zone is valid at %d, not 0", errTimeSyncInvalid, tz.ValidAt)
		case i != 0 && tz.ValidAt == 0:
			return false, fmt.Errorf("%w: time zone %d is valid at 0", errTimeSyncInvalid, i)
		case tz.Name != nil && len(*tz.Name) > timeZoneNameMaxLen:
			return false, fmt.Errorf("%w: name of %d bytes, at most %d", errTimeSyncInvalid, len(*tz.Name), timeZoneNameMaxLen)
		}
		entry := timesyncdef.TimeZoneStruct{Offset: tz.Offset, ValidAt: tz.ValidAt}
		if tz.Name != nil && *tz.Name != "" { // an empty name is no name (:681-698)
			name := *tz.Name
			entry.Name = &name
		}
		next = append(next, entry)
	}
	t.state.timeZone = next
	if lastState != timeStateInvalid && t.updateTimeZoneStateLocked(ctx) != timeStateInvalid {
		tz := t.state.timeZone[0]
		statusChanged = tz.Offset != last.Offset ||
			(tz.Name != nil && last.Name != nil && *tz.Name != *last.Name)
	}
	p.changed(timesyncdef.AttrTimeZone)
	return statusChanged, t.save(ctx, timeSyncKeyTimeZone, t.state.timeZone)
}

// handleSetTimeZone is chip's HandleSetTimeZone (:1161-1213).
func (t *TimeSynchronization) handleSetTimeZone(ctx context.Context, fields any) (any, error) {
	req, ok := requestFields[timesyncdef.SetTimeZoneRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "core: SetTimeZone carried %T", fields)
	}
	var p pending
	t.mu.Lock()
	defer func() {
		t.mu.Unlock()
		t.flush(contract.OriginFrom(ctx), &p)
	}()
	saved := slices.Clone(t.state.timeZone)
	statusChanged, err := t.setTimeZoneLocked(ctx, req.TimeZone, &p)
	if err != nil {
		t.state.timeZone = saved
		p.attrs = nil
		return nil, timeSyncStatusOf("SetTimeZone", err)
	}
	if statusChanged {
		t.timeZoneStatusEventLocked(&p)
	}
	resp := timesyncdef.SetTimeZoneResponse{DstOffsetRequired: true}
	t.updateTimeZoneStateLocked(ctx)
	if t.timeZoneDatabase() != timesyncdef.TimeZoneDatabaseNone && len(t.state.timeZone) > 0 && t.cfg.DSTOffsetsFor != nil {
		if name := t.state.timeZone[0].Name; name != nil {
			if offsets, ok := t.cfg.DSTOffsetsFor(*name); ok && validDSTOffsets(offsets) == nil && len(offsets) <= dstOffsetListMaxSize {
				t.state.dstOffset = slices.Clone(offsets)
				if err := t.save(ctx, timeSyncKeyDSTOffset, t.state.dstOffset); err != nil {
					t.logger().Warn("matter.timesync.store_failed", slog.String("what", "DSTOffset"), slog.String("reason", err.Error()))
				}
				p.changed(timesyncdef.AttrDstOffset)
				resp.DstOffsetRequired = false
				t.dstStatusEvent(&p, true)
			}
		}
	}
	if resp.DstOffsetRequired {
		state := t.updateDSTOffsetStateLocked(ctx, &p)
		t.clearDSTOffsetLocked(ctx, &p)
		if state == timeStateActive || state == timeStateChanged {
			t.dstStatusEvent(&p, false)
		}
	}
	return resp, nil
}

// validDSTOffsets is chip's SetDSTOffset list check (:785-810): each
// entry ends after it starts, starts no earlier than the previous ended,
// and only the last may run open-ended.
func validDSTOffsets(list []timesyncdef.DSTOffsetStruct) error {
	var lastValidUntil uint64
	for i, d := range list {
		if !d.ValidUntil.Null && d.ValidStarting >= d.ValidUntil.Value {
			return fmt.Errorf("%w: DST offset %d ends before it starts", errTimeSyncInvalid, i)
		}
		if d.ValidStarting < lastValidUntil {
			return fmt.Errorf("%w: DST offset %d starts before the previous one ends", errTimeSyncInvalid, i)
		}
		if !d.ValidUntil.Null {
			lastValidUntil = d.ValidUntil.Value
		}
		if d.ValidUntil.Null && i != len(list)-1 {
			return fmt.Errorf("%w: DST offset %d is open-ended but not the last", errTimeSyncInvalid, i)
		}
	}
	return nil
}

// setDSTOffsetLocked is chip's SetDSTOffset (:750-816): at most two
// entries, validDSTOffsets' order; none clears the list (DSTTableEmpty).
func (t *TimeSynchronization) setDSTOffsetLocked(ctx context.Context, list []timesyncdef.DSTOffsetStruct, p *pending) error {
	if len(list) > dstOffsetListMaxSize {
		return errTimeSyncTooMany
	}
	if len(list) == 0 {
		t.clearDSTOffsetLocked(ctx, p)
		return nil
	}
	if err := validDSTOffsets(list); err != nil {
		return err
	}
	t.state.dstOffset = slices.Clone(list)
	p.changed(timesyncdef.AttrDstOffset)
	return t.save(ctx, timeSyncKeyDSTOffset, t.state.dstOffset)
}

// handleSetDSTOffset is chip's HandleSetDSTOffset (:1216-1243): a change
// of the DST state emits DSTStatus.
func (t *TimeSynchronization) handleSetDSTOffset(ctx context.Context, fields any) error {
	req, ok := requestFields[timesyncdef.SetDstOffsetRequest](fields)
	if !ok {
		return spec.Errorf(im.StatusInvalidCommand, "core: SetDSTOffset carried %T", fields)
	}
	var p pending
	t.mu.Lock()
	defer func() {
		t.mu.Unlock()
		t.flush(contract.OriginFrom(ctx), &p)
	}()
	before := t.updateDSTOffsetStateLocked(ctx, &p)
	if err := t.setDSTOffsetLocked(ctx, req.DstOffset, &p); err != nil {
		return timeSyncStatusOf("SetDSTOffset", err)
	}
	if after := t.updateDSTOffsetStateLocked(ctx, &p); after != before {
		t.dstStatusEvent(&p, after == timeStateActive)
	}
	return nil
}

// ntpAddressValid is chip's DefaultTimeSyncDelegate::IsNTPAddressValid
// (DefaultTimeSyncDelegate.cpp:26-31): an IPv6 address literal.
func ntpAddressValid(s string) bool {
	a, err := netip.ParseAddr(s)
	return err == nil && a.Is6() && !a.Is4In6()
}

// ntpAddressDomain is IsNTPAddressDomain (:33-39): no address, and a dot.
func ntpAddressDomain(s string) bool { return !ntpAddressValid(s) && strings.Contains(s, ".") }

// handleSetDefaultNTP is chip's HandleSetDefaultNTP (:1246-1275): a
// non-empty address must fit 128 bytes (CONSTRAINT_ERROR) and be an IPv6
// address, or a domain the node resolves (INVALID_COMMAND); a store
// failure is FAILURE. Null clears it.
func (t *TimeSynchronization) handleSetDefaultNTP(ctx context.Context, fields any) error {
	req, ok := requestFields[timesyncdef.SetDefaultNtpRequest](fields)
	if !ok {
		return spec.Errorf(im.StatusInvalidCommand, "core: SetDefaultNTP carried %T", fields)
	}
	var ntp *string
	if !req.DefaultNtp.Null {
		v := req.DefaultNtp.Value
		ntp = &v
	}
	if err := t.checkDefaultNTP(ntp); err != nil {
		return err
	}
	var p pending
	t.mu.Lock()
	t.state.defaultNTP = ntp
	err := t.save(ctx, timeSyncKeyDefaultNTP, ntp)
	if err == nil {
		p.changed(timesyncdef.AttrDefaultNtp)
	}
	t.mu.Unlock()
	t.flush(contract.OriginFrom(ctx), &p)
	if err != nil {
		return spec.Errorf(im.StatusFailure, "core: SetDefaultNTP: %v", err)
	}
	return nil
}

func (t *TimeSynchronization) checkDefaultNTP(ntp *string) error {
	if ntp == nil || *ntp == "" {
		return nil
	}
	if len(*ntp) > defaultNTPMaxLen {
		return spec.Errorf(im.StatusConstraintError, "core: DefaultNTP of %d bytes, at most %d", len(*ntp), defaultNTPMaxLen)
	}
	if !ntpAddressValid(*ntp) && !(ntpAddressDomain(*ntp) && t.cfg.SupportsDNSResolve) {
		return spec.Errorf(im.StatusInvalidCommand, "core: DefaultNTP %q is neither an IPv6 address nor a domain the node resolves", *ntp)
	}
	return nil
}

// SetNTPServerAvailable sets NTPServerAvailable (NTPS) and reports it.
func (t *TimeSynchronization) SetNTPServerAvailable(available bool) {
	if !t.has(timesyncdef.FeatureNtpServer) {
		return
	}
	t.mu.Lock()
	changed := t.state.ntpServer != available
	t.state.ntpServer = available
	t.mu.Unlock()
	if changed {
		t.changes.NotifyFrom(contract.OriginHostSet, timesyncdef.AttrNtpServerAvailable)
	}
}

// TrustedTimeSource is the stored trusted time source (TSC); false when
// it is null.
func (t *TimeSynchronization) TrustedTimeSource() (timesyncdef.TrustedTimeSourceStruct, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state.trusted == nil {
		return timesyncdef.TrustedTimeSourceStruct{}, false
	}
	return *t.state.trusted, true
}

// DefaultNTP is the stored default NTP server (NTPC); false when it is
// null.
func (t *TimeSynchronization) DefaultNTP() (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state.defaultNTP == nil {
		return "", false
	}
	return *t.state.defaultNTP, true
}
