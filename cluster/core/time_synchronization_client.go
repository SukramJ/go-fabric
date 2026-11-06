// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	timesyncdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// The node's UTC as a served time source, and the time client after
// connectedhomeip. matter.js's TimeSynchronizationServer is the generated
// behaviour with no logic (TimeSynchronizationServer.ts:14 at 862ad75f).
//
// connectedhomeip's server takes its time from the platform first, then
// from the trusted time source's node, then from the default NTP server,
// and emits TimeFailure when none gives it (AttemptToGetTime,
// TimeSynchronizationCluster.cpp:541-554 at 6170af84). It sets the system
// clock; here the host clock is never touched: the node's UTC is the
// server's clock plus an offset that SetUTCTime, the trusted time source
// and the NTP fallback set, held in memory (owner decision 2026-11-06).

// TimePeerReader reads attributes of a node of one of the node's fabrics
// over a CASE session the node opens to it or already holds. It is
// connectedhomeip's CASESessionManager::FindOrEstablishSession with a
// ReadClient on the session (TimeSynchronizationCluster.cpp:401-428,
// :525-539 at 6170af84). The result holds a value for each requested path
// the report carried one for, keyed by that path; a path answered with a
// status is absent. The node implements it over its client stack
// (node.Config.Client).
type TimePeerReader interface {
	ReadPeerAttributes(ctx context.Context, fabricIndex uint8, nodeID uint64, paths []im.ConcreteAttributePath) (map[im.ConcreteAttributePath]spec.Node, error)
}

// NTPClient asks an NTP server for the current time. It is
// connectedhomeip's Delegate::UpdateTimeUsingNTPFallback
// (time-synchronization-delegate.h:86-97 at 6170af84), which sets the
// system clock; here the time it returns becomes the node's UTC. server
// is the DefaultNTP attribute: an IPv6 address, or a domain name when
// SupportsDNSResolve is set.
type NTPClient interface {
	QueryTime(ctx context.Context, server string) (time.Time, error)
}

// timeSyncUTC is the node's UTC over the server's clock: the offset
// SetUTCTime, the trusted time source or the NTP fallback set, with the
// granularity and the time source they gave. Until one of them sets it
// (adopted false), the platform source speaks for the host clock
// ([TimeSynchronizationConfig].PlatformTime) and the offset is zero.
type timeSyncUTC struct {
	offset      time.Duration
	granularity timesyncdef.GranularityEnum
	source      timesyncdef.TimeSourceEnum
	adopted     bool
}

// timeSyncClient is the time client's run state; its mu guards it.
type timeSyncClient struct {
	mu sync.Mutex
	// ctx bounds the attempts from Start to Stop; nil outside them.
	ctx    context.Context //nolint:containedctx // the attempts' lifetime, from Start to Stop
	cancel context.CancelFunc
	// running is an attempt in flight; again asks it to run once more.
	running bool
	again   bool
	reader  TimePeerReader
	wg      sync.WaitGroup
}

// errTimeSyncUTCRange is a UTC past what epoch-s holds (connectedhomeip's
// UpdateUTCTime, TimeSynchronizationCluster.cpp:74-86 at 6170af84), or
// before the Matter epoch.
var errTimeSyncUTCRange = errors.New("core: UTC time outside the Matter epoch range")

// now is the server's clock.
func (t *TimeSynchronization) now() time.Time { return clock.Or(t.clock.Load()).Now() }

// platformTime is the host clock as a time source: the configured
// PlatformTime, else connectedhomeip's DefaultTimeSyncDelegate
// (DefaultTimeSyncDelegate.cpp:42-58 at 6170af84) — MixedNTP with
// milliseconds while the clock reads a real time, none before the Matter
// epoch (the threshold of GetClock_RealTime,
// src/platform/Linux/SystemTimeSupport.cpp:53-63 at 6170af84). A
// NoTimeGranularity is no source.
func (t *TimeSynchronization) platformTime() (timesyncdef.TimeSourceEnum, timesyncdef.GranularityEnum) {
	if f := t.cfg.PlatformTime; f != nil {
		if src, gran := f(); gran != timesyncdef.GranularityNoTimeGranularity {
			return src, gran
		}
		return timesyncdef.TimeSourceNone, timesyncdef.GranularityNoTimeGranularity
	}
	if _, ok := utcOf(t.clock.Load()); ok {
		return timesyncdef.TimeSourceMixedNtp, timesyncdef.GranularityMillisecondsGranularity
	}
	return timesyncdef.TimeSourceNone, timesyncdef.GranularityNoTimeGranularity
}

// quality is the node's UTC state: the adopted one, else the platform
// source's with no offset.
func (t *TimeSynchronization) quality() timeSyncUTC {
	if st := t.utc.Load(); st != nil && st.adopted {
		return *st
	}
	src, gran := t.platformTime()
	return timeSyncUTC{granularity: gran, source: src}
}

