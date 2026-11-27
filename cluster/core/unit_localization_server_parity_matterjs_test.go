// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/unit-localization/
// UnitLocalizationServerTest.ts at db91886c, by meaning. matter.js sets
// the server's state on the root (rootWith :17-24) and writes as a
// controller (writeTemperatureUnit :37-49); the Go form is the
// constructor's configuration and a MatterWrite on the built server, the
// path the generated runtime runs for a controller's write. A restart is
// matter.js's storage carrying the written unit to the next start; here
// the host persists the unit OnWrite hands it and passes it back as
// TemperatureUnit.
package core_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/cluster/spec"
	unitdef "thingfabric.dev/go-fabric/cluster/spec/unitlocalization"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// enum8ListTLV is the anonymous-tag TLV of a list[enum8] attribute value
// read off a server, the form a reader receives.
func enum8ListTLV(t *testing.T, v any) []byte {
	t.Helper()
	e, ok := v.(spec.Encodable)
	if !ok {
		t.Fatalf("%T is not Encodable", v)
	}
	enc := tlv.NewEncoder()
	e.EncodeTLV(enc, tlv.AnonymousTag())
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// wantEnum8List is the TLV of a list[enum8] holding values.
func wantEnum8List[T ~uint8](values ...T) []byte {
	b := []byte{0x16}
	for _, v := range values {
		b = append(b, 0x04, byte(v))
	}
	return append(b, 0x18)
}

// TestUnitLocalizationServerParityMatterJS ports the describe
// "UnitLocalizationServer" (UnitLocalizationServerTest.ts:51-171) where
// it has a Go subject. Left: the replacement of a configured or stored
// unit the list lacks (:88-100, :153-170) and the warning that goes with
// it (:102-111) — the module refuses a configured contradictory unit
// (BD-Matter-Localization-ConfiguredStartValueRefused).
func TestUnitLocalizationServerParityMatterJS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	unitOf := func(t *testing.T, srv *spec.Server) unitdef.TempUnitEnum {
		t.Helper()
		v, ok := srv.MatterRead(unitdef.AttrTemperatureUnit)
		if !ok {
			t.Fatal("no TemperatureUnit")
		}
		return unitdef.TempUnitEnum(v.(uint8))
	}
	build := func(t *testing.T, cfg core.UnitLocalizationConfig) *spec.Server {
		t.Helper()
		srv, err := core.NewUnitLocalization(cfg)
		if err != nil {
			t.Fatal(err)
		}
		return srv
	}

	// :71-77.
	t.Run("accepts a controller write of a unit in SupportedTemperatureUnits", func(t *testing.T) {
		t.Parallel()
		srv := build(t, core.UnitLocalizationConfig{})
		if err := srv.MatterWrite(ctx, unitdef.AttrTemperatureUnit, uint64(unitdef.TempUnitFahrenheit)); statusOf(err) != im.StatusSuccess {
			t.Fatalf("write of Fahrenheit: %v, want SUCCESS", err)
		}
		if got := unitOf(t, srv); got != unitdef.TempUnitFahrenheit {
			t.Errorf("TemperatureUnit %v, want Fahrenheit", got)
		}
	})

	// :79-86. matter.js names the failed constraint "2 to 3"; the Go
	// constructor refuses the list as a value outside its constraint.
	t.Run("refuses a SupportedTemperatureUnits list with a single entry", func(t *testing.T) {
		t.Parallel()
		_, err := core.NewUnitLocalization(core.UnitLocalizationConfig{SupportedTemperatureUnits: []unitdef.TempUnitEnum{unitdef.TempUnitCelsius}})
		if !errors.Is(err, spec.ErrInvalidValue) {
			t.Fatalf("one unit: %v, want spec.ErrInvalidValue", err)
		}
	})

	// :113-125. Refused with ErrLocalizationConfig before the hybrid start
	// (decision 289).
	t.Run("starts with a supported unit when Celsius is not in the configured SupportedTemperatureUnits", func(t *testing.T) {
		t.Parallel()
		srv := build(t, core.UnitLocalizationConfig{SupportedTemperatureUnits: []unitdef.TempUnitEnum{unitdef.TempUnitFahrenheit, unitdef.TempUnitKelvin}})
		if got := unitOf(t, srv); got != unitdef.TempUnitFahrenheit {
			t.Errorf("TemperatureUnit %v, want Fahrenheit", got)
		}
		v, _ := srv.MatterRead(unitdef.AttrSupportedTemperatureUnits)
		if got, want := enum8ListTLV(t, v), wantEnum8List(unitdef.TempUnitFahrenheit, unitdef.TempUnitKelvin); !slices.Equal(got, want) {
			t.Errorf("SupportedTemperatureUnits % X, want % X", got, want)
		}
	})

	// :127-134.
	t.Run("keeps Celsius as the default when it is in the configured SupportedTemperatureUnits but not first", func(t *testing.T) {
		t.Parallel()
		srv := build(t, core.UnitLocalizationConfig{SupportedTemperatureUnits: []unitdef.TempUnitEnum{unitdef.TempUnitFahrenheit, unitdef.TempUnitCelsius}})
		if got := unitOf(t, srv); got != unitdef.TempUnitCelsius {
			t.Errorf("TemperatureUnit %v, want Celsius", got)
		}
	})

	// :136-151.
	t.Run("keeps a written TemperatureUnit across a restart", func(t *testing.T) {
		t.Parallel()
		var stored *unitdef.TempUnitEnum
		first := build(t, core.UnitLocalizationConfig{OnWrite: func(_ context.Context, u unitdef.TempUnitEnum) error {
			stored = &u
			return nil
		}})
		if err := first.MatterWrite(ctx, unitdef.AttrTemperatureUnit, uint64(unitdef.TempUnitFahrenheit)); statusOf(err) != im.StatusSuccess {
			t.Fatalf("write of Fahrenheit: %v, want SUCCESS", err)
		}
		if stored == nil {
			t.Fatal("OnWrite handed the host nothing to persist")
		}
		second := build(t, core.UnitLocalizationConfig{TemperatureUnit: stored})
		if got := unitOf(t, second); got != unitdef.TempUnitFahrenheit {
			t.Errorf("TemperatureUnit after the restart %v, want Fahrenheit", got)
		}
	})

	// Go's own: an empty, non-nil list is no configuration, as matter.js's
	// `!supportedTemperatureUnits?.length` (UnitLocalizationServer.ts:18
	// at db91886c).
	t.Run("seeds an empty SupportedTemperatureUnits with Celsius and Fahrenheit", func(t *testing.T) {
		t.Parallel()
		srv := build(t, core.UnitLocalizationConfig{SupportedTemperatureUnits: []unitdef.TempUnitEnum{}})
		if got := unitOf(t, srv); got != unitdef.TempUnitCelsius {
			t.Errorf("TemperatureUnit %v, want Celsius", got)
		}
		v, _ := srv.MatterRead(unitdef.AttrSupportedTemperatureUnits)
		if got, want := enum8ListTLV(t, v), wantEnum8List(unitdef.TempUnitCelsius, unitdef.TempUnitFahrenheit); !slices.Equal(got, want) {
			t.Errorf("SupportedTemperatureUnits % X, want % X", got, want)
		}
	})

	// Go's own (BD-Matter-Localization-ConfiguredStartValueRefused): a
	// configured unit the configured list lacks is refused, where
	// matter.js replaces it (:88-100).
	t.Run("refuses a configured TemperatureUnit that is not in the configured SupportedTemperatureUnits", func(t *testing.T) {
		t.Parallel()
		kelvin := unitdef.TempUnitKelvin
		_, err := core.NewUnitLocalization(core.UnitLocalizationConfig{
			TemperatureUnit:           &kelvin,
			SupportedTemperatureUnits: []unitdef.TempUnitEnum{unitdef.TempUnitFahrenheit, unitdef.TempUnitCelsius},
		})
		if !errors.Is(err, core.ErrLocalizationConfig) {
			t.Fatalf("Kelvin of [Fahrenheit, Celsius]: %v, want ErrLocalizationConfig", err)
		}
	})
}
