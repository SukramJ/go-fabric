// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

// The node's UTC as a served source and the time client after
// connectedhomeip's TimeSynchronizationCluster.cpp at 6170af84:
// AttemptToGetTime and its triggers (:541-593), the trusted node's read
// with its conservative granularity (:401-539), the NTP fallback and
// TimeFailure (:379-398, :510-523), SetUTCTime over the offset
// (:856-872, :1110-1136). chip's unit tests stub the platform source so
// that no attempt runs (TestTimeSynchronizationCluster.cpp:41-51); these
// cases drive the client through the ports instead.

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tsdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// noPlatformTime is a host clock that is no time source.
func noPlatformTime() (tsdef.TimeSourceEnum, tsdef.GranularityEnum) {
	return tsdef.TimeSourceNone, tsdef.GranularityNoTimeGranularity
}

// peerRead is one ReadPeerAttributes call.
type peerRead struct {
	fabricIndex uint8
	nodeID      uint64
	paths       []im.ConcreteAttributePath
}

// fakePeer is a TimePeerReader whose node serves utc and gran (nil: the
// attribute is absent from the report), or fails with err.
type fakePeer struct {
	mu    sync.Mutex
	utc   *spec.Node
	gran  *spec.Node
	err   error
	reads []peerRead
}

func (f *fakePeer) ReadPeerAttributes(_ context.Context, fabricIndex uint8, nodeID uint64, paths []im.ConcreteAttributePath) (map[im.ConcreteAttributePath]spec.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, peerRead{fabricIndex, nodeID, slices.Clone(paths)})
	if f.err != nil {
		return nil, f.err
	}
	out := map[im.ConcreteAttributePath]spec.Node{}
	for _, p := range paths {
		switch {
		case p.Attribute == tsdef.AttrUtcTime && f.utc != nil:
			out[p] = *f.utc
		case p.Attribute == tsdef.AttrGranularity && f.gran != nil:
			out[p] = *f.gran
		}
	}
	return out, nil
}

func (f *fakePeer) took() []peerRead {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.reads)
}

func uintNode(v uint64) *spec.Node {
	n := spec.Node{El: tlv.Element{Type: tlv.TypeUnsignedInt8, Uint: v}}
	return &n
}

func nullNode() *spec.Node           { n := spec.Node{El: tlv.Element{Type: tlv.TypeNull}}; return &n }
func peerAt(at time.Time) *spec.Node { return uintNode(uint64(at.UnixMicro() - 946684800_000_000)) } //nolint:gosec // after the epoch

// fakeNTP is an NTPClient answering at, or err.
type fakeNTP struct {
	mu      sync.Mutex
	at      time.Time
	err     error
	servers []string
}

func (f *fakeNTP) QueryTime(_ context.Context, server string) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.servers = append(f.servers, server)
	return f.at, f.err
}

// tsSignal records the events and the attribute changes, signalling each.
type tsSignal struct {
	mu      sync.Mutex
	events  []uint32
	changes []uint32
	ch      chan struct{}
}

func (s *tsSignal) MatterEmitEvent(_ uint16, _, event uint32, _ any, _ contract.EventPriority) {
	s.mu.Lock()
	s.events = append(s.events, event)
	s.mu.Unlock()
	s.ch <- struct{}{}
}

func (s *tsSignal) changed(c contract.Change) {
	s.mu.Lock()
	s.changes = append(s.changes, c.Attributes...)
	s.mu.Unlock()
	if slices.Contains(c.Attributes, tsdef.AttrUtcTime) {
		s.ch <- struct{}{}
	}
}

func (s *tsSignal) seen() (events, changes []uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.events), slices.Clone(s.changes)
}

// wait waits for one signal: an event, or a change of UTCTime.
func (s *tsSignal) wait(t *testing.T, what string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	select {
	case <-s.ch:
	case <-ctx.Done():
		t.Fatalf("no %s", what)
	}
}

