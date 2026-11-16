// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec/accesscontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/contentlauncher"
	"github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	eem "github.com/SukramJ/go-fabric/cluster/spec/electricalenergymeasurement"
	"github.com/SukramJ/go-fabric/cluster/spec/zonemanagement"
)

// populate gives every field reachable from v a non-zero value, the same
// for every call with the same seed: two entries per list, a pointer to a
// populated value for every pointer.
func populate(v reflect.Value, seed int64, depth int) {
	if depth > 6 {
		return
	}
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(seed%2 == 1)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(seed)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(uint64(seed)) //nolint:gosec // a small test seed
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(seed))
	case reflect.String:
		v.SetString(string(rune('a' + seed%26)))
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 2, 2)
		for i := range 2 {
			populate(s.Index(i), seed, depth+1)
		}
		v.Set(s)
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		populate(p.Elem(), seed, depth+1)
		v.Set(p)
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				populate(v.Field(i), seed, depth+1)
			}
		}
	default:
	}
}

// TestGeneratedCloneIsDeep holds a generated struct's Clone to a deep copy
// (decision 142): every list, octet string, optional field, nullable list
// and nested struct of the clone is its own — overwriting every reachable
// value of the clone leaves the original as it was — and a nil list stays
// nil while an empty one stays empty. The types cover a list of structs
// with an optional list inside (ContentLauncher), nullable lists
// (AccessControl, DoorLock), an optional nested struct (ZoneManagement)
// and optional scalars (ElectricalEnergyMeasurement).
func TestGeneratedCloneIsDeep(t *testing.T) {
	t.Parallel()
	for _, typ := range []reflect.Type{
		reflect.TypeFor[contentlauncher.ContentSearchStruct](),
		reflect.TypeFor[accesscontrol.AccessControlEntryStruct](),
		reflect.TypeFor[doorlock.GetUserResponse](),
		reflect.TypeFor[zonemanagement.ZoneInformationStruct](),
		reflect.TypeFor[zonemanagement.TwoDCartesianZoneStruct](),
		reflect.TypeFor[eem.EnergyMeasurementStruct](),
	} {
		orig := reflect.New(typ).Elem()
		populate(orig, 1, 0)
		want := reflect.New(typ).Elem()
		populate(want, 1, 0)
		clone := orig.MethodByName("Clone").Call(nil)[0]
		if !reflect.DeepEqual(clone.Interface(), orig.Interface()) {
			t.Errorf("%s: Clone differs from the original", typ)
		}
		mutable := reflect.New(typ).Elem()
		mutable.Set(clone)
		overwrite(mutable, 0)
		if !reflect.DeepEqual(orig.Interface(), want.Interface()) {
			t.Errorf("%s: overwriting the clone reached the original", typ)
		}
	}

	empty := zonemanagement.TwoDCartesianZoneStruct{Vertices: []zonemanagement.TwoDCartesianVertexStruct{}}
	if c := empty.Clone(); c.Vertices == nil || len(c.Vertices) != 0 {
		t.Errorf("an empty list cloned to %#v, want an empty list", c.Vertices)
	}
	if c := (zonemanagement.TwoDCartesianZoneStruct{}).Clone(); c.Vertices != nil || c.Color != nil {
		t.Errorf("a nil list or pointer cloned to %#v, %v; want nil", c.Vertices, c.Color)
	}
}

// overwrite sets every value reachable from v — through the slices and
// pointers v holds, not replacing them — to another non-zero value.
func overwrite(v reflect.Value, depth int) {
	if depth > 6 {
		return
	}
	switch v.Kind() {
	case reflect.Slice:
		for i := range v.Len() {
			overwrite(v.Index(i), depth+1)
		}
	case reflect.Pointer:
		if !v.IsNil() {
			overwrite(v.Elem(), depth+1)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				overwrite(v.Field(i), depth+1)
			}
		}
	default:
		if v.CanSet() {
			populate(v, 9, depth)
		}
	}
}
