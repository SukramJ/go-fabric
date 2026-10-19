// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

// The node wirings of the phase-18 system set (brief BO, BN's register
// candidates): the GeneralDiagnostics feature selection and
// MaxPathsPerInvoke in node.Config with the DataModelTest check at Start
// (GeneralDiagnosticsServer.ts #online :352, #assertDataModelTest
// :125-143), MaxPathsPerInvoke enforced as advertised
// (InteractionServer.ts:198, :1017), CapabilityMinima in node.Config
// (CapabilityMinimaTest.ts "caller-provided values win"), and the grouped
// ConfigurationVersion on the Aggregator, persisted once
// (BasicInformationServer.ts:132-138, BridgedDeviceBasicInformationServer.ts:72-90).

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	bidef "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"
)

// infoLog records the messages of level INFO and above.
type infoLog struct {
	mu   sync.Mutex
	msgs []string
}

func (h *infoLog) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelInfo }

func (h *infoLog) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.msgs = append(h.msgs, r.Message)
	h.mu.Unlock()
	return nil
}
func (h *infoLog) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *infoLog) WithGroup(string) slog.Handler      { return h }

func (h *infoLog) contains(sub string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, m := range h.msgs {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}

// TestStartChecksDataModelTest: without DataModelTest a MaxPathsPerInvoke
// above one fails Start and leaves the node stopped; at one it starts, and
// FeatureMap and BasicInformation say so; with the feature at one the
// advisory is logged.
func TestStartChecksDataModelTest(t *testing.T) {
	t.Parallel()
	noDMTest := &mattercore.GeneralDiagnosticsFeatures{}
	start := func(t *testing.T, features *mattercore.GeneralDiagnosticsFeatures, maxPaths uint16) (*node.Node, *infoLog, error) {
		t.Helper()
		cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
		log := &infoLog{}
		cfg.Logger = slog.New(log)
		cfg.GeneralDiagnosticsFeatures = features
		cfg.MaxPathsPerInvoke = maxPaths
		n := newNode(t, cfg)
		err := n.Start(context.Background())
		return n, log, err
	}

	t.Run("fails without DataModelTest above one path", func(t *testing.T) {
		t.Parallel()
		for _, maxPaths := range []uint16{0, 2} { // 0 is the default, 10
			if _, _, err := start(t, noDMTest, maxPaths); err == nil || !strings.Contains(err.Error(), "DataModelTest feature is mandatory") {
				t.Errorf("MaxPathsPerInvoke %d without DataModelTest: Start = %v, want the DataModelTest error", maxPaths, err)
			}
		}
	})
	t.Run("starts without DataModelTest at one path", func(t *testing.T) {
		t.Parallel()
		n, _, err := start(t, noDMTest, 1)
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		if v, _ := n.Root().GeneralDiagnostics.MatterRead(cluster.AttrGlobalFeatureMap); v != uint32(0) {
			t.Errorf("GeneralDiagnostics FeatureMap = %v, want 0", v)
		}
		if v, _ := n.Root().BasicInformation.MatterRead(bidef.AttrMaxPathsPerInvoke); v != uint16(1) {
			t.Errorf("BasicInformation MaxPathsPerInvoke = %v (%T), want 1", v, v)
		}
	})
	t.Run("logs the advisory with DataModelTest at one path", func(t *testing.T) {
		t.Parallel()
		_, log, err := start(t, nil, 1)
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		if !log.contains("The DataModelTest feature is enabled but is only required") {
			t.Error("no DataModelTest advisory logged")
		}
	})
}

// TestMaxPathsPerInvokeIsEnforced: a node advertising one path per invoke
// refuses a batch of two with InvalidAction, as InteractionServer.ts:1017
// does; the default node takes it.
func TestMaxPathsPerInvokeIsEnforced(t *testing.T) {
	holdSite(t)
	// refused reports whether the device refused the batch as a whole with
	// InvalidAction.
	refused := func(t *testing.T, maxPaths uint16) bool {
		t.Helper()
		site := quietSite(t)
		light := newLight(t, site, "light:1")
		p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
			Bridged: []node.Endpoint{light},
			Configure: func(cfg *node.Config) {
				cfg.GeneralDiagnosticsFeatures = &mattercore.GeneralDiagnosticsFeatures{DataModelTest: maxPaths != 1}
				cfg.MaxPathsPerInvoke = maxPaths
			},
		}})
		ep, _ := p.Device.EndpointOf("light:1")
		// Off and On on the light, as one batch.
		batch := encodeInvoke(t, im.InvokeRequest{Invokes: []im.CommandInvocation{
			{Path: cmdPath(ep, 0x0006, 0x00), CommandRef: 1, HasCommandRef: true},
			{Path: cmdPath(ep, 0x0006, 0x01), CommandRef: 2, HasCommandRef: true},
		}})
		msg, err := fabrictest.Exchange(parityCtx(t), p, im.OpcodeInvokeRequest, batch)
		if err != nil {
			t.Fatal(err)
		}
		if msg.Opcode != im.OpcodeStatusResponse {
			return false
		}
		sr, err := im.UnmarshalStatusResponseTLV(tlv.NewDecoder(msg.Payload))
		if err != nil {
			t.Fatal(err)
		}
		return sr.Status == im.StatusInvalidAction
	}
	if !refused(t, 1) {
		t.Error("a batch of two at MaxPathsPerInvoke 1 was not refused with InvalidAction")
	}
	if refused(t, 0) {
		t.Error("a batch of two at the default MaxPathsPerInvoke was refused with InvalidAction")
	}
}

