// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/localization-configuration/
// LocalizationConfigurationServerTest.ts at b25ed960, by meaning.
// matter.js runs EnglishServer, whose detected locale is "en-US" (:16-21),
// sets its state on the root (rootWith :23-25) and writes as a controller
// (writeActiveLocale :27-39). The Go server has no detected locale of its
// own: the host detects it and passes it as ActiveLocale, so a matter.js
// start with only the detected locale is a Go start with ActiveLocale
// "en-US", and a start that knows no locale but the list is one with
// ActiveLocale empty. A write is a MatterWrite on the built server, the
// path the generated runtime runs for a controller's write; a restart is
// the host passing back the locale OnWrite handed it.
package core_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lcfgdef "github.com/SukramJ/go-fabric/cluster/spec/localizationconfiguration"
	"github.com/SukramJ/go-fabric/im"
)

// TestLocalizationConfigurationServerParityMatterJS ports the describe
// "LocalizationConfigurationServer"
// (LocalizationConfigurationServerTest.ts:52-201) where it has a Go
// subject. Left: the replacement of a configured or stored locale the
// list lacks (:101-110, :152-169, :171-186) and the warning that goes
// with it (:112-118) — the module refuses a configured contradictory
// locale (BD-Matter-Localization-ConfiguredStartValueRefused); the
// detected locale overridden by a getter (:188-200), a TypeScript class
// mechanism.
func TestLocalizationConfigurationServerParityMatterJS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	build := func(t *testing.T, cfg core.LocalizationConfigurationConfig) *spec.Server {
		t.Helper()
		srv, err := core.NewLocalizationConfiguration(cfg)
		if err != nil {
			t.Fatal(err)
		}
		return srv
	}
	check := func(t *testing.T, srv *spec.Server, active string, supported []string) {
		t.Helper()
		if v, _ := srv.MatterRead(lcfgdef.AttrActiveLocale); v != active {
			t.Errorf("ActiveLocale %v, want %q", v, active)
		}
		if supported == nil {
			return
		}
		if v, _ := srv.MatterRead(lcfgdef.AttrSupportedLocales); !slices.Equal(v.([]string), supported) {
			t.Errorf("SupportedLocales %v, want %q", v, supported)
		}
	}

	// :53-58.
	t.Run("defaults to the detected locale and lists it as the only supported locale", func(t *testing.T) {
		t.Parallel()
		check(t, build(t, core.LocalizationConfigurationConfig{ActiveLocale: "en-US"}), "en-US", []string{"en-US"})
	})

	// :60-65.
	t.Run("treats an explicitly undefined SupportedLocales as unset", func(t *testing.T) {
		t.Parallel()
		check(t, build(t, core.LocalizationConfigurationConfig{ActiveLocale: "en-US", SupportedLocales: nil}), "en-US", []string{"en-US"})
	})

	// :77-83.
	t.Run("accepts a controller write of a locale in SupportedLocales", func(t *testing.T) {
		t.Parallel()
		srv := build(t, core.LocalizationConfigurationConfig{ActiveLocale: "en-US", SupportedLocales: []string{"en-US", "de-DE"}})
		if err := srv.MatterWrite(ctx, lcfgdef.AttrActiveLocale, "de-DE"); statusOf(err) != im.StatusSuccess {
			t.Fatalf("write of de-DE: %v, want SUCCESS", err)
		}
		check(t, srv, "de-DE", nil)
	})

	// :85-92. matter.js names the failed constraint "max 35"; the Go
	// constructor refuses the list as a value outside its constraint.
	t.Run("refuses a SupportedLocales entry longer than 35 characters", func(t *testing.T) {
		t.Parallel()
		_, err := core.NewLocalizationConfiguration(core.LocalizationConfigurationConfig{
			ActiveLocale: "en-US", SupportedLocales: []string{"en-US", strings.Repeat("x", 36)},
		})
		if !errors.Is(err, spec.ErrInvalidValue) {
			t.Fatalf("a 36-character locale: %v, want spec.ErrInvalidValue", err)
		}
	})

	// :94-99.
	t.Run("builds SupportedLocales from a configured ActiveLocale", func(t *testing.T) {
		t.Parallel()
		check(t, build(t, core.LocalizationConfigurationConfig{ActiveLocale: "de-DE"}), "de-DE", []string{"de-DE"})
	})

	// :120-127. The detected locale "en-US" is not listed, so matter.js
	// starts at the first; a Go host that knows no locale but the list
	// leaves ActiveLocale empty. Refused with ErrLocalizationConfig before
	// the hybrid start (decision 296).
	t.Run("starts with a supported locale when the detected locale is not in the configured SupportedLocales", func(t *testing.T) {
		t.Parallel()
		check(t, build(t, core.LocalizationConfigurationConfig{SupportedLocales: []string{"de-DE", "fr-FR"}}), "de-DE", []string{"de-DE", "fr-FR"})
	})

	// :129-133.
	t.Run("prefers the detected locale when it is in the configured SupportedLocales", func(t *testing.T) {
		t.Parallel()
		check(t, build(t, core.LocalizationConfigurationConfig{ActiveLocale: "en-US", SupportedLocales: []string{"de-DE", "en-US"}}), "en-US", nil)
	})

	// :135-150.
	t.Run("keeps a written ActiveLocale across a restart", func(t *testing.T) {
		t.Parallel()
		var stored string
		first := build(t, core.LocalizationConfigurationConfig{
			ActiveLocale: "en-US", SupportedLocales: []string{"en-US", "de-DE"},
			OnWrite: func(_ context.Context, l string) error { stored = l; return nil },
		})
		if err := first.MatterWrite(ctx, lcfgdef.AttrActiveLocale, "de-DE"); statusOf(err) != im.StatusSuccess {
			t.Fatalf("write of de-DE: %v, want SUCCESS", err)
		}
		second := build(t, core.LocalizationConfigurationConfig{ActiveLocale: stored, SupportedLocales: []string{"en-US", "de-DE"}})
		check(t, second, "de-DE", nil)
	})

	// Go's own: an empty, non-nil list is no configuration, as matter.js's
	// `!supportedLocales?.length` (LocalizationConfigurationServer.ts:17
	// at b25ed960). Refused with ErrLocalizationConfig before (decision 296).
	t.Run("seeds an empty SupportedLocales with the ActiveLocale", func(t *testing.T) {
		t.Parallel()
		check(t, build(t, core.LocalizationConfigurationConfig{ActiveLocale: "de-DE", SupportedLocales: []string{}}), "de-DE", []string{"de-DE"})
	})

	// Go's own: with neither a locale nor a list there is nothing to start
	// at — matter.js would take Intl's locale (:35-38), the host's part here.
	t.Run("refuses a configuration with neither ActiveLocale nor SupportedLocales", func(t *testing.T) {
		t.Parallel()
		for _, supported := range [][]string{nil, {}} {
			if _, err := core.NewLocalizationConfiguration(core.LocalizationConfigurationConfig{SupportedLocales: supported}); !errors.Is(err, core.ErrLocalizationConfig) {
				t.Errorf("SupportedLocales %#v: %v, want ErrLocalizationConfig", supported, err)
			}
		}
	})

	// Go's own (BD-Matter-Localization-ConfiguredStartValueRefused): a
	// configured locale the configured list lacks is refused, where
	// matter.js replaces it (:101-110).
	t.Run("refuses a configured ActiveLocale that is not in the configured SupportedLocales", func(t *testing.T) {
		t.Parallel()
		_, err := core.NewLocalizationConfiguration(core.LocalizationConfigurationConfig{ActiveLocale: "fr-FR", SupportedLocales: []string{"de-DE", "en-US"}})
		if !errors.Is(err, core.ErrLocalizationConfig) {
			t.Fatalf("fr-FR of [de-DE en-US]: %v, want ErrLocalizationConfig", err)
		}
	})
}
