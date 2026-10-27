// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otbr

import (
	"encoding/json"
	"log/slog"
	"math"
	"regexp"
	"strconv"

	"github.com/SukramJ/go-fabric/thread"
)

// The OTBR /diagnostics record translated into a
// [thread.DiagnosticResponse], after matter.js
// packages/thread-br-client/src/otbr-rest/translation.ts at 862ad75f — the
// records the ThreadNetworkDiagnostics server reads: the extended address,
// RLOC16, mode, Route64, leader data, MAC and MLE counters, child table and
// Thread version. Connectivity, network data, the IPv6 addresses, the
// channel pages and the vendor strings matter.js also translates are left
// out; nothing the module serves reads them.

// timeoutExpMin is the child timeout exponent's floor
// (translation.ts:22).
const timeoutExpMin = 4

// requireUint is a record's key as an integer within [0, max]
// (requireNumber, translation.ts:60-64, narrowed to the Go field).
func requireUint(rec map[string]any, key, where string, maxValue uint64) (uint64, error) {
	n, ok := asUint(rec[key], maxValue)
	if !ok {
		return 0, protocolError("%s: missing numeric %s", where, key)
	}
	return n, nil
}

var decimalDigits = regexp.MustCompile(`^\d+$`)

// requireUint64 is requireBigInt (translation.ts:140-160): a 64-bit
// counter as a JSON number or, beyond 2^53, a decimal string, in uint64
// range.
func requireUint64(rec map[string]any, key, where string) (uint64, error) {
	switch v := rec[key].(type) {
	case string:
		if decimalDigits.MatchString(v) {
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return 0, protocolError("%s: %s outside uint64 range", where, key)
			}
			return n, nil
		}
	case json.Number:
		if n, err := strconv.ParseUint(v.String(), 10, 64); err == nil {
			return n, nil
		}
		f, ok := asNumber(v)
		if !ok {
			break
		}
		f = math.Trunc(f)
		if f < 0 || f >= math.MaxUint64 {
			return 0, protocolError("%s: %s outside uint64 range", where, key)
		}
		return uint64(f), nil
	}
	return 0, protocolError("%s: missing numeric %s", where, key)
}

// fields collects a record's first error.
type fields struct {
	rec   map[string]any
	where string
	err   error
}

func (f *fields) uint(key string, maxValue uint64) uint64 {
	n, err := requireUint(f.rec, key, f.where, maxValue)
	if f.err == nil {
		f.err = err
	}
	return n
}

func (f *fields) uint64(key string) uint64 {
	n, err := requireUint64(f.rec, key, f.where)
	if f.err == nil {
		f.err = err
	}
	return n
}

// translateMode is translateMode (translation.ts:66-72): a flag is set
// when its number is not 0.
func translateMode(rec map[string]any) (thread.Mode, error) {
	f := &fields{rec: rec, where: "mode"}
	m := thread.Mode{
		RxOnWhenIdle:     f.uint("rxOnWhenIdle", math.MaxUint32) != 0,
		FullThreadDevice: f.uint("deviceType", math.MaxUint32) != 0,
		FullNetworkData:  f.uint("networkData", math.MaxUint32) != 0,
	}
	return m, f.err
}

// translateRoute is translateRoute (translation.ts:94-114).
func translateRoute(rec map[string]any) (thread.Route64, error) {
	f := &fields{rec: rec, where: "route"}
	r := thread.Route64{IDSequence: uint8(f.uint("idSequence", math.MaxUint8))} //nolint:gosec // bounded
	data, ok := rec["routeData"].([]any)
	if !ok {
		return thread.Route64{}, protocolError("route: missing routeData array")
	}
	for _, raw := range data {
		e, ok := raw.(map[string]any)
		if !ok {
			return thread.Route64{}, protocolError("route.routeData: entry is not an object")
		}
		g := &fields{rec: e, where: "route.routeData"}
		r.Entries = append(r.Entries, thread.Route64Entry{
			RouterID:       uint8(g.uint("routeId", math.MaxUint8)),        //nolint:gosec // bounded
			LinkQualityOut: uint8(g.uint("linkQualityOut", math.MaxUint8)), //nolint:gosec // bounded
			LinkQualityIn:  uint8(g.uint("linkQualityIn", math.MaxUint8)),  //nolint:gosec // bounded
			RouteCost:      uint8(g.uint("routeCost", math.MaxUint8)),      //nolint:gosec // bounded
		})
		if g.err != nil {
			return thread.Route64{}, g.err
		}
	}
	return r, f.err
}

// translateMacCounters is translateMacCounters (translation.ts:126-138).
func translateMacCounters(rec map[string]any) (thread.MacCounters, error) {
	f := &fields{rec: rec, where: "macCounters"}
	u := func(key string) uint32 { return uint32(f.uint(key, math.MaxUint32)) } //nolint:gosec // bounded
	c := thread.MacCounters{
		IfInUnknownProtos: u("ifInUnknownProtos"), IfInErrors: u("ifInErrors"), IfOutErrors: u("ifOutErrors"),
		IfInUcastPkts: u("ifInUcastPkts"), IfInBroadcastPkts: u("ifInBroadcastPkts"), IfInDiscards: u("ifInDiscards"),
		IfOutUcastPkts: u("ifOutUcastPkts"), IfOutBroadcastPkts: u("ifOutBroadcastPkts"), IfOutDiscards: u("ifOutDiscards"),
	}
	return c, f.err
}

