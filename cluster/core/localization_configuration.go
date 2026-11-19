// SPDX-License-Identifier: Apache-2.0
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
)

// ErrLocalizationConfig rejects a localization configuration (of
// LocalizationConfiguration, TimeFormatLocalization or UnitLocalization)
// whose configured active value is not among its supported ones, that
// lists a temperature unit twice, or that names neither an active locale
// nor a supported one.
var ErrLocalizationConfig = errors.New("core: localization: active value not supported")

// LocalizationConfigurationConfig carries the construction parameters of
// a LocalizationConfiguration server.
type LocalizationConfigurationConfig struct {
	// ActiveLocale is the locale the node starts with: the host's
	// detected locale (matter.js takes Intl's), or the one it persisted
	// from an earlier write. Empty is nothing configured: the first of
	// SupportedLocales. A locale SupportedLocales does not hold is
	// refused.
	ActiveLocale string
	// SupportedLocales lists the locales a controller may choose; empty
	// (nil or zero length) is [ActiveLocale]. One of the two must be
	// given.
	SupportedLocales []string
	// OnWrite, when set, is handed a controller-written ActiveLocale that
	// passed the checks, before it is stored — the host persists it here.
	// An error refuses the write.
	OnWrite func(ctx context.Context, activeLocale string) error
	// DataVersion is an optional host-owned tracker.
	//
	// Deprecated: a host-owned tracker adds nothing — the wire carries the
	// endpoint's DataVersion for the cluster, or the server's own where it
	// serves one (decision 165, F-db021da0-46); leave it nil. Removable in
	// v0.25.0.
	DataVersion *cluster.DataVersionTracker
}

// NewLocalizationConfiguration builds a LocalizationConfiguration (0x002B)
// server on the generated definition (cluster/spec/
// localizationconfiguration, ADR 0013).
//
// Mirrors matter.js packages/node/src/behaviors/localization-configuration/
// LocalizationConfigurationServer.ts initialize (:16-33 at b25ed960): an
// empty or unset SupportedLocales is seeded with [ActiveLocale], the
// detected locale when none is set (:17-21); an ActiveLocale that is set
// and supported is kept (:22-25); otherwise the detected locale when the
// list holds it, else the list's first (:27-28, :32). The detected locale
// is the host's here — matter.js's detectedLocale getter is an override
// point (:35-38) — and arrives as ActiveLocale, so with ActiveLocale
// empty no detected locale is known and the start is the list's first.
// One rule departs, the same for the three localization servers
// (decisions 289, 296): matter.js replaces a set locale the list lacks,
// with a warning (:29-31); here a configured ActiveLocale is the host's
// explicit statement, and one the list lacks is refused with
// [ErrLocalizationConfig] (notes/parity/by_design.md
// BD-Matter-Localization-ConfiguredStartValueRefused).
//
// A written ActiveLocale that SupportedLocales does not hold is
// CONSTRAINT_ERROR: the attribute's model constraint is
// "in supportedLocales", which matter.js's validator enforces
// (packages/node/src/behavior/state/validation/constraint.ts:157-168 at
// b25ed960, packages/model/src/aspects/Constraint.ts test :164-170) and the
// generated runtime evaluates before the sink sees the value
// (spec.Instance.ValidateWrite); connectedhomeip answers the same
// (src/app/clusters/localization-configuration-server/
// LocalizationConfigurationCluster.cpp SetActiveLocale :123-128,
// IsSupportedLocale :172-187, at the harness pin
// 6170af8461b10b1766044122ac83332c6d00ab20).
func NewLocalizationConfiguration(cfg LocalizationConfigurationConfig) (*spec.Server, error) {
	active := cfg.ActiveLocale
	supported := slices.Clone(cfg.SupportedLocales)
	switch {
	case len(supported) == 0 && active == "":
		return nil, fmt.Errorf("%w: LocalizationConfiguration: neither ActiveLocale nor SupportedLocales", ErrLocalizationConfig)
	case len(supported) == 0:
		supported = []string{active}
	case active == "":
		active = supported[0]
	case !slices.Contains(supported, active):
		return nil, fmt.Errorf("%w: LocalizationConfiguration: ActiveLocale %q not in SupportedLocales %q", ErrLocalizationConfig, active, supported)
	}
	srv, err := spec.NewServer(lcfgdef.Definition, spec.Options{}, spec.ServerConfig{
		DataVersion: cfg.DataVersion, //nolint:staticcheck // the deprecated tracker is passed on for its window (decision 165)
		Sink:        localeSink{onWrite: cfg.OnWrite},
		Initial: map[uint32]any{
			lcfgdef.AttrActiveLocale:     active,
			lcfgdef.AttrSupportedLocales: supported,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("core: LocalizationConfiguration: %w", err)
	}
	return srv, nil
}

// localeSink hands a written ActiveLocale to the host. Membership in
// SupportedLocales is the runtime's "in" constraint, refused before the
// sink is called.
type localeSink struct {
	onWrite func(ctx context.Context, activeLocale string) error
}

// MatterWriteAttribute implements [spec.Sink]; ActiveLocale is the only
// writable attribute.
func (s localeSink) MatterWriteAttribute(ctx context.Context, _ uint32, value any) error {
	locale, _ := value.(string)
	if s.onWrite != nil {
		if err := s.onWrite(ctx, locale); err != nil {
			return fmt.Errorf("core: LocalizationConfiguration: %w", err)
		}
	}
	return nil
}
