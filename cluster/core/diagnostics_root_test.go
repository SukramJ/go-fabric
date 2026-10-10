// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"slices"
	"testing"

	ethdiagdef "github.com/SukramJ/go-fabric/cluster/spec/ethernetnetworkdiagnostics"
	swdiagdef "github.com/SukramJ/go-fabric/cluster/spec/softwarediagnostics"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/im"
)

type heap struct {
	free, used, mark uint64
	known            bool
	resets           int
	resetErr         error
}

func (h *heap) CurrentHeapFree() (uint64, bool)          { return h.free, h.known }
func (h *heap) CurrentHeapUsed() (uint64, bool)          { return h.used, h.known }
func (h *heap) CurrentHeapHighWatermark() (uint64, bool) { return h.mark, h.known }
func (h *heap) ResetWatermarks(context.Context) error {
	h.resets++
	return h.resetErr
}

// TestParityMatterJS_SoftwareDiagnostics: the generated server (matter.js
// SoftwareDiagnosticsServer.ts:14) serving the host's heap values, 0 where
// the host cannot tell (chip SoftwareDiagnosticsCluster.cpp:98-109), and
// WTRMRK with ResetWatermarks only when the host provides watermarks
// (SoftwareDiagnosticsCluster.h:82-90).
func TestParityMatterJS_SoftwareDiagnostics(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bare, err := NewSoftwareDiagnostics(SoftwareDiagnosticsConfig{})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, bare, swdiagdef.Definition, 0)
	if len(bare.MatterAttributes()) != 0 || len(bare.MatterAcceptedCommands()) != 0 {
		t.Errorf("bare server lists %v / %v", bare.MatterAttributes(), bare.MatterAcceptedCommands())
	}

	h := &heap{free: 100, used: 200, mark: 300, known: true}
	srv, err := NewSoftwareDiagnostics(SoftwareDiagnosticsConfig{Heap: h, Watermarks: h})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, srv, swdiagdef.Definition, uint32(swdiagdef.FeatureWatermarks))
	for id, want := range map[uint32]uint64{
		swdiagdef.AttrCurrentHeapFree: 100, swdiagdef.AttrCurrentHeapUsed: 200, swdiagdef.AttrCurrentHeapHighWatermark: 300,
	} {
		if v, ok := srv.MatterRead(id); !ok || v != want {
			t.Errorf("attribute 0x%04X = %v (%v), want %d", id, v, ok, want)
		}
	}
	h.known = false
	if v, _ := srv.MatterRead(swdiagdef.AttrCurrentHeapUsed); v != uint64(0) {
		t.Errorf("unknown heap reads %v, want 0", v)
	}
	if _, ok := srv.MatterRead(swdiagdef.AttrThreadMetrics); ok {
		t.Error("ThreadMetrics served")
	}
	if !slices.Equal(srv.MatterAcceptedCommands(), []uint32{swdiagdef.CmdResetWatermarks}) {
		t.Errorf("accepted %v", srv.MatterAcceptedCommands())
	}
	if _, err := srv.MatterInvoke(ctx, swdiagdef.CmdResetWatermarks, nil); err != nil || h.resets != 1 {
		t.Errorf("ResetWatermarks: %v, %d resets", err, h.resets)
	}
	h.resetErr = errors.New("no")
	if _, err := srv.MatterInvoke(ctx, swdiagdef.CmdResetWatermarks, nil); err == nil {
		t.Error("ResetWatermarks error not answered")
	}
	if _, err := bare.MatterInvoke(ctx, swdiagdef.CmdResetWatermarks, nil); statusOf(err) != im.StatusUnsupportedCommand {
		t.Errorf("ResetWatermarks without WTRMRK: %v", err)
	}
}

type ether struct{ known bool }

func (e ether) PHYRate() (ethdiagdef.PHYRateEnum, bool) { return ethdiagdef.PHYRateRate1G, e.known }
func (e ether) FullDuplex() (duplex, ok bool)           { return true, e.known }
func (e ether) CarrierDetect() (carrier, ok bool)       { return true, e.known }
func (e ether) TimeSinceReset() (uint64, bool)          { return 42, e.known }

// TestParityMatterJS_EthernetNetworkDiagnostics: the generated server
// (matter.js EthernetNetworkDiagnosticsServer.ts:14) without PKTCNT /
// ERRCNT, serving the host's values; null for PHYRate, FullDuplex and
// CarrierDetect and 0 for TimeSinceReset where the host cannot tell (chip
// EthernetDiagnosticsCluster.cpp:69-121).
func TestParityMatterJS_EthernetNetworkDiagnostics(t *testing.T) {
	t.Parallel()
	optional := []uint32{ethdiagdef.AttrPhyRate, ethdiagdef.AttrFullDuplex, ethdiagdef.AttrCarrierDetect, ethdiagdef.AttrTimeSinceReset}
	srv, err := NewEthernetNetworkDiagnostics(EthernetNetworkDiagnosticsConfig{Reporter: ether{known: true}, Attributes: optional})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, srv, ethdiagdef.Definition, 0)
	if len(srv.MatterAcceptedCommands()) != 0 {
		t.Errorf("accepted %v", srv.MatterAcceptedCommands())
	}
	for id, want := range map[uint32]any{
		ethdiagdef.AttrPhyRate: uint8(ethdiagdef.PHYRateRate1G), ethdiagdef.AttrFullDuplex: true,
		ethdiagdef.AttrCarrierDetect: true, ethdiagdef.AttrTimeSinceReset: uint64(42),
	} {
		if v, ok := srv.MatterRead(id); !ok || v != want {
			t.Errorf("attribute 0x%04X = %v (%v), want %v", id, v, ok, want)
		}
	}
	unknown, err := NewEthernetNetworkDiagnostics(EthernetNetworkDiagnosticsConfig{Reporter: ether{}, Attributes: optional})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[uint32]any{
		ethdiagdef.AttrPhyRate: nil, ethdiagdef.AttrFullDuplex: nil, ethdiagdef.AttrCarrierDetect: nil, ethdiagdef.AttrTimeSinceReset: uint64(0),
	} {
		if v, ok := unknown.MatterRead(id); !ok || v != want {
			t.Errorf("unknown attribute 0x%04X = %v (%v), want %v", id, v, ok, want)
		}
	}
	if _, ok := unknown.MatterRead(ethdiagdef.AttrPacketRxCount); ok {
		t.Error("PacketRxCount served without PKTCNT")
	}
	if _, err := NewEthernetNetworkDiagnostics(EthernetNetworkDiagnosticsConfig{}); !errors.Is(err, ErrNilReporter) {
		t.Errorf("no reporter: %v", err)
	}
	if _, err := NewEthernetNetworkDiagnostics(EthernetNetworkDiagnosticsConfig{Reporter: ether{}, Attributes: []uint32{ethdiagdef.AttrPacketRxCount}}); err == nil {
		t.Error("PacketRxCount accepted without PKTCNT")
	}
}
