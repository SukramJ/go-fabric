// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"fmt"
	"log/slog"
	"sort"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/device"
)

// quieterExceptions are the servers of the kit that serve a quieter ("Q")
// attribute and tell their change listeners, but are no report notifier
// or no quiescer, by Go type, with the reason each may. The list is exact.
var quieterExceptions = map[string]string{}

// TestQuieterServersReportAndQuiesce guards the throttled report of a
// quieter attribute (R2-P5-apispec-03, F-db021da0-22): every server the
// device kit builds with its defaults that serves a Q attribute of its
// cluster's definition and is a contract.AttributeChangeNotifier is also a
// contract.AttributeReportNotifier — so the bridge reports the attribute
// through its throttle, not at every change — and a
// contract.ClusterQuiescer, so the throttle's deferred report ends with
// the server. Each type is built with the values New requires of it
// (device.InitialFor, decision 17); the walk moved here from
// script/quiescecheck for them.
func TestQuieterServersReportAndQuiesce(t *testing.T) {
	t.Parallel()
	seen := map[string]string{}
	built := 0
	for _, ty := range device.Types() {
		dev, err := device.New(ty, device.Options{StableKey: device.StringKey("q:" + ty.Name), Name: ty.Name, Logger: slog.New(slog.DiscardHandler)}, device.InitialFor(ty.Name)...)
		if err != nil {
			continue // a type whose defaults need a feature selection or a port
		}
		built++
		for _, srv := range dev.Servers() {
			def := spec.Lookup(srv.MatterClusterID())
			if def == nil {
				continue
			}
			quiet := ""
			if l, ok := srv.(contract.ClusterAttributeLister); ok {
				for _, id := range l.MatterAttributes() {
					if a := def.Attribute(id); a != nil && a.Quality.Quieter {
						quiet = a.Name
						break
					}
				}
			}
			if quiet == "" {
				continue
			}
			if _, ok := srv.(contract.AttributeChangeNotifier); !ok {
				continue
			}
			_, reports := srv.(contract.AttributeReportNotifier)
			_, quiesces := srv.(contract.ClusterQuiescer)
			if reports && quiesces {
				continue
			}
			typ := fmt.Sprintf("%T", srv)
			seen[typ] = fmt.Sprintf("%s serves %s.%s; report notifier %v, quiescer %v", ty.Name, def.Name, quiet, reports, quiesces)
		}
	}
	if built < 60 {
		t.Fatalf("only %d device types build with their defaults: the walk broke", built)
	}
	names := make([]string, 0, len(seen))
	for typ := range seen {
		names = append(names, typ)
	}
	sort.Strings(names)
	for _, typ := range names {
		if _, ok := quieterExceptions[typ]; !ok {
			t.Errorf("%s: %s — implement contract.AttributeReportNotifier and contract.ClusterQuiescer (forward them from the spec.Server it keeps)", typ, seen[typ])
		}
	}
	for typ := range quieterExceptions {
		if _, ok := seen[typ]; !ok {
			t.Errorf("exception %s no longer applies: delete the entry", typ)
		}
	}
}
