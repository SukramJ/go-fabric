// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/cluster/spec"
	tsuic "thingfabric.dev/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	"thingfabric.dev/go-fabric/cluster/thermo"
	"thingfabric.dev/go-fabric/contract"
)

// keypadStorePaths is every way a host or a controller stores
// KeypadLockout on a ThermostatUIServer, by the spec.Server method name it
// goes through.
var keypadStorePaths = map[string]func(srv *thermo.ThermostatUIServer, v thermo.KeypadLockout) error{
	"Set": func(srv *thermo.ThermostatUIServer, v thermo.KeypadLockout) error {
		return srv.Set(tsuic.AttrKeypadLockout, v)
	},
	"SetAttributes": func(srv *thermo.ThermostatUIServer, v thermo.KeypadLockout) error {
		return srv.SetAttributes(map[uint32]any{tsuic.AttrKeypadLockout: v})
	},
	"SetAttributesFrom": func(srv *thermo.ThermostatUIServer, v thermo.KeypadLockout) error {
		return srv.SetAttributesFrom(contract.OriginCommand, map[uint32]any{tsuic.AttrKeypadLockout: v})
	},
	"MatterWrite": func(srv *thermo.ThermostatUIServer, v thermo.KeypadLockout) error {
		return srv.MatterWrite(context.Background(), tsuic.AttrKeypadLockout, uint64(v))
	},
	"MatterWriteChanged": func(srv *thermo.ThermostatUIServer, v thermo.KeypadLockout) error {
		_, err := srv.MatterWriteChanged(context.Background(), tsuic.AttrKeypadLockout, uint64(v))
		return err
	},
	"MatterWriteCommit": func(srv *thermo.ThermostatUIServer, v thermo.KeypadLockout) error {
		_, err := srv.MatterWriteCommit(context.Background(), tsuic.AttrKeypadLockout, uint64(v))
		return err
	},
}

// notStoring are the spec.Server methods named like a store that store no
// attribute value.
var notStoring = []string{"SetBeforeWrite", "SetEndpoint", "SetMatterEventEmitter"}

// TestThermostatUIEveryStorePathSavesKeypadLockout (R3-store-3202) is the
// standing guard of the class: every spec.Server method that stores an
// attribute (a Set… or MatterWrite… name) is in keypadStorePaths, and
// each of them leaves the Settings at the served KeypadLockout. Before the
// fix the promoted SetAttributesFrom stored without the save.
func TestThermostatUIEveryStorePathSavesKeypadLockout(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeFor[*spec.Server]()
	for i := range typ.NumMethod() {
		name := typ.Method(i).Name
		if !strings.HasPrefix(name, "Set") && !strings.HasPrefix(name, "MatterWrite") || slices.Contains(notStoring, name) {
			continue
		}
		if _, ok := keypadStorePaths[name]; !ok {
			t.Errorf("spec.Server.%s stores attributes but is not in keypadStorePaths: does ThermostatUIServer save KeypadLockout through it?", name)
		}
	}
	for name, store := range keypadStorePaths {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			settings := &flakySettings{values: map[string]string{}}
			srv := newTSUIC(t, thermo.ThermostatUIConfig{Settings: settings})
			if err := store(srv, tsuic.KeypadLockoutLockout1); err != nil {
				t.Fatal(err)
			}
			v, _ := srv.Value(tsuic.AttrKeypadLockout)
			if got, want := settings.values[thermo.DefaultKeypadLockoutSetting], fmt.Sprintf("%d", v); got != want {
				t.Errorf("served KeypadLockout %s, Settings hold %q", want, got)
			}
		})
	}
}

// TestThermostatUIKeypadListenerSetsAgain (R3-conc-3201): a change
// listener that, told of a KeypadLockout change on the caller's
// goroutine, sets KeypadLockout again returns, and the Settings end at
// the served value. Before the fix the host's Set held the save slot
// while the store told its listeners, so the listener's Set waited for
// the slot its own caller held.
func TestThermostatUIKeypadListenerSetsAgain(t *testing.T) {
	t.Parallel()
	for _, outer := range []string{"Set", "SetAttributesFrom", "MatterWrite"} {
		t.Run(outer, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				settings := &flakySettings{values: map[string]string{}}
				srv := newTSUIC(t, thermo.ThermostatUIConfig{Settings: settings})
				again := true
				srv.OnAttributeChange(func(contract.Change) {
					if again {
						again = false
						_ = srv.Set(tsuic.AttrKeypadLockout, tsuic.KeypadLockoutLockout2)
					}
				})
				done := make(chan error, 1)
				go func() { done <- keypadStorePaths[outer](srv, tsuic.KeypadLockoutLockout1) }()
				synctest.Wait()
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				default:
					t.Fatal("the call is durably blocked: its listener's Set waits for the slot the call holds")
				}
				v, _ := srv.Value(tsuic.AttrKeypadLockout)
				if got, want := settings.values[thermo.DefaultKeypadLockoutSetting], fmt.Sprintf("%d", v); got != want || want != fmt.Sprintf("%d", tsuic.KeypadLockoutLockout2) {
					t.Errorf("served KeypadLockout %v, Settings hold %q; want both Lockout2", v, got)
				}
			})
		})
	}
}
