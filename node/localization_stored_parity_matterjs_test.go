// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	lcfgdef "thingfabric.dev/go-fabric/cluster/spec/localizationconfiguration"
	tfldef "thingfabric.dev/go-fabric/cluster/spec/timeformatlocalization"
	unitdef "thingfabric.dev/go-fabric/cluster/spec/unitlocalization"
	epmem "thingfabric.dev/go-fabric/endpoint/memstore"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/store/memstore"
	"thingfabric.dev/go-fabric/tlv"
)

// The stored-value cases of matter.js's three localization server tests
// (all at b25ed960, #4901): a value a controller wrote is stored, the
// node restarts with a list that no longer holds it, and the server starts
// with a supported value, with one warning. In matter.js the stored value
// is the server's state at initialize, which replaces it
// (LocalizationConfigurationServer.ts:27-32, UnitLocalizationServer.ts:29-37,
// TimeFormatLocalizationServer.ts:34-41). Here the cluster/core
// constructors hold no stored value: the node facade restores the stored
// value after construction through SetAttributes (node/nonvolatile.go
// restore, setRestored), which refuses one the list lacks — the "in"
// constraint — and logs "nonvolatile.restore" at WARN; the server keeps
// its start value, the one the node built it with.
//
// The node's own lists: LocalizationConfiguration's is Config.Locales,
// the first of them its start value (node/root.go buildRootOptional);
// UnitLocalization and TimeFormatLocalization take no list from the
// node's configuration — [Celsius, Fahrenheit] and [Gregorian]. A stored
// value outside those two lists cannot come from a controller's write on
// this node, so the cases put it into the store directly, as a run with
// another list would have left it.

// settingsRecorder is a settings port that records every key set.
type settingsRecorder struct {
	store.Port
	mu  sync.Mutex
	set []string
}

func (s *settingsRecorder) SetSetting(ctx context.Context, key, value string) error {
	s.mu.Lock()
	s.set = append(s.set, key)
	s.mu.Unlock()
	return s.Port.SetSetting(ctx, key, value)
}

func (s *settingsRecorder) SetSettings(ctx context.Context, values map[string]string) error {
	s.mu.Lock()
	for k := range values {
		s.set = append(s.set, k)
	}
	s.mu.Unlock()
	return s.Port.SetSettings(ctx, values)
}

// keyEndingIn is the one recorded key with suffix: the node's own key of
// an attribute, read off the store rather than composed.
func (s *settingsRecorder) keyEndingIn(t *testing.T, suffix string) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	var found []string
	for _, k := range s.set {
		if strings.HasPrefix(k, "nonvolatile.") && strings.HasSuffix(k, suffix) && !slices.Contains(found, k) {
			found = append(found, k)
		}
	}
	if len(found) != 1 {
		t.Fatalf("keys ending in %q: %q, want one", suffix, found)
	}
	return found[0]
}

// restoreWarnings records the "nonvolatile.restore" WARN lines per key.
type restoreWarnings struct {
	mu   sync.Mutex
	keys []string
}

func (h *restoreWarnings) Enabled(context.Context, slog.Level) bool { return true }

func (h *restoreWarnings) Handle(_ context.Context, r slog.Record) error {
	if r.Level != slog.LevelWarn || r.Message != "nonvolatile.restore" {
		return nil
	}
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "key" {
			h.mu.Lock()
			h.keys = append(h.keys, a.Value.String())
			h.mu.Unlock()
		}
		return true
	})
	return nil
}

func (h *restoreWarnings) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *restoreWarnings) WithGroup(string) slog.Handler      { return h }

// expectOneReset is matter.js's expectOneReset: one warning, about key.
func (h *restoreWarnings) expectOneReset(t *testing.T, key string) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.keys) != 1 || h.keys[0] != key {
		t.Errorf("restore warnings %q, want one for %q", h.keys, key)
	}
}