// TestConfigCapabilityMinima: the host's CapabilityMinima reach the root
// BasicInformation, its zero fields take the defaults
// (CapabilityMinimaTest.ts "caller-provided values win").
func TestConfigCapabilityMinima(t *testing.T) {
	t.Parallel()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.CapabilityMinima = mattercore.CapabilityMinimaStruct{CaseSessionsPerFabric: 5, ReadPathsSupported: 30}
	n := newNode(t, cfg)
	v, ok := n.Root().BasicInformation.MatterRead(bidef.AttrCapabilityMinima)
	if !ok {
		t.Fatal("CapabilityMinima unreadable")
	}
	got := v.(mattercore.CapabilityMinimaStruct)
	want := mattercore.CapabilityMinimaStruct{
		CaseSessionsPerFabric: 5, SubscriptionsPerFabric: 3, SimultaneousInvocationsSupported: 20,
		SimultaneousWritesSupported: 20, ReadPathsSupported: 30, SubscribePathsSupported: 20,
	}
	if got != want {
		t.Errorf("CapabilityMinima = %+v, want %+v", got, want)
	}
}

// TestAggregatorIncreaseConfigurationVersions: one grouped change raises
// each bridged device once and the node once, after them, and the versions
// survive a restart; an unknown key raises nothing.
func TestAggregatorIncreaseConfigurationVersions(t *testing.T) {
	holdSite(t)
	site := quietSite(t)
	a, b := newLight(t, site, "light:a"), newLight(t, site, "light:b")
	d := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{a, b}})
	rootVersion := func() uint32 {
		v, _ := d.Node().Root().BasicInformation.MatterRead(bidef.AttrConfigurationVersion)
		return v.(uint32)
	}
	bridgedVersion := func(key string) uint32 {
		id, ok := d.EndpointOf(key)
		if !ok {
			t.Fatalf("no endpoint %q", key)
		}
		return d.Bridge().Topology().FindByID(id).ConfigurationVersion()
	}
	root, va, vb := rootVersion(), bridgedVersion("light:a"), bridgedVersion("light:b")

	if err := d.Node().Aggregator().IncreaseConfigurationVersions(endpoint.StringKey("light:a"), endpoint.StringKey("nope")); !errors.Is(err, node.ErrUnknownKey) {
		t.Fatalf("an unknown key: %v, want ErrUnknownKey", err)
	}
	if rootVersion() != root || bridgedVersion("light:a") != va {
		t.Fatal("a refused change raised a version")
	}

	if err := d.Node().Aggregator().IncreaseConfigurationVersions(endpoint.StringKey("light:a"), endpoint.StringKey("light:b"), endpoint.StringKey("light:a")); err != nil {
		t.Fatal(err)
	}
	want := [3]uint32{root + 1, va + 1, vb + 1}
	if got := [3]uint32{rootVersion(), bridgedVersion("light:a"), bridgedVersion("light:b")}; got != want {
		t.Fatalf("versions (node, a, b) = %v, want %v", got, want)
	}

	if err := d.Restart(parityCtx(t)); err != nil {
		t.Fatal(err)
	}
	if got := [3]uint32{rootVersion(), bridgedVersion("light:a"), bridgedVersion("light:b")}; got != want {
		t.Errorf("after a restart (node, a, b) = %v, want %v", got, want)
	}
}
