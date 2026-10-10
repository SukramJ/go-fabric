// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	lcfgdef "github.com/SukramJ/go-fabric/cluster/spec/localizationconfiguration"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	tfldef "github.com/SukramJ/go-fabric/cluster/spec/timeformatlocalization"
	unitdef "github.com/SukramJ/go-fabric/cluster/spec/unitlocalization"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestParityMatterJS_LocalizationConfiguration: SupportedLocales defaults
// to [ActiveLocale] (matter.js LocalizationConfigurationServer.ts:18-20);
// a written ActiveLocale outside it is CONSTRAINT_ERROR ("in
// supportedLocales"; chip LocalizationConfigurationCluster.cpp:125-128).
func TestParityMatterJS_LocalizationConfiguration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	def, err := NewLocalizationConfiguration(LocalizationConfigurationConfig{ActiveLocale: "de-DE"})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, def, lcfgdef.Definition, 0)
	if v, _ := def.MatterRead(lcfgdef.AttrSupportedLocales); !slices.Equal(v.([]string), []string{"de-DE"}) {
		t.Errorf("default SupportedLocales %v", v)
	}

	var written []string
	srv, err := NewLocalizationConfiguration(LocalizationConfigurationConfig{
		ActiveLocale: "de-DE", SupportedLocales: []string{"de-DE", "en-US"},
		OnWrite: func(_ context.Context, l string) error { written = append(written, l); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterWrite(ctx, lcfgdef.AttrActiveLocale, "en-US"); err != nil {
		t.Fatalf("supported locale: %v", err)
	}
	if err := srv.MatterWrite(ctx, lcfgdef.AttrActiveLocale, "fr-FR"); statusOf(err) != im.StatusConstraintError {
		t.Errorf("unsupported locale: %v, want CONSTRAINT_ERROR", err)
	}
	if err := srv.MatterWrite(ctx, lcfgdef.AttrActiveLocale, uint64(1)); statusOf(err) != im.StatusConstraintError {
		t.Errorf("not a string: %v, want CONSTRAINT_ERROR", err)
	}
	if v, _ := srv.MatterRead(lcfgdef.AttrActiveLocale); v != "en-US" || !slices.Equal(written, []string{"en-US"}) {
		t.Errorf("ActiveLocale %v, persisted %v", v, written)
	}

	for _, cfg := range []LocalizationConfigurationConfig{
		{},
		{ActiveLocale: "fr-FR", SupportedLocales: []string{"de-DE"}},
	} {
		if _, err := NewLocalizationConfiguration(cfg); !errors.Is(err, ErrLocalizationConfig) {
			t.Errorf("%+v: %v", cfg, err)
		}
	}
	if _, err := NewLocalizationConfiguration(LocalizationConfigurationConfig{ActiveLocale: "a", SupportedLocales: slices.Repeat([]string{"a"}, 33)}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("33 locales: %v", err)
	}
	refusing, _ := NewLocalizationConfiguration(LocalizationConfigurationConfig{
		ActiveLocale: "de-DE", SupportedLocales: []string{"de-DE", "en-US"},
		OnWrite: func(context.Context, string) error { return errors.New("no") },
	})
	if err := refusing.MatterWrite(ctx, lcfgdef.AttrActiveLocale, "en-US"); err == nil {
		t.Error("host refusal not answered")
	}
}

// TestParityMatterJS_TimeFormatLocalization: the server has CalendarFormat
// (matter.js TimeFormatLocalizationServer.ts:16), SupportedCalendarTypes
// defaults to [ActiveCalendarType] (:24-26), and a written
// ActiveCalendarType outside it is CONSTRAINT_ERROR (chip
// TimeFormatLocalizationCluster.cpp:138-141).
func TestParityMatterJS_TimeFormatLocalization(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	type write struct {
		attr  uint32
		value uint8
	}
	var written []write
	srv, err := NewTimeFormatLocalization(TimeFormatLocalizationConfig{
		HourFormat: tfldef.HourFormatV24Hr, ActiveCalendarType: tfldef.CalendarTypeGregorian,
		OnWrite: func(_ context.Context, a uint32, v uint8) error { written = append(written, write{a, v}); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, srv, tfldef.Definition, uint32(tfldef.FeatureCalendarFormat))
	v, _ := srv.MatterRead(tfldef.AttrSupportedCalendarTypes)
	if l, ok := v.(enumList[tfldef.CalendarTypeEnum]); !ok || !slices.Equal(l, enumList[tfldef.CalendarTypeEnum]{tfldef.CalendarTypeGregorian}) {
		t.Errorf("default SupportedCalendarTypes %#v", v)
	}
	enc := tlv.NewEncoder()
	v.(spec.Encodable).EncodeTLV(enc, tlv.AnonymousTag())
	if got, err := enc.Bytes(); err != nil || !slices.Equal(got, []byte{0x16, 0x04, 0x04, 0x18}) {
		t.Errorf("SupportedCalendarTypes TLV % X", got)
	}
	if v, _ := srv.MatterRead(tfldef.AttrHourFormat); v != uint8(tfldef.HourFormatV24Hr) {
		t.Errorf("HourFormat %v", v)
	}
	if err := srv.MatterWrite(ctx, tfldef.AttrHourFormat, uint64(tfldef.HourFormatV12Hr)); err != nil {
		t.Errorf("HourFormat 12Hr: %v", err)
	}
	if err := srv.MatterWrite(ctx, tfldef.AttrHourFormat, uint64(7)); statusOf(err) != im.StatusConstraintError {
		t.Errorf("HourFormat 7: %v, want CONSTRAINT_ERROR", err)
	}
	if err := srv.MatterWrite(ctx, tfldef.AttrActiveCalendarType, uint64(tfldef.CalendarTypeGregorian)); err != nil {
		t.Errorf("Gregorian: %v", err)
	}
	if err := srv.MatterWrite(ctx, tfldef.AttrActiveCalendarType, uint64(tfldef.CalendarTypeBuddhist)); statusOf(err) != im.StatusConstraintError {
		t.Errorf("Buddhist, not supported: %v, want CONSTRAINT_ERROR", err)
	}
	if want := []write{{tfldef.AttrHourFormat, 0}, {tfldef.AttrActiveCalendarType, 4}}; !slices.Equal(written, want) {
		t.Errorf("persisted %v, want %v", written, want)
	}
	if _, err := NewTimeFormatLocalization(TimeFormatLocalizationConfig{
		ActiveCalendarType: tfldef.CalendarTypeBuddhist, SupportedCalendarTypes: []tfldef.CalendarTypeEnum{tfldef.CalendarTypeGregorian},
	}); !errors.Is(err, ErrLocalizationConfig) {
		t.Errorf("active not supported: %v", err)
	}
	refusing, _ := NewTimeFormatLocalization(TimeFormatLocalizationConfig{OnWrite: func(context.Context, uint32, uint8) error { return errors.New("no") }})
	if err := refusing.MatterWrite(ctx, tfldef.AttrHourFormat, uint64(1)); err == nil {
		t.Error("host refusal not answered")
	}
}

// TestParityMatterJS_UnitLocalization: TemperatureUnit defaults to Celsius
// and SupportedTemperatureUnits to [Celsius, Fahrenheit] (matter.js
// UnitLocalizationServer.ts:16-24); a written unit outside the supported
// list is CONSTRAINT_ERROR (chip UnitLocalizationCluster.cpp:105-115).
func TestParityMatterJS_UnitLocalization(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var written []unitdef.TempUnitEnum
	srv, err := NewUnitLocalization(UnitLocalizationConfig{
		OnWrite: func(_ context.Context, u unitdef.TempUnitEnum) error { written = append(written, u); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, srv, unitdef.Definition, uint32(unitdef.FeatureTemperatureUnit))
	if v, _ := srv.MatterRead(unitdef.AttrTemperatureUnit); v != uint8(unitdef.TempUnitCelsius) {
		t.Errorf("default TemperatureUnit %v", v)
	}
	want := enumList[unitdef.TempUnitEnum]{unitdef.TempUnitCelsius, unitdef.TempUnitFahrenheit}
	if v, _ := srv.MatterRead(unitdef.AttrSupportedTemperatureUnits); !slices.Equal(v.(enumList[unitdef.TempUnitEnum]), want) {
		t.Errorf("default SupportedTemperatureUnits %v", v)
	}
	if err := srv.MatterWrite(ctx, unitdef.AttrTemperatureUnit, uint64(unitdef.TempUnitFahrenheit)); err != nil {
		t.Errorf("Fahrenheit: %v", err)
	}
	if err := srv.MatterWrite(ctx, unitdef.AttrTemperatureUnit, uint64(unitdef.TempUnitKelvin)); statusOf(err) != im.StatusConstraintError {
		t.Errorf("Kelvin, not supported: %v, want CONSTRAINT_ERROR", err)
	}
	if err := srv.MatterWrite(ctx, unitdef.AttrTemperatureUnit, uint64(3)); statusOf(err) != im.StatusConstraintError {
		t.Errorf("3: %v, want CONSTRAINT_ERROR", err)
	}
	if !slices.Equal(written, []unitdef.TempUnitEnum{unitdef.TempUnitFahrenheit}) {
		t.Errorf("persisted %v", written)
	}

	kelvin := unitdef.TempUnitKelvin
	all, err := NewUnitLocalization(UnitLocalizationConfig{
		TemperatureUnit:           &kelvin,
		SupportedTemperatureUnits: []unitdef.TempUnitEnum{unitdef.TempUnitFahrenheit, unitdef.TempUnitCelsius, unitdef.TempUnitKelvin},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := all.MatterWrite(ctx, unitdef.AttrTemperatureUnit, uint64(unitdef.TempUnitCelsius)); err != nil {
		t.Errorf("Celsius of three: %v", err)
	}
	if _, err := NewUnitLocalization(UnitLocalizationConfig{TemperatureUnit: &kelvin}); !errors.Is(err, ErrLocalizationConfig) {
		t.Errorf("Kelvin of the default two: %v", err)
	}
	// SupportedTemperatureUnits is "2 to 3".
	if _, err := NewUnitLocalization(UnitLocalizationConfig{SupportedTemperatureUnits: []unitdef.TempUnitEnum{unitdef.TempUnitCelsius}}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("one unit: %v", err)
	}
	refusing, _ := NewUnitLocalization(UnitLocalizationConfig{OnWrite: func(context.Context, unitdef.TempUnitEnum) error { return errors.New("no") }})
	if err := refusing.MatterWrite(ctx, unitdef.AttrTemperatureUnit, uint64(unitdef.TempUnitFahrenheit)); err == nil {
		t.Error("host refusal not answered")
	}
}