// tsClient is a server on the test clock with a recorder of its events
// and changes; ntp names DefaultNTP in its store.
func tsClient(t *testing.T, cfg core.TimeSynchronizationConfig, ntp string) (*core.TimeSynchronization, *tsSignal, *clocktest.Fake) {
	t.Helper()
	if ntp != "" {
		cfg.Store = &tsSettings{m: map[string]string{"timesync.default_ntp": `"` + ntp + `"`}}
	}
	srv, err := core.NewTimeSynchronizationWith(cfg)
	if err != nil {
		t.Fatal(err)
	}
	clk := clocktest.New(tsNow)
	srv.SetClock(clk)
	sig := &tsSignal{ch: make(chan struct{}, 16)}
	srv.SetMatterEventEmitter(sig)
	srv.OnAttributeChange(sig.changed)
	t.Cleanup(srv.Stop)
	return srv, sig, clk
}

// setTrusted sets the trusted time source on fabric 1.
func setTrusted(t *testing.T, srv *core.TimeSynchronization, nodeID uint64) {
	t.Helper()
	ctx := im.WithFabricFilter(context.Background(), false, 1)
	req := tsdef.SetTrustedTimeSourceRequest{TrustedTimeSource: spec.ValueOf(tsdef.FabricScopedTrustedTimeSourceStruct{NodeId: nodeID, Endpoint: 7})}
	if _, err := srv.MatterInvoke(ctx, tsdef.CmdSetTrustedTimeSource, req); err != nil {
		t.Fatal(err)
	}
}

func readQuality(srv *core.TimeSynchronization) (gran, src any) {
	gran, _ = srv.MatterRead(tsdef.AttrGranularity)
	src, _ = srv.MatterRead(tsdef.AttrTimeSource)
	return gran, src
}

// The host clock is the platform source by default: chip's
// DefaultTimeSyncDelegate gives MixedNTP with milliseconds while the clock
// reads a real time (DefaultTimeSyncDelegate.cpp:42-58), so Start asks
// no peer and emits nothing; before the Matter epoch there is no time and
// UTCTime is null (:255-267).
func TestTimeSyncPlatformSourceIsTheHostClock(t *testing.T) {
	t.Parallel()
	peer := &fakePeer{utc: peerAt(tsNow.Add(time.Hour)), gran: uintNode(3)}
	srv, sig, clk := tsClient(t, core.TimeSynchronizationConfig{Features: tsdef.FeatureTimeSyncClient, PeerReader: peer}, "")
	setTrusted(t, srv, 0x42)
	srv.Start(context.Background())
	srv.Stop()
	if gran, src := readQuality(srv); gran != uint8(tsdef.GranularityMillisecondsGranularity) || src != uint8(tsdef.TimeSourceMixedNtp) {
		t.Errorf("Granularity %v TimeSource %v, want Milliseconds MixedNTP", gran, src)
	}
	if utc, ok := srv.UTC(); !ok || !utc.Equal(tsNow) {
		t.Errorf("UTC %v %v, want the clock", utc, ok)
	}
	if r := peer.took(); len(r) != 0 {
		t.Errorf("reads %v, want none", r)
	}
	if ev, _ := sig.seen(); len(ev) != 0 {
		t.Errorf("events %v, want none", ev)
	}
	clk.Set(time.Date(1999, 12, 31, 0, 0, 0, 0, time.UTC))
	if v, _ := srv.MatterRead(tsdef.AttrUtcTime); v != nil {
		t.Errorf("UTCTime %v before the Matter epoch, want null", v)
	}
	if gran, src := readQuality(srv); gran != uint8(tsdef.GranularityNoTimeGranularity) || src != uint8(tsdef.TimeSourceNone) {
		t.Errorf("Granularity %v TimeSource %v before the epoch, want NoTime None", gran, src)
	}
}

