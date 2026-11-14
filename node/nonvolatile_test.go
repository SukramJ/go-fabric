// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	epmem "github.com/SukramJ/go-fabric/endpoint/memstore"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// nvCase is one nonvolatile attribute a controller writes: on the root
// (key "") or on the bridged device build makes under key.
type nvCase struct {
	key     string
	build   func(t *testing.T, key string) node.Endpoint
	cluster uint32
	attr    uint32
	write   any // the value as the bridge decodes it from the wire
	want    string
	// edit, when set, adjusts the node's configuration.
	edit func(*node.Config)
}

func nvDevice[D node.Endpoint](build func(device.Options, ...device.Declaration) (D, error), decls ...device.Declaration) func(*testing.T, string) node.Endpoint {
	return func(t *testing.T, key string) node.Endpoint {
		t.Helper()
		d, err := build(device.Options{StableKey: endpoint.StringKey(key), Name: key}, decls...)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
}

// nvCases are one attribute per class the persister keeps: the root's
// generated-store servers, and the kit's self-held servers of a bridged
// device.
var nvCases = map[string]nvCase{
	"root LocalizationConfiguration.ActiveLocale": {cluster: 0x002B, attr: 0x0000, write: "de-DE", want: "de-DE"},
	"root TimeFormatLocalization.HourFormat":      {cluster: 0x002C, attr: 0x0000, write: uint64(0), want: "0"},
	"root UnitLocalization.TemperatureUnit":       {cluster: 0x002D, attr: 0x0000, write: uint64(0), want: "0"},
	"OnOff.StartUpOnOff":                          {key: "light", build: nvDevice(device.NewOnOffLight), cluster: 0x0006, attr: 0x4003, write: uint64(1), want: "1"},
	"LevelControl.StartUpCurrentLevel":            {key: "dimmer", build: nvDevice(device.NewDimmableLight), cluster: 0x0008, attr: 0x4000, write: uint64(77), want: "77"},
	"ColorControl.StartUpColorTemperatureMireds":  {key: "ct", build: nvDevice(device.NewColorTemperatureLight), cluster: 0x0300, attr: 0x4010, write: uint64(300), want: "300"},
	"WindowCovering.Mode":                         {key: "cover", build: nvDevice(device.NewWindowCovering, wcdef.Declare{Features: wcdef.FeatureLift}), cluster: 0x0102, attr: 0x0017, write: uint64(4), want: "4"},
	"root NetworkCommissioning.InterfaceEnabled":  {cluster: 0x0031, attr: 0x0004, write: false, want: "false"},
	"root NetworkCommissioning.InterfaceEnabled, Wi-Fi": {
		cluster: 0x0031, attr: 0x0004, write: false, want: "false",
		edit: func(cfg *node.Config) { cfg.Network.WiFi = &node.WiFi{Driver: joinLab{}} },
	},
	"Thermostat.MinHeatSetpointLimit": {key: "thermo", build: nvDevice(device.NewThermostat, thermostatdef.Declare{Features: thermostatdef.FeatureHeating}), cluster: 0x0201, attr: 0x0015, write: int64(1000), want: "1000"},
}

// nvRun is a node over st and eps with the case's device added, started.
func nvRun(t *testing.T, cfg node.Config, tc nvCase) (n *node.Node, ep uint16) {
	t.Helper()
	ctx := t.Context()
	if tc.edit != nil {
		tc.edit(&cfg)
	}
	n = newNode(t, cfg)
	if tc.key != "" {
		if err := n.Aggregator().Add(ctx, tc.build(t, tc.key)); err != nil {
			t.Fatal(err)
		}
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if tc.key != "" {
		id, ok := endpointOf(t, n, tc.key)
		if !ok {
			t.Fatalf("no endpoint for %q", tc.key)
		}
		ep = id
	}
	return n, ep
}

// dispatchWrite writes v over the dispatcher a controller's write reaches.
func dispatchWrite(t *testing.T, n *node.Node, path im.ConcreteAttributePath, v any) im.StatusCode {
	t.Helper()
	res := endpoint.NewTopologyDispatcher(n.Bridge().Topology()).Write(t.Context(), path, im.AttributeValue{Value: v})
	if len(res) != 1 {
		t.Fatalf("write %v: %d results", path, len(res))
	}
	return res[0].Status
}

// dispatchRead reads the attribute as a controller does, formatted.
func dispatchRead(t *testing.T, n *node.Node, path im.ConcreteAttributePath) string {
	t.Helper()
	res := endpoint.NewTopologyDispatcher(n.Bridge().Topology()).Read(t.Context(), path)
	if len(res) != 1 || res[0].Status != im.StatusSuccess {
		t.Fatalf("read %v: %+v", path, res)
	}
	return fmt.Sprint(res[0].Value.Value)
}

// TestNonVolatileWriteSurvivesARestart: a controller's write of a
// nonvolatile attribute the module holds is read back after a restart
// over the same store (R2-P2-store-04, owner decision 38).
func TestNonVolatileWriteSurvivesARestart(t *testing.T) {
	t.Parallel()
	for name, tc := range nvCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := minimalConfig(nil)
			cfg.Locales = []string{"en-US", "de-DE"}
			cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
			n, ep := nvRun(t, cfg, tc)
			path := attrPath(ep, tc.cluster, tc.attr)
			before := dispatchRead(t, n, path)
			if before == tc.want {
				t.Fatalf("the attribute starts at %s already", before)
			}
			if st := dispatchWrite(t, n, path, tc.write); st != im.StatusSuccess {
				t.Fatalf("write: %v", st)
			}
			if err := n.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
			again, ep := nvRun(t, cfg, tc)
			if got := dispatchRead(t, again, attrPath(ep, tc.cluster, tc.attr)); got != tc.want {
				t.Errorf("after the restart %s, want %s (before the write %s)", got, tc.want, before)
			}
		})
	}
}

