// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphome

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
)

// staticBrowser answers every browse with the same instances.
type staticBrowser struct{ out []discovery.Instance }

func (b staticBrowser) Browse(context.Context, string) ([]discovery.Instance, error) {
	return b.out, nil
}

// TestDegenerateInstances (R2-P6-err-2006) is the guard of the class "an
// instance another Browser returns that discovery.Names never does": each
// case is skipped by Discover and by a central's discovery — no panic, no
// dial — and dialFirst over no address is an error, not a nil connection.
func TestDegenerateInstances(t *testing.T) {
	for name, in := range map[string]discovery.Instance{
		"no address":      {Name: "noaddr", QName: "noaddr._esphomelib._tcp.local"},
		"invalid address": {Name: "zero", QName: "zero._esphomelib._tcp.local", Addresses: []discovery.Address{{}}},
	} {
		t.Run(name, func(t *testing.T) {
			dials := 0
			o := Options{
				Dial: func(context.Context, string, string) (net.Conn, error) {
					dials++
					return nil, errors.New("no dial expected")
				},
				Discover: &DiscoverOptions{Browser: staticBrowser{out: []discovery.Instance{in}}, AllowPlaintext: true, Timeout: time.Millisecond},
			}
			ps, err := Discover(t.Context(), o)
			if err != nil || len(ps) != 0 {
				t.Fatalf("Discover = %v, %v; want nothing", ps, err)
			}
			if _, err := NewCentral(nil, o); !errors.Is(err, ErrNoProxies) {
				t.Fatalf("NewCentral = %v, want ErrNoProxies", err)
			}
			if dials != 0 {
				t.Fatalf("%d dials of an instance without an address", dials)
			}
		})
	}
	o := Options{}
	o.normalize()
	if a, _, err := dialFirst(t.Context(), nil, nil, &o); a != nil || err == nil {
		t.Fatalf("dialFirst(no addresses) = %v, %v; want an error", a, err)
	}
}
