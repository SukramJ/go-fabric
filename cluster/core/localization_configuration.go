// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lcfgdef "github.com/SukramJ/go-fabric/cluster/spec/localizationconfiguration"
	"github.com/SukramJ/go-fabric/im"
)

// ErrLocalizationConfig rejects a localization configuration (of
// LocalizationConfiguration, TimeFormatLocalization or UnitLocalization)
// whose active value is not among its supported ones, or that names no
// active locale.
var ErrLocalizationConfig = errors.New("core: localization: active value not supported")

// LocalizationConfigurationConfig carries the construction parameters of
// a LocalizationConfiguration server.
type LocalizationConfigurationConfig struct {
	// ActiveLocale is the locale the node starts with: the host's
	// detected locale (matter.js takes Intl's), or the one it persisted
	// from an earlier write. Required.
	ActiveLocale string
	// SupportedLocales lists the locales a controller may choose; nil is
	// [ActiveLocale]. It must hold ActiveLocale.
	SupportedLocales []string
	// OnWrite, when set, is handed a controller-written ActiveLocale that
	// passed the checks, before it is stored — the host persists it here.
	// An error refuses the write.
	OnWrite func(ctx context.Context, activeLocale string) error
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// NewLocalizationConfiguration builds a LocalizationConfiguration (0x002B)
// server on the generated definition (cluster/spec/
// localizationconfiguration, ADR 0013).
//
// Mirrors matter.js packages/node/src/behaviors/localization-configuration/
// LocalizationConfigurationServer.ts initialize (:14-21): ActiveLocale
// defaults to the detected locale — the host's here, as matter.js's
// detectedLocale getter is an override point (:23-26) — and
// SupportedLocales to [ActiveLocale] when unset.
//
// A written ActiveLocale that SupportedLocales does not hold is
// CONSTRAINT_ERROR: the attribute's model constraint is
// "in supportedLocales", which matter.js's validator enforces
// (packages/node/src/behavior/state/validation/constraint.ts:153-163,
// packages/model/src/aspects/Constraint.ts test :164-170) and the
// generated runtime does not evaluate; connectedhomeip answers the same
// (src/app/clusters/localization-configuration-server/
// LocalizationConfigurationCluster.cpp SetActiveLocale :123-128,
// IsSupportedLocale :172-187, at the harness pin
// 6170af8461b10b1766044122ac83332c6d00ab20).
func NewLocalizationConfiguration(cfg LocalizationConfigurationConfig) (*spec.Server, error) {
	if cfg.ActiveLocale == "" {
		return nil, fmt.Errorf("%w: LocalizationConfiguration: no ActiveLocale", ErrLocalizationConfig)
	}
	supported := slices.Clone(cfg.SupportedLocales)
	if supported == nil {
		supported = []string{cfg.ActiveLocale}
	}
	if !slices.Contains(supported, cfg.ActiveLocale) {
		return nil, fmt.Errorf("%w: LocalizationConfiguration: ActiveLocale %q not in SupportedLocales %q", ErrLocalizationConfig, cfg.ActiveLocale, supported)
	}
	srv, err := spec.NewServer(lcfgdef.Definition, spec.Options{}, spec.ServerConfig{
		DataVersion: cfg.DataVersion,
		Sink:        localeSink{supported: supported, onWrite: cfg.OnWrite},
		Initial: map[uint32]any{
			lcfgdef.AttrActiveLocale:     cfg.ActiveLocale,
			lcfgdef.AttrSupportedLocales: supported,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("core: LocalizationConfiguration: %w", err)
	}
	return srv, nil
}

// localeSink holds a written ActiveLocale to SupportedLocales, which is
// fixed, and hands it to the host.
type localeSink struct {
	supported []string
	onWrite   func(ctx context.Context, activeLocale string) error
}

// MatterWriteAttribute implements [spec.Sink]; ActiveLocale is the only
// writable attribute.
func (s localeSink) MatterWriteAttribute(ctx context.Context, _ uint32, value any) error {
	locale, _ := value.(string)
	if !slices.Contains(s.supported, locale) {
		return spec.Errorf(im.StatusConstraintError, "LocalizationConfiguration: ActiveLocale %q is not in SupportedLocales", locale)
	}
	if s.onWrite != nil {
		if err := s.onWrite(ctx, locale); err != nil {
			return fmt.Errorf("core: LocalizationConfiguration: %w", err)
		}
	}
	return nil
}