// TestNonVolatileWriteRefusedByTheStore: a write the store refuses
// answers FAILURE and the old value stays in effect.
func TestNonVolatileWriteRefusedByTheStore(t *testing.T) {
	t.Parallel()
	for name, tc := range nvCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			st := &refusingKeys{Port: memstore.New(), keys: []string{"nonvolatile."}}
			cfg := minimalConfig(nil)
			cfg.Locales = []string{"en-US", "de-DE"}
			cfg.Store = node.Store{Credentials: st, Endpoints: epmem.New()}
			n, ep := nvRun(t, cfg, tc)
			path := attrPath(ep, tc.cluster, tc.attr)
			before := dispatchRead(t, n, path)
			st.refuse.Store(true)
			if got := dispatchWrite(t, n, path, tc.write); got != im.StatusFailure {
				t.Errorf("write with the store refusing: %v, want FAILURE", got)
			}
			if got := dispatchRead(t, n, path); got != before {
				t.Errorf("value %s after the refused write, %s before it", got, before)
			}
		})
	}
}

// TestNonVolatileWriteRefusedByTheServer: a value the server refuses
// leaves the store as it was, so the restart serves the earlier value.
func TestNonVolatileWriteRefusedByTheServer(t *testing.T) {
	t.Parallel()
	tc := nvCases["LevelControl.StartUpCurrentLevel"]
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
	n, ep := nvRun(t, cfg, tc)
	path := attrPath(ep, tc.cluster, tc.attr)
	if st := dispatchWrite(t, n, path, tc.write); st != im.StatusSuccess {
		t.Fatalf("write: %v", st)
	}
	if st := dispatchWrite(t, n, path, "not a level"); st == im.StatusSuccess {
		t.Fatal("the server took a string as StartUpCurrentLevel")
	}
	if err := n.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	again, ep := nvRun(t, cfg, tc)
	if got := dispatchRead(t, again, attrPath(ep, tc.cluster, tc.attr)); got != tc.want {
		t.Errorf("after the restart %s, want %s", got, tc.want)
	}
}

