// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spectest

import (
	"reflect"
	"testing"
)

// cloneDepth bounds how deep [CheckClone] fills a value: every list,
// pointer and struct field is one level. It reaches below the deepest
// generated struct, and stops a type that holds itself.
const cloneDepth = 12

// CheckClone holds the generated Clone of T to a deep copy (decision 142):
// a value filled to every depth from none to [cloneDepth] — every list two
// entries long, every optional field and pointer set, and at the cut each
// list nil, then again empty — clones to a value deeply equal to it, nil
// lists and pointers nil and empty lists empty, and overwriting every
// value reachable through the clone's lists and pointers leaves the
// original unchanged. A field of type any or a map is left nil.
func CheckClone[T interface{ Clone() T }](tb testing.TB) {
	tb.Helper()
	for cut := range cloneDepth + 1 {
		for _, empty := range []bool{false, true} {
			var orig, want T
			fill(reflect.ValueOf(&orig).Elem(), 1, 0, cut, empty)
			fill(reflect.ValueOf(&want).Elem(), 1, 0, cut, empty)
			c := orig.Clone()
			if !reflect.DeepEqual(c, orig) {
				tb.Errorf("%T filled to depth %d (empty lists %t): Clone = %+v, want %+v", orig, cut, empty, c, orig)
				return
			}
			overwrite(reflect.ValueOf(&c).Elem())
			if !reflect.DeepEqual(orig, want) {
				tb.Errorf("%T filled to depth %d (empty lists %t): overwriting the clone reached the original", orig, cut, empty)
				return
			}
		}
	}
}

// fill gives every value reachable from v above depth cut the non-zero
// value seed: two entries per list, a pointer to a filled value for every
// pointer. At the cut a list is nil, or empty when empty is set, and a
// pointer nil.
func fill(v reflect.Value, seed int64, depth, cut int, empty bool) {
	if depth >= cut {
		if empty && v.Kind() == reflect.Slice {
			v.Set(reflect.MakeSlice(v.Type(), 0, 0))
		}
		if v.Kind() != reflect.Struct {
			return
		}
	}
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(seed%2 == 1)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(seed)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(uint64(seed)) //nolint:gosec // a small seed
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(seed))
	case reflect.String:
		v.SetString(string(rune('a' + seed%26)))
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 2, 2)
		for i := range 2 {
			fill(s.Index(i), seed, depth+1, cut, empty)
		}
		v.Set(s)
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		fill(p.Elem(), seed, depth+1, cut, empty)
		v.Set(p)
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				fill(v.Field(i), seed, depth+1, cut, empty)
			}
		}
	default:
	}
}

// overwrite sets every scalar reachable from v — through the lists and
// pointers v holds, not replacing them — to a value fill's seed 1 never
// gives. It reaches as deep as fill went, no deeper.
func overwrite(v reflect.Value) {
	switch v.Kind() {
	case reflect.Slice:
		for i := range v.Len() {
			overwrite(v.Index(i))
		}
	case reflect.Pointer:
		if !v.IsNil() {
			overwrite(v.Elem())
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				overwrite(v.Field(i))
			}
		}
	default:
		fill(v, 2, 0, 1, false)
	}
}