// translateMleCounters is translateMleCounters (translation.ts:162-183):
// the OTBR names MLE counters differently from the record's fields
// (radioDisabledCount is disabledRole, totalTrackingTime trackedTime, …).
func translateMleCounters(rec map[string]any) (thread.MleCounters, error) {
	f := &fields{rec: rec, where: "mleCounters"}
	u := func(key string) uint16 { return uint16(f.uint(key, math.MaxUint16)) } //nolint:gosec // bounded
	c := thread.MleCounters{
		DisabledRole:                  u("radioDisabledCount"),
		DetachedRole:                  u("detachedRoleCount"),
		ChildRole:                     u("childRoleCount"),
		RouterRole:                    u("routerRoleCount"),
		LeaderRole:                    u("leaderRoleCount"),
		AttachAttempts:                u("attachAttemptsCount"),
		PartitionIDChanges:            u("partIdChangesCount"),
		BetterPartitionAttachAttempts: u("betterPartIdAttachAttemptsCount"),
		ParentChanges:                 u("newParentCount"),
		TrackedTime:                   f.uint64("totalTrackingTime"),
		DisabledTime:                  f.uint64("radioDisabledTime"),
		DetachedTime:                  f.uint64("detachedRoleTime"),
		ChildTime:                     f.uint64("childRoleTime"),
		RouterTime:                    f.uint64("routerRoleTime"),
		LeaderTime:                    f.uint64("leaderRoleTime"),
	}
	return c, f.err
}

// translateChildTable is translateChildTable (translation.ts:185-209):
// the timeout in seconds is 2^(exponent-4), the exponent floored at 4; the
// OTBR leaves out the incoming link quality, 0 as OpenThread's default.
func translateChildTable(list []any) ([]thread.ChildTableEntry, error) {
	out := []thread.ChildTableEntry{}
	for _, raw := range list {
		rec, ok := raw.(map[string]any)
		if !ok {
			return nil, protocolError("childTable: entry is not an object")
		}
		modeRec, ok := rec["mode"].(map[string]any)
		if !ok {
			return nil, protocolError("childTable: entry missing mode")
		}
		f := &fields{rec: rec, where: "childTable"}
		exp := f.uint("timeout", timeoutExpMin+31)
		id := f.uint("childId", math.MaxUint16)
		if f.err != nil {
			return nil, f.err
		}
		mode, err := translateMode(modeRec)
		if err != nil {
			return nil, err
		}
		out = append(out, thread.ChildTableEntry{
			TimeoutExponent: uint8(exp),                                             //nolint:gosec // bounded
			TimeoutSeconds:  uint32(1) << (max(exp, timeoutExpMin) - timeoutExpMin), //nolint:gosec // bounded
			ChildID:         uint16(id),                                             //nolint:gosec // bounded
			Mode:            mode,
		})
	}
	return out, nil
}

// optional runs a field's translation and keeps the field only when it
// succeeds — one node's malformed record must not abort the mesh view
// (optionalField, translation.ts:26-38).
func optional[T any](field string, fn func() (T, error)) *T {
	v, err := fn()
	if err != nil {
		slog.Debug("otbr.diagnostics.field_skipped", slog.String("field", field), slog.Any("err", err))
		return nil
	}
	return &v
}

// Translate types one /diagnostics entry, keys already normalized
// (translateNodeJson, translation.ts:264-377): a field the entry does not
// carry, or carries in an unexpected shape, stays nil. An entry that is
// not an object is [CodeProtocol].
func Translate(entry any) (thread.DiagnosticResponse, error) {
	rec, ok := entry.(map[string]any)
	if !ok {
		return thread.DiagnosticResponse{}, protocolError("translateNodeJson: input is not an object")
	}
	var out thread.DiagnosticResponse
	if s, ok := rec["extAddress"].(string); ok { // :276-279
		if b := optional("extAddress", func() ([]byte, error) { return parseHexBytes(s, "extAddress", 8) }); b != nil {
			out.ExtMACAddress = *b
		}
	}
	if r, ok := parseRloc16(rec["rloc16"]); ok { // :281-282
		out.Rloc16 = &r
	}
	if m, ok := rec["mode"].(map[string]any); ok { // :284-285
		out.Mode = optional("mode", func() (thread.Mode, error) { return translateMode(m) })
	}
	if r, ok := rec["route"].(map[string]any); ok { // :295-296
		out.Route64 = optional("route", func() (thread.Route64, error) { return translateRoute(r) })
	}
	if l, ok := rec["leaderData"].(map[string]any); ok { // :298-300
		out.LeaderData = optional("leaderData", func() (thread.LeaderData, error) { return leaderData(l, "leaderData") })
	}
	if m, ok := rec["macCounters"].(map[string]any); ok { // :327-329
		out.MacCounters = optional("macCounters", func() (thread.MacCounters, error) { return translateMacCounters(m) })
	}
	if m, ok := rec["mleCounters"].(map[string]any); ok { // :331-333
		out.MleCounters = optional("mleCounters", func() (thread.MleCounters, error) { return translateMleCounters(m) })
	}
	if l, ok := rec["childTable"].([]any); ok { // :335-337
		if t := optional("childTable", func() ([]thread.ChildTableEntry, error) { return translateChildTable(l) }); t != nil {
			out.ChildTable = *t
		}
	}
	if v, ok := asUint(rec["version"], math.MaxUint16); ok { // :352-353
		version := uint16(v) //nolint:gosec // G115: bounded by asUint
		out.Version = &version
	}
	return out, nil
}
