// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/cover"
	matterfan "github.com/SukramJ/go-fabric/cluster/fan"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	"github.com/SukramJ/go-fabric/contract"
)

// stepAll is [clusterWatch.step] for several attributes: how far the
// cluster's DataVersion advanced and how many times each of attrs was
// reported meanwhile.
func (w *clusterWatch) stepAll(what func(), attrs ...uint32) (advanced uint32, reports map[uint32]int) {
	w.t.Helper()
	what()
	w.now = w.now.Add(2 * time.Second)
	w.mgr.Tick(context.Background(), w.now)
	v := w.ep.ClusterDataVersion(w.cluster)
	advanced, w.version = v-w.version, v
	reports = map[uint32]int{}
	w.spy.mu.Lock()
	defer w.spy.mu.Unlock()
	for _, call := range w.spy.calls {
		for _, p := range call {
			if p.Endpoint != w.ep.ID || p.Cluster != w.cluster {
				continue
			}
			for _, a := range attrs {
				if p.Attribute == a {
					reports[a]++
				}
			}
		}
	}
	w.spy.calls = nil
	return advanced, reports
}

// TestWriteCommitSiblingsReportedOnceBesideAHostNotifier (F-db021da0-57):
// a write whose MatterWriteCommit names siblings it moved — FanControl's
// PercentSetting moving FanMode and SpeedSetting, WindowCovering's Mode
// moving ConfigStatus — advances the cluster's DataVersion once and
// reports the written attribute and each sibling once, while the host's
// change notifier covering them fires as well: inside the write (the fan
// host notifies in ApplyFanSettings) or after it (the endpoint source's
// notifier of the cover) — and, for the fan, when no notifier fires, the
// commit's names alone report them.
func TestWriteCommitSiblingsReportedOnceBesideAHostNotifier(t *testing.T) {
	t.Parallel()
	write := func(h *appHarness, epID uint16, cluster, attr uint32, v uint64) func() {
		return func() {
			if st := h.writeAttribute(epID, cluster, attr, uintValue(v)); st != 0 {
				t.Fatalf("write 0x%04X/0x%04X: status 0x%02X", cluster, attr, st)
			}
		}
	}

	fan := func(t *testing.T, notifying bool) {
		t.Helper()
		dev := &notifyingFanHost{}
		var src matterfan.StateSource = &dev.fanDevice
		if notifying {
			src = dev
		}
		srv, err := matterfan.NewServer(matterfan.Config{
			Source:   src,
			Features: matterfan.FeatureMultiSpeed | matterfan.FeatureAuto | matterfan.FeatureStep,
			Sequence: matterfan.SequenceOffLowMedHighAuto, SpeedMax: 4,
			DeviceType: matterfan.DeviceTypeFan,
		})
		if err != nil {
			t.Fatal(err)
		}
		h := newAppHarness(t, appDevice{deviceType: matterfan.DeviceTypeFan, servers: []contract.ClusterServer{srv}})
		epID := h.endpoints[matterfan.DeviceTypeFan]
		w := watchCluster(t, h, epID, matterfan.ClusterID)
		before := dev.FanState()
		advanced, reports := w.stepAll(write(h, epID, matterfan.ClusterID, matterfan.AttrPercentSetting, 50),
			matterfan.AttrFanMode, matterfan.AttrPercentSetting, matterfan.AttrSpeedSetting)
		after := dev.FanState()
		moved := map[uint32]bool{
			matterfan.AttrFanMode:        before.FanMode != after.FanMode,
			matterfan.AttrPercentSetting: !sameSetting(before.PercentSetting, after.PercentSetting),
			matterfan.AttrSpeedSetting:   !sameSetting(before.SpeedSetting, after.SpeedSetting),
		}
		if !moved[matterfan.AttrFanMode] && !moved[matterfan.AttrSpeedSetting] {
			t.Fatalf("the PercentSetting write moved no sibling (before %+v, after %+v): the test checks nothing", before, after)
		}
		if advanced != 1 {
			t.Errorf("DataVersion advanced %d, want 1", advanced)
		}
		for attr, m := range moved {
			want := 0
			if m {
				want = 1
			}
			if reports[attr] != want {
				t.Errorf("attribute 0x%04X (moved %v) reported %d times, want %d", attr, m, reports[attr], want)
			}
		}
	}
	t.Run("FanControl PercentSetting, the host notifies inside ApplyFanSettings", func(t *testing.T) {
		t.Parallel()
		fan(t, true)
	})
	// Without a host notification only the commit's names report the
	// siblings.
	t.Run("FanControl PercentSetting, a host that does not notify", func(t *testing.T) {
		t.Parallel()
		fan(t, false)
	})

	t.Run("WindowCovering Mode, the endpoint source notifies after the write", func(t *testing.T) {
		t.Parallel()
		srv, err := cover.New(cover.Config{FeatureMap: uint32(wcdef.FeatureLift | wcdef.FeaturePositionAwareLift)})
		if err != nil {
			t.Fatal(err)
		}
		const windowCovering = 0x0202
		n := &hostNotifier{}
		h := newNotifyingAppHarness(t, notifyingAppDevice{appDevice{deviceType: windowCovering, servers: []contract.ClusterServer{srv}}, n})
		epID := h.endpoints[windowCovering]
		w := watchCluster(t, h, epID, wcdef.ClusterID)
		writeThenNotify := func() {
			write(h, epID, wcdef.ClusterID, wcdef.AttrMode, uint64(wcdef.ModeMotorDirectionReversed))()
			n.fire()
		}
		advanced, reports := w.stepAll(writeThenNotify, wcdef.AttrMode, wcdef.AttrConfigStatus)
		if advanced != 1 || reports[wcdef.AttrMode] != 1 || reports[wcdef.AttrConfigStatus] != 1 {
			t.Errorf("the Mode write: DataVersion advanced %d, Mode reported %d times, ConfigStatus %d; want 1, 1 and 1",
				advanced, reports[wcdef.AttrMode], reports[wcdef.AttrConfigStatus])
		}
		advanced, reports = w.stepAll(n.fire, wcdef.AttrMode, wcdef.AttrConfigStatus)
		if advanced != 0 || len(reports) != 0 {
			t.Errorf("a later notification that moved nothing: DataVersion advanced %d, reports %v; want none", advanced, reports)
		}
	})
}

// sameSetting reports whether two held fan settings are equal (nil for
// null).
func sameSetting(a, b *uint8) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