// storedEnum is the stored form of an enum value as the node writes it:
// the anonymous TLV element, hex-encoded (nonvolatile.go storedForm).
func storedEnum(t *testing.T, v uint64) string {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.PutUint(tlv.AnonymousTag(), v)
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// localizationRun starts a node over st with locales and logger, stops it
// at the test's end; the caller stops it earlier for a restart.
func localizationRun(t *testing.T, st store.Port, eps *epmem.Store, locales []string, logger slog.Handler) *node.Node {
	t.Helper()
	cfg := minimalConfig(nil)
	cfg.Locales = locales
	cfg.Store = node.Store{Credentials: st, Endpoints: eps}
	if logger != nil {
		cfg.Logger = slog.New(logger)
	}
	n, _ := nvRun(t, cfg, nvCase{})
	return n
}

// TestLocalizationConfigurationStoredParityMatterJS ports the two
// stored-value cases of matter.js's describe
// "LocalizationConfigurationServer"
// (packages/node/test/behaviors/localization-configuration/LocalizationConfigurationServerTest.ts
// :152-169, :171-186 at b25ed960). matter.js's detected locale is en-US
// (EnglishServer :16-21); the node has no detection, its start value is
// the first of Config.Locales, which is en-US in the first case and the
// list's first in the second, where matter.js takes the first because the
// detected one is not listed (LocalizationConfigurationServer.ts:27-28,
// :32).
func TestLocalizationConfigurationStoredParityMatterJS(t *testing.T) {
	t.Parallel()
	path := attrPath(0, lcfgdef.ClusterID, lcfgdef.AttrActiveLocale)
	writeDeDE := func(t *testing.T) (*settingsRecorder, *epmem.Store, string) {
		t.Helper()
		st, eps := &settingsRecorder{Port: memstore.New()}, epmem.New()
		first := localizationRun(t, st, eps, []string{"en-US", "de-DE"}, nil)
		if got := dispatchWrite(t, first, path, "de-DE"); got != im.StatusSuccess {
			t.Fatalf("write of de-DE: %v", got)
		}
		if err := first.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
		key := st.keyEndingIn(t, fmt.Sprintf(".%04x.%04x", lcfgdef.ClusterID, lcfgdef.AttrActiveLocale))
		return st, eps, key
	}

	t.Run("starts with a supported locale when the stored ActiveLocale is no longer in SupportedLocales", func(t *testing.T) {
		t.Parallel()
		st, eps, key := writeDeDE(t)
		log := &restoreWarnings{}
		again := localizationRun(t, st, eps, []string{"en-US", "fr-FR"}, log)
		if got := dispatchRead(t, again, path); got != "en-US" {
			t.Errorf("ActiveLocale %s, want en-US", got)
		}
		log.expectOneReset(t, key)
	})

	t.Run("falls back to the first supported locale when the stored one and the detected one are not listed", func(t *testing.T) {
		t.Parallel()
		st, eps, _ := writeDeDE(t)
		again := localizationRun(t, st, eps, []string{"fr-FR", "it-IT"}, nil)
		if got := dispatchRead(t, again, path); got != "fr-FR" {
			t.Errorf("ActiveLocale %s, want fr-FR", got)
		}
	})
}

// TestUnitLocalizationStoredParityMatterJS ports the stored-value case of
// matter.js's describe "UnitLocalizationServer"
// (packages/node/test/behaviors/unit-localization/UnitLocalizationServerTest.ts
// :153-170 at b25ed960): Fahrenheit stored, the list [Kelvin, Celsius],
// the start Celsius (UnitLocalizationServer.ts:29-31). The node's list is
// [Celsius, Fahrenheit] always, so the stored unit the list lacks is
// Kelvin, put into the store under the key the first run's write of
// Fahrenheit used; the start is Celsius, as matter.js's.
func TestUnitLocalizationStoredParityMatterJS(t *testing.T) {
	t.Parallel()
	path := attrPath(0, unitdef.ClusterID, unitdef.AttrTemperatureUnit)

	t.Run("starts with a supported unit when the stored TemperatureUnit is no longer in SupportedTemperatureUnits", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		st, eps := &settingsRecorder{Port: memstore.New()}, epmem.New()
		first := localizationRun(t, st, eps, nil, nil)
		if got := dispatchWrite(t, first, path, uint64(unitdef.TempUnitFahrenheit)); got != im.StatusSuccess {
			t.Fatalf("write of Fahrenheit: %v", got)
		}
		if err := first.Stop(ctx); err != nil {
			t.Fatal(err)
		}
		key := st.keyEndingIn(t, fmt.Sprintf(".%04x.%04x", unitdef.ClusterID, unitdef.AttrTemperatureUnit))
		if v, _, err := st.GetSetting(ctx, key); err != nil || v != storedEnum(t, uint64(unitdef.TempUnitFahrenheit)) {
			t.Fatalf("stored %q (%v), want Fahrenheit's stored form", v, err)
		}
		if err := st.SetSetting(ctx, key, storedEnum(t, uint64(unitdef.TempUnitKelvin))); err != nil {
			t.Fatal(err)
		}
		log := &restoreWarnings{}
		again := localizationRun(t, st, eps, nil, log)
		if got := dispatchRead(t, again, path); got != strconv.FormatUint(uint64(unitdef.TempUnitCelsius), 10) {
			t.Errorf("TemperatureUnit %s, want Celsius", got)
		}
		log.expectOneReset(t, key)
	})
}

