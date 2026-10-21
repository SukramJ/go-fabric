// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package media

import (
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	woldef "github.com/SukramJ/go-fabric/cluster/spec/wakeonlan"
)

// WakeOnLanState is the WakeOnLan attribute state (wake-on-lan.element.ts:
// 15-22), the cluster's generated State; both attributes are optional and
// served when the configuration lists them: MacAddress, the 48-bit MAC as
// 12 hex digits (max 12), and LinkLocalAddress, the 16-byte IPv6
// link-local address.
type WakeOnLanState = woldef.State

func wakeOnLanValues(st WakeOnLanState) map[uint32]any {
	return map[uint32]any{
		woldef.AttrMacAddress:       st.MacAddress,
		woldef.AttrLinkLocalAddress: st.LinkLocalAddress,
	}
}

func cloneWakeOnLan(st WakeOnLanState) WakeOnLanState {
	st.LinkLocalAddress = slices.Clone(st.LinkLocalAddress)
	return st
}

// WakeOnLanConfig carries the construction parameters of a WakeOnLan
// server; the cluster has no commands.
type WakeOnLanConfig struct {
	// Attributes lists the optional attributes served: AttrMacAddress,
	// AttrLinkLocalAddress.
	Attributes []uint32
	// Initial is the state the server starts with.
	Initial WakeOnLanState
	// DataVersion is an optional host-owned tracker.
	DataVersion dataVersion
}

// WakeOnLanServer is a WakeOnLan (0x0503) server.
type WakeOnLanServer struct {
	*spec.Server
	stateful[WakeOnLanState]
}

// NewWakeOnLan builds a WakeOnLan server.
func NewWakeOnLan(cfg WakeOnLanConfig) (*WakeOnLanServer, error) {
	srv, err := spec.NewServer(woldef.Definition, spec.Options{Attributes: cfg.Attributes},
		spec.ServerConfig{DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("media: WakeOnLan: %w", err)
	}
	s := &WakeOnLanServer{Server: srv}
	if err := s.init(srv, cfg.Initial, wakeOnLanValues, cloneWakeOnLan, storeState[WakeOnLanState]); err != nil {
		return nil, fmt.Errorf("media: WakeOnLan: %w", err)
	}
	return s, nil
}

// SetAttributes implements [spec.AttributeSetter] ([stateful.setAttributes]).
func (s *WakeOnLanServer) SetAttributes(values map[uint32]any) error { return s.setAttributes(values) }