// setUTCLocked is connectedhomeip's SetUTCTime
// (TimeSynchronizationCluster.cpp:856-872 at 6170af84) over the offset:
// the node's UTC becomes utcTime (Matter epoch microseconds) with gran
// and src, and UTCTime, Granularity and TimeSource are reported. A time
// whose seconds exceed 32 bits is refused, as UpdateUTCTime refuses it
// (:74-86). t.mu is held.
func (t *TimeSynchronization) setUTCLocked(utcTime uint64, gran timesyncdef.GranularityEnum, src timesyncdef.TimeSourceEnum, p *pending) error {
	if utcTime/1_000_000 > math.MaxUint32 {
		return errTimeSyncUTCRange
	}
	t.adoptLocked(time.UnixMicro(int64(utcTime)+matterEpochOffsetSec*1_000_000), gran, src, p) //nolint:gosec // bounded above
	return nil
}

// adoptLocked makes at the node's UTC now, with gran and src; t.mu is
// held.
func (t *TimeSynchronization) adoptLocked(at time.Time, gran timesyncdef.GranularityEnum, src timesyncdef.TimeSourceEnum, p *pending) {
	before := t.quality()
	t.utc.Store(&timeSyncUTC{offset: at.Sub(t.now()), granularity: gran, source: src, adopted: true})
	p.changed(timeSyncAttrUTCTime)
	if before.granularity != gran {
		p.changed(timeSyncAttrGranularity)
	}
	if before.source != src && t.instance().Serves(timesyncdef.AttrTimeSource) {
		p.changed(timesyncdef.AttrTimeSource)
	}
}

// AttachTimePeerReader gives the server the node's reader of the trusted
// time source's node when its configuration
// ([TimeSynchronizationConfig].PeerReader) names none; the node calls it
// with its client stack. A configured reader stays.
func (t *TimeSynchronization) AttachTimePeerReader(r TimePeerReader) {
	t.client.mu.Lock()
	defer t.client.mu.Unlock()
	if t.client.reader == nil {
		t.client.reader = r
	}
}

// peerReader is the reader the trusted time source is read through.
func (t *TimeSynchronization) peerReader() TimePeerReader {
	if t.cfg.PeerReader != nil {
		return t.cfg.PeerReader
	}
	t.client.mu.Lock()
	defer t.client.mu.Unlock()
	return t.client.reader
}

// Start is connectedhomeip's server-ready event (OnPlatformEventFn,
// TimeSynchronizationCluster.cpp:556-569 at 6170af84): without a
// granularity the server asks for the time — the trusted time source's
// node, then the default NTP server — and emits TimeFailure when neither
// gives it. A new trusted time source asks again while the node has no
// time (SetTrustedTimeSource, :571-593). ctx bounds every attempt until
// [TimeSynchronization.Stop]; the attempts run in their own goroutine.
func (t *TimeSynchronization) Start(ctx context.Context) {
	t.client.mu.Lock()
	if t.client.ctx != nil {
		t.client.mu.Unlock()
		return
	}
	t.client.ctx, t.client.cancel = context.WithCancel(ctx)
	t.client.mu.Unlock()
	t.requestTime()
}

// Stop ends the attempts and waits for the one in flight.
func (t *TimeSynchronization) Stop() {
	t.client.mu.Lock()
	if t.client.cancel != nil {
		t.client.cancel()
	}
	t.client.ctx, t.client.cancel = nil, nil
	t.client.mu.Unlock()
	t.client.wg.Wait()
}

// requestTime starts an attempt while the node has no time
// (:561-564, :584-587); one in flight runs once more instead.
func (t *TimeSynchronization) requestTime() {
	if t.quality().granularity != timesyncdef.GranularityNoTimeGranularity {
		return
	}
	t.client.mu.Lock()
	defer t.client.mu.Unlock()
	if t.client.ctx == nil {
		return
	}
	if t.client.running {
		t.client.again = true
		return
	}
	t.client.running = true
	t.client.wg.Add(1)
	go t.runAttempts(t.client.ctx)
}

// runAttempts runs attempts until none is asked for.
func (t *TimeSynchronization) runAttempts(ctx context.Context) {
	defer t.client.wg.Done()
	for {
		if t.quality().granularity == timesyncdef.GranularityNoTimeGranularity {
			t.attemptToGetTime(ctx)
		}
		t.client.mu.Lock()
		if !t.client.again || ctx.Err() != nil {
			t.client.running, t.client.again = false, false
			t.client.mu.Unlock()
			return
		}
		t.client.again = false
		t.client.mu.Unlock()
	}
}

// attemptToGetTime is connectedhomeip's AttemptToGetTime past the
// platform source, which [TimeSynchronization.quality] reads live: the
// trusted time source's node, else the NTP fallback (:541-554).
func (t *TimeSynchronization) attemptToGetTime(ctx context.Context) {
	if t.timeFromTrustedNode(ctx) {
		return
	}
	if ctx.Err() != nil {
		return
	}
	t.fallbackNTP(ctx)
}