// Without a platform time the node reads UTCTime and Granularity from the
// trusted time source's root over the peer reader at Start
// (:525-539, :401-428) and adopts the time with TimeSource
// NodeTimeCluster (:479) and chip's conservative granularity (:466-477).
func TestTimeSyncAdoptsTheTrustedNodesTime(t *testing.T) {
	t.Parallel()
	peerNow := tsNow.Add(36 * time.Hour)
	for _, tc := range []struct {
		peer, want tsdef.GranularityEnum
	}{
		{tsdef.GranularityMinutesGranularity, tsdef.GranularityMinutesGranularity},
		{tsdef.GranularitySecondsGranularity, tsdef.GranularityMinutesGranularity},
		{tsdef.GranularityMillisecondsGranularity, tsdef.GranularitySecondsGranularity},
		{tsdef.GranularityMicrosecondsGranularity, tsdef.GranularitySecondsGranularity},
	} {
		peer := &fakePeer{utc: peerAt(peerNow), gran: uintNode(uint64(tc.peer))}
		srv, sig, clk := tsClient(t, core.TimeSynchronizationConfig{
			Features: tsdef.FeatureTimeSyncClient, PlatformTime: noPlatformTime, PeerReader: peer,
		}, "")
		setTrusted(t, srv, 0x42)
		if v, _ := srv.MatterRead(tsdef.AttrUtcTime); v != nil {
			t.Fatalf("UTCTime %v without a time, want null", v)
		}
		srv.Start(context.Background())
		sig.wait(t, "UTCTime change")
		reads := peer.took()
		want := []im.ConcreteAttributePath{
			{Endpoint: 0, Cluster: tsdef.ClusterID, Attribute: tsdef.AttrUtcTime, HasEndpoint: true, HasCluster: true, HasAttribute: true},
			{Endpoint: 0, Cluster: tsdef.ClusterID, Attribute: tsdef.AttrGranularity, HasEndpoint: true, HasCluster: true, HasAttribute: true},
		}
		if len(reads) != 1 || reads[0].fabricIndex != 1 || reads[0].nodeID != 0x42 || !slices.Equal(reads[0].paths, want) {
			t.Fatalf("reads %+v, want the root's UTCTime and Granularity of 1:0x42", reads)
		}
		if gran, src := readQuality(srv); gran != uint8(tc.want) || src != uint8(tsdef.TimeSourceNodeTimeCluster) {
			t.Errorf("peer %d: Granularity %v TimeSource %v, want %d NodeTimeCluster", tc.peer, gran, src, tc.want)
		}
		clk.Advance(time.Minute)
		if utc, ok := srv.UTC(); !ok || !utc.Equal(peerNow.Add(time.Minute)) {
			t.Errorf("peer %d: UTC %v %v, want the peer's time moving with the clock", tc.peer, utc, ok)
		}
		if ev, ch := sig.seen(); len(ev) != 0 || !slices.Contains(ch, tsdef.AttrGranularity) || !slices.Contains(ch, tsdef.AttrTimeSource) {
			t.Errorf("peer %d: events %v changes %v, want no event and Granularity, TimeSource reported", tc.peer, ev, ch)
		}
	}
}