// TestTimeFormatLocalizationStoredParityMatterJS ports the stored-value
// case of matter.js's describe "TimeFormatLocalizationServer"
// (packages/node/test/behaviors/time-format-localization/TimeFormatLocalizationServerTest.ts
// :241-260 at b25ed960): Buddhist stored, the list [Gregorian, Chinese],
// the start the detected Gregorian (TimeFormatLocalizationServer.ts:34-35).
// The case runs on the node's start without Config.TimeFormat, whose
// list is [Gregorian] (node/config.go TimeFormatConfig.core), so
// Buddhist is put into the store under the calendar's key: the slot of
// the first run's HourFormat write with the attribute id
// ActiveCalendarType, in nonVolatileKey's form ("%s.%04x",
// node/nonvolatile.go). :262-279, the list's first when neither the
// stored nor the detected calendar is listed, is
// TestTimeFormatConfigStoredParityMatterJS (node/time_format_config_test.go).
func TestTimeFormatLocalizationStoredParityMatterJS(t *testing.T) {
	t.Parallel()

	t.Run("starts with a supported calendar when the stored ActiveCalendarType is no longer supported", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		st, eps := &settingsRecorder{Port: memstore.New()}, epmem.New()
		first := localizationRun(t, st, eps, nil, nil)
		if got := dispatchWrite(t, first, attrPath(0, tfldef.ClusterID, tfldef.AttrHourFormat), uint64(tfldef.HourFormatV12Hr)); got != im.StatusSuccess {
			t.Fatalf("write of 12Hr: %v", got)
		}
		if err := first.Stop(ctx); err != nil {
			t.Fatal(err)
		}
		hourKey := st.keyEndingIn(t, fmt.Sprintf(".%04x.%04x", tfldef.ClusterID, tfldef.AttrHourFormat))
		key := strings.TrimSuffix(hourKey, fmt.Sprintf(".%04x", tfldef.AttrHourFormat)) + fmt.Sprintf(".%04x", tfldef.AttrActiveCalendarType)
		if err := st.SetSetting(ctx, key, storedEnum(t, uint64(tfldef.CalendarTypeBuddhist))); err != nil {
			t.Fatal(err)
		}
		log := &restoreWarnings{}
		again := localizationRun(t, st, eps, nil, log)
		if got := dispatchRead(t, again, attrPath(0, tfldef.ClusterID, tfldef.AttrActiveCalendarType)); got != strconv.FormatUint(uint64(tfldef.CalendarTypeGregorian), 10) {
			t.Errorf("ActiveCalendarType %s, want Gregorian", got)
		}
		// The stored HourFormat is restored as well: the restart is real.
		if got := dispatchRead(t, again, attrPath(0, tfldef.ClusterID, tfldef.AttrHourFormat)); got != strconv.FormatUint(uint64(tfldef.HourFormatV12Hr), 10) {
			t.Errorf("HourFormat %s, want the stored 12Hr", got)
		}
		log.expectOneReset(t, key)
	})
}
