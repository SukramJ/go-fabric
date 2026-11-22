// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/device"
)

// writeForm is a value MatterRead answered in the form a controller's
// decoded write carries it; false for a value no scalar write carries.
func writeForm(v any) (any, bool) {
	if v == nil {
		return nil, true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return r.Uint(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return r.Int(), true
	case reflect.Bool:
		return r.Bool(), true
	case reflect.String:
		return r.String(), true
	default:
		return nil, false
	}
}

// TestHeldValueWriteReachesNoVetoInTheKit (R2-P5-apispec-02, coordinator
// round 2 Q2b): every server the device kit builds with its defaults that
// takes a host veto (spec.BeforeWriteSetter) answers a controller's write
// of the value it holds without calling the veto, for every scalar
// writable attribute it serves — the veto sits behind the held-value
// comparison on the generated and on every self-held server.
func TestHeldValueWriteReachesNoVetoInTheKit(t *testing.T) {
	t.Parallel()
	checked, built := 0, 0
	for _, ty := range device.Types() {
		dev, err := device.New(ty, device.Options{StableKey: device.StringKey("veto:" + ty.Name), Name: ty.Name, Logger: slog.New(slog.DiscardHandler)}, device.InitialFor(ty.Name)...)
		if err != nil {
			continue // a type whose defaults need a feature selection or a port
		}
		built++
		for _, srv := range dev.Servers() {
			setter, ok := srv.(spec.BeforeWriteSetter)
			def := spec.Lookup(srv.MatterClusterID())
			lister, lists := srv.(contract.ClusterAttributeLister)
			if !ok || def == nil || !lists {
				continue
			}
			for _, id := range lister.MatterAttributes() {
				a := def.Attribute(id)
				if a == nil || !a.Writable() || a.Access.Fabric == "F" {
					continue
				}
				cur, ok := srv.MatterRead(id)
				if !ok {
					continue
				}
				w, ok := writeForm(cur)
				if !ok {
					continue
				}
				calls := 0
				setter.SetBeforeWrite(func(context.Context, uint32, any) error { calls++; return errors.New("veto") })
				_ = srv.MatterWrite(context.Background(), id, w)
				setter.SetBeforeWrite(nil)
				checked++
				if calls != 0 {
					t.Errorf("%s: %s.%s: a write of the held value %v called the host veto", ty.Name, def.Name, a.Name, cur)
				}
			}
		}
	}
	if built < 60 || checked < 20 {
		t.Fatalf("the walk broke: %d device types built, %d held writes checked", built, checked)
	}
	t.Logf("%d held writes checked over %d device types", checked, built)
}