// A failed or useless read goes to the NTP fallback (:430-434, :486-489);
// without DefaultNTP or an NTPClient, or with a failing one, the node
// emits TimeFailure (:384-397, :519-522).
func TestTimeSyncFallbackAndTimeFailure(t *testing.T) {
	t.Parallel()
	ntpAt := tsNow.Add(-2 * time.Hour)
	for label, tc := range map[string]struct {
		features tsdef.Feature
		peer     *fakePeer
		ntp      *fakeNTP
		server   string
		adopted  bool
	}{
		"read fails, NTP answers":         {tsdef.FeatureTimeSyncClient, &fakePeer{err: errors.New("no route")}, &fakeNTP{at: ntpAt}, "fd00::1", true},
		"null UTCTime, NTP answers":       {tsdef.FeatureTimeSyncClient, &fakePeer{utc: nullNode(), gran: uintNode(3)}, &fakeNTP{at: ntpAt}, "fd00::1", true},
		"no granularity, NTP answers":     {tsdef.FeatureTimeSyncClient, &fakePeer{utc: peerAt(tsNow), gran: uintNode(0)}, &fakeNTP{at: ntpAt}, "fd00::1", true},
		"granularity absent, NTP answers": {tsdef.FeatureTimeSyncClient, &fakePeer{utc: peerAt(tsNow)}, &fakeNTP{at: ntpAt}, "fd00::1", true},
		"no TSC, NTP answers":             {tsdef.FeatureNtpClient, &fakePeer{utc: peerAt(tsNow), gran: uintNode(3)}, &fakeNTP{at: ntpAt}, "fd00::1", true},
		"no reader, NTP answers":          {tsdef.FeatureTimeSyncClient, nil, &fakeNTP{at: ntpAt}, "fd00::1", true},
		"read fails, no DefaultNTP":       {tsdef.FeatureTimeSyncClient, &fakePeer{err: errors.New("no route")}, &fakeNTP{at: ntpAt}, "", false},
		"read fails, no NTPClient":        {tsdef.FeatureTimeSyncClient, &fakePeer{err: errors.New("no route")}, nil, "fd00::1", false},
		"read fails, NTP fails":           {tsdef.FeatureTimeSyncClient, &fakePeer{err: errors.New("no route")}, &fakeNTP{err: errors.New("timeout")}, "fd00::1", false},
		"read fails, NTP before epoch":    {tsdef.FeatureTimeSyncClient, &fakePeer{err: errors.New("no route")}, &fakeNTP{at: time.Unix(0, 0)}, "fd00::1", false},
	} {
		cfg := core.TimeSynchronizationConfig{Features: tc.features, PlatformTime: noPlatformTime}
		if tc.peer != nil {
			cfg.PeerReader = tc.peer
		}
		if tc.ntp != nil {
			cfg.NTPClient = tc.ntp
		}
		srv, sig, _ := tsClient(t, cfg, tc.server)
		if tc.features&tsdef.FeatureTimeSyncClient != 0 {
			setTrusted(t, srv, 0x42)
		}
		srv.Start(context.Background())
		sig.wait(t, label+": outcome")
		srv.Stop()
		ev, _ := sig.seen()
		gran, src := readQuality(srv)
		if !tc.adopted {
			if !slices.Equal(ev, []uint32{tsdef.EventTimeFailure}) {
				t.Errorf("%s: events %v, want TimeFailure", label, ev)
			}
			if v, _ := srv.MatterRead(tsdef.AttrUtcTime); v != nil || gran != uint8(0) || src != uint8(tsdef.TimeSourceNone) {
				t.Errorf("%s: UTCTime %v Granularity %v TimeSource %v, want no time", label, v, gran, src)
			}
			continue
		}
		if len(ev) != 0 {
			t.Errorf("%s: events %v, want none", label, ev)
		}
		if gran != uint8(tsdef.GranularityMillisecondsGranularity) || src != uint8(tsdef.TimeSourceNonMatterSntp) {
			t.Errorf("%s: Granularity %v TimeSource %v, want Milliseconds NonMatterSNTP", label, gran, src)
		}
		if utc, ok := srv.UTC(); !ok || !utc.Equal(ntpAt) {
			t.Errorf("%s: UTC %v %v, want the NTP time", label, utc, ok)
		}
		if !slices.Equal(tc.ntp.servers, []string{"fd00::1"}) {
			t.Errorf("%s: NTP asked %v, want DefaultNTP", label, tc.ntp.servers)
		}
		if tc.features&tsdef.FeatureTimeSyncClient == 0 && len(tc.peer.took()) != 0 {
			t.Errorf("%s: the trusted node was read without TSC", label)
		}
	}
}

