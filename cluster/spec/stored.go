// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

// Stored returns v, a value of an element of type t that the definition's
// checks admitted, in its stored form: the form [Server.MatterRead]
// answers and [WriteVeto.Check] hands the host's veto — the Go type of
// the element's width (uint8 to uint64, int8 to int64, float32 or
// float64), nil for null. The uint64, int64 or float64
// [Instance.ValidateWrite] returns and a value of a named scalar type (a
// generated enum or bitmap) are narrowed to it; a string, []byte, struct
// or list is returned as given. It is the conversion [Server.MatterWrite]
// applies before its veto, for a self-held server that runs its veto
// itself (R2-P6-api-1505).
func Stored(t Type, v any) any { return stored(t, underlying(v)) }