// timeFromTrustedNode is connectedhomeip's
// AttemptToGetTimeFromTrustedNode and its read (:525-539, :401-492): the
// root endpoint's UTCTime and Granularity read from the trusted time
// source's node and fabric — the root, not the source's endpoint, as chip
// reads kRootEndpointId (:405-406); a value that does not decode is null
// or NoTimeGranularity (:445-456). With both, the node's UTC is set with
// a conservative granularity — minutes for a minutes or seconds source,
// seconds for a finer one, for the network delay (:466-477) — and
// TimeSource NodeTimeCluster (:479). False sends the attempt to the NTP
// fallback: no TSC feature (:536-537), no source (:535), no reader or a
// failed read (:430-434), or no usable time (:486-489).
func (t *TimeSynchronization) timeFromTrustedNode(ctx context.Context) bool {
	if !t.has(timesyncdef.FeatureTimeSyncClient) {
		return false
	}
	tts, ok := t.TrustedTimeSource()
	if !ok {
		return false
	}
	reader := t.peerReader()
	if reader == nil {
		t.logger().Info("matter.timesync.trusted_source_unreachable", slog.String("reason", "no peer reader"))
		return false
	}
	utcPath := timeSyncRootPath(timeSyncAttrUTCTime)
	granPath := timeSyncRootPath(timeSyncAttrGranularity)
	vals, err := reader.ReadPeerAttributes(ctx, tts.FabricIndex, tts.NodeId, []im.ConcreteAttributePath{utcPath, granPath})
	if err != nil {
		t.logger().Info("matter.timesync.trusted_source_unreachable", slog.String("reason", err.Error()))
		return false
	}
	var utc spec.Nullable[uint64]
	utc.Null = true
	if v, ok := vals[utcPath]; ok {
		if d, err := spec.DecodeNullable(spec.DecodeUint[uint64](64))(v); err == nil {
			utc = d
		}
	}
	gran := timesyncdef.GranularityNoTimeGranularity
	if v, ok := vals[granPath]; ok {
		if d, err := spec.DecodeUint[timesyncdef.GranularityEnum](8)(v); err == nil {
			gran = d
		}
	}
	if utc.Null || gran == timesyncdef.GranularityNoTimeGranularity {
		return false
	}
	ours := timesyncdef.GranularitySecondsGranularity
	if gran == timesyncdef.GranularityMinutesGranularity || gran == timesyncdef.GranularitySecondsGranularity {
		ours = timesyncdef.GranularityMinutesGranularity
	}
	var p pending
	t.mu.Lock()
	err = t.setUTCLocked(utc.Value, ours, timesyncdef.TimeSourceNodeTimeCluster, &p)
	t.mu.Unlock()
	if err != nil {
		return false
	}
	t.flush(contract.OriginHostSet, &p)
	return true
}

// timeSyncRootPath is the concrete request path of a TimeSynchronization
// attribute on the root endpoint (:405-406).
func timeSyncRootPath(attrID uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{
		Endpoint: 0, Cluster: timeSyncClusterID, Attribute: attrID,
		HasEndpoint: true, HasCluster: true, HasAttribute: true,
	}
}

// fallbackNTP is connectedhomeip's AttemptToGetFallbackNTPTimeFromDelegate
// and its completion (:379-398, :510-523): no DefaultNTP, no NTPClient
// (the delegate's default answers not implemented,
// time-synchronization-delegate.h:93-97) or a failed query emits
// TimeFailure; a time sets the node's UTC with milliseconds and
// TimeSource NonMatterSNTP. A time before the Matter epoch or past
// epoch-s counts as a failed query.
func (t *TimeSynchronization) fallbackNTP(ctx context.Context) {
	server, ok := t.DefaultNTP()
	if !ok || t.cfg.NTPClient == nil {
		t.emitTimeFailure()
		return
	}
	at, err := t.cfg.NTPClient.QueryTime(ctx, server)
	if err == nil {
		if s := at.Unix() - matterEpochOffsetSec; s < 0 || s > math.MaxUint32 {
			err = errTimeSyncUTCRange
		}
	}
	if err != nil {
		if ctx.Err() == nil {
			t.logger().Info("matter.timesync.ntp_failed", slog.String("host", server), slog.String("reason", err.Error()))
			t.emitTimeFailure()
		}
		return
	}
	var p pending
	t.mu.Lock()
	t.adoptLocked(at, timesyncdef.GranularityMillisecondsGranularity, timesyncdef.TimeSourceNonMatterSntp, &p)
	t.mu.Unlock()
	t.flush(contract.OriginHostSet, &p)
}

// emitTimeFailure is connectedhomeip's EmitTimeFailureEvent
// (TimeSynchronizationCluster.cpp:143-153 at 6170af84) on the root
// endpoint.
func (t *TimeSynchronization) emitTimeFailure() {
	t.emit(timesyncdef.EventTimeFailure, timesyncdef.TimeFailureEvent{})
}