// A trusted time source set while the node has no time starts an attempt
// (:584-587), once the server is started; before Start nothing runs and
// Start makes the attempt.
func TestTimeSyncNewTrustedSourceAsksAgain(t *testing.T) {
	t.Parallel()
	peerNow := tsNow.Add(time.Hour)
	peer := &fakePeer{utc: peerAt(peerNow), gran: uintNode(2)}
	srv, sig, _ := tsClient(t, core.TimeSynchronizationConfig{Features: tsdef.FeatureTimeSyncClient, PlatformTime: noPlatformTime}, "")
	srv.AttachTimePeerReader(peer)
	srv.AttachTimePeerReader(&fakePeer{err: errors.New("not this one")}) // the first reader stays
	setTrusted(t, srv, 0x42)
	if r := peer.took(); len(r) != 0 {
		t.Fatalf("reads %v before Start, want none", r)
	}
	// Started without a source: TimeFailure (:535, :384-388).
	ctx := im.WithFabricFilter(context.Background(), false, 1)
	if _, err := srv.MatterInvoke(ctx, tsdef.CmdSetTrustedTimeSource, tsdef.SetTrustedTimeSourceRequest{TrustedTimeSource: spec.NullOf[tsdef.FabricScopedTrustedTimeSourceStruct]()}); err != nil {
		t.Fatal(err)
	}
	sig.wait(t, "MissingTrustedTimeSource")
	srv.Start(context.Background())
	sig.wait(t, "TimeFailure")
	if ev, _ := sig.seen(); !slices.Equal(ev, []uint32{tsdef.EventMissingTrustedTimeSource, tsdef.EventTimeFailure}) {
		t.Fatalf("events %v, want MissingTrustedTimeSource, TimeFailure", ev)
	}
	setTrusted(t, srv, 0x42)
	sig.wait(t, "UTCTime change")
	if utc, ok := srv.UTC(); !ok || !utc.Equal(peerNow) {
		t.Errorf("UTC %v %v, want the peer's", utc, ok)
	}
	if gran, src := readQuality(srv); gran != uint8(tsdef.GranularityMinutesGranularity) || src != uint8(tsdef.TimeSourceNodeTimeCluster) {
		t.Errorf("Granularity %v TimeSource %v, want Minutes NodeTimeCluster", gran, src)
	}
	// With a time, a new source starts no read (:584).
	setTrusted(t, srv, 0x43)
	srv.Stop()
	if r := peer.took(); len(r) != 1 {
		t.Errorf("reads %d, want 1", len(r))
	}
}

