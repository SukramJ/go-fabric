// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster"
	gencommdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/surface"
)

// termsRow is the register row GeneralCommissioning's TC feature
// implements (cluster/core/ahead.go).
const termsRow = "BD-chip-GeneralCommissioning-TermsAndConditions"

// The node serves GeneralCommissioning's TermsAndConditions feature when
// its configuration states the terms and the row is enabled: the FeatureMap
// bit, the TC attributes with the configured requirement, and
// SetTCAcknowledgements accepted (BD-chip-GeneralCommissioning-TermsAndConditions).
func TestNodeTermsAndConditionsServed(t *testing.T) {
	t.Parallel()
	deadline := uint32(1_800_000_000)
	m := newMockPair(t, mockOptions{
		configure: func(cfg *node.Config) {
			cfg.Commissioning.TermsAndConditions = &node.TermsAndConditions{Version: 2, Acknowledgements: 0b11, UpdateDeadline: &deadline}
			cfg.Surface = surface.Policies{Extensions: surface.NewExtensions(termsRow)}
		},
		// The commissioner acknowledges the terms (the commissioner's
		// ConfigureTCAcknowledgements step); without it the device refuses
		// CommissioningComplete with TCAcknowledgementsNotReceived.
		commission: func(o *commissioner.Options) {
			o.TermsAndConditions = &commissioner.TermsAndConditionsAcknowledgement{Version: 2, Acknowledgements: 0b11}
		},
	})
	r, err := fabrictest.ReadWith(parityCtx(t), m.pair, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{
		{Cluster: gencommdef.ClusterID, HasCluster: true, Attribute: cluster.AttrGlobalFeatureMap, HasAttribute: true},
		{Cluster: gencommdef.ClusterID, HasCluster: true, Attribute: gencommdef.AttrTcMinRequiredVersion, HasAttribute: true},
		{Cluster: gencommdef.ClusterID, HasCluster: true, Attribute: gencommdef.AttrTcAcknowledgementsRequired, HasAttribute: true},
		{Cluster: gencommdef.ClusterID, HasCluster: true, Attribute: gencommdef.AttrTcAcceptedVersion, HasAttribute: true},
		{Cluster: gencommdef.ClusterID, HasCluster: true, Attribute: cluster.AttrGlobalAcceptedCommandList, HasAttribute: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Statuses) != 0 {
		t.Fatalf("statuses %+v", r.Statuses)
	}
	if v, ok := r.Value(0, gencommdef.ClusterID, cluster.AttrGlobalFeatureMap); !ok || uint32(v.El.Uint)&uint32(gencommdef.FeatureTermsAndConditions) == 0 {
		t.Fatalf("FeatureMap = %v (%v), want the TC bit", v, ok)
	}
	if v, ok := r.Value(0, gencommdef.ClusterID, gencommdef.AttrTcMinRequiredVersion); !ok || v.El.Uint != 2 {
		t.Fatalf("TcMinRequiredVersion = %v, want 2", v)
	}
	if v, ok := r.Value(0, gencommdef.ClusterID, gencommdef.AttrTcAcknowledgementsRequired); !ok || v.El.Bool {
		t.Fatalf("TcAcknowledgementsRequired = %v, want false after the commissioner's acknowledgement", v)
	}
	if v, ok := r.Value(0, gencommdef.ClusterID, gencommdef.AttrTcAcceptedVersion); !ok || v.El.Uint != 2 {
		t.Fatalf("TcAcceptedVersion = %v, want the acknowledged 2", v)
	}
	gc := m.pair.Device.Node().Root().GeneralCommissioning
	if gc == nil {
		t.Fatal("no GeneralCommissioning")
	}
	if tc := gc.TermsAndConditions(); tc == nil || tc.RequiredVersion != 2 || tc.RequiredAcknowledgements != 0b11 || tc.UpdateDeadline == nil || *tc.UpdateDeadline != deadline {
		t.Fatalf("TermsAndConditions() = %+v", tc)
	}
	// The report lists the row's decision on the root.
	var found bool
	for _, c := range m.pair.Device.Node().Surface().Effective.Root.Clusters {
		if c.ID == gencommdef.ClusterID {
			found = c.FeatureMap&uint32(gencommdef.FeatureTermsAndConditions) != 0
		}
	}
	if !found {
		t.Fatal("the surface report's root GeneralCommissioning lacks the TC feature")
	}
}

// Without the terms the surface is the one of before: no bit, the TC
// attributes unsupported (node/attribute_read_response_parity_matterjs_test.go
// pins TcAcceptedVersion as UnsupportedAttribute).
func TestNodeTermsAndConditionsOffByDefault(t *testing.T) {
	t.Parallel()
	m := newMockPair(t, mockOptions{})
	r, err := fabrictest.ReadWith(parityCtx(t), m.pair, interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{
		{Cluster: gencommdef.ClusterID, HasCluster: true, Attribute: cluster.AttrGlobalFeatureMap, HasAttribute: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := r.Value(0, gencommdef.ClusterID, cluster.AttrGlobalFeatureMap); !ok || v.El.Uint != 0 {
		t.Fatalf("FeatureMap = %v, want 0", v)
	}
	if gc := m.pair.Device.Node().Root().GeneralCommissioning; gc.TermsAndConditions() != nil {
		t.Fatal("terms served without configuration")
	}
}

// The row is served behind surface.Policies.Extensions (ADR 0030): a
// configuration that states the terms without enabling the row is
// refused, naming the row and the root.
func TestNodeTermsAndConditionsNeedsTheExtension(t *testing.T) {
	t.Parallel()
	db := openDB(t, filepath.Join(t.TempDir(), "tc.db"))
	cfg := minimalConfig(db)
	cfg.Commissioning.TermsAndConditions = &node.TermsAndConditions{Version: 1, Acknowledgements: 1}
	_, err := node.New(context.Background(), cfg)
	if !errors.Is(err, surface.ErrExtensionNotEnabled) || !errors.Is(err, surface.ErrRefused) {
		t.Fatalf("err = %v, want ErrExtensionNotEnabled", err)
	}
	if !strings.Contains(err.Error(), termsRow) || !strings.Contains(err.Error(), "the root endpoint") {
		t.Fatalf("error %q does not name the row and the root", err)
	}
	// A policy that removes the feature leaves the node without it.
	cfg.Surface = surface.Policies{Extensions: surface.NewExtensions(termsRow)}
	n := newNode(t, cfg)
	if n.Root().GeneralCommissioning.TermsAndConditions() == nil {
		t.Fatal("the enabled row serves no terms")
	}
}

// A committed acceptance survives the node's restart on the same store and
// goes with the store's erasure (TC-CGEN-2.8); the last fabric's removal
// resets it (TC-CGEN-2.9, node.fabricRemoved).
func TestNodeTermsAndConditionsPersistence(t *testing.T) {
	t.Parallel()
	db := openDB(t, filepath.Join(t.TempDir(), "tc.db"))
	cfg := minimalConfig(db)
	cfg.Commissioning.TermsAndConditions = &node.TermsAndConditions{Version: 1, Acknowledgements: 1}
	cfg.Surface = surface.Policies{Extensions: surface.NewExtensions(termsRow)}
	n := newNode(t, cfg)
	gc := n.Root().GeneralCommissioning
	ctx := im.WithFabricFilter(context.Background(), false, 1)
	resp, err := gc.MatterInvoke(ctx, gencommdef.CmdSetTcAcknowledgements, gencommdef.SetTcAcknowledgementsRequest{TcVersion: 1, TcUserResponse: 1})
	if err != nil || resp.(gencommdef.SetTcAcknowledgementsResponse).ErrorCode != gencommdef.CommissioningErrorOk {
		t.Fatalf("SetTCAcknowledgements: %v %v", resp, err)
	}
	if v, ok, err := store.New(db).GetSetting(context.Background(), store.SettingTermsAcceptance); err != nil || !ok || v != "1:1" {
		t.Fatalf("persisted acceptance %q %v %v", v, ok, err)
	}
	if err := n.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	again := newNode(t, cfg)
	if v, _ := again.Root().GeneralCommissioning.MatterRead(gencommdef.AttrTcAcceptedVersion); v.(uint16) != 1 {
		t.Fatalf("TcAcceptedVersion after restart = %v, want 1", v)
	}
}
