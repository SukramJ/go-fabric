// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package surface

import (
	"encoding/hex"
	"fmt"
	"time"
)

// SurfaceChange is one announced change of an endpoint's served surface
// (ADR 0030 decision 6, concept v11 §C.9): the endpoint with key Endpoint
// came back — after a restart, a reassembly or a replacement — with an
// effective surface whose [Endpoint.Digest] is After where the one last
// persisted for it was Before, and its node raised the ConfigurationVersion
// at At. Changes is the [Diff] of the two, empty when the earlier surface
// itself was not available (only its digest was).
type SurfaceChange struct { //nolint:revive // the name brief S4 gives; Change is taken by the element-level difference
	Endpoint      string
	Before, After [32]byte
	Changes       []Change
	At            time.Time
}

// The wire forms of an announced change, fields in the order of their
// JSON keys.
type (
	wireSurfaceChange struct {
		After    string       `json:"after"`
		At       time.Time    `json:"at"`
		Before   string       `json:"before"`
		Changes  []wireChange `json:"changes"`
		Endpoint string       `json:"endpoint"`
	}
	wireChange struct {
		After    string `json:"after"`
		Before   string `json:"before"`
		Cluster  id32   `json:"cluster"`
		Endpoint string `json:"endpoint"`
		ID       kindID `json:"id"`
		Kind     Kind   `json:"kind"`
	}
)

func (c SurfaceChange) wire() wireSurfaceChange {
	w := wireSurfaceChange{
		Endpoint: c.Endpoint, At: c.At,
		Before: hex.EncodeToString(c.Before[:]), After: hex.EncodeToString(c.After[:]),
		Changes: make([]wireChange, 0, len(c.Changes)),
	}
	for _, ch := range c.Changes {
		w.Changes = append(w.Changes, wireChange{
			Endpoint: ch.Endpoint, Cluster: id32(ch.Cluster), Kind: ch.Kind, ID: idOf(ch.Kind, ch.ID),
			Before: ch.Before, After: ch.After,
		})
	}
	return w
}

func (w wireSurfaceChange) value() (SurfaceChange, error) {
	c := SurfaceChange{Endpoint: w.Endpoint, At: w.At}
	for _, d := range []struct {
		dst *[32]byte
		src string
	}{{&c.Before, w.Before}, {&c.After, w.After}} {
		b, err := hex.DecodeString(d.src)
		if err != nil || len(b) != len(d.dst) {
			return SurfaceChange{}, fmt.Errorf("surface: change of %q: digest %q is no SHA-256 digest", w.Endpoint, d.src)
		}
		copy(d.dst[:], b)
	}
	for _, ch := range w.Changes {
		c.Changes = append(c.Changes, Change{
			Endpoint: ch.Endpoint, Cluster: uint32(ch.Cluster), Kind: ch.Kind, ID: ch.ID.v,
			Before: ch.Before, After: ch.After,
		})
	}
	return c, nil
}