// SetUTCTime sets the node's UTC (:856-872): on a node without time any
// granularity but NoTimeGranularity is accepted (:1127-1129), then one at
// least as fine as the current; a time past epoch-s is TimeNotAccepted
// (UpdateUTCTime, :74-86). GeneralDiagnostics' TimeSnapshot follows.
func TestTimeSyncSetUTCTimeSetsTheOffset(t *testing.T) {
	t.Parallel()
	srv, _, clk := tsClient(t, core.TimeSynchronizationConfig{PlatformTime: noPlatformTime}, "")
	gd := core.NewGeneralDiagnostics(core.BootReasonPowerOnReboot)
	gd.SetUTCClock(srv.UTC)
	set := func(at time.Time, gran tsdef.GranularityEnum) error {
		_, err := srv.MatterInvoke(context.Background(), tsdef.CmdSetUtcTime,
			tsdef.SetUtcTimeRequest{UtcTime: uint64(at.UnixMicro() - 946684800_000_000), Granularity: gran}) //nolint:gosec // after the epoch
		return err
	}
	day := tsNow.Add(24 * time.Hour)
	if err := set(day, tsdef.GranularitySecondsGranularity); err != nil {
		t.Fatalf("seconds on a node without time: %v", err)
	}
	clk.Advance(time.Second)
	if utc, ok := srv.UTC(); !ok || !utc.Equal(day.Add(time.Second)) {
		t.Errorf("UTC %v %v, want the set time moving with the clock", utc, ok)
	}
	snap, err := gd.MatterInvoke(context.Background(), 0x01, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p := snap.(core.TimeSnapshotResponse).PosixTimeMs; p == nil || int64(*p) != day.Add(time.Second).UnixMilli() {
		t.Errorf("TimeSnapshot PosixTimeMs %v, want the node's UTC", p)
	}
	if err := set(day, tsdef.GranularityMicrosecondsGranularity); err != nil {
		t.Fatalf("microseconds after seconds: %v", err)
	}
	if got := tsStatus(set(tsNow, tsdef.GranularityMillisecondsGranularity)); got != im.StatusFailure {
		t.Errorf("milliseconds after microseconds: %v, want TimeNotAccepted", got)
	}
	_, err = srv.MatterInvoke(context.Background(), tsdef.CmdSetUtcTime,
		tsdef.SetUtcTimeRequest{UtcTime: (1 << 32) * 1_000_000, Granularity: tsdef.GranularityMicrosecondsGranularity})
	var cs interface{ MatterClusterStatus() uint8 }
	if !errors.As(err, &cs) || cs.MatterClusterStatus() != uint8(tsdef.StatusCodeTimeNotAccepted) {
		t.Errorf("a time past epoch-s: %v, want TimeNotAccepted", err)
	}
	if utc, _ := srv.UTC(); !utc.Equal(day) {
		t.Errorf("UTC %v after the refusals, want the last accepted %v", utc, day)
	}
}

// hangingPeer is a TimePeerReader whose node never answers: the read
// ends only with its context; entered is signalled on each call.
type hangingPeer struct{ entered chan struct{} }

func (h *hangingPeer) ReadPeerAttributes(ctx context.Context, _ uint8, _ uint64, _ []im.ConcreteAttributePath) (map[im.ConcreteAttributePath]spec.Node, error) {
	h.entered <- struct{}{}
	<-ctx.Done()
	return nil, ctx.Err()
}

// hangingNTP is an NTPClient whose server never answers.
type hangingNTP struct{ entered chan struct{} }

func (h *hangingNTP) QueryTime(ctx context.Context, _ string) (time.Time, error) {
	h.entered <- struct{}{}
	<-ctx.Done()
	return time.Time{}, ctx.Err()
}

// Each call of an attempt ends after TimeSyncAttemptTimeout on the
// server's clock: a trusted node that never answers is abandoned for the
// NTP fallback, an NTP server that never answers ends in TimeFailure.
func TestTimeSyncBoundsEachCallOfAnAttempt(t *testing.T) {
	t.Parallel()
	waitEntered := func(t *testing.T, entered chan struct{}, what string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatalf("no %s", what)
		}
	}
	t.Run("a trusted node that never answers", func(t *testing.T) {
		t.Parallel()
		ntpAt := tsNow.Add(-2 * time.Hour)
		peer := &hangingPeer{entered: make(chan struct{}, 1)}
		ntp := &fakeNTP{at: ntpAt}
		srv, sig, clk := tsClient(t, core.TimeSynchronizationConfig{
			Features: tsdef.FeatureTimeSyncClient, PlatformTime: noPlatformTime, PeerReader: peer, NTPClient: ntp,
		}, "fd00::1")
		setTrusted(t, srv, 0x42)
		srv.Start(context.Background())
		waitEntered(t, peer.entered, "read of the trusted node")
		if !slices.Contains(clk.Armed(), core.TimeSyncAttemptTimeout) {
			t.Fatalf("no %v bound armed (armed %v)", core.TimeSyncAttemptTimeout, clk.Armed())
		}
		clk.Advance(core.TimeSyncAttemptTimeout)
		sig.wait(t, "UTCTime change")
		if gran, src := readQuality(srv); gran != uint8(tsdef.GranularityMillisecondsGranularity) || src != uint8(tsdef.TimeSourceNonMatterSntp) {
			t.Errorf("Granularity %v TimeSource %v, want the NTP fallback's", gran, src)
		}
		if utc, ok := srv.UTC(); !ok || !utc.Equal(ntpAt) {
			t.Errorf("UTC %v %v, want the NTP time", utc, ok)
		}
	})
	t.Run("an NTP server that never answers", func(t *testing.T) {
		t.Parallel()
		ntp := &hangingNTP{entered: make(chan struct{}, 1)}
		srv, sig, clk := tsClient(t, core.TimeSynchronizationConfig{
			Features: tsdef.FeatureTimeSyncClient, PlatformTime: noPlatformTime,
			PeerReader: &fakePeer{err: errors.New("no route")}, NTPClient: ntp,
		}, "fd00::1")
		setTrusted(t, srv, 0x42)
		srv.Start(context.Background())
		waitEntered(t, ntp.entered, "NTP query")
		clk.Advance(core.TimeSyncAttemptTimeout)
		sig.wait(t, "TimeFailure")
		if ev, _ := sig.seen(); !slices.Equal(ev, []uint32{tsdef.EventTimeFailure}) {
			t.Errorf("events %v, want TimeFailure", ev)
		}
	})
}