// TestBridgedUserLabelStoredBeforeTheWriteAnswers: a bridged endpoint's
// UserLabel list is stored before the write answers, so a store that
// refuses it fails the write, as the root's does (R2-P2-store-07).
func TestBridgedUserLabelStoredBeforeTheWriteAnswers(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	st := &refusingKeys{Port: memstore.New(), keys: []string{"bridged_user_label."}}
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: st, Endpoints: epmem.New()}
	tc := nvCase{key: "light", build: func(t *testing.T, key string) node.Endpoint {
		t.Helper()
		d, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey(key), Name: key}, userlabeldef.Declare{})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}}
	n, ep := nvRun(t, cfg, tc)
	path := attrPath(ep, userlabeldef.ClusterID, userlabeldef.AttrLabelList)
	list := spec.List[userlabeldef.LabelStruct]{{Label: "room", Value: "kitchen"}}
	st.refuse.Store(true)
	if got := dispatchWrite(t, n, path, list); got != im.StatusFailure {
		t.Errorf("UserLabel write with the store refusing: %v, want FAILURE", got)
	}
	if got := dispatchRead(t, n, path); got != "[]" {
		t.Errorf("LabelList %s after the refused write, want []", got)
	}
	st.refuse.Store(false)
	if got := dispatchWrite(t, n, path, list); got != im.StatusSuccess {
		t.Fatalf("UserLabel write: %v", got)
	}
	if v, ok, err := st.GetSetting(ctx, "bridged_user_label.light"); err != nil || !ok || v != `[{"Label":"room","Value":"kitchen"}]` {
		t.Errorf("stored list %q (found %v, %v) after the write answered", v, ok, err)
	}
}

// TestNonVolatileStoresTheCommittedValue: a write the server commits in a
// normalised form — WindowCovering clears CalibrationMode under
// MaintenanceMode, so a Mode of 6 is served as 4 — is served the same
// after a restart.
func TestNonVolatileStoresTheCommittedValue(t *testing.T) {
	t.Parallel()
	tc := nvCases["WindowCovering.Mode"]
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
	n, ep := nvRun(t, cfg, tc)
	path := attrPath(ep, tc.cluster, tc.attr)
	if st := dispatchWrite(t, n, path, uint64(6)); st != im.StatusSuccess {
		t.Fatalf("write: %v", st)
	}
	served := dispatchRead(t, n, path)
	if err := n.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	again, ep := nvRun(t, cfg, tc)
	if got := dispatchRead(t, again, attrPath(ep, tc.cluster, tc.attr)); got != served {
		t.Fatalf("Mode after the restart %s, served %s before it", got, served)
	}
}

// TestBridgedUserLabelRollbackOwedUntilTheStop: a bridged LabelList write
// the server refuses, whose rollback the store refuses too, is put right
// when the node stops: the restart serves the list the server held, not
// the refused one (R2-P2-rr-ctl-05).
func TestBridgedUserLabelRollbackOwedUntilTheStop(t *testing.T) {
	t.Parallel()
	st := &refusingKeys{Port: memstore.New(), keys: []string{"bridged_user_label."}}
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: st, Endpoints: epmem.New()}
	var veto atomic.Bool
	tc := nvCase{key: "light", build: func(t *testing.T, key string) node.Endpoint {
		t.Helper()
		d, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey(key), Name: key}, userlabeldef.Declare{
			OnWrite: func(context.Context, userlabeldef.State, uint32) error {
				if !veto.Load() {
					return nil
				}
				// The store refuses from here on: the rollback after the
				// veto is refused.
				st.refuse.Store(true)
				return errors.New("vetoed")
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}}
	n, ep := nvRun(t, cfg, tc)
	path := attrPath(ep, userlabeldef.ClusterID, userlabeldef.AttrLabelList)
	if got := dispatchWrite(t, n, path, spec.List[userlabeldef.LabelStruct]{{Label: "room", Value: "kitchen"}}); got != im.StatusSuccess {
		t.Fatalf("UserLabel write: %v", got)
	}
	served := dispatchRead(t, n, path)
	veto.Store(true)
	if got := dispatchWrite(t, n, path, spec.List[userlabeldef.LabelStruct]{{Label: "room", Value: "attic"}}); got == im.StatusSuccess {
		t.Fatal("the vetoed UserLabel write succeeded")
	}
	st.refuse.Store(false)
	veto.Store(false)
	if err := n.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	again, ep := nvRun(t, cfg, tc)
	if got := dispatchRead(t, again, attrPath(ep, userlabeldef.ClusterID, userlabeldef.AttrLabelList)); got != served {
		t.Errorf("LabelList after the restart %s, served %s before it", got, served)
	}
}
