// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"fmt"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/spec"
	pscfgdef "github.com/SukramJ/go-fabric/cluster/spec/powersourceconfiguration"
)

// PowerSourceConfigurationMaxSources is the most endpoints Sources lists:
// connectedhomeip's kMaxPowerSources
// (src/app/clusters/power-source-configuration-server/
// power-source-configuration-server.cpp:41 at the harness pin
// 6170af8461b10b1766044122ac83332c6d00ab20), the definition's "max 6".
const PowerSourceConfigurationMaxSources = 6

// PowerSourceConfiguration is a PowerSourceConfiguration (0x002E) server:
// the generated server whose one attribute, Sources, lists the endpoints
// of the node's PowerSource clusters in priority order.
//
// Mirrors matter.js packages/node/src/behaviors/power-source-configuration/
// PowerSourceConfigurationServer.ts:14, `export class
// PowerSourceConfigurationServer extends PowerSourceConfigurationBehavior
// {}`: no server logic. The list is the host's. connectedhomeip computes
// it on every read from the enabled endpoints that carry PowerSource,
// takes at most kMaxPowerSources of them and orders them by their
// PowerSource Order (power-source-configuration-server.cpp Read :65-90);
// a host here hands the ordered list over and the server keeps the first
// six, as chip's loop stops at six (:72-73).
type PowerSourceConfiguration struct {
	*spec.Server

	mu       sync.RWMutex
	provider func() []uint16
}

// NewPowerSourceConfiguration builds the server with Sources holding
// sources, the PowerSource endpoint ids in priority order (the first is
// the most preferred); entries past the sixth are dropped.
func NewPowerSourceConfiguration(sources []uint16) (*PowerSourceConfiguration, error) {
	p := &PowerSourceConfiguration{}
	srv, err := spec.NewServer(pscfgdef.Definition, spec.Options{}, spec.ServerConfig{
		Source: pscfgSource{p},
		Initial: map[uint32]any{
			pscfgdef.AttrSources: boundSources(sources),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("core: PowerSourceConfiguration: %w", err)
	}
	p.Server = srv
	return p, nil
}

// SetSourcesProvider makes Sources read fn on every read, in place of the
// list given at construction — the way connectedhomeip derives it from
// the endpoints at read time (:65-90), for a host whose PowerSource
// endpoints are numbered only once its topology is assembled. A nil fn
// restores the constructed list.
func (p *PowerSourceConfiguration) SetSourcesProvider(fn func() []uint16) {
	p.mu.Lock()
	p.provider = fn
	p.mu.Unlock()
}

// boundSources keeps the first [PowerSourceConfigurationMaxSources]
// entries of sources, as a fresh slice.
func boundSources(sources []uint16) []uint16 {
	if len(sources) > PowerSourceConfigurationMaxSources {
		sources = sources[:PowerSourceConfigurationMaxSources]
	}
	if sources == nil {
		return []uint16{}
	}
	return slices.Clone(sources)
}

// pscfgSource answers Sources from the provider when one is set.
type pscfgSource struct{ p *PowerSourceConfiguration }

// MatterAttribute implements [spec.Source].
func (s pscfgSource) MatterAttribute(attrID uint32) (any, bool) {
	if attrID != pscfgdef.AttrSources {
		return nil, false
	}
	s.p.mu.RLock()
	fn := s.p.provider
	s.p.mu.RUnlock()
	if fn == nil {
		return nil, false
	}
	return boundSources(fn()), true
}
